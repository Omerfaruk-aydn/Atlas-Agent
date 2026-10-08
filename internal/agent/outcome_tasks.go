package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/interaction"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/outcome"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
)

const (
	sharedContinuationMarker        = "[Atlas task completion check]"
	maxSharedContinuations          = 8
	maxSharedNoProgressContinuation = 2
)

type sharedContinuation struct {
	Prompt     string
	Continue   bool
	StopReason string
	Open       int
}

// sharedTaskDispatchRefusal prevents task implementation from crossing an
// unverified dependency. Callers decide which inspection tools remain usable.
func sharedTaskDispatchRefusal(ctx context.Context, store *engineering.Store, sessions session.Service, sessionID, taskID string) string {
	if taskID == "" || sessions == nil {
		return ""
	}
	scope := engineering.GetScope(ctx, sessionID)
	current, err := sessions.Get(ctx, scope.SessionID)
	if err != nil {
		return fmt.Sprintf("Task %q dependency state is unavailable: %v. Inspect the task before implementation.", taskID, err)
	}
	if !slices.ContainsFunc(current.Todos, func(todo session.Todo) bool { return todo.ID == taskID }) {
		return ""
	}
	requirements, err := sharedTaskRequirements(ctx, store, sessions, sessionID)
	if err != nil {
		return fmt.Sprintf("Task %q dependency state is unavailable: %v. Inspect the task before implementation.", taskID, err)
	}
	for _, resolved := range outcome.Resolve(requirements) {
		if resolved.State == outcome.StateUnresolved {
			return fmt.Sprintf("Task %q cannot dispatch while outcome %q is unknown. Inspect and reconcile the actual effects before implementation; do not replay the unknown mutation.", taskID, resolved.ID)
		}
		if resolved.ID != taskID || resolved.State == outcome.StateReady || resolved.State == outcome.StateDone {
			continue
		}
		return fmt.Sprintf("Task %q cannot dispatch: %s (%s). Verify its upstream requirements or resolve the concrete blocker before implementation.", taskID, resolved.State, resolved.Detail)
	}
	return ""
}

// sharedTaskRequirements interprets task reports using independent execution
// evidence. A model-written completed or passed field is never proof alone.
func sharedTaskRequirements(ctx context.Context, store *engineering.Store, sessions session.Service, sessionID string) ([]outcome.Requirement, error) {
	if store == nil || sessions == nil {
		return nil, fmt.Errorf("task outcome accounting is unavailable")
	}
	scope := engineering.GetScope(ctx, sessionID)
	current, err := sessions.Get(ctx, scope.SessionID)
	if err != nil {
		return nil, fmt.Errorf("read task reports: %w", err)
	}
	state, err := store.Read(ctx, scope.SessionID)
	if err != nil {
		return nil, fmt.Errorf("read task outcomes: %w", err)
	}
	_, board, err := store.ReadControlBoard(ctx, scope.SessionID)
	if err != nil {
		return nil, fmt.Errorf("read task controls: %w", err)
	}
	requirements := state.Outcomes.Requirements()
	for i, todo := range current.Todos {
		id := todo.ID
		if id == "" {
			id = fmt.Sprintf("todo:%d", i)
		}
		req := outcome.Requirement{ID: id, Description: todo.Content, DependsOn: slices.Clone(todo.DependsOn), Phase: outcome.PhasePlanned}
		switch todo.Status {
		case session.TodoStatusInProgress:
			req.Phase = outcome.PhaseDispatched
		case session.TodoStatusCompleted:
			req.Phase = outcome.PhaseApplied
			if sharedTaskVerified(ctx, store, scope.SessionID, todo, state) {
				req.Phase = outcome.PhaseVerified
			}
		}
		if todo.Verification == "failed" {
			req.Phase, req.Reason = outcome.PhaseBlocked, "Reported verification failed; inspect the actual task evidence"
		}
		if state.Paused || slices.Contains(board.HeldTasks, todo.ID) {
			req.Phase, req.Reason = outcome.PhaseBlocked, "Task paused by the user"
		}
		requirements = append(requirements, req)
	}
	return requirements, nil
}

