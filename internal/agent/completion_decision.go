package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/harnessmetrics"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/outcome"
)

// completionVerdict is the single answer to "may this work be called done?".
// The pieces that feed it (todos, work items, the verification contract, the
// independent review, unknown effects) used to be consulted separately by
// each ending path, so they could disagree. Applied, verified and delivered
// stay distinct: only complete means every requirement is verified with
// current evidence and no required review is missing.
type completionVerdict string

const (
	// completeVerdict: every requirement is verified, nothing is open, and the
	// required review passed or was explicitly waived.
	completeVerdict completionVerdict = "complete"
	// incompleteVerdict: work remains (unstarted, applied but failing, open
	// findings, a failed review or a missing required check).
	incompleteVerdict completionVerdict = "incomplete"
	// unverifiedVerdict: the work looks applied but proof is missing or the
	// required review could not run. It may end the turn; it may not be
	// reported as verified.
	unverifiedVerdict completionVerdict = "unverified"
	// blockedVerdict: an unknown effect, a pause or a concrete blocker stops
	// the work until it is resolved.
	blockedVerdict completionVerdict = "blocked"
)

// unifiedInput is everything the decision reads. It is gathered by the
// caller so the rules themselves stay a pure, testable function.
type unifiedInput struct {
	Checkpoint   *engineering.CompletionCheckpoint
	Requirements []outcome.Requirement
	// ContractGreen means the project's verification contract passed after the
	// last code change; it supports applied requirements, and is reported as
	// such, rather than silently relabelling them verified.
	ContractGreen bool
	// ContractGaps are required checks that are missing, stale or failing.
	ContractGaps []string
	// OpenFindings are unresolved reviewer or contract findings.
	OpenFindings []string
	// Review is the status of the required independent review; empty means no
	// review was required for this work.
	Review reviewStatus
	// UnknownEffects counts mutations whose outcome is not observed.
	UnknownEffects int
	Paused         bool
	Blocker        string
}

type unifiedDecision struct {
	Verdict completionVerdict `json:"verdict"`
	// Verified, Applied and Open list requirement IDs by standing.
	Verified []string `json:"verified,omitempty"`
	Applied  []string `json:"applied,omitempty"`
	Open     []string `json:"open,omitempty"`
	// ByContract lists applied requirements counted as verified only because
	// the project contract is green, so the basis stays visible.
	ByContract []string `json:"by_contract,omitempty"`
	Missing    []string `json:"missing,omitempty"`
	Reasons    []string `json:"reasons"`
}

func (d unifiedDecision) Complete() bool { return d.Verdict == completeVerdict }

