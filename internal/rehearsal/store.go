package rehearsal

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

var idPattern = regexp.MustCompile(`^rh-[0-9]{8}-[0-9]{6}-[0-9a-f]{6}$`)

// Store persists rehearsals under <data dir>/rehearsals/<id>/: the report,
// the reviewable patch and the rehearsed file contents.
type Store struct{ dir string }

// NewStore returns a store rooted below the Atlas data directory.
func NewStore(dataDir string) *Store {
	return &Store{dir: filepath.Join(dataDir, "rehearsals")}
}

func (s *Store) path(id string) (string, error) {
	if !idPattern.MatchString(id) {
		return "", fmt.Errorf("invalid rehearsal id %q", id)
	}
	return filepath.Join(s.dir, id), nil
}

func (s *Store) putFile(id, rel string, data []byte) error {
	dir, err := s.path(id)
	if err != nil {
		return err
	}
	if _, err := within(dir, rel); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	name := filepath.Join("files", filepath.FromSlash(rel))
	if err := root.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return err
	}
	return root.WriteFile(name, data, 0o644)
}

func (s *Store) save(r *Report, patch string) error {
	dir, err := s.path(r.ID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if patch != "" {
		if err := os.WriteFile(filepath.Join(dir, "patch.diff"), []byte(patch), 0o644); err != nil {
			return err
		}
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "report.json"), data, 0o644)
}

// Load reads one report.
func (s *Store) Load(id string) (*Report, error) {
	dir, err := s.path(id)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "report.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("rehearsal %s not found", id)
	}
	if err != nil {
		return nil, err
	}
	var r Report
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("rehearsal %s is corrupt: %w", id, err)
	}
	return &r, nil
}

// Patch returns the stored unified diff.
func (s *Store) Patch(id string) (string, error) {
	dir, err := s.path(id)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(dir, "patch.diff"))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	return string(data), err
}

