package util

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"unicode/utf8"

	powernap "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-powernap/pkg/lsp"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-powernap/pkg/lsp/protocol"
	"github.com/google/uuid"
)

const (
	missingFileHash = "missing"
	maxEditFile     = 1024 * 1024
	maxEditBytes    = 8 * 1024 * 1024
	maxEditTargets  = 128
)

type PlannedChange struct {
	Path        string `json:"path"`
	Destination string `json:"destination,omitempty"`
	Operation   string `json:"operation"`
	BeforeHash  string `json:"before_hash"`
	AfterHash   string `json:"after_hash"`
	Content     []byte `json:"content,omitempty"`
	Mode        uint32 `json:"mode"`
}

type EditPlan struct {
	ID       string                  `json:"id"`
	Root     string                  `json:"root"`
	Encoding powernap.OffsetEncoding `json:"encoding"`
	Changes  []PlannedChange         `json:"changes"`
}

type EditResult struct {
	PlanID     string   `json:"plan_id"`
	Status     string   `json:"status"`
	Applied    []string `json:"applied"`
	Restored   []string `json:"restored"`
	Conflicted []string `json:"conflicted"`
}

type fileSnapshot struct {
	content []byte
	hash    string
	mode    os.FileMode
}

func contentHash(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func editRoot(root string) (string, *os.Root, error) {
	if !filepath.IsAbs(root) {
		return "", nil, fmt.Errorf("edit root must be absolute")
	}
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", nil, err
	}
	opened, err := os.OpenRoot(canonical)
	return filepath.Clean(canonical), opened, err
}

func pathKey(path string) string {
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return path
}

func editRelative(root, target string) (string, error) {
	if !filepath.IsAbs(target) {
		target = filepath.Join(root, target)
	}
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == "." || !filepath.IsLocal(rel) || len(rel) > 4096 || strings.ContainsAny(rel, ":\x00") {
		return "", fmt.Errorf("edit target %q escapes root %q or is not a file (relative %q): %v", target, root, rel, err)
	}
	for _, component := range strings.Split(filepath.ToSlash(rel), "/") {
		if strings.EqualFold(component, ".git") {
			return "", fmt.Errorf("edit targets cannot modify Git metadata")
		}
	}
	return rel, nil
}

// readEditFile rejects symbolic links and special files through a rooted handle.
// Absent parent directories are allowed for newly created files.
func readEditFile(ctx context.Context, root *os.Root, relative string) (fileSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return fileSnapshot{}, err
	}
	parts := strings.Split(filepath.ToSlash(relative), "/")
	for i := range parts {
		info, err := root.Lstat(filepath.FromSlash(strings.Join(parts[:i+1], "/")))
		if os.IsNotExist(err) {
			return fileSnapshot{hash: missingFileHash, mode: 0o644}, nil
		}
		if err != nil {
			return fileSnapshot{}, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fileSnapshot{}, fmt.Errorf("symbolic edit target is unsupported")
		}
		if i < len(parts)-1 && !info.IsDir() {
			return fileSnapshot{}, fmt.Errorf("edit parent is not a directory")
		}
		if i == len(parts)-1 {
			if !info.Mode().IsRegular() || info.Size() > maxEditFile {
				return fileSnapshot{}, fmt.Errorf("edit target must be a regular file of at most 1 MiB")
			}
			file, err := root.Open(relative)
			if err != nil {
				return fileSnapshot{}, err
			}
			defer file.Close()
			data, err := io.ReadAll(io.LimitReader(file, maxEditFile+1))
			if err != nil {
				return fileSnapshot{}, fmt.Errorf("cannot read bounded edit target: %w", err)
			}
			if len(data) > maxEditFile {
				return fileSnapshot{}, fmt.Errorf("edit target grew beyond 1 MiB")
			}
			return fileSnapshot{content: data, hash: contentHash(data), mode: info.Mode().Perm()}, nil
		}
	}
	return fileSnapshot{}, fmt.Errorf("invalid edit target")
}

