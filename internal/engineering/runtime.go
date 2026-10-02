// Package engineering provides durable execution accounting and recovery.
package engineering

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/lock"
	"github.com/google/uuid"
)

const maxStateBytes = 1024 * 1024

type Limits struct {
	MaxTokens     int64   `json:"max_tokens,omitempty"`
	MaxToolCalls  int64   `json:"max_tool_calls,omitempty"`
	MaxDurationMS int64   `json:"max_duration_ms,omitempty"`
	MaxCost       float64 `json:"max_cost_usd,omitempty"`
}

type Usage struct {
	RepeatedFailedCalls int64   `json:"repeated_failed_calls"`
	Tokens              int64   `json:"tokens"`
	ToolCalls           int64   `json:"tool_calls"`
	DurationMS          int64   `json:"duration_ms"`
	Cost                float64 `json:"cost_usd"`
}

type Operation struct {
	AgentName       string `json:"agent_name,omitempty"`
	EvidenceHash    string `json:"evidence_hash,omitempty"`
	OutcomeObserved bool   `json:"outcome_observed,omitempty"`
	ID              string `json:"id"`
	CallID          string `json:"call_id"`
	Tool            string `json:"tool"`
	TaskID          string `json:"task_id,omitempty"`
	Fingerprint     string `json:"fingerprint"`
	Status          string `json:"status"`
	StartedAt       int64  `json:"started_at"`
	FinishedAt      int64  `json:"finished_at,omitempty"`
	BackgroundID    string `json:"background_id,omitempty"`
}

type TaskAccount struct {
	Limits Limits `json:"limits"`
	Usage  Usage  `json:"usage"`
}

type Check struct {
	FindingID     string `json:"finding_id,omitempty"`
	FindingReview string `json:"finding_review,omitempty"`
	ContractHash  string `json:"contract_hash,omitempty"`
	InputHash     string `json:"input_hash,omitempty"`
	TaskID        string `json:"task_id,omitempty"`
	RunID         string `json:"run_id,omitempty"`
	Name          string `json:"name"`
	Tool          string `json:"tool"`
	Passed        bool   `json:"passed"`
	Evidence      string `json:"evidence"`
	CheckedAt     int64  `json:"checked_at"`
}

type Workspace struct {
	AppliedPatchHash string `json:"applied_patch_hash,omitempty"`
	CommonDir        string `json:"common_dir"`
	ID               string `json:"id"`
	Path             string `json:"path"`
	Base             string `json:"base"`
	TaskID           string `json:"task_id,omitempty"`
}

type State struct {
	Paused           bool                         `json:"paused,omitempty"`
	SemanticEdits    map[string]SemanticEditState `json:"semantic_edits,omitempty"`
	Recipe           *RecipeBinding               `json:"recipe,omitempty"`
	ContractRoot     string                       `json:"contract_root,omitempty"`
	TaskContractRefs map[string][]Record          `json:"task_contract_refs,omitempty"`
	Resume           *ResumePlan                  `json:"resume,omitempty"`
	Delivery         *DeliveryPlan                `json:"delivery,omitempty"`
	Profile          string                       `json:"profile,omitempty"`
	Brief            *ProjectBrief                `json:"project_brief,omitempty"`
	Knowledge        []KnowledgeRecord            `json:"knowledge,omitempty"`
	UIEvidence       []UIEvidence                 `json:"ui_evidence,omitempty"`
	RoleExecutions   map[string]RoleExecution     `json:"role_executions,omitempty"`
	Version          int                          `json:"version"`
	Revision         uint64                       `json:"revision"`
	Generation       uint64                       `json:"generation"`
	Limits           Limits                       `json:"limits"`
	Usage            Usage                        `json:"usage"`
	Tasks            map[string]TaskAccount       `json:"task_accounts,omitempty"`
	Operations       []Operation                  `json:"operations,omitempty"`
	Failures         map[string]int               `json:"failures,omitempty"`
	Checks           []Check                      `json:"checks,omitempty"`
	Workspaces       []Workspace                  `json:"workspaces,omitempty"`
}

type Store struct{ dir string }

func NewStore(dir string) *Store { return &Store{dir: filepath.Join(dir, "engineering")} }

func (s *Store) Dir() string { return s.dir }

func Hash(value string) string {
	h := sha256.Sum256([]byte(value))
	return hex.EncodeToString(h[:])
}

func (l Limits) Validate() error {
	if l.MaxTokens < 0 || l.MaxToolCalls < 0 || l.MaxDurationMS < 0 || l.MaxCost < 0 || math.IsNaN(l.MaxCost) || math.IsInf(l.MaxCost, 0) {
		return errors.New("budget limits must be finite and non-negative")
	}
	return nil
}

func (l Limits) Check(u Usage) error {
	if l.MaxTokens > 0 && u.Tokens >= l.MaxTokens || l.MaxToolCalls > 0 && u.ToolCalls >= l.MaxToolCalls || l.MaxDurationMS > 0 && u.DurationMS >= l.MaxDurationMS || l.MaxCost > 0 && u.Cost >= l.MaxCost {
		return errors.New("execution budget exhausted; inspect workflow status before changing the budget")
	}
	return nil
}

