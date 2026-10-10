// Package taskstate is the shared lifecycle for device and environment work:
// isolated desktops, Android sequences, node plans, app loops and the phone
// view of a running task. It only models states and their legal transitions
// and persists records durably; it never runs anything itself.
package taskstate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sync"
	"time"
)

// State is one lifecycle state.
type State string

const (
	Queued               State = "queued"
	Preparing            State = "preparing"
	Running              State = "running"
	WaitingForUser       State = "waiting_for_user"
	WaitingForPermission State = "waiting_for_permission"
	Paused               State = "paused"
	Reconnecting         State = "reconnecting"
	Completed            State = "completed"
	Failed               State = "failed"
	Canceled             State = "canceled"
)

// All lists every state in lifecycle order.
var All = []State{Queued, Preparing, Running, WaitingForUser, WaitingForPermission, Paused, Reconnecting, Completed, Failed, Canceled}

// ErrTerminal is returned when a record in a terminal state is asked to move.
var ErrTerminal = errors.New("record is in a terminal state")

// ErrRevision is returned when a compare-and-swap update lost a race.
var ErrRevision = errors.New("record revision changed")

// Terminal reports whether s ends the lifecycle.
func (s State) Terminal() bool {
	return s == Completed || s == Failed || s == Canceled
}

// Waiting reports whether s is a pause for a person, not a stop.
func (s State) Waiting() bool {
	return s == WaitingForUser || s == WaitingForPermission || s == Paused
}

// Valid reports whether s is a known state.
func (s State) Valid() bool { return slices.Contains(All, s) }

// Transition validates a move. Terminal states never move, so an event that
// arrives after cancellation cannot bring a record back to running. A move
// to the same state is allowed (idempotent updates).
func Transition(from, to State) error {
	if !from.Valid() || !to.Valid() {
		return fmt.Errorf("unknown state transition %q -> %q", from, to)
	}
	if from.Terminal() {
		if from == to {
			return nil
		}
		return fmt.Errorf("%w: %s cannot become %s", ErrTerminal, from, to)
	}
	if to == Queued && from != Queued {
		return fmt.Errorf("a started record cannot return to queued")
	}
	return nil
}

// Record is the persisted lifecycle of one unit of work.
type Record struct {
	ID        string            `json:"id"`
	Kind      string            `json:"kind"`
	State     State             `json:"state"`
	Revision  int64             `json:"revision"`
	Reason    string            `json:"reason,omitempty"`
	Owner     string            `json:"owner,omitempty"`
	SessionID string            `json:"session_id,omitempty"`
	Target    string            `json:"target,omitempty"`
	CreatedAt int64             `json:"created_at"`
	UpdatedAt int64             `json:"updated_at"`
	Labels    map[string]string `json:"labels,omitempty"`
}

var idPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// ValidID reports whether id is safe to use as a file name.
func ValidID(id string) bool { return idPattern.MatchString(id) }

// Store persists records as one JSON file each. Writes are atomic
// (temp file, fsync, rename) and updates are compare-and-swap on Revision.
type Store struct {
	Dir string
	mu  sync.Mutex
	now func() time.Time
}

// NewStore returns a store rooted at dir.
func NewStore(dir string) *Store { return &Store{Dir: dir, now: time.Now} }

func (s *Store) path(id string) (string, error) {
	if !ValidID(id) {
		return "", fmt.Errorf("invalid record id %q", id)
	}
	return filepath.Join(s.Dir, id+".json"), nil
}

// Get reads a record.
func (s *Store) Get(id string) (Record, error) {
	path, err := s.path(id)
	if err != nil {
		return Record{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Record{}, err
	}
	var r Record
	if err := json.Unmarshal(data, &r); err != nil {
		return Record{}, fmt.Errorf("corrupt record %s: %w", id, err)
	}
	return r, nil
}

// Create writes a new record in state. It fails if the id exists.
func (s *Store) Create(r Record) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path, err := s.path(r.ID)
	if err != nil {
		return Record{}, err
	}
	if _, err := os.Stat(path); err == nil {
		return Record{}, fmt.Errorf("record %s already exists", r.ID)
	}
	if r.State == "" {
		r.State = Queued
	}
	if !r.State.Valid() {
		return Record{}, fmt.Errorf("unknown state %q", r.State)
	}
	now := s.clock().UnixMilli()
	r.Revision, r.CreatedAt, r.UpdatedAt = 1, now, now
	return r, WriteJSONAtomic(path, r)
}

// Move transitions a record. expected is the revision the caller observed;
// zero skips the check. The returned record is what was durably written.
func (s *Store) Move(id string, expected int64, to State, reason string) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.Get(id)
	if err != nil {
		return Record{}, err
	}
	if expected != 0 && r.Revision != expected {
		return r, fmt.Errorf("%w: have %d, expected %d", ErrRevision, r.Revision, expected)
	}
	if err := Transition(r.State, to); err != nil {
		return r, err
	}
	if r.State == to && r.Reason == reason {
		return r, nil
	}
	r.State, r.Reason = to, reason
	r.Revision++
	r.UpdatedAt = s.clock().UnixMilli()
	path, _ := s.path(id)
	if err := WriteJSONAtomic(path, r); err != nil {
		return Record{}, fmt.Errorf("persist %s state %s: %w", id, to, err)
	}
	return r, nil
}

// List returns all records of kind (all kinds when empty).
func (s *Store) List(kind string) ([]Record, error) {
	entries, err := os.ReadDir(s.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Record
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		r, err := s.Get(e.Name()[:len(e.Name())-len(".json")])
		if err != nil {
			continue
		}
		if kind == "" || r.Kind == kind {
			out = append(out, r)
		}
	}
	slices.SortFunc(out, func(a, b Record) int { return int(a.CreatedAt - b.CreatedAt) })
	return out, nil
}

func (s *Store) clock() time.Time {
	if s.now == nil {
		return time.Now()
	}
	return s.now()
}

// WriteJSONAtomic marshals v and writes it to path through a synced temp
// file and rename, so a crash never leaves a half-written record.
func WriteJSONAtomic(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return WriteFileAtomic(path, data, 0o600)
}

// WriteFileAtomic writes data to path through a synced temp file and rename.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, perm); err != nil {
		return err
	}
	return os.Rename(name, path)
}
