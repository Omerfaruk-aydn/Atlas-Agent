package design

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Dir is where a project's design files live, relative to its root.
const Dir = ".atlas/design/files"

// MaxFileBytes bounds one design file; images placed in it count.
const MaxFileBytes = 64 << 20

var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// ErrConflict means the file changed since the revision a write was based on.
var ErrConflict = errors.New("the design file changed since it was read")

// ValidName reports whether s can name a design file: lower case letters,
// digits, dots, dashes and underscores, starting with a letter or digit.
func ValidName(s string) bool {
	return namePattern.MatchString(s) && !strings.Contains(s, "..")
}

// Slug turns a title into a file name.
func Slug(title string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(strings.TrimSpace(title)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case strings.ContainsRune("çğıöşüâîû", r):
			b.WriteString(map[rune]string{'ç': "c", 'ğ': "g", 'ı': "i", 'ö': "o", 'ş': "s", 'ü': "u", 'â': "a", 'î': "i", 'û': "u"}[r])
			dash = false
		default:
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
				dash = true
			}
		}
		if b.Len() >= 64 {
			break
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "untitled"
	}
	return out
}

// Store reads and writes the design files of one project.
type Store struct {
	root string
	mu   sync.Mutex
}

// NewStore returns the store of the project at root.
func NewStore(root string) *Store { return &Store{root: root} }

// Path is a design file's location on disk.
func (s *Store) Path(name string) (string, error) {
	if !ValidName(name) {
		return "", fmt.Errorf("invalid design name %q: use lower case letters, digits, '.', '-' and '_'", name)
	}
	return filepath.Join(s.root, filepath.FromSlash(Dir), name+".json"), nil
}

// RelPath is a design file's path from the project root, with forward slashes.
func RelPath(name string) string { return Dir + "/" + name + ".json" }

// Info describes a design file.
type Info struct {
	Name       string `json:"name"`
	Rev        string `json:"rev"`
	Size       int64  `json:"size"`
	ModifiedAt int64  `json:"modified_at"`
}

// Rev identifies one version of a file: it changes with every write.
func Rev(info fs.FileInfo) string {
	return fmt.Sprintf("%x-%x", info.ModTime().UnixNano(), info.Size())
}

// List returns the project's design files, newest first.
func (s *Store) List() ([]Info, error) {
	dir := filepath.Join(s.root, filepath.FromSlash(Dir))
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []Info{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []Info{}
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok || e.IsDir() || !ValidName(name) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, Info{Name: name, Rev: Rev(info), Size: info.Size(), ModifiedAt: info.ModTime().Unix()})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ModifiedAt != out[j].ModifiedAt {
			return out[i].ModifiedAt > out[j].ModifiedAt
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// Stat returns the current revision of a file.
func (s *Store) Stat(name string) (string, error) {
	path, err := s.Path(name)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	return Rev(info), nil
}

// Load reads a design file and its revision.
func (s *Store) Load(name string) (*Doc, string, error) {
	path, err := s.Path(name)
	if err != nil {
		return nil, "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, "", err
	}
	if info.Size() > MaxFileBytes {
		return nil, "", fmt.Errorf("design file %s is larger than %d MB", name, MaxFileBytes>>20)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	doc, err := Parse(data)
	if err != nil {
		return nil, "", fmt.Errorf("%s: %w", RelPath(name), err)
	}
	return doc, Rev(info), nil
}

// Save writes a document. A non-empty base is the revision the change was
// made on; when the file has moved on since, nothing is written and
// ErrConflict is returned. create refuses to overwrite an existing file.
func (s *Store) Save(name string, doc *Doc, base string, create bool) (string, error) {
	return s.SaveWith(name, doc, base, create, SaveOptions{})
}

// SaveWith is Save with a say in how the previous state is remembered: the
// person's saves keep one now and then, the agent's keep one every time.
func (s *Store) SaveWith(name string, doc *Doc, base string, create bool, opts SaveOptions) (string, error) {
	path, err := s.Path(name)
	if err != nil {
		return "", err
	}
	data, err := Marshal(Sanitize(doc))
	if err != nil {
		return "", err
	}
	if len(data) > MaxFileBytes {
		return "", fmt.Errorf("the design is larger than %d MB", MaxFileBytes>>20)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	info, statErr := os.Stat(path)
	switch {
	case statErr == nil && create:
		return "", fmt.Errorf("a design named %q already exists", name)
	case statErr == nil && base != "" && Rev(info) != base:
		return "", ErrConflict
	case statErr != nil && !errors.Is(statErr, os.ErrNotExist):
		return "", statErr
	}
	if statErr == nil {
		if opts.Agent {
			_ = s.snapshot(name, "agent", opts.Label, nil)
		} else {
			_ = s.snapshot(name, "auto", "", func(newest *Snapshot) bool { return newest == nil || time.Since(newest.Time) > autoEvery })
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+name+".*.tmp")
	if err != nil {
		return "", err
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(tmp.Name())
		return "", errors.Join(werr, cerr)
	}
	if err := replaceFile(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
		return "", err
	}
	// A write in the same clock tick as the last, at the same size, would
	// keep the revision; move the time on so every write is seen.
	if statErr == nil {
		if now, err := os.Stat(path); err == nil && Rev(now) == Rev(info) {
			later := info.ModTime().Add(time.Microsecond)
			_ = os.Chtimes(path, later, later)
		}
	}
	now, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	return Rev(now), nil
}

// replaceFile moves src over dst, retrying briefly: on Windows a reader
// holding dst open makes the rename fail for a moment.
func replaceFile(src, dst string) error {
	var err error
	for range 10 {
		if err = os.Rename(src, dst); err == nil {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return err
}
