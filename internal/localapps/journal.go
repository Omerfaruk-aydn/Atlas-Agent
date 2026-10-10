package localapps

import (
	"slices"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/pubsub"
)

// StageName is a step users see while an operation runs. Stages are only
// recorded when the step really starts or ends.
type StageName string

// Stages, in creation order.
const (
	StageDesigning    StageName = "designing"
	StageFiles        StageName = "writing_files"
	StageDependencies StageName = "checking_dependencies"
	StageBuilding     StageName = "building"
	StageTesting      StageName = "testing"
	StageStarting     StageName = "starting"
	StageVerifying    StageName = "verifying"
	StageBackingUp    StageName = "backing_up"
	StageRestoring    StageName = "restoring"
	StageStopping     StageName = "stopping"
)

// Stage states.
const (
	StageRunning = "running"
	StagePassed  = "passed"
	StageFailed  = "failed"
)

// Stage is one recorded step.
type Stage struct {
	Name   StageName `json:"name"`
	State  string    `json:"state"`
	Detail string    `json:"detail,omitempty"`
	At     time.Time `json:"at"`
}

// Journal is the progress of the latest operation on one app.
type Journal struct {
	AppID    string    `json:"app_id"`
	Op       string    `json:"op"`
	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished,omitzero"`
	Stages   []Stage   `json:"stages"`
	Err      string    `json:"error,omitempty"`
}

// Done reports whether the operation ended.
func (j Journal) Done() bool { return !j.Finished.IsZero() }

// Event carries a journal snapshot.
type Event struct {
	AppID   string
	Journal Journal
}

func (m *Manager) startJournal(id, op string) {
	m.mu.Lock()
	j := &Journal{AppID: id, Op: op, Started: time.Now()}
	m.journals[id] = j
	snap := cloneJournal(j)
	m.mu.Unlock()
	m.broker.Publish(pubsub.UpdatedEvent, Event{AppID: id, Journal: snap})
}

// stage records a step. A running step for the same name is replaced by
// its outcome, so each step shows once.
func (m *Manager) stage(id string, name StageName, state, detail string) {
	m.mu.Lock()
	j := m.journals[id]
	if j == nil {
		j = &Journal{AppID: id, Op: "operation", Started: time.Now()}
		m.journals[id] = j
	}
	st := Stage{Name: name, State: state, Detail: detail, At: time.Now()}
	replaced := false
	for i := len(j.Stages) - 1; i >= 0; i-- {
		if j.Stages[i].Name == name && j.Stages[i].State == StageRunning {
			j.Stages[i] = st
			replaced = true
			break
		}
	}
	if !replaced {
		j.Stages = append(j.Stages, st)
	}
	snap := cloneJournal(j)
	m.mu.Unlock()
	m.broker.Publish(pubsub.UpdatedEvent, Event{AppID: id, Journal: snap})
}

func (m *Manager) finishJournal(id string, err error) {
	m.mu.Lock()
	j := m.journals[id]
	if j == nil {
		m.mu.Unlock()
		return
	}
	j.Finished = time.Now()
	if err != nil {
		j.Err = err.Error()
	}
	snap := cloneJournal(j)
	m.mu.Unlock()
	m.broker.Publish(pubsub.UpdatedEvent, Event{AppID: id, Journal: snap})
}

func cloneJournal(j *Journal) Journal {
	c := *j
	c.Stages = slices.Clone(j.Stages)
	return c
}

// Journal returns the latest operation journal of id.
func (m *Manager) Journal(id string) (Journal, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.journals[id]
	if !ok {
		return Journal{}, false
	}
	return cloneJournal(j), true
}

// LatestCreate returns the newest "create" journal started at or after
// since, so the UI can follow an app the agent is building.
func (m *Manager) LatestCreate(since time.Time) (Journal, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var best *Journal
	for _, j := range m.journals {
		if j.Op != "create" || j.Started.Before(since) {
			continue
		}
		if best == nil || j.Started.After(best.Started) {
			best = j
		}
	}
	if best == nil {
		return Journal{}, false
	}
	return cloneJournal(best), true
}

// continueJournal reuses the create journal while the app is still being
// built, so scaffold and verify add to the same progress view.
func (m *Manager) continueJournal(id, op string) {
	m.mu.Lock()
	j := m.journals[id]
	if j != nil && j.Op == "create" {
		j.Finished, j.Err = time.Time{}, ""
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()
	m.startJournal(id, op)
}
