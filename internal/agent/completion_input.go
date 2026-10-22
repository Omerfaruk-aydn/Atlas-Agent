package agent

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workplan"
)

// Review findings that are not verified, waived or stale still stand against
// the work.
func openFindingLines(ctx context.Context, store *engineering.Store, sessionID string) []string {
	if store == nil {
		return nil
	}
	findings, err := store.TaskFindings(ctx, sessionID, "")
	if err != nil {
		// A findings record that cannot be read is not a clean bill.
		return []string{"review findings could not be read: " + err.Error()}
	}
	var open []string
	for _, f := range findings {
		switch f.Status {
		case "open", "assigned", "fixed":
			open = append(open, fmt.Sprintf("%s (%s:%d, %s)", f.Issue, f.Path, f.StartLine, f.Status))
		}
	}
	return open
}

// deliveryGapLines keeps registered stage gates in the aggregate decision,
// even when individual task reports already say completed.
func deliveryGapLines(state engineering.State) []string {
	if state.Delivery == nil {
		return nil
	}
	if err := state.Delivery.Validate(); err != nil {
		return []string{"delivery plan could not be validated: " + err.Error()}
	}
	var gaps []string
	for _, stage := range state.Delivery.Stages[state.Delivery.CurrentStage:] {
		gaps = append(gaps, fmt.Sprintf("delivery stage %s (%s) has not passed; complete its checks with workflow advance", stage.ID, stage.Title))
	}
	return gaps
}

// contractGapLines lists the required checks of the work plan that have not
// passed since the last code change.
func (a *sessionAgent) contractGapLines(sessionID string) []string {
	if !a.harnessOptions().GateEnabled("contract") {
		return nil
	}
	if a.evaluatorLedgers == nil || a.workingDir == "" {
		return nil
	}
	ledger, ok := a.evaluatorLedgers.Get(sessionID)
	if !ok {
		return nil
	}
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	if !ledger.changed {
		return nil
	}
	plan, _, err := workplan.Load(a.ledgerRoot(ledger))
	if err != nil {
		return []string{"verification plan could not be read: " + err.Error()}
	}
	if plan == nil {
		return nil
	}
	var lines []string
	for _, gap := range ledger.contractGaps(plan.Requirements()) {
		lines = append(lines, gap.String())
	}
	return lines
}

// completionInput gathers what the unified decision reads from the live
// session.
func (a *sessionAgent) completionInput(ctx context.Context, sessionID string, evaluation evaluatorDecision) (unifiedInput, bool) {
	if a.isSubAgent || a.engineering == nil || a.sessions == nil {
		return unifiedInput{}, false
	}
	// Evidence is about to be relied on: expose changes nobody reported first.
	a.checkSourceDrift(ctx, sessionID)
	scope := engineering.GetScope(ctx, sessionID).SessionID
	state, err := a.engineering.Read(ctx, scope)
	if err != nil {
		return unifiedInput{ContractGaps: []string{"execution state could not be read: " + err.Error()}}, true
	}
	root := a.projectRoot(sessionID)
	stamp, _, stampErr := a.engineering.WorkflowRecords(ctx, root, scope)
	board, _, boardErr := a.engineering.ReadControlBoard(ctx, scope)
	tasks, taskErr := completionTaskFingerprint(ctx, a.sessions, scope)
	source, sourceErr := engineering.ProjectFingerprint(ctx, root, a.engineering.Dir())
	requirements, reqErr := sharedTaskRequirements(ctx, a.engineering, a.sessions, sessionID)
	input := unifiedInput{
		Checkpoint: &engineering.CompletionCheckpoint{
			Revision: state.Revision, Generation: state.Generation,
			Root: root, RecordsFingerprint: stamp, BoardRevision: board.Revision, TasksFingerprint: tasks, SourceFingerprint: source, WorkFingerprint: engineering.WorkContractFingerprint(state.Work),
		},
		Requirements:   requirements,
		ContractGreen:  a.contractGreen(sessionID),
		ContractGaps:   a.contractGapLines(sessionID),
		OpenFindings:   openFindingLines(ctx, a.engineering, scope),
		Review:         evaluation.Review,
		UnknownEffects: len(state.Outcomes.Unresolved()),
		Paused:         state.Paused,
	}
	// Only command obligations survive a missing in-memory ledger. Delivery
	// stage gaps are recomputed from the live plan so advancing a stage clears
	// its former gap rather than preserving it as a permanent blocker.
	input.Checkpoint.ContractGaps = slices.Clone(input.ContractGaps)
	input.ContractGaps = append(input.ContractGaps, deliveryGapLines(state)...)
	for _, failure := range []error{stampErr, boardErr, taskErr, sourceErr, reqErr} {
		if failure != nil {
			input.ContractGaps = append(input.ContractGaps, "completion evidence could not be read: "+failure.Error())
		}
	}
	if state.Completion != nil {
		if input.Review == "" {
			input.Review = reviewStatus(state.Completion.Review)
			if input.Review == reviewPassed && (state.Completion.Generation != state.Generation || state.Completion.SourceFingerprint != source || state.Completion.TasksFingerprint != tasks || state.Completion.WorkFingerprint != engineering.WorkContractFingerprint(state.Work)) {
				input.Review = reviewUnavailable
			}
		}
		// After restart the in-memory contract ledger is absent. Its required
		// gaps remain obligations until a new ledger has checked the work.
		if a.evaluatorLedgers == nil {
			input.ContractGaps = append(input.ContractGaps, state.Completion.ContractGaps...)
			input.Checkpoint.ContractGaps = append(input.Checkpoint.ContractGaps, state.Completion.ContractGaps...)
		} else {
			ledger, ok := a.evaluatorLedgers.Get(sessionID)
			fresh := false
			if ok {
				ledger.mu.Lock()
				fresh = ledger.changed
				ledger.mu.Unlock()
			}
			if !fresh && !input.ContractGreen {
				input.ContractGaps = append(input.ContractGaps, state.Completion.ContractGaps...)
				input.Checkpoint.ContractGaps = append(input.Checkpoint.ContractGaps, state.Completion.ContractGaps...)
			}
		}
	}
	if evaluation.Continue != "" {
		input.OpenFindings = append(input.OpenFindings, evaluation.Continue)
	}
	if evaluation.Unresolved != "" {
		input.OpenFindings = append(input.OpenFindings, evaluation.Unresolved)
	}
	if evaluation.Unverified != "" && input.Review == "" {
		input.Review = reviewUnavailable
	}
	return input, true
}