func positionOffset(content []byte, position protocol.Position, encoding powernap.OffsetEncoding) (int, error) {
	start := 0
	for range position.Line {
		index := bytes.IndexByte(content[start:], '\n')
		if index < 0 {
			return 0, fmt.Errorf("edit line exceeds source")
		}
		start += index + 1
	}
	end := bytes.IndexByte(content[start:], '\n')
	if end < 0 {
		end = len(content) - start
	}
	line := content[start : start+end]
	line = bytes.TrimSuffix(line, []byte{'\r'})
	if encoding == powernap.UTF8 {
		offset := int(position.Character)
		if offset > len(line) || offset < len(line) && !utf8.RuneStart(line[offset]) {
			return 0, fmt.Errorf("invalid UTF-8 edit boundary")
		}
		return start + offset, nil
	}
	var units uint32
	for offset, r := range string(line) {
		if units == position.Character {
			return start + offset, nil
		}
		width := uint32(1)
		if encoding == powernap.UTF16 && r > 0xffff {
			width = 2
		}
		if units+width > position.Character {
			return 0, fmt.Errorf("edit bisects a UTF-16 surrogate pair")
		}
		units += width
	}
	if units == position.Character {
		return start + len(line), nil
	}
	return 0, fmt.Errorf("edit character exceeds source")
}

func prepareText(content []byte, edits []protocol.TextEdit, encoding powernap.OffsetEncoding) ([]byte, error) {
	if !utf8.Valid(content) || len(edits) > 4096 {
		return nil, fmt.Errorf("text edits require bounded UTF-8 source")
	}
	for i, value := range content {
		if value == '\r' && (i+1 == len(content) || content[i+1] != '\n') {
			return nil, fmt.Errorf("bare CR line endings are unsupported")
		}
	}
	type replacement struct {
		start, end int
		text       []byte
	}
	replacements := make([]replacement, len(edits))
	for i, edit := range edits {
		start, err := positionOffset(content, edit.Range.Start, encoding)
		if err != nil {
			return nil, err
		}
		end, err := positionOffset(content, edit.Range.End, encoding)
		if err != nil || end < start || !utf8.ValidString(edit.NewText) || len(edit.NewText) > maxEditFile {
			return nil, fmt.Errorf("invalid edit range or replacement")
		}
		text := edit.NewText
		if bytes.Contains(content, []byte("\r\n")) {
			text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\n", "\r\n")
		}
		replacements[i] = replacement{start, end, []byte(text)}
	}
	slices.SortFunc(replacements, func(a, b replacement) int {
		if a.start != b.start {
			return a.start - b.start
		}
		return a.end - b.end
	})
	for i := 1; i < len(replacements); i++ {
		previous, current := replacements[i-1], replacements[i]
		if current.start < previous.end || current.start == previous.start {
			return nil, fmt.Errorf("overlapping or ambiguous text edits")
		}
	}
	result := bytes.Clone(content)
	for _, replacement := range slices.Backward(replacements) {
		if len(result)-replacement.end+replacement.start+len(replacement.text) > maxEditFile {
			return nil, fmt.Errorf("edited file exceeds 1 MiB")
		}
		result = append(append(append([]byte{}, result[:replacement.start]...), replacement.text...), result[replacement.end:]...)
	}
	return result, nil
}