// decideCompletion applies the completion rules in priority order: what
// blocks, what is still to do, what lacks proof, and only then complete.
func decideCompletion(in unifiedInput) unifiedDecision {
	d := unifiedDecision{}
	blocked := func(reason string) {
		d.Verdict = blockedVerdict
		d.Reasons = append(d.Reasons, reason)
	}

	var failed, unstarted, applied []string
	for _, res := range outcome.Resolve(in.Requirements) {
		req := in.Requirements[slices.IndexFunc(in.Requirements, func(r outcome.Requirement) bool { return r.ID == res.ID })]
		switch res.State {
		case outcome.StateDone:
			d.Verified = append(d.Verified, res.ID)
		case outcome.StateUnresolved, outcome.StateBlocked, outcome.StateBlockedDep, outcome.StateUnknownDeps, outcome.StateCanceled, outcome.StateFailed:
			failed = append(failed, fmt.Sprintf("%s: %s %s", res.ID, res.State, res.Detail))
		default:
			d.Open = append(d.Open, res.ID)
			switch req.Phase {
			case outcome.PhaseApplied:
				applied = append(applied, res.ID)
				d.Applied = append(d.Applied, res.ID)
			case outcome.PhaseVerified, outcome.PhaseDelivered:
				// Verified itself but waiting on an unverified upstream; the
				// upstream's standing decides.
			default:
				unstarted = append(unstarted, res.ID)
			}
		}
	}
	// A green project contract is evidence for applied work, but it is not a
	// requirement-specific check. Record that basis instead of hiding it.
	// Effects outside the project tree are never covered by it.
	var covered []string
	for _, id := range applied {
		if !strings.HasPrefix(id, "external:") && !strings.HasPrefix(id, "work:") {
			covered = append(covered, id)
		}
	}
	if in.ContractGreen && len(covered) > 0 && len(unstarted) == 0 && len(failed) == 0 {
		d.ByContract = slices.Clone(covered)
		d.Verified = append(d.Verified, covered...)
		d.Open = slices.DeleteFunc(d.Open, func(id string) bool { return slices.Contains(covered, id) })
		applied = slices.DeleteFunc(applied, func(id string) bool { return slices.Contains(covered, id) })
	}

	switch {
	case in.Blocker != "":
		blocked(in.Blocker)
	case in.Paused:
		blocked("the task is paused by the user")
	case in.UnknownEffects > 0:
		blocked(fmt.Sprintf("%d mutation(s) have an unobserved outcome; inspect the actual effects before continuing", in.UnknownEffects))
	case len(failed) > 0:
		blocked(strings.Join(failed, "; "))
	}
	if d.Verdict == blockedVerdict {
		return d
	}

	d.Missing = slices.Clone(in.ContractGaps)
	if len(unstarted) > 0 {
		d.Reasons = append(d.Reasons, "not yet applied: "+strings.Join(unstarted, ", "))
	}
	if len(in.OpenFindings) > 0 {
		d.Reasons = append(d.Reasons, fmt.Sprintf("%d open finding(s)", len(in.OpenFindings)))
	}
	if len(in.ContractGaps) > 0 {
		d.Reasons = append(d.Reasons, fmt.Sprintf("%d required check(s) missing, stale or failing", len(in.ContractGaps)))
	}
	if in.Review.Blocking() {
		d.Reasons = append(d.Reasons, "the independent review failed the work")
	}
	if len(unstarted) > 0 || len(in.OpenFindings) > 0 || len(in.ContractGaps) > 0 || in.Review.Blocking() {
		d.Verdict = incompleteVerdict
		return d
	}

	if len(applied) > 0 {
		d.Verdict = unverifiedVerdict
		d.Reasons = append(d.Reasons, "applied but not verified with current evidence: "+strings.Join(applied, ", "))
		return d
	}
	if in.Review != "" && !in.Review.Verified() && in.Review != reviewWaived {
		d.Verdict = unverifiedVerdict
		d.Reasons = append(d.Reasons, "the required review did not produce a verdict ("+string(in.Review)+")")
		return d
	}
	d.Verdict = completeVerdict
	if in.Review == reviewWaived {
		d.Reasons = append(d.Reasons, "no independent reviewer is configured")
	}
	if len(d.ByContract) > 0 {
		d.Reasons = append(d.Reasons, "applied work counted as verified through the project contract")
	}
	return d
}

// legacyComplete reproduces what the existing ending paths conclude for the
// same turn: the evaluator gate let it end without sending it back or
// flagging it, and the shared completion check found every requirement done
// once a green contract relabelled applied work as verified.
func legacyComplete(evaluation evaluatorDecision, requirements []outcome.Requirement) bool {
	if evaluation.Continue != "" || evaluation.Unresolved != "" {
		return false
	}
	for _, res := range outcome.Resolve(requirements) {
		if res.State != outcome.StateDone {
			return false
		}
	}
	return true
}

// decideTurnCompletion computes the unified decision for a finished turn,
// records where it differs from the legacy paths, and returns the evaluation
// with the decision bound in. A missing check, finding or unobserved effect
// withholds completion; actionable incomplete work shares the durable task
// continuation budget. Required unavailable reviews stop without certification.
func (a *sessionAgent) decideTurnCompletion(ctx context.Context, sessionID string, evaluation evaluatorDecision) (result evaluatorDecision, divergedResult bool) {
	_, observation := startGate(ctx, "aggregate_completion")
	defer func() { observation.decision(result, !a.isSubAgent && a.engineering != nil && a.sessions != nil) }()
	var input unifiedInput
	var decision unifiedDecision
	// The decision relies on checks made earlier. The tree is read again after
	// it is made: a background job or an editor that wrote in between would
	// leave the decision describing a tree that no longer exists.
	for attempt := 0; ; attempt++ {
		var ok bool
		if input, ok = a.completionInput(ctx, sessionID, evaluation); !ok {
			return evaluation, false
		}
		decision = decideCompletion(input)
		if a.beforeDriftRecheck != nil {
			a.beforeDriftRecheck()
		}
		if !a.checkSourceDrift(ctx, sessionID) {
			break
		}
		if attempt >= 2 {
			decision.Verdict = unverifiedVerdict
			decision.Reasons = append(decision.Reasons, "project files kept changing while the completion was being decided")
			break
		}
	}
	requirements := input.Requirements
	legacy := legacyComplete(evaluation, a.verifiedByContract(sessionID, slices.Clone(requirements)))
	diverged := legacy != decision.Complete()
	if diverged {
		slog.Warn("Completion decision diverged from the legacy paths",
			"session_id", sessionID, "legacy_complete", legacy, "verdict", decision.Verdict,
			"reasons", strings.Join(decision.Reasons, "; "))
		a.recordHarness(harnessmetrics.CompletionDiverged, sessionID, map[string]string{
			"legacy": fmt.Sprint(legacy), "verdict": string(decision.Verdict),
		})
	}
	continueWork := evaluation.Continue == "" && evaluation.Unresolved == "" && evaluation.Unverified == ""
	prompt, err := a.persistCompletion(ctx, sessionID, input, decision, continueWork)
	if err != nil {
		decision.Verdict = unverifiedVerdict
		decision.Reasons = append(decision.Reasons, "completion could not be committed against the current evidence: "+err.Error())
	}
	bound := bindCompletion(evaluation, decision)
	if err == nil && prompt != "" {
		bound.Continue = prompt
	}
	return bound, diverged
}