func completionTaskFingerprint(ctx context.Context, sessions session.Service, id string) (string, error) {
	current, err := sessions.Get(ctx, id)
	if err != nil {
		return "", err
	}
	return session.TodosFingerprint(current.Todos), nil
}

// completionBlockers is the part of the unified decision a coordinator can
// evaluate without a session agent: whether requirements, open findings or
// unobserved effects stand between the work and a verified completion.
// VerificationBlockers exposes the same completion evidence used by goal mode
// to headless callers, without creating a second completion decision path.
func (c *coordinator) VerificationBlockers(ctx context.Context, sessionID string) []string {
	return completionBlockers(ctx, c.engineering, c.sessions, sessionID)
}

func completionBlockers(ctx context.Context, store *engineering.Store, sessions session.Service, sessionID string) []string {
	if store == nil || sessions == nil {
		return nil
	}
	requirements, err := sharedTaskRequirements(ctx, store, sessions, sessionID)
	if err != nil {
		return []string{"task completion evidence could not be read: " + err.Error()}
	}
	scope := engineering.GetScope(ctx, sessionID).SessionID
	state, err := store.Read(ctx, scope)
	if err != nil {
		return []string{"execution state could not be read: " + err.Error()}
	}
	input := unifiedInput{
		Requirements:   requirements,
		OpenFindings:   openFindingLines(ctx, store, scope),
		UnknownEffects: len(state.Outcomes.Unresolved()),
		Paused:         state.Paused,
	}
	input.ContractGaps = deliveryGapLines(state)
	if state.Completion != nil {
		input.Review = reviewStatus(state.Completion.Review)
		input.ContractGaps = append(input.ContractGaps, state.Completion.ContractGaps...)
		if input.Review == reviewPassed && state.Completion.Generation != state.Generation {
			input.Review = reviewUnavailable
		}
	}
	decision := decideCompletion(input)
	if decision.Complete() {
		if state.Completion != nil || len(requirements) > 0 {
			completion, err := store.CompletionStatus(ctx, scope)
			if err != nil {
				return []string{"saved completion evidence could not be read: " + err.Error()}
			}
			if completion == nil || completion.Verdict != "complete" {
				return []string{"no current aggregate completion decision; reevaluate the task before claiming the goal"}
			}
			tasks, err := completionTaskFingerprint(ctx, sessions, scope)
			if err != nil || tasks != completion.TasksFingerprint {
				return []string{"task specifications changed since the aggregate completion decision"}
			}
		}
		return nil
	}
	return decision.Reasons
}

// bindCompletion makes the unified decision binding in the one direction it
// can be trusted today: an ending path that would let the work stand as
// complete cannot do so while the unified decision finds a missing required
// check, an open finding, an unobserved effect or an unverified requirement.
// It never upgrades: when a legacy path already withholds completion, or the
// unified decision is complete, the evaluation is returned unchanged.
func bindCompletion(evaluation evaluatorDecision, decision unifiedDecision) evaluatorDecision {
	if decision.Complete() || decision.Verdict == "" {
		return evaluation
	}
	if evaluation.Continue != "" || evaluation.Unresolved != "" || evaluation.Unverified != "" {
		return evaluation
	}
	reasons := slices.Clone(decision.Reasons)
	if len(reasons) == 0 {
		reasons = []string{string(decision.Verdict)}
	}
	evaluation.Unverified = "Completion is " + string(decision.Verdict) + ": " + strings.Join(reasons, "; ")
	return evaluation
}