// PrepareWorkspaceEdit computes every final file state in memory. Rename
// operations become a checked source deletion and destination creation/write,
// both included in permissions and recovery. No filesystem mutation occurs.
func PrepareWorkspaceEdit(ctx context.Context, root string, edit protocol.WorkspaceEdit, encoding powernap.OffsetEncoding) (EditPlan, error) {
	canonical, opened, err := editRoot(root)
	if err != nil {
		return EditPlan{}, err
	}
	defer opened.Close()
	plan := EditPlan{ID: uuid.NewString(), Root: canonical, Encoding: encoding}
	if encoding < powernap.UTF8 || encoding > powernap.UTF32 || len(edit.Changes)+len(edit.DocumentChanges) > 4096 || len(edit.Changes) > 0 && len(edit.DocumentChanges) > 0 {
		return plan, fmt.Errorf("invalid encoding or ambiguous workspace edit representation")
	}
	type virtualFile struct {
		path            string
		before, current fileSnapshot
		destination     string
		forceTarget     bool
	}
	files := map[string]*virtualFile{}
	total := 0
	load := func(uri protocol.DocumentURI) (*virtualFile, error) {
		file, err := uri.Path()
		if err != nil {
			return nil, err
		}
		rel, err := editRelative(canonical, file)
		if err != nil {
			// Windows may return an 8.3 alias from the original workspace URI.
			// Translate its contained relative path into the opened root, then
			// apply the same rooted symlink/special-file checks below.
			rel, err = editRelative(filepath.Clean(root), file)
		}
		if err != nil {
			return nil, err
		}
		file = filepath.Join(canonical, rel)
		key := pathKey(file)
		if existing := files[key]; existing != nil {
			return existing, nil
		}
		if len(files) >= maxEditTargets {
			return nil, fmt.Errorf("edit exceeds 128 file targets")
		}
		snapshot, err := readEditFile(ctx, opened, rel)
		if err != nil {
			return nil, err
		}
		total += len(snapshot.content)
		if total > maxEditBytes {
			return nil, fmt.Errorf("edit source exceeds 8 MiB")
		}
		entry := &virtualFile{path: file, before: snapshot, current: snapshot}
		files[key] = entry
		return entry, nil
	}
	apply := func(uri protocol.DocumentURI, edits []protocol.TextEdit) error {
		entry, err := load(uri)
		if err != nil {
			return err
		}
		if entry.current.hash == missingFileHash {
			return fmt.Errorf("text edit target does not exist")
		}
		content, err := prepareText(entry.current.content, edits, encoding)
		if err != nil {
			return err
		}
		entry.current.content, entry.current.hash = content, contentHash(content)
		return nil
	}
	for uri, edits := range edit.Changes {
		if err := apply(uri, edits); err != nil {
			return plan, err
		}
	}
	for _, change := range edit.DocumentChanges {
		if err := ctx.Err(); err != nil {
			return plan, err
		}
		if !change.Valid() {
			return plan, fmt.Errorf("invalid document change union")
		}
		switch {
		case change.TextDocumentEdit != nil:
			edits := make([]protocol.TextEdit, len(change.TextDocumentEdit.Edits))
			for i, edit := range change.TextDocumentEdit.Edits {
				text, err := edit.AsTextEdit()
				if err != nil {
					return plan, err
				}
				edits[i] = text
			}
			if err := apply(change.TextDocumentEdit.TextDocument.URI, edits); err != nil {
				return plan, err
			}
		case change.CreateFile != nil:
			entry, err := load(change.CreateFile.URI)
			if err != nil {
				return plan, err
			}
			if entry.current.hash != missingFileHash {
				options := change.CreateFile.Options
				if options == nil || !options.Overwrite {
					if options != nil && options.IgnoreIfExists {
						continue
					}
					return plan, fmt.Errorf("create target already exists")
				}
			}
			entry.current.content, entry.current.hash = []byte{}, contentHash(nil)
		case change.DeleteFile != nil:
			entry, err := load(change.DeleteFile.URI)
			if err != nil {
				return plan, err
			}
			if entry.current.hash == missingFileHash {
				if change.DeleteFile.Options != nil && change.DeleteFile.Options.IgnoreIfNotExists {
					continue
				}
				return plan, fmt.Errorf("delete target does not exist")
			}
			entry.current.content, entry.current.hash = nil, missingFileHash
		case change.RenameFile != nil:
			source, err := load(change.RenameFile.OldURI)
			if err != nil {
				return plan, err
			}
			target, err := load(change.RenameFile.NewURI)
			if err != nil {
				return plan, err
			}
			if source == target || source.current.hash == missingFileHash {
				return plan, fmt.Errorf("rename requires distinct existing source and destination")
			}
			if target.current.hash != missingFileHash {
				options := change.RenameFile.Options
				if options == nil || !options.Overwrite {
					if options != nil && options.IgnoreIfExists {
						continue
					}
					return plan, fmt.Errorf("rename destination exists")
				}
			}
			target.current = source.current
			target.forceTarget = true
			source.current.content, source.current.hash = nil, missingFileHash
			source.destination = target.path
		}
	}
	for _, file := range files {
		if file.before.hash == file.current.hash && !file.forceTarget && (file.current.hash == missingFileHash || file.before.mode == file.current.mode) {
			continue
		}
		operation := "write"
		if file.before.hash == missingFileHash {
			operation = "create"
		}
		if file.current.hash == missingFileHash {
			operation = "delete"
		}
		plan.Changes = append(plan.Changes, PlannedChange{Path: file.path, Destination: file.destination, Operation: operation, BeforeHash: file.before.hash, AfterHash: file.current.hash, Content: file.current.content, Mode: uint32(file.current.mode.Perm())})
	}
	slices.SortFunc(plan.Changes, func(a, b PlannedChange) int { return strings.Compare(pathKey(a.Path), pathKey(b.Path)) })
	if len(plan.Changes) == 0 {
		return plan, nil
	}
	return plan, ValidateEditPlan(ctx, root, plan)
}