// persistCompletion commits the decision and item delivery together, after
// the evaluator has run. An answer being sent earlier is only delivery history.
func (a *sessionAgent) persistCompletion(ctx context.Context, sessionID string, input unifiedInput, decision unifiedDecision, continueWork bool) (string, error) {
	if input.Checkpoint == nil {
		return "", fmt.Errorf("completion evidence is unavailable")
	}
	scope := engineering.GetScope(ctx, sessionID).SessionID
	c := *input.Checkpoint
	stamp, _, err := a.engineering.WorkflowRecords(ctx, c.Root, scope)
	if err != nil {
		return "", err
	}
	board, _, err := a.engineering.ReadControlBoard(ctx, scope)
	if err != nil {
		return "", err
	}
	tasks, err := completionTaskFingerprint(ctx, a.sessions, scope)
	if err != nil {
		return "", err
	}
	source, err := engineering.ProjectFingerprint(ctx, c.Root, a.engineering.Dir())
	if err != nil {
		return "", err
	}
	if stamp != c.RecordsFingerprint || board.Revision != c.BoardRevision || tasks != c.TasksFingerprint || source != c.SourceFingerprint {
		return "", fmt.Errorf("source, workflow records or task specifications changed during the decision")
	}
	c.Verdict, c.Reasons = string(decision.Verdict), slices.Clone(decision.Reasons)
	c.Review = string(input.Review)
	var prompt string
	err = a.engineering.Update(ctx, scope, func(st *engineering.State) error {
		if st.Revision != c.Revision || st.Generation != c.Generation {
			return fmt.Errorf("execution state changed during the decision")
		}
		c.Revision = st.Revision + 1
		st.Completion = &c
		if continueWork && decision.Verdict == incompleteVerdict && input.Review != reviewUnavailable && input.Review != reviewInvalidReport {
			prompt = reserveUnifiedContinuation(st, input, decision)
		}
		if decision.Complete() && st.Work != nil && st.Work.Phase != "waiting" {
			st.Work.MarkDelivered(st.Generation, time.Now())
			st.Work.Phase = "deliver"
		}
		return nil
	})
	return prompt, err
}

// reserveUnifiedContinuation shares the durable retry budget with the task
// continuation path. Unknown effects and unavailable reviews never reach it.
func reserveUnifiedContinuation(st *engineering.State, input unifiedInput, decision unifiedDecision) string {
	progress, _ := json.Marshal([]any{st.Generation, input.Requirements, input.ContractGaps, input.OpenFindings, input.Review})
	mark := engineering.Hash(string(progress))
	c := &st.Outcomes.Continuation
	if c.Count >= maxSharedContinuations {
		c.StopReason = "continuation_limit"
		return ""
	}
	if c.ProgressSignature == mark {
		c.NoProgress++
	} else {
		c.NoProgress = 0
	}
	c.ProgressSignature = mark
	if c.NoProgress >= maxSharedNoProgressContinuation {
		c.StopReason = "no_progress"
		return ""
	}
	c.Count++
	c.StopReason = ""
	return sharedContinuationMarker + " The runtime completion decision is incomplete: " + strings.Join(decision.Reasons, "; ") + ". Required checks: " + strings.Join(decision.Missing, "; ") + ". Continue the authorized work, address the open findings and run the missing requirement-specific checks. Do not mark the task complete until the runtime evidence is current. Do not replay uncertain effects."
}