func (s *Store) withState(ctx context.Context, id string, write bool, fn func(*State) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if id == "" {
		return errors.New("session ID is required")
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	name := filepath.Join(s.dir, Hash(id))
	lockCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	release, err := lock.File(lockCtx, name+".lock")
	if err != nil {
		return err
	}
	defer release()
	st := State{Version: 1, Tasks: map[string]TaskAccount{}, Failures: map[string]int{}}
	f, err := os.Open(name + ".json")
	if err == nil {
		data, readErr := io.ReadAll(io.LimitReader(f, maxStateBytes+1))
		f.Close()
		if readErr != nil {
			return readErr
		}
		if len(data) > maxStateBytes {
			return errors.New("execution state exceeds 1 MiB")
		}
		if err := json.Unmarshal(data, &st); err != nil {
			return fmt.Errorf("decode execution state: %w", err)
		}
		if st.Version != 1 {
			return errors.New("unsupported execution state version")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if st.Tasks == nil {
		st.Tasks = map[string]TaskAccount{}
	}
	if st.RoleExecutions == nil {
		st.RoleExecutions = map[string]RoleExecution{}
	}
	if st.Failures == nil {
		st.Failures = map[string]int{}
	}
	if err := fn(&st); err != nil {
		return err
	}
	if !write {
		return nil
	}
	if err := st.Limits.Validate(); err != nil {
		return err
	}
	if len(st.Tasks) > 512 || len(st.Workspaces) > 256 || len(st.RoleExecutions) > 256 {
		return errors.New("execution state collection limit exceeded")
	}
	if len(st.Knowledge) > 128 || len(st.UIEvidence) > 128 {
		return errors.New("delivery collection limit exceeded")
	}
	if st.Profile != "" && !ValidProfile(st.Profile) {
		return errors.New("unknown work profile")
	}
	if st.Delivery != nil {
		if err := st.Delivery.Validate(); err != nil {
			return err
		}
	}
	for _, t := range st.Tasks {
		if err := t.Limits.Validate(); err != nil {
			return err
		}
	}
	if len(st.Checks) > 64 {
		st.Checks = st.Checks[len(st.Checks)-64:]
	}
	st.Revision++
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	if len(data) > maxStateBytes {
		return errors.New("execution state exceeds 1 MiB")
	}
	return AtomicWrite(name+".json", data)
}

func AtomicWrite(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".atlas-state-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}

func (s *Store) Update(ctx context.Context, id string, fn func(*State) error) error {
	return s.withState(ctx, id, true, fn)
}

func (s *Store) Read(ctx context.Context, id string) (st State, err error) {
	err = s.withState(ctx, id, false, func(state *State) error { st = *state; return nil })
	return
}

func (s *Store) Check(ctx context.Context, id, task string) error {
	return s.withState(ctx, id, false, func(st *State) error {
		if err := st.Limits.Check(st.Usage); err != nil {
			return err
		}
		if task != "" {
			return st.Tasks[task].Limits.Check(st.Tasks[task].Usage)
		}
		return nil
	})
}

func fingerprint(tool, input string, generation uint64) string {
	var value any
	if json.Unmarshal([]byte(input), &value) == nil {
		if fields, ok := value.(map[string]any); ok {
			delete(fields, "description")
		}
		data, _ := json.Marshal(value)
		input = string(data)
	}
	return Hash(fmt.Sprintf("%d\x00%s\x00%s", generation, tool, input))
}

func (s *Store) Begin(ctx context.Context, session, call, tool, input, task string) (id string, err error) {
	release, lockErr := s.WorkflowLock(ctx, session)
	if lockErr != nil {
		return "", lockErr
	}
	defer release()
	id = uuid.NewString()
	err = s.Update(ctx, session, func(st *State) error {
		if st.Paused && (tool == "agent" || tool == "delegate" || tool == "orchestrate" || tool == "debate") {
			return fmt.Errorf("workflow dispatch is paused")
		}
		if err := st.Limits.Check(st.Usage); err != nil {
			return err
		}
		account := st.Tasks[task]
		if task != "" {
			if err := account.Limits.Check(account.Usage); err != nil {
				return err
			}
		}
		fp := fingerprint(task+"\x00"+tool, input, st.Generation)
		if st.Failures[fp] >= 3 {
			return errors.New("Identical tool call failed three times without a successful change. Change the approach or inspect the cause; it will not be executed again")
		}
		if len(st.Operations) >= 256 {
			kept := make([]Operation, 0, 256)
			for _, op := range st.Operations {
				if op.Status == "running" {
					kept = append(kept, op)
				}
			}
			if len(kept) >= 128 {
				return errors.New("too many unresolved operations; inspect recovery status")
			}
			for _, op := range st.Operations[max(0, len(st.Operations)-64):] {
				if op.Status != "running" {
					kept = append(kept, op)
				}
			}
			st.Operations = kept
		}
		st.Usage.ToolCalls++
		if st.Failures[fp] > 0 {
			st.Usage.RepeatedFailedCalls++
			account.Usage.RepeatedFailedCalls++
		}
		if task != "" {
			account.Usage.ToolCalls++
			st.Tasks[task] = account
		}
		st.Operations = append(st.Operations, Operation{ID: id, CallID: call, Tool: tool, TaskID: task, Fingerprint: fp, Status: "running", StartedAt: time.Now().UnixMilli()})
		return nil
	})
	return
}

func (s *Store) Finish(ctx context.Context, session, id string, passed, mutation bool) error {
	return s.FinishObserved(ctx, session, id, passed, mutation, "", false)
}

// FinishObserved retains a stable result hash and whether a machine outcome
// was available. Permission and infrastructure failures are not code diagnoses.
func (s *Store) FinishObserved(ctx context.Context, session, id string, passed, mutation bool, evidence string, observed bool) error {
	if evidence != "" && !validArtifactHash(evidence) {
		return fmt.Errorf("invalid operation evidence hash")
	}
	return s.Update(ctx, session, func(st *State) error {
		for i := range st.Operations {
			op := &st.Operations[i]
			if op.ID != id {
				continue
			}
			if op.Status != "running" {
				return nil
			}
			op.FinishedAt = time.Now().UnixMilli()
			op.EvidenceHash, op.OutcomeObserved = evidence, observed
			op.Status = "failed"
			if passed {
				op.Status = "completed"
				delete(st.Failures, op.Fingerprint)
			} else {
				if len(st.Failures) >= 128 {
					st.Failures = map[string]int{}
				}
				st.Failures[op.Fingerprint]++
			}
			if passed && mutation {
				st.Generation++
				st.Failures = map[string]int{}
			}
			if err := s.retainVerification(ctx, session, *op, st.Checks); err != nil {
				return err
			}
			return nil
		}
		return errors.New("operation not found")
	})
}

func (s *Store) Charge(ctx context.Context, id, task string, tokens int64, cost float64, duration time.Duration) error {
	if tokens < 0 || cost < 0 || math.IsNaN(cost) || math.IsInf(cost, 0) || duration < 0 {
		return errors.New("usage deltas must be non-negative")
	}
	return s.Update(ctx, id, func(st *State) error {
		st.Usage.Tokens += tokens
		st.Usage.Cost += cost
		st.Usage.DurationMS += duration.Milliseconds()
		if task != "" {
			a := st.Tasks[task]
			a.Usage.Tokens += tokens
			a.Usage.Cost += cost
			a.Usage.DurationMS += duration.Milliseconds()
			st.Tasks[task] = a
		}
		return nil
	})
}

func (s *Store) SetBudget(ctx context.Context, id, task string, limits Limits) error {
	if err := limits.Validate(); err != nil {
		return err
	}
	return s.Update(ctx, id, func(st *State) error {
		if task == "" {
			st.Limits = limits
		} else {
			a := st.Tasks[task]
			a.Limits = limits
			st.Tasks[task] = a
		}
		return nil
	})
}

func (s *Store) Resolve(ctx context.Context, id, operation, resolution, evidence string) error {
	if evidence == "" || len(evidence) > 2048 || resolution != "completed" && resolution != "failed" && resolution != "abandoned" {
		return errors.New("recovery requires inspection evidence and completed/failed/abandoned resolution")
	}
	return s.Update(ctx, id, func(st *State) error {
		for i := range st.Operations {
			if st.Operations[i].ID == operation && st.Operations[i].Status == "running" {
				st.Operations[i].Status = resolution
				st.Operations[i].FinishedAt = time.Now().UnixMilli()
				st.Checks = append(st.Checks, Check{Name: "recovery " + operation, Tool: "inspection", Passed: resolution == "completed", Evidence: evidence, CheckedAt: time.Now().UnixMilli()})
				return nil
			}
		}
		return errors.New("unresolved operation not found")
	})
}

type (
	scopeKey struct{}
	Scope    struct {
		SessionID, TaskID string
		WriteRoot         string
		OwnedPaths        []string
	}
)

func WithScope(ctx context.Context, session, task string) context.Context {
	scope, _ := ctx.Value(scopeKey{}).(Scope)
	scope.SessionID, scope.TaskID = session, task
	return context.WithValue(ctx, scopeKey{}, scope)
}

func WithOwnership(ctx context.Context, root string, paths []string) context.Context {
	scope := GetScope(ctx, "")
	scope.WriteRoot = root
	scope.OwnedPaths = append([]string(nil), paths...)
	return context.WithValue(ctx, scopeKey{}, scope)
}

func GetScope(ctx context.Context, fallback string) Scope {
	if s, ok := ctx.Value(scopeKey{}).(Scope); ok && s.SessionID != "" {
		return s
	}
	return Scope{SessionID: fallback}
}