func sharedTaskVerified(ctx context.Context, store *engineering.Store, sessionID string, todo session.Todo, state engineering.State) bool {
	if todo.ID == "" {
		return false
	}
	taskFingerprint := session.TaskFingerprint(todo)
	legacyRuns := map[string]bool{}
	if run, ok := state.RoleExecutions[todo.ID]; ok {
		if run.TaskFingerprint != taskFingerprint || run.Error != "" {
			return false
		}
		if run.SourceFingerprint != "" {
			current, err := engineering.SourceFingerprint(ctx, run.Root, store.Dir())
			if err != nil || current != run.SourceFingerprint {
				return false
			}
		}
		for _, check := range run.MachineChecks {
			if check.TaskID == todo.ID && check.RunID != "" {
				legacyRuns[check.RunID] = true
			}
		}
	}
	// A verify run binds actual machine checks to the task and call. The
	// durable journal also works after rolling operations have aged out.
	checks := slices.Clone(state.Checks)
	if run, ok := state.RoleExecutions[todo.ID]; ok {
		checks = append(checks, run.MachineChecks...)
	}
	seen := map[string]bool{}
	for _, check := range checks {
		if check.TaskID != todo.ID || check.RunID == "" || seen[check.RunID] {
			continue
		}
		seen[check.RunID] = true
		op, actual, err := store.VerificationObservation(ctx, sessionID, check.RunID, todo.ID, state)
		if err != nil || op.TaskID != todo.ID || op.Status != "completed" || !op.OutcomeObserved || len(op.EvidenceHash) != 64 || len(actual) == 0 || sharedProofStale(state, op) {
			continue
		}
		if op.TaskFingerprint != taskFingerprint && (op.TaskFingerprint != "" || !legacyRuns[op.CallID]) {
			continue
		}
		valid := true
		for _, observed := range actual {
			if !observed.Passed || observed.TaskID != todo.ID || observed.RunID != op.CallID || observed.Evidence == "" || !engineering.IsVerificationTool(observed.Tool) {
				valid = false
			}
		}
		if valid {
			return true
		}
	}
	// Other verification adapters need both an actual task-scoped operation
	// and current output evidence from that same source.
	for _, op := range state.Operations {
		if op.TaskID != todo.ID || op.Status != "completed" || !op.OutcomeObserved || len(op.EvidenceHash) != 64 || !engineering.IsVerificationTool(op.Tool) || sharedProofStale(state, op) {
			continue
		}
		if op.TaskFingerprint != taskFingerprint && (op.TaskFingerprint != "" || !legacyRuns[op.CallID]) {
			continue
		}
		for _, evidence := range state.Outcomes.Evidence {
			if evidence.Kind == outcome.KindOutput && evidence.Source == op.ID && evidence.Subject == todo.ID && evidence.Claim != "" && evidence.StaleBy == "" {
				return true
			}
		}
	}
	return false
}

func sharedProofStale(state engineering.State, op engineering.Operation) bool {
	if state.Outcomes.LastMutationAt > 0 && state.Outcomes.LastMutationAt >= op.FinishedAt {
		return true
	}
	for _, record := range state.Outcomes.Records {
		if record.Mutation && record.Phase == outcome.PhaseDispatched {
			return true
		}
		if record.OpID != op.ID && record.Mutation && record.At >= op.FinishedAt && (record.Phase == outcome.PhaseApplied || record.Phase == outcome.PhaseVerified || record.Phase == outcome.PhaseUnknown) {
			return true
		}
	}
	return false
}