// List returns stored reports, newest first.
func (s *Store) List() ([]*Report, error) {
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []*Report
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if r, err := s.Load(e.Name()); err == nil {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

// Discard drops the rehearsed contents and marks the report discarded.
func (s *Store) Discard(id string) (*Report, error) {
	r, err := s.Load(id)
	if err != nil {
		return nil, err
	}
	if r.Status == "applied" {
		return nil, fmt.Errorf("rehearsal %s was already applied", id)
	}
	dir, _ := s.path(id)
	if err := os.RemoveAll(filepath.Join(dir, "files")); err != nil {
		return nil, err
	}
	r.Status = "discarded"
	return r, s.save(r, "")
}

// ApplyOptions controls Apply.
type ApplyOptions struct {
	// Root is the real project directory to write into.
	Root string
	// Force allows applying a rehearsal whose verdict is failed or
	// breaks_checks.
	Force bool
}

// Apply writes a previewed rehearsal into the real tree. It refuses when
// any touched file changed since the rehearsal, so a stale preview can
// never overwrite newer work, and it restores the tree if a write fails.
func (s *Store) Apply(ctx context.Context, id string, opts ApplyOptions) (*Report, error) {
	r, err := s.Load(id)
	if err != nil {
		return nil, err
	}
	root, err := filepath.Abs(opts.Root)
	if err != nil {
		return nil, err
	}
	switch {
	case r.Status != "previewed":
		return nil, fmt.Errorf("rehearsal %s is %s, not previewed", id, r.Status)
	case !sameDir(r.Root, root):
		return nil, fmt.Errorf("rehearsal %s was made for %s, not %s", id, r.Root, root)
	case r.Verdict == VerdictNoChanges:
		return nil, errors.New("rehearsal produced no changes; nothing to apply")
	case (r.Verdict == VerdictFailed || r.Verdict == VerdictBreakChecks) && !opts.Force:
		return nil, fmt.Errorf("rehearsal verdict is %s; review the report and apply with force only if that is acceptable", r.Verdict)
	}
	dir, _ := s.path(id)
	projectRoot, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer projectRoot.Close()
	storedRoot, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer storedRoot.Close()

	// Verify everything before touching anything.
	type write struct {
		target string
		change FileChange
		data   []byte
	}
	var writes []write
	for _, fc := range r.Files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		_, err := within(root, fc.Path)
		if err != nil {
			return nil, err
		}
		name := filepath.FromSlash(fc.Path)
		current, hashErr := hashRootFile(projectRoot, name)
		switch {
		case fc.OriginalHash == "" && hashErr == nil:
			return nil, fmt.Errorf("stale rehearsal: %s now exists in the project", fc.Path)
		case fc.OriginalHash != "" && hashErr != nil:
			return nil, fmt.Errorf("stale rehearsal: %s is missing or unreadable in the project", fc.Path)
		case fc.OriginalHash != "" && current != fc.OriginalHash:
			return nil, fmt.Errorf("stale rehearsal: %s changed since it was rehearsed", fc.Path)
		}
		w := write{target: name, change: fc}
		if fc.Status != "deleted" {
			w.data, err = storedRoot.ReadFile(filepath.Join("files", name))
			if err != nil {
				return nil, fmt.Errorf("rehearsed contents of %s are gone: %w", fc.Path, err)
			}
			if hashBytes(w.data) != fc.NewHash {
				return nil, fmt.Errorf("rehearsed contents of %s are corrupt", fc.Path)
			}
		}
		writes = append(writes, w)
	}

	backups := map[string][]byte{}
	var done []write
	rollback := func() {
		for i := len(done) - 1; i >= 0; i-- {
			w := done[i]
			if old, ok := backups[w.target]; ok {
				_ = writeAtomicRoot(projectRoot, w.target, old, os.FileMode(w.change.Mode))
			} else {
				_ = projectRoot.Remove(w.target)
			}
		}
	}
	for _, w := range writes {
		if w.change.Status != "added" {
			old, err := projectRoot.ReadFile(w.target)
			if err != nil {
				rollback()
				return nil, err
			}
			backups[w.target] = old
		}
		var err error
		if w.change.Status == "deleted" {
			err = projectRoot.Remove(w.target)
		} else {
			err = writeAtomicRoot(projectRoot, w.target, w.data, os.FileMode(w.change.Mode))
		}
		if err != nil {
			rollback()
			return nil, fmt.Errorf("apply %s: %w (earlier writes were rolled back)", w.change.Path, err)
		}
		done = append(done, w)
	}
	now := time.Now().UTC()
	r.Status, r.AppliedAt = "applied", &now
	patch, _ := s.Patch(id)
	if err := s.save(r, patch); err != nil {
		return r, fmt.Errorf("changes applied but the report could not be updated: %w", err)
	}
	return r, nil
}

// writeAtomicRoot confines both the temporary file and the final rename to
// the selected project, including when a directory is replaced by a link.
func writeAtomicRoot(root *os.Root, name string, data []byte, mode os.FileMode) error {
	if mode == 0 {
		mode = 0o644
	}
	f, tmp, err := createTempRoot(root, name, mode)
	if err != nil {
		return err
	}
	defer root.Remove(tmp)
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return err
	}
	return root.Rename(tmp, name)
}

func createTempRoot(root *os.Root, name string, mode os.FileMode) (*os.File, string, error) {
	if err := root.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return nil, "", err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, "", err
	}
	tmp := filepath.Join(filepath.Dir(name), ".atlas-rehearsal-"+hex.EncodeToString(nonce[:])+".tmp")
	f, err := root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return nil, "", err
	}
	return f, tmp, nil
}

func hashRootFile(root *os.Root, name string) (string, error) {
	f, err := root.Open(name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%q is not a regular file", name)
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// within resolves a slash-relative path under root and rejects escapes.
func within(root, rel string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(rel))
	if rel == "" || !filepath.IsLocal(clean) {
		return "", fmt.Errorf("rehearsal path %q escapes the project", rel)
	}
	return filepath.Join(root, clean), nil
}

func sameDir(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b)) && (filepath.Clean(a) == filepath.Clean(b) || caseInsensitiveFS())
}

func caseInsensitiveFS() bool { return runtime.GOOS == "windows" || runtime.GOOS == "darwin" }

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
