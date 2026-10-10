package nodes

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/taskstate"
)

var opIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{7,127}$`)

// ErrOperationConflict means an operation_id was reused for a different
// request; replaying it would hide a real side effect.
var ErrOperationConflict = errors.New("operation_id was already used for a different request")

// Journal records every side-effecting operation so a repeated request (a
// retry after a timeout, a double tap, a reconnect) returns the recorded
// outcome instead of running the effect again.
type Journal struct {
	dir string
	mu  sync.Mutex
	ops map[string]*Operation
}

// OpenJournal loads dir. Operations still marked running belong to a
// previous node process; their outcome is unknown and they are closed as
// failed with effect "unknown" rather than re-executed.
func OpenJournal(dir string) (*Journal, error) {
	j := &Journal{dir: dir, ops: map[string]*Operation{}}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var op Operation
		if json.Unmarshal(data, &op) != nil || op.ID == "" {
			continue
		}
		if !op.State.Terminal() {
			op.State = taskstate.Failed
			op.Effect = "unknown"
			op.Error = "node restarted while the operation was running; outcome unknown, not re-run"
			op.EndedAt = time.Now().UnixMilli()
			_ = j.write(&op)
		}
		j.ops[op.ID] = &op
	}
	return j, nil
}

// RequestHash fingerprints the request body that defines an operation.
func RequestHash(v any) string {
	data, _ := json.Marshal(v)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Begin registers a new operation. When the id exists with the same request
// hash and device, it returns the existing record and existing=true. A
// different request under the same id is a conflict.
func (j *Journal) Begin(op Operation) (*Operation, bool, error) {
	if !opIDPattern.MatchString(op.ID) {
		return nil, false, fmt.Errorf("operation_id must be 8-128 characters of letters, digits, '.', '_', ':' or '-'")
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if prior, ok := j.ops[op.ID]; ok {
		if prior.RequestHash != op.RequestHash || prior.DeviceID != op.DeviceID {
			return nil, false, ErrOperationConflict
		}
		replay := *prior
		replay.Replayed = true
		return &replay, true, nil
	}
	op.State = taskstate.Running
	op.Effect = "unknown"
	op.StartedAt = time.Now().UnixMilli()
	if err := j.write(&op); err != nil {
		return nil, false, fmt.Errorf("journal operation before running it: %w", err)
	}
	stored := op
	j.ops[op.ID] = &stored
	return &stored, false, nil
}

// Replay returns the recorded operation for a repeated request before any
// precondition is re-evaluated, so a retried input whose observation has
// since been consumed still gets its original result instead of an error
// that would tempt the caller to send it again.
func (j *Journal) Replay(id, deviceID, requestHash string) (*Operation, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	prior, ok := j.ops[id]
	if !ok {
		return nil, nil
	}
	if prior.RequestHash != requestHash || prior.DeviceID != deviceID {
		return nil, ErrOperationConflict
	}
	replay := *prior
	replay.Replayed = true
	return &replay, nil
}

// Finish records the outcome durably. The caller must report failure if
// Finish fails, because the result would not survive a restart.
func (j *Journal) Finish(id string, update func(*Operation)) (Operation, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	op, ok := j.ops[id]
	if !ok {
		return Operation{}, fmt.Errorf("unknown operation %s", id)
	}
	if op.State.Terminal() {
		return *op, nil
	}
	next := *op
	update(&next)
	if err := taskstate.Transition(op.State, next.State); err != nil {
		return *op, err
	}
	next.EndedAt = time.Now().UnixMilli()
	if err := j.write(&next); err != nil {
		return *op, fmt.Errorf("persist operation result: %w", err)
	}
	*op = next
	return next, nil
}

// Get returns an operation visible to device.
func (j *Journal) Get(id, deviceID string) (Operation, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	op, ok := j.ops[id]
	if !ok || op.DeviceID != deviceID {
		return Operation{}, false
	}
	return *op, true
}

func (j *Journal) write(op *Operation) error {
	return taskstate.WriteJSONAtomic(filepath.Join(j.dir, opFile(op.ID)), op)
}

func opFile(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:12]) + ".json"
}