// sharedCompletionPrompt continues only normal turns that actually used task
// tools. The persisted contract and progress bounds prevent restart loops.
func (a *sessionAgent) sharedCompletionPrompt(ctx context.Context, sessionID string, result *fantasy.AgentResult, queued int) sharedContinuation {
	if a.isSubAgent || a.engineering == nil || a.sessions == nil || result == nil || ctx.Err() != nil || queued > 0 || len(result.Steps) == 0 {
		return sharedContinuation{}
	}
	steps := result.Steps
	worked := turnWorkedOnSharedTask(steps)
	// A desktop turn without a task plan still answers for effects the
	// runtime itself observed: unknown mutations and windows that appeared
	// after an input but were never reported to the model.
	usedDesktop := turnUsedDesktop(steps)
	desktopOnly := !worked && usedDesktop
	if steps[len(steps)-1].FinishReason != fantasy.FinishReasonStop || !worked && !desktopOnly {
		return sharedContinuation{}
	}
	if a.maxStepsPerTurn != nil && maxStepsReached(steps, a.maxStepsPerTurn.Get()) || hasRepeatedToolCalls(steps, loopDetectionWindowSize, loopDetectionMaxRepeats) {
		return sharedContinuation{StopReason: "turn_bound"}
	}
	for _, step := range steps {
		if stoppedForUser(step.Content.ToolResults()) {
			return sharedContinuation{StopReason: "waiting_for_user"}
		}
	}
	scope := engineering.GetScope(ctx, sessionID)
	if control := interaction.Default.Snapshot(scope.SessionID); control.Paused || control.Canceled {
		return sharedContinuation{StopReason: "user_control"}
	}
	requirements, err := sharedTaskRequirements(ctx, a.engineering, a.sessions, sessionID)
	if err != nil {
		return sharedContinuation{StopReason: "outcome_unavailable: " + err.Error()}
	}
	if usedDesktop {
		requirements, err = desktopTurnRequirements(ctx, a.engineering, scope.SessionID, requirements, tools.DesktopOpenTransitions(ctx, scope.SessionID), worked)
		if err != nil {
			return sharedContinuation{StopReason: "outcome_unavailable: " + err.Error()}
		}
		if len(requirements) == 0 {
			return sharedContinuation{}
		}
	}
	var actionable []outcome.Requirement
	decision := sharedContinuation{}
	for i, resolved := range outcome.Resolve(requirements) {
		if resolved.State != outcome.StateDone {
			decision.Open++
		}
		switch resolved.State {
		case outcome.StateUnresolved, outcome.StateBlocked, outcome.StateBlockedDep, outcome.StateUnknownDeps, outcome.StateCanceled, outcome.StateFailed:
			decision.StopReason = "unresolved_or_blocked"
		case outcome.StateReady:
			actionable = append(actionable, requirements[i])
		}
	}
	if decision.StopReason != "" || len(actionable) == 0 {
		return decision
	}
	identity := make([]string, 0, len(requirements))
	progress := make([]string, 0, len(requirements))
	for _, req := range requirements {
		data, _ := json.Marshal(struct {
			ID, Description string
			DependsOn       []string
		}{req.ID, req.Description, req.DependsOn})
		identity = append(identity, string(data))
		if req.Phase == outcome.PhaseVerified || req.Phase == outcome.PhaseDelivered {
			progress = append(progress, req.ID+":"+string(req.Phase))
		}
	}
	slices.Sort(identity)
	slices.Sort(progress)
	contract := engineering.Hash(strings.Join(identity, "\x00"))
	err = a.engineering.Update(ctx, scope.SessionID, func(state *engineering.State) error {
		if state.Paused || len(state.Outcomes.Unresolved()) > 0 {
			decision.StopReason = "unresolved_or_blocked"
			return nil
		}
		continuation := &state.Outcomes.Continuation
		if continuation.ContractFingerprint != contract {
			// Model-authored task rewrites cannot reset execution bounds.
			continuation.ContractFingerprint = contract
		}
		if continuation.Count >= maxSharedContinuations {
			continuation.StopReason = "continuation_limit"
			decision.StopReason = continuation.StopReason
			return nil
		}
		mark := engineering.Hash(fmt.Sprintf("%d\x00%s", state.Generation, strings.Join(progress, "\x00")))
		if continuation.ProgressSignature == mark {
			continuation.NoProgress++
		} else {
			continuation.NoProgress = 0
		}
		continuation.ProgressSignature = mark
		if continuation.NoProgress >= maxSharedNoProgressContinuation {
			continuation.StopReason = "no_progress"
			decision.StopReason = continuation.StopReason
			return nil
		}
		continuation.Count++
		continuation.StopReason = ""
		decision.Continue = true
		return nil
	})
	if err != nil {
		decision.Continue, decision.StopReason = false, "checkpoint_unwritable: "+err.Error()
		return decision
	}
	if decision.Continue {
		data, _ := json.Marshal(actionable)
		decision.Prompt = sharedContinuationMarker + " Authorized task requirements remain actionable: " + string(data) + "\nContinue the ready work and verify applied work against its actual acceptance criteria. Use task-scoped machine checks and current evidence. Dependents wait for verified upstream work. Report unknown effects or concrete blockers honestly; do not replay unknown mutations. Update todos using the actual result."
	}
	return decision
}

// desktopTurnRequirements keeps the runtime-observed requirements of a
// desktop turn: unresolved ledger records, never stale todos the turn did
// not touch (unless this turn used its plan), plus windows that appeared
// after an input and were never
// reported to the model.
func desktopTurnRequirements(ctx context.Context, store *engineering.Store, sessionID string, all []outcome.Requirement, transitions []string, planned ...bool) ([]outcome.Requirement, error) {
	state, err := store.Read(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	ledger := map[string]bool{}
	for _, req := range state.Outcomes.Requirements() {
		ledger[req.ID] = true
	}
	var out []outcome.Requirement
	for _, req := range all {
		if ledger[req.ID] || len(planned) > 0 && planned[0] {
			out = append(out, req)
		}
	}
	for i, open := range transitions {
		out = append(out, outcome.Requirement{
			ID:          fmt.Sprintf("desktop:transition:%d", i),
			Description: open + "; observe that window and reconcile it with the task before reporting completion",
			Phase:       outcome.PhaseApplied,
		})
	}
	return out, nil
}

// turnUsedDesktop reports a turn that ran desktop tools.
func turnUsedDesktop(steps []fantasy.StepResult) bool {
	for _, step := range steps {
		for _, result := range step.Content.ToolResults() {
			if result.ToolName == tools.ComputerToolName || result.ToolName == tools.ToolPipelineName {
				return true
			}
		}
	}
	return false
}

func turnWorkedOnSharedTask(steps []fantasy.StepResult) bool {
	for _, step := range steps {
		for _, result := range step.Content.ToolResults() {
			if result.Result == nil || result.Result.GetType() == fantasy.ToolResultContentTypeError || result.StopTurn {
				continue
			}
			switch result.ToolName {
			case tools.TodosToolName, "workflow", "agent", "delegate", "orchestrate", "debate", "verify":
				return true
			}
		}
	}
	return false
}
