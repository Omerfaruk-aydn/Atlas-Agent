package agent

import (
	"context"
	"log/slog"
	"slices"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workplan"
)

func (l *evaluatorLedger) observeRedGreen(run evaluatorCommand) {
	if workplan.IsScratchDir(run.dir) {
		return
	}
	if l.redRuns == nil {
		l.redRuns = map[string]evaluatorCommand{}
	}
	key := runKey(run)
	if run.failed {
		if len(l.redRuns) >= 64 {
			oldKey := ""
			oldSeq := run.seq
			for key, old := range l.redRuns {
				if old.seq < oldSeq {
					oldKey = key
					oldSeq = old.seq
				}
			}
			delete(l.redRuns, oldKey)
		}
		if _, exists := l.redRuns[key]; !exists {
			l.redRuns[key] = run
		}
		return
	}
	red, ok := l.redRuns[key]
	if !ok || red.seq >= l.lastEdit || l.lastEdit >= run.seq {
		return
	}
	l.redGreen = append(l.redGreen, workplan.RedEvidence{Command: run.command, Directory: run.dir, RedSequence: red.seq, EditSequence: l.lastEdit, GreenSequence: run.seq})
	l.redGreen = l.redGreen[max(0, len(l.redGreen)-64):]
	delete(l.redRuns, key)
}

func (a *sessionAgent) checkRedGreen(ctx context.Context, sessionID string, l *evaluatorLedger) (evaluatorDecision, bool) {
	_, gate := startGate(ctx, "red_green")
	plan, _, err := workplan.Load(a.ledgerRoot(l))
	if err != nil {
		gate.finish("unverified", "requirement_contract_unavailable")
		return evaluatorDecision{Unverified: "Red-to-green evidence could not be bound to the work contract"}, true
	}
	// Load returns a nil plan when the session has no work contract.
	if plan == nil {
		plan = &workplan.Plan{}
	}
	for i := range l.redGreen {
		l.redGreen[i].RequirementIDs = nil
		for _, feature := range plan.Features {
			if feature.Verify != "" && workplan.Satisfies(l.redGreen[i].Command, feature.Verify) {
				l.redGreen[i].RequirementIDs = append(l.redGreen[i].RequirementIDs, feature.ID)
			}
		}
	}
	if len(l.redGreen) > 0 {
		if err := workplan.UpdateState(a.ledgerRoot(l), sessionID, func(s *workplan.State) error { s.RedEvidence = slices.Clone(l.redGreen); return nil }); err != nil {
			slog.Warn("Failed to save red-to-green evidence", "session_id", sessionID, "error", err)
			gate.finish("unverified", "evidence_write_failed")
			return evaluatorDecision{Unverified: "Red-to-green evidence could not be persisted"}, true
		}
	}
	if !a.harnessOptions().RedGreenEnabled() || !l.changed {
		gate.finish("skipped", "not_required")
		return evaluatorDecision{}, false
	}
	for _, pair := range l.redGreen {
		if len(plan.Features) > 0 && len(pair.RequirementIDs) == 0 {
			continue
		}
		if pair.GreenSequence > l.lastEdit {
			if opts := a.harnessOptions(); opts == nil || len(opts.CompletionCommands) == 0 {
				gate.finish("unverified", "regression_contract_missing")
				return evaluatorDecision{Unverified: "Red-to-green evidence exists, but no completion_commands regression check is configured. Configure an affected package/integration regression check; a focused reproduction alone cannot certify completion."}, true
			}
			gate.finish("pass", "same_check_red_then_green")
			return evaluatorDecision{}, false
		}
	}
	gate.finish("unverified", "missing_red_evidence")
	return evaluatorDecision{Unverified: "Red-to-green enforcement requires a completed failing check, an implementation change, then the same check passing and current regression checks. Record a real reproduction; do not invent red evidence."}, true
}
