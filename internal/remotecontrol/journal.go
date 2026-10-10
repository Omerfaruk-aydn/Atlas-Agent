package remotecontrol

import (
	"encoding/json"
	"errors"
	"os"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/taskstate"
)

// Step and operation states.
const (
	StatusPending     = "pending"
	StatusRunning     = "running"
	StatusDone        = "done"
	StatusSkipped     = "skipped"
	StatusFailed      = "failed"
	StatusSucceeded   = "succeeded"
	StatusCanceled    = "canceled"
	StatusInterrupted = "interrupted"
	StatusRolledBack  = "rolled_back"
	StatusIncomplete  = "incomplete"
)

// Operation kinds.
const (
	OpTest     = "test"
	OpInstall  = "install"
	OpRollback = "rollback"
	OpRemove   = "remove"
)

// JournalStep is one recorded step.
type JournalStep struct {
	ID       string    `json:"id"`
	Status   string    `json:"status"`
	Detail   Message   `json:"detail,omitzero"`
	Started  time.Time `json:"started,omitzero"`
	Finished time.Time `json:"finished,omitzero"`
}

// Journal records an installation, rollback or removal step by step, so a
// later start can tell what was applied and resume.
type Journal struct {
	Kind      string `json:"kind"`
	ProfileID string `json:"profile_id"`
	Token     string `json:"token"`
	PlanHash  string `json:"plan_hash,omitempty"`
	// Plan is the approved scope, kept so an interrupted installation can
	// resume without asking again when nothing new is needed.
	Plan     *Plan         `json:"plan,omitempty"`
	Approved bool          `json:"approved"`
	Status   string        `json:"status"`
	Backup   string        `json:"backup,omitempty"`
	Steps    []JournalStep `json:"steps"`
	Error    *Error        `json:"error,omitempty"`
	Started  time.Time     `json:"started"`
	Finished time.Time     `json:"finished,omitzero"`
}

// Step returns the recorded step, adding it when missing.
func (j *Journal) Step(id string) *JournalStep {
	for i := range j.Steps {
		if j.Steps[i].ID == id {
			return &j.Steps[i]
		}
	}
	j.Steps = append(j.Steps, JournalStep{ID: id, Status: StatusPending})
	return &j.Steps[len(j.Steps)-1]
}

// Running reports whether the journal was left mid-operation.
func (j *Journal) Running() bool { return j != nil && j.Status == StatusRunning }

func loadJournal(path string) (*Journal, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var j Journal
	if err := json.Unmarshal(data, &j); err != nil {
		return nil, err
	}
	return &j, nil
}

func saveJournal(path string, j *Journal) error { return taskstate.WriteJSONAtomic(path, j) }
