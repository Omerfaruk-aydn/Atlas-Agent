package engineering

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type ProjectBrief struct {
	BoundaryHints  map[string][]string `json:"boundary_candidates,omitempty"`
	Root           string              `json:"root"`
	MapFingerprint string              `json:"map_fingerprint"`
	Stacks         []string            `json:"stacks"`
	EntryPoints    []string            `json:"entry_points"`
	Instructions   []string            `json:"instructions"`
	Commands       []string            `json:"discovered_commands"`
	Truncated      bool                `json:"truncated"`
	PreparedAt     int64               `json:"prepared_at"`
}

type SourceReference struct {
	Path        string `json:"path"`
	Fingerprint string `json:"fingerprint"`
}

type KnowledgeRecord struct {
	Revision           int               `json:"revision"`
	ID                 string            `json:"id"`
	Kind               string            `json:"kind"`
	Context            string            `json:"context"`
	Decision           string            `json:"decision"`
	Alternatives       []string          `json:"alternatives,omitempty"`
	Consequences       []string          `json:"consequences"`
	Sources            []SourceReference `json:"sources"`
	FailureOperationID string            `json:"failure_operation_id,omitempty"`
	VerificationRunID  string            `json:"verification_run_id,omitempty"`
	Evidence           []Check           `json:"evidence,omitempty"`
	RecordedAt         int64             `json:"recorded_at"`
	Superseded         bool              `json:"superseded"`
}

type KnowledgeView struct {
	Record KnowledgeRecord `json:"record"`
	Status string          `json:"status"`
}

func projectKey(root string) (string, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	return "project-knowledge-" + Hash(filepath.Clean(absolute)), nil
}

