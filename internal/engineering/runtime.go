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
	"io/fs"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/lock"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/outcome"
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
	ProviderInvocations int64   `json:"provider_invocations"`
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
	TaskFingerprint string `json:"task_fingerprint,omitempty"`
	Fingerprint     string `json:"fingerprint"`
	Status          string `json:"status"`
	StartedAt       int64  `json:"started_at"`
	FinishedAt      int64  `json:"finished_at,omitempty"`
	BackgroundID    string `json:"background_id,omitempty"`
	// Generation is the source generation after the operation finished.
	// A check observed at an older generation does not verify current
	// source.
	Generation uint64 `json:"generation,omitempty"`
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

// SourceMark is the source fingerprint observed right after a passing check,
// with the generation it was taken at. Comparing it later exposes changes no
// tracked tool reported: a background job that kept writing, an edit made
// outside the agent, a partial write by a tool that failed.
type SourceMark struct {
	Generation  uint64 `json:"generation"`
	Fingerprint string `json:"fingerprint"`
}

type State struct {
	Completion *CompletionCheckpoint `json:"completion,omitempty"`
	// VerifiedSource is set when a check passed; see SourceMark.
	VerifiedSource *SourceMark `json:"verified_source,omitempty"`

	// journal resolves operations the rolling list no longer holds. It is set
	// on read-only views handed to work-state logic, never persisted.
	journal func(ref string) (Operation, bool)

	Expertise        []ExpertiseRecord            `json:"expertise,omitempty"`
	LearnedMethods   []LearnedMethod              `json:"learned_methods,omitempty"`
	Branches         []PatchBranch                `json:"patch_branches,omitempty"`
	Paused           bool                         `json:"paused,omitempty"`
	SemanticEdits    map[string]SemanticEditState `json:"semantic_edits,omitempty"`
	Outcomes         outcome.Ledger               `json:"outcomes,omitempty"`
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
	// Recovered is set when a damaged state file was quarantined. Records
	// from before that point are unavailable, not absent.
	Recovered *StateRecovery `json:"recovered,omitempty"`
	// Work is the task's working memory: contract, method decisions,
	// hypotheses, experiments, candidates and open questions.
	Work *WorkState `json:"work,omitempty"`
}

// StateRecovery describes a quarantined, undecodable execution state.
type StateRecovery struct {
	QuarantinedPath string `json:"quarantined_path"`
	Reason          string `json:"reason"`
	RecoveredAt     int64  `json:"recovered_at"`
}

// quarantineState moves an undecodable state file aside without deleting
// it, so it can be inspected or restored manually.
func quarantineState(path string, cause error) (string, error) {
	target := fmt.Sprintf("%s.corrupt-%d", path, time.Now().UnixNano())
	if err := os.Rename(path, target); err != nil {
		return "", err
	}
	slog.Warn("Quarantined undecodable execution state", "path", target, "error", cause)
	return target, nil
}

