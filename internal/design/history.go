package design

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// HistoryDir is where a project's design history lives, relative to its root.
const HistoryDir = ".atlas/design/history"

const (
	// autoEvery is how often a save of the person's makes a snapshot.
	autoEvery = 5 * time.Minute
	// keepAuto bounds the unnamed snapshots kept per file.
	keepAuto = 60
)

// Snapshot is one saved state of a design file.
type Snapshot struct {
	// ID names the version; pass it to Restore.
	ID string `json:"id"`
	// Time is when it was saved.
	Time time.Time `json:"time"`
	// Kind is "named" (kept until deleted), "agent" (before the agent edited)
	// or "auto" (kept for a while).
	Kind string `json:"kind"`
	// Label is the name of a named version, or what the agent was doing.
	Label string `json:"label,omitempty"`
	Size  int64  `json:"size"`
}

func (s *Store) historyDir(name string) (string, error) {
	if !ValidName(name) {
		return "", fmt.Errorf("invalid design name %q", name)
	}
	return filepath.Join(s.root, filepath.FromSlash(HistoryDir), name), nil
}

// versionFile is "<yyyymmddThhmmss.mmm>-<kind>[-<label slug>].json".
func versionFile(t time.Time, kind, label string) string {
	slug := Slug(label)
	if label == "" || slug == "" {
		return fmt.Sprintf("%s-%s.json", t.UTC().Format("20060102T150405.000"), kind)
	}
	return fmt.Sprintf("%s-%s-%s.json", t.UTC().Format("20060102T150405.000"), kind, slug)
}

func parseVersion(file string, size int64) (Snapshot, bool) {
	base := strings.TrimSuffix(file, ".json")
	if base == file || len(base) < 24 {
		return Snapshot{}, false
	}
	t, err := time.Parse("20060102T150405.000", base[:19])
	if err != nil {
		return Snapshot{}, false
	}
	rest := strings.TrimPrefix(base[19:], "-")
	kind, label, _ := strings.Cut(rest, "-")
	if kind != "named" && kind != "agent" && kind != "auto" {
		return Snapshot{}, false
	}
	return Snapshot{ID: strings.TrimSuffix(file, ".json"), Time: t, Kind: kind, Label: label, Size: size}, true
}

// History lists a file's versions, newest first.
func (s *Store) History(name string) ([]Snapshot, error) {
	dir, err := s.historyDir(name)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return []Snapshot{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []Snapshot{}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || e.IsDir() {
			continue
		}
		if v, ok := parseVersion(e.Name(), info.Size()); ok {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}

// Version reads one version of a file.
func (s *Store) Version(name, id string) (*Doc, error) {
	dir, err := s.historyDir(name)
	if err != nil {
		return nil, err
	}
	if strings.ContainsAny(id, `/\`) || strings.Contains(id, "..") {
		return nil, fmt.Errorf("invalid version %q", id)
	}
	data, err := os.ReadFile(filepath.Join(dir, id+".json"))
	if err != nil {
		return nil, fmt.Errorf("no version %q of %q", id, name)
	}
	return Parse(data)
}

// snapshot copies the file's current content into its history when it
// differs from the newest version. Callers hold s.mu.
func (s *Store) snapshot(name, kind, label string, only func(newest *Snapshot) bool) error {
	path, err := s.Path(name)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	dir, err := s.historyDir(name)
	if err != nil {
		return err
	}
	versions, err := s.History(name)
	if err != nil {
		return err
	}
	var newest *Snapshot
	if len(versions) > 0 {
		newest = &versions[0]
	}
	if only != nil && !only(newest) {
		return nil
	}
	if newest != nil {
		if prev, err := os.ReadFile(filepath.Join(dir, newest.ID+".json")); err == nil && bytes.Equal(prev, data) {
			// Same content: a name is worth keeping, a duplicate is not.
			if kind == "named" && newest.Kind != "named" {
				return os.Rename(filepath.Join(dir, newest.ID+".json"), filepath.Join(dir, versionFile(newest.Time, kind, label)))
			}
			return nil
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, versionFile(time.Now(), kind, label)), data, 0o644); err != nil {
		return err
	}
	return s.prune(name)
}

// prune drops the oldest unnamed versions beyond keepAuto.
func (s *Store) prune(name string) error {
	versions, err := s.History(name)
	if err != nil {
		return err
	}
	dir, _ := s.historyDir(name)
	kept := 0
	for _, v := range versions {
		if v.Kind == "named" {
			continue
		}
		if kept++; kept > keepAuto {
			_ = os.Remove(filepath.Join(dir, v.ID+".json"))
		}
	}
	return nil
}

// SaveVersion names the file's current state, keeping it until deleted.
func (s *Store) SaveVersion(name, label string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(label) == "" {
		return errors.New("a version needs a name")
	}
	return s.snapshot(name, "named", label, nil)
}

// DeleteVersion removes one version.
func (s *Store) DeleteVersion(name, id string) error {
	dir, err := s.historyDir(name)
	if err != nil {
		return err
	}
	if strings.ContainsAny(id, `/\`) || strings.Contains(id, "..") {
		return fmt.Errorf("invalid version %q", id)
	}
	return os.Remove(filepath.Join(dir, id+".json"))
}

// SaveOptions says how a write is remembered.
type SaveOptions struct {
	// Agent marks an edit by the agent: the state before it is kept, so the
	// person can go back, whatever the clock says.
	Agent bool
	// Label says what the agent is doing.
	Label string
}
