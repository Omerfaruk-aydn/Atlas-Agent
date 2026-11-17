// Package knowledge keeps distilled, reviewable notes about a project:
// how the build works, why a design was chosen, where a tricky part lives.
// The agent can only propose an item. Nothing reaches the standing context
// until the user approves it, and every item records the files it was
// derived from, so it is marked stale when they change.
package knowledge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Status of an item.
type Status string

const (
	Proposed Status = "proposed"
	Approved Status = "approved"
)

// Source is a file an item was derived from and its fingerprint at the time.
type Source struct {
	Path string `json:"path"`
	// Hash is the file's content hash when the item was written, or empty
	// if the file did not exist.
	Hash string `json:"hash"`
}

// Item is one note.
type Item struct {
	ID      string    `json:"id"`
	Title   string    `json:"title"`
	Summary string    `json:"summary"`
	Body    string    `json:"body"`
	Sources []Source  `json:"sources,omitempty"`
	Status  Status    `json:"status"`
	Created time.Time `json:"created"`
}

const (
	maxItems   = 100
	maxBody    = 16 * 1024
	maxSummary = 240
)

var idRe = regexp.MustCompile(`[^a-z0-9]+`)

// Store keeps items as JSON files under one directory.
type Store struct {
	dir  string
	root string
}

// New returns a store in dir for a project rooted at root. The directory is
// created on first write.
func New(dir, root string) *Store { return &Store{dir: dir, root: root} }

func fingerprint(root, rel string) string {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:8])
}

// Propose records a new item as proposed. sources are project-relative
// paths; their current fingerprints are stored.
func (s *Store) Propose(title, summary, body string, sources []string) (Item, error) {
	title, summary, body = strings.TrimSpace(title), strings.TrimSpace(summary), strings.TrimSpace(body)
	switch {
	case title == "" || summary == "" || body == "":
		return Item{}, errors.New("an item needs a title, a one-line summary and a body")
	case len(summary) > maxSummary:
		return Item{}, fmt.Errorf("the summary must be at most %d characters", maxSummary)
	case len(body) > maxBody:
		return Item{}, fmt.Errorf("the body must be at most %d bytes", maxBody)
	}
	existing, err := s.List()
	if err != nil {
		return Item{}, err
	}
	if len(existing) >= maxItems {
		return Item{}, fmt.Errorf("at most %d items", maxItems)
	}
	id := strings.Trim(idRe.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if len(id) > 48 {
		id = strings.Trim(id[:48], "-")
	}
	if id == "" {
		id = "item"
	}
	for _, e := range existing {
		if e.ID == id {
			return Item{}, fmt.Errorf("an item %q already exists; remove it first or pick another title", id)
		}
	}
	item := Item{ID: id, Title: title, Summary: summary, Body: body, Status: Proposed, Created: time.Now().UTC().Truncate(time.Second)}
	for _, p := range sources {
		p = filepath.ToSlash(strings.TrimSpace(p))
		if p == "" || strings.HasPrefix(p, "../") || filepath.IsAbs(p) {
			return Item{}, fmt.Errorf("source %q must be a path inside the project", p)
		}
		item.Sources = append(item.Sources, Source{Path: p, Hash: fingerprint(s.root, p)})
	}
	return item, s.write(item)
}

func (s *Store) write(item Item) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(item, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.dir, item.ID+".json"), data, 0o644)
}

// List returns every item, sorted by ID.
func (s *Store) List() ([]Item, error) {
	entries, err := os.ReadDir(s.dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Item
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}
		var item Item
		if json.Unmarshal(data, &item) == nil && item.ID != "" {
			out = append(out, item)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Get returns one item by ID.
func (s *Store) Get(id string) (Item, error) {
	items, err := s.List()
	if err != nil {
		return Item{}, err
	}
	for _, it := range items {
		if it.ID == id {
			return it, nil
		}
	}
	return Item{}, fmt.Errorf("no knowledge item %q", id)
}

// Approve promotes a proposed item. Only the user calls this.
func (s *Store) Approve(id string) error {
	it, err := s.Get(id)
	if err != nil {
		return err
	}
	it.Status = Approved
	return s.write(it)
}

// Remove deletes an item, proposed or approved.
func (s *Store) Remove(id string) error {
	if _, err := s.Get(id); err != nil {
		return err
	}
	return os.Remove(filepath.Join(s.dir, id+".json"))
}

// Stale lists the sources whose content changed or vanished since the item
// was written. A source that never existed is not reported.
func (s *Store) Stale(it Item) []string {
	var out []string
	for _, src := range it.Sources {
		if src.Hash == "" {
			continue
		}
		if fingerprint(s.root, src.Path) != src.Hash {
			out = append(out, src.Path)
		}
	}
	return out
}

// Index renders the standing-context text: one line per approved item with
// its ID, title, summary and a staleness mark. The body is not included;
// the agent reads an item with the knowledge tool when it matters. It
// returns "" when nothing is approved.
func (s *Store) Index() string {
	items, err := s.List()
	if err != nil {
		return ""
	}
	var lines []string
	for _, it := range items {
		if it.Status != Approved {
			continue
		}
		line := fmt.Sprintf("- %s: %s -- %s", it.ID, it.Title, it.Summary)
		if stale := s.Stale(it); len(stale) > 0 {
			line += " [STALE: " + strings.Join(stale, ", ") + " changed since this was written; verify before relying on it]"
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return ""
	}
	return "## Project knowledge (approved notes; read one with the knowledge tool, action read)\n" + strings.Join(lines, "\n") + "\n"
}

// DirFor is where a project's items live.
func DirFor(root string) string { return filepath.Join(root, ".atlas", "knowledge") }