type Store struct {
	dir     string
	runners runnerRegistry
}

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
		decodeErr := json.Unmarshal(data, &st)
		if decodeErr == nil && st.Version == 0 {
			decodeErr = errors.New("missing execution state version")
		}
		if decodeErr != nil {
			// A damaged record must not lock the session out forever. It is
			// moved aside intact for inspection and the session continues
			// with an explicit recovery marker: earlier operation records
			// are unavailable, so their effects must be inspected, never
			// assumed or replayed.
			quarantined, qErr := quarantineState(name+".json", decodeErr)
			if qErr != nil {
				return fmt.Errorf("decode execution state: %w (quarantine failed: %v)", decodeErr, qErr)
			}
			st = State{Version: 1, Tasks: map[string]TaskAccount{}, Failures: map[string]int{}, Recovered: &StateRecovery{
				QuarantinedPath: quarantined, Reason: decodeErr.Error(), RecoveredAt: time.Now().UnixMilli(),
			}}
			write = true
		} else if st.Version != 1 {
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
	if st.Work != nil {
		if err := st.Work.validate(); err != nil {
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
	return renameRetry(f.Name(), path)
}

// renameRetry replaces path with tmp. On Windows a rename over a file that
// another process has open (an antivirus scan, a concurrent reader) fails
// with "access denied" for a moment; that ended a whole agent turn, so a
// permission error is retried briefly before it is returned.
func renameRetry(tmp, path string) error {
	delay := 10 * time.Millisecond
	for attempt := 0; ; attempt++ {
		err := os.Rename(tmp, path)
		if err == nil || runtime.GOOS != "windows" || !errors.Is(err, fs.ErrPermission) || attempt == renameAttempts-1 {
			return err
		}
		time.Sleep(delay)
		delay = min(delay*2, 400*time.Millisecond)
	}
}

// renameAttempts bounds renameRetry to about two seconds.
const renameAttempts = 9

func (s *Store) Update(ctx context.Context, id string, fn func(*State) error) error {
	return s.withState(ctx, id, true, fn)
}

func (s *Store) Read(ctx context.Context, id string) (st State, err error) {
	err = s.withState(ctx, id, false, func(state *State) error { st = *state; return nil })
	return
}

func (s *Store) Check(ctx context.Context, id, task string) error {
	if task != "" {
		_, board, err := s.ReadControlBoard(ctx, id)
		if err != nil {
			return err
		}
		if slices.Contains(board.HeldTasks, task) {
			return fmt.Errorf("task paused by user")
		}
	}
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
	if task != "" {
		_, board, err := s.ReadControlBoard(ctx, session)
		if err != nil {
			return "", err
		}
		if slices.Contains(board.HeldTasks, task) {
			return "", fmt.Errorf("task paused by user")
		}
	}
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
			return errors.New("identical tool call failed three times without a successful change. Change the approach or inspect the cause; it will not be executed again")
		}
		if len(st.Operations) >= 256 {
			kept := make([]Operation, 0, 256)
			for _, op := range st.Operations {
				if op.Status == "running" || unknownOperationOutcome(st, op.ID) {
					kept = append(kept, op)
				}
			}
			if len(kept) >= 128 {
				return errors.New("too many unresolved operations; inspect recovery status")
			}
			for _, op := range st.Operations[max(0, len(st.Operations)-64):] {
				if op.Status != "running" && !unknownOperationOutcome(st, op.ID) {
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
			op.Generation = st.Generation
			if err := s.retainVerification(ctx, session, *op, st.Checks); err != nil {
				return err
			}
			return nil
		}
		return errors.New("operation not found")
	})
}

// NoteSourceChange advances the source generation for a change detected
// outside a successful operation (partial failure, cancellation, an editor
// or shell the tool classification could not see). Evidence recorded at the
// earlier generation becomes stale; history is not rewritten.
func (s *Store) NoteSourceChange(ctx context.Context, session string) error {
	return s.Update(ctx, session, func(st *State) error {
		st.Generation++
		st.Failures = map[string]int{}
		return nil
	})
}

// RecordVerifiedSource stores the fingerprint observed after a passing check,
// at the generation that check ran in.
func (s *Store) RecordVerifiedSource(ctx context.Context, session, fingerprint string) error {
	if fingerprint == "" {
		return nil
	}
	return s.Update(ctx, session, func(st *State) error {
		st.VerifiedSource = &SourceMark{Generation: st.Generation, Fingerprint: fingerprint}
		return nil
	})
}

// SourceMarked returns the stored mark when it still describes the current
// generation, so a caller knows whether a fingerprint comparison can say
// anything. A mark from an older generation was already staled by a tracked
// change.
func (s *Store) SourceMarked(ctx context.Context, session string) (SourceMark, bool) {
	st, err := s.Read(ctx, session)
	if err != nil || st.VerifiedSource == nil || st.VerifiedSource.Generation != st.Generation {
		return SourceMark{}, false
	}
	return *st.VerifiedSource, true
}

// NoteSourceDrift compares the current fingerprint with the mark taken after
// the last passing check. A difference at the same generation is a change no
// tracked tool reported, so the source generation advances and every earlier
// check becomes stale. It reports whether it did.
func (s *Store) NoteSourceDrift(ctx context.Context, session, current string) (bool, error) {
	drifted := false
	err := s.Update(ctx, session, func(st *State) error {
		mark := st.VerifiedSource
		if mark == nil || mark.Generation != st.Generation || mark.Fingerprint == current {
			return nil
		}
		st.Generation++
		st.Failures = map[string]int{}
		st.VerifiedSource = nil
		drifted = true
		return nil
	})
	return drifted, err
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
	return s.resolve(ctx, id, operation, resolution, evidence, "")
}

// ResolveObserved settles an external effect as verified. The observation is
// not text typed by the caller: a trusted adapter must have recorded a
// read-only check of this exact effect. Opaque MCP replies do not establish
// which resource or expected result was checked and cannot supply this proof.
func (s *Store) ResolveObserved(ctx context.Context, id, operation, observedOp, evidence string) error {
	if observedOp == "" {
		return errors.New("an observed operation is required")
	}
	return s.resolve(ctx, id, operation, "completed", evidence, observedOp)
}

func (s *Store) resolve(ctx context.Context, id, operation, resolution, evidence, observedOp string) error {
	if evidence == "" || len(evidence) > 2048 || resolution != "completed" && resolution != "failed" && resolution != "abandoned" {
		return errors.New("recovery requires inspection evidence and completed/failed/abandoned resolution")
	}
	return s.Update(ctx, id, func(st *State) error {
		if resolution == "completed" {
			if err := checkExternalObservation(st, operation, observedOp); err != nil {
				return err
			}
		}
		now := time.Now().UnixMilli()
		found := false
		for i := range st.Operations {
			op := &st.Operations[i]
			if op.ID == operation && (op.Status == "running" || unknownOperationOutcome(st, operation)) {
				op.Status, op.FinishedAt = resolution, now
				op.EvidenceHash, op.OutcomeObserved = Hash(evidence), true
				found = true
				break
			}
		}
		// Older state compaction could drop the operation before the shared
		// ledger learned its outcome was unknown. Explicit inspection can
		// still settle that retained record without inventing execution.
		if !found && !unknownOperationOutcome(st, operation) {
			return errors.New("unresolved operation not found")
		}
		phase := outcome.PhaseCanceled
		external := slices.ContainsFunc(st.Outcomes.Records, func(r outcome.Record) bool { return r.OpID == operation && r.External && r.Mutation })
		switch resolution {
		case "completed":
			phase = outcome.PhaseApplied
			if external {
				// Inspection of the target system observed the effect.
				phase = outcome.PhaseVerified
			}
		case "failed":
			phase = outcome.PhaseFailed
		}
		st.Outcomes.Resolve(operation, phase)
		st.Outcomes.Add(outcome.Evidence{ID: "recovery:" + operation, Kind: outcome.KindObservation, Source: operation, Subject: operation, Claim: evidence, ObservedAt: now})
		st.Checks = append(st.Checks, Check{Name: "recovery " + operation, Tool: "inspection", Passed: resolution == "completed", Evidence: evidence, CheckedAt: now})
		return nil
	})
}

// checkExternalObservation refuses to call a pending external effect
// completed on the strength of the caller's words or a successful unrelated
// call. Output evidence must attest to the exact dispatched request, including
// its target and requested change, and come from a later read-only check.
func checkExternalObservation(st *State, operation, observedOp string) error {
	var effect *outcome.Record
	effectAt := -1
	for i := range st.Outcomes.Records {
		if r := &st.Outcomes.Records[i]; r.OpID == operation && r.External && r.Mutation && !r.Resolved {
			effect, effectAt = &st.Outcomes.Records[i], i
		}
	}
	if effect == nil {
		return nil
	}
	if observedOp == "" {
		return errors.New("an external effect is verified by a recorded observation, not by a description: inspect the target system with a tool call, then resolve with the observing operation id")
	}
	// Records are kept in the order calls finished, so a later index is a
	// later call even within one millisecond.
	for i, r := range st.Outcomes.Records {
		if r.OpID != observedOp {
			continue
		}
		if r.OpID == operation || i <= effectAt || r.Mutation || r.Phase != outcome.PhaseVerified {
			return errors.New("the observing operation must be a later read-only check of the exact external effect; an opaque MCP call or another mutation is not proof")
		}
		op, ok := findOperation(*st, observedOp)
		if !ok || op.Status != "completed" || !op.OutcomeObserved || !validArtifactHash(op.EvidenceHash) || op.StartedAt <= effect.At {
			return errors.New("the observing check has no runtime-observed successful result")
		}
		if effect.Fingerprint != "" && slices.ContainsFunc(st.Outcomes.Evidence, func(e outcome.Evidence) bool {
			return e.Kind == outcome.KindOutput && e.Source == observedOp && e.Subject == operation && e.Claim == ExternalResultClaim(*effect) && e.StaleBy == ""
		}) {
			return nil
		}
		return errors.New("the observing check has no current adapter evidence bound to this operation's exact target and requested result; opaque MCP effects remain unverified")
	}
	return errors.New("observing operation not found in the recorded outcomes")
}

// ExternalResultClaim is the assertion a trusted verification adapter records
// only after comparing the actual target state with the dispatched request.
// A caller-supplied description or a server's generic success is insufficient.
func ExternalResultClaim(effect outcome.Record) string {
	return "external-result:" + effect.Fingerprint
}

func unknownOperationOutcome(st *State, id string) bool {
	return slices.ContainsFunc(st.Outcomes.Records, func(record outcome.Record) bool {
		return record.OpID == id && (record.Phase == outcome.PhaseUnknown || record.Phase == outcome.PhaseDispatched || record.PendingExternal())
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
