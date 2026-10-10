package localapps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/lock"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/procown"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/taskstate"
)

// State is an app's lifecycle stage. It is persisted; whether the app is
// running is never persisted as truth and is re-derived on every read.
type State string

// States.
const (
	StateDesigning    State = "designing"
	StateScaffolded   State = "scaffolded"
	StateVerifying    State = "verifying"
	StateVerified     State = "verified"
	StateReady        State = "ready"
	StateFailed       State = "failed"
	StateUpdating     State = "updating"
	StateUpdateFailed State = "update_failed"
)

// Check is one verification step's outcome.
type Check struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail,omitempty"`
}

// Verification is the last full verification.
type Verification struct {
	At         time.Time `json:"at"`
	Passed     bool      `json:"passed"`
	Checks     []Check   `json:"checks"`
	SourceHash string    `json:"source_hash"`
	Error      string    `json:"error,omitempty"`
}

// Acceptance is the recorded evidence for one acceptance criterion.
type Acceptance struct {
	Index      int       `json:"index"`
	Criterion  string    `json:"criterion"`
	Passed     bool      `json:"passed"`
	Method     string    `json:"method"`
	Evidence   string    `json:"evidence"`
	At         time.Time `json:"at"`
	SourceHash string    `json:"source_hash"`
}

// ProcessRecord identifies the process Atlas started for an app.
type ProcessRecord struct {
	Identity  procown.Identity `json:"identity"`
	Port      int              `json:"port"`
	URL       string           `json:"url"`
	StartedAt time.Time        `json:"started_at"`
	// AtlasPID is the Atlas process that started it.
	AtlasPID int `json:"atlas_pid"`
}

// UpdateRecord tracks an update in progress or a failed one.
type UpdateRecord struct {
	Task      string    `json:"task"`
	StartedAt time.Time `json:"started_at"`
	BackupID  string    `json:"backup_id"`
	PrevState State     `json:"prev_state"`
}

// Record is the registry entry. Name, description and tech are cached from
// the manifest so a broken manifest still shows a usable card.
type Record struct {
	ID               string         `json:"id"`
	Root             string         `json:"root"`
	Name             string         `json:"name"`
	Description      string         `json:"description"`
	Runtime          string         `json:"runtime"`
	State            State          `json:"state"`
	CreatedByAtlas   bool           `json:"created_by_atlas"`
	RegisteredAt     time.Time      `json:"registered_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
	ManifestHash     string         `json:"manifest_hash,omitempty"`
	Spec             *Spec          `json:"spec,omitempty"`
	LastVerification *Verification  `json:"last_verification,omitempty"`
	Acceptance       []Acceptance   `json:"acceptance,omitempty"`
	Process          *ProcessRecord `json:"process,omitempty"`
	Update           *UpdateRecord  `json:"update,omitempty"`
	LastError        string         `json:"last_error,omitempty"`
}

type registryFile struct {
	Version int      `json:"version"`
	Apps    []Record `json:"apps"`
}

// registry stores records in one JSON file guarded by a cross-process lock.
type registry struct {
	path string
}

func (r registry) lock(ctx context.Context) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(r.path), 0o700); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return lock.File(ctx, r.path+".lock")
}

func (r registry) read() ([]Record, error) {
	data, err := os.ReadFile(r.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var f registryFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("the app registry %s is damaged: %w", r.path, err)
	}
	if f.Version > 1 {
		return nil, fmt.Errorf("the app registry was written by a newer Atlas (version %d)", f.Version)
	}
	return f.Apps, nil
}

func (r registry) write(apps []Record) error {
	slices.SortFunc(apps, func(a, b Record) int { return strings.Compare(a.ID, b.ID) })
	return taskstate.WriteJSONAtomic(r.path, registryFile{Version: 1, Apps: apps})
}

// update applies fn to the record list under the lock and persists it.
func (r registry) update(ctx context.Context, fn func([]Record) ([]Record, error)) error {
	unlock, err := r.lock(ctx)
	if err != nil {
		return fmt.Errorf("lock the app registry: %w", err)
	}
	defer unlock()
	apps, err := r.read()
	if err != nil {
		return err
	}
	apps, err = fn(apps)
	if err != nil {
		return err
	}
	return r.write(apps)
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	return taskstate.WriteFileAtomic(path, data, perm)
}