func validEditHash(hash string) bool {
	decoded, err := hex.DecodeString(hash)
	return err == nil && len(decoded) == sha256.Size && hash == strings.ToLower(hash)
}

func validatePlanShape(root string, plan EditPlan) error {
	if _, err := uuid.Parse(plan.ID); err != nil || pathKey(root) != pathKey(plan.Root) || plan.Encoding < powernap.UTF8 || plan.Encoding > powernap.UTF32 || len(plan.Changes) == 0 || len(plan.Changes) > maxEditTargets {
		return fmt.Errorf("invalid edit plan identity or bounds")
	}
	seen := map[string]bool{}
	total := 0
	for _, change := range plan.Changes {
		rel, err := editRelative(root, change.Path)
		if err != nil || !filepath.IsAbs(change.Path) || pathKey(filepath.Join(root, rel)) != pathKey(change.Path) || seen[pathKey(change.Path)] || change.Mode & ^uint32(0o777) != 0 {
			return fmt.Errorf("invalid or duplicate edit target")
		}
		seen[pathKey(change.Path)] = true
		total += len(change.Content)
		if len(change.Content) > maxEditFile || total > maxEditBytes {
			return fmt.Errorf("edit contents exceed bounds")
		}
		switch change.Operation {
		case "create":
			if change.BeforeHash != missingFileHash || change.AfterHash != contentHash(change.Content) {
				return fmt.Errorf("invalid create hashes")
			}
		case "write":
			if !validEditHash(change.BeforeHash) || change.AfterHash != contentHash(change.Content) {
				return fmt.Errorf("invalid write hashes")
			}
		case "delete":
			if !validEditHash(change.BeforeHash) || change.AfterHash != missingFileHash || len(change.Content) != 0 {
				return fmt.Errorf("invalid delete hashes")
			}
		default:
			return fmt.Errorf("unsupported edit operation")
		}
	}
	for _, change := range plan.Changes {
		if change.Destination != "" && !seen[pathKey(change.Destination)] {
			return fmt.Errorf("rename destination is missing from the complete target set")
		}
	}
	return nil
}

func ValidateEditPlan(ctx context.Context, root string, plan EditPlan) error {
	canonical, opened, err := editRoot(root)
	if err != nil {
		return err
	}
	defer opened.Close()
	if err := validatePlanShape(canonical, plan); err != nil {
		return err
	}
	total := 0
	for _, change := range plan.Changes {
		rel, _ := editRelative(canonical, change.Path)
		snapshot, err := readEditFile(ctx, opened, rel)
		if err != nil {
			return err
		}
		if snapshot.hash != change.BeforeHash {
			return fmt.Errorf("edit source changed: %s", change.Path)
		}
		total += len(snapshot.content)
		if total > maxEditBytes {
			return fmt.Errorf("edit source exceeds bounds")
		}
	}
	return nil
}

// ReadEditSource uses the same bounded rooted read as plan validation.
func ReadEditSource(ctx context.Context, root, target string) ([]byte, string, error) {
	canonical, opened, err := editRoot(root)
	if err != nil {
		return nil, "", err
	}
	defer opened.Close()
	rel, err := editRelative(canonical, target)
	if err != nil {
		rel, err = editRelative(filepath.Clean(root), target)
	}
	if err != nil {
		return nil, "", err
	}
	snapshot, err := readEditFile(ctx, opened, rel)
	return snapshot.content, snapshot.hash, err
}