// readProjectFile rejects escaped paths and symlink traversal before bounded reads.
func readProjectFile(ctx context.Context, root, relative string, limit int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	relative = filepath.FromSlash(strings.ReplaceAll(relative, `\`, "/"))
	clean := filepath.Clean(relative)
	if clean == "." || clean == ".." || filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || strings.ContainsAny(clean, "*?:") {
		return nil, fmt.Errorf("source must be a literal project-relative file")
	}
	current := root
	for _, segment := range strings.Split(clean, string(filepath.Separator)) {
		current = filepath.Join(current, segment)
		info, err := os.Lstat(current)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("source symlinks are not supported")
		}
	}
	f, err := os.Open(current)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, fmt.Errorf("source is not a bounded regular file")
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("source exceeds size limit")
	}
	return data, ctx.Err()
}

// ReadProjectEvidence reads a bounded literal artifact without escaping its root.
func ReadProjectEvidence(ctx context.Context, root, path string) ([]byte, error) {
	return readProjectFile(ctx, root, path, 512*1024)
}

func CaptureSources(ctx context.Context, root string, paths []string) ([]SourceReference, error) {
	if len(paths) == 0 || len(paths) > 16 {
		return nil, fmt.Errorf("1-16 source references are required")
	}
	seen := map[string]bool{}
	var references []SourceReference
	for _, path := range paths {
		if len(path) > 512 {
			return nil, fmt.Errorf("source path exceeds limit")
		}
		path = filepath.ToSlash(filepath.Clean(filepath.FromSlash(strings.ReplaceAll(path, `\`, "/"))))
		if seen[path] {
			return nil, fmt.Errorf("duplicate source reference")
		}
		seen[path] = true
		data, err := readProjectFile(ctx, root, path, 512*1024)
		if err != nil {
			return nil, err
		}
		references = append(references, SourceReference{Path: path, Fingerprint: Hash(string(data))})
	}
	return references, nil
}

func (s *Store) PrepareProject(ctx context.Context, root string) (ProjectBrief, error) {
	m, err := s.RefreshProject(ctx, root)
	if err != nil {
		return ProjectBrief{}, err
	}
	data, _ := json.Marshal(m.Files)
	b := ProjectBrief{Root: m.Root, MapFingerprint: Hash(string(data)), Truncated: m.Truncated, PreparedAt: time.Now().UnixMilli(), BoundaryHints: map[string][]string{}}
	stacks := map[string]bool{}
	paths := map[string]bool{}
	for _, f := range m.Files {
		paths[f.Path] = true
	}
	for _, name := range []string{"pnpm-lock.yaml", "yarn.lock", "bun.lock", "bun.lockb"} {
		if info, err := os.Lstat(filepath.Join(root, name)); err == nil && info.Mode().IsRegular() {
			paths[name] = true
		}
	}
	for _, f := range m.Files {
		base := filepath.Base(f.Path)
		lower := strings.ToLower("/" + f.Path)
		for kind, needles := range map[string][]string{
			"configuration": {"/config/", "/shellconfig/", "/go.mod", "/package.json", "/atlasrc"},
			"persistence":   {"/db/", "/storage/", "/migrations/", "/repository/"},
			"authorization": {"/auth/", "/permission/", "/security/"},
			"presentation":  {"/ui/", "/frontend/", "/components/", "/pages/"},
			"transport":     {"/server/", "/api/", "/proto/", "/handlers/"},
		} {
			for _, needle := range needles {
				if strings.Contains(lower, needle) {
					if len(b.BoundaryHints[kind]) < 12 {
						b.BoundaryHints[kind] = append(b.BoundaryHints[kind], f.Path)
					}
					break
				}
			}
		}
		if f.Entry && len(b.EntryPoints) < 32 {
			b.EntryPoints = append(b.EntryPoints, f.Path)
		}
		switch strings.ToUpper(base) {
		case "AGENTS.MD", "ATLAS-AGENT.MD", "CLAUDE.MD", "GEMINI.MD":
			if len(b.Instructions) < 32 {
				b.Instructions = append(b.Instructions, f.Path)
			}
		}
		switch base {
		case "go.mod":
			stacks["go"] = true
			if f.Path == "go.mod" {
				b.Commands = append(b.Commands, "go build ./...", "go test -count=1 ./...", "go vet ./...")
			}
		case "Cargo.toml":
			stacks["rust"] = true
		case "pyproject.toml":
			stacks["python"] = true
		case "package.json":
			stacks["node"] = true
			if f.Path != "package.json" {
				continue
			}
			data, err := readProjectFile(ctx, root, f.Path, 64*1024)
			if err != nil {
				return b, err
			}
			var pkg struct {
				Scripts map[string]json.RawMessage `json:"scripts"`
			}
			if json.Unmarshal(data, &pkg) != nil {
				continue
			}
			manager := "npm"
			if paths["pnpm-lock.yaml"] {
				manager = "pnpm"
			} else if paths["yarn.lock"] {
				manager = "yarn"
			} else if paths["bun.lock"] || paths["bun.lockb"] {
				manager = "bun"
			}
			for _, name := range []string{"build", "test", "lint", "typecheck", "dev"} {
				if _, exists := pkg.Scripts[name]; exists {
					b.Commands = append(b.Commands, manager+" run "+name)
				}
			}
		}
	}
	for stack := range stacks {
		b.Stacks = append(b.Stacks, stack)
	}
	sort.Strings(b.Stacks)
	key, err := projectKey(root)
	if err != nil {
		return b, err
	}
	err = s.Update(ctx, key, func(st *State) error { st.Brief = &b; return nil })
	return b, err
}

func (s *Store) ProjectKnowledge(ctx context.Context, root string, windows ...int) (*ProjectBrief, []KnowledgeView, error) {
	key, err := projectKey(root)
	if err != nil {
		return nil, nil, err
	}
	st, err := s.Read(ctx, key)
	if err != nil {
		return nil, nil, err
	}
	var views []KnowledgeView
	if len(windows) > 0 && windows[0] > 0 && len(st.Knowledge) > windows[0] {
		st.Knowledge = st.Knowledge[len(st.Knowledge)-windows[0]:]
	}
	for _, record := range st.Knowledge {
		status := "current"
		if record.Superseded {
			status = "superseded"
		} else {
			paths := []string{}
			for _, source := range record.Sources {
				paths = append(paths, source.Path)
			}
			current, err := CaptureSources(ctx, root, paths)
			if err != nil {
				if ctx.Err() != nil {
					return nil, nil, ctx.Err()
				}
				status = "unavailable"
			} else {
				for i := range current {
					if current[i].Fingerprint != record.Sources[i].Fingerprint {
						status = "stale"
					}
				}
			}
		}
		if status == "current" && time.Now().UnixMilli()-record.RecordedAt > 90*24*60*60*1000 {
			status = "aged"
		}
		views = append(views, KnowledgeView{Record: record, Status: status})
	}
	return st.Brief, views, nil
}

func (s *Store) SaveKnowledge(ctx context.Context, root string, record KnowledgeRecord) error {
	if !validID(record.ID) || record.Kind != "decision" && record.Kind != "lesson" || !boundedText(record.Context, 2048) || !boundedText(record.Decision, 4096) || len(record.Consequences) == 0 || len(record.Consequences) > 16 || len(record.Alternatives) > 16 {
		return fmt.Errorf("invalid knowledge record")
	}
	for _, values := range [][]string{record.Consequences, record.Alternatives} {
		for _, value := range values {
			if !boundedText(value, 1024) {
				return fmt.Errorf("invalid knowledge detail")
			}
		}
	}
	if len(record.Sources) == 0 || len(record.Sources) > 16 {
		return fmt.Errorf("source evidence is required")
	}
	paths := []string{}
	for _, reference := range record.Sources {
		paths = append(paths, reference.Path)
	}
	current, err := CaptureSources(ctx, root, paths)
	if err != nil {
		return err
	}
	for i := range current {
		if current[i] != record.Sources[i] {
			return fmt.Errorf("knowledge sources changed before save")
		}
	}
	if record.Kind == "lesson" && (record.FailureOperationID == "" || record.VerificationRunID == "" || len(record.Evidence) == 0) {
		return fmt.Errorf("lessons require linked failed operation and actual verification")
	}
	record.RecordedAt = time.Now().UnixMilli()
	key, err := projectKey(root)
	if err != nil {
		return err
	}
	return s.Update(ctx, key, func(st *State) error {
		latestRevision := 0
		for i := range st.Knowledge {
			if st.Knowledge[i].ID == record.ID {
				latestRevision = max(latestRevision, st.Knowledge[i].Revision)
				st.Knowledge[i].Superseded = true
			}
		}
		record.Revision = latestRevision + 1
		st.Knowledge = append(st.Knowledge, record)
		return nil
	})
}
