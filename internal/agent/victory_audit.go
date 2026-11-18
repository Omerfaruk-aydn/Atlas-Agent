package agent

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// maxVictoryReruns bounds how many claimed-green commands one audit runs.
const maxVictoryReruns = 3

// verificationCommandRe picks commands whose pass is a claim worth
// re-checking: tests, builds, type checks and linters.
var verificationCommandRe = regexp.MustCompile(`(?i)\b(test|pytest|vitest|jest|build|vet|lint|tsc|typecheck|check|clippy|mypy)\b`)

// victoryCandidates returns the verification commands the agent ran and saw
// pass since its last edit, newest first, at most maxVictoryReruns.
func victoryCandidates(l *evaluatorLedger) []evaluatorCommand {
	var passed []evaluatorCommand
	for _, run := range l.runs {
		if run.failed || run.seq < l.lastEdit || !verificationCommandRe.MatchString(run.command) {
			continue
		}
		passed = append(passed, run)
	}
	sort.Slice(passed, func(i, j int) bool { return passed[i].seq > passed[j].seq })
	if len(passed) > maxVictoryReruns {
		passed = passed[:maxVictoryReruns]
	}
	return passed
}

// victoryAudit re-runs the verification commands the agent reported as
// passing, through the same permission-aware path as configured checks, and
// sends the turn back if a fresh run fails. A pass that was real stays a
// pass; one that depended on stale state, a cache or a misread result does
// not survive. It is off unless harness.victory_audit is set.
func (a *sessionAgent) victoryAudit(ctx context.Context, id string, l *evaluatorLedger) (evaluatorDecision, bool) {
	opts := a.harnessOptions()
	if opts == nil || !opts.VictoryAudit || !opts.GateEnabled("victory_audit") {
		return evaluatorDecision{}, false
	}
	for _, run := range victoryCandidates(l) {
		checkCtx, cancel := context.WithTimeout(ctx, opts.CompletionTimeout())
		resp, err := a.invokeCheck(checkCtx, id, a.ledgerRoot(l), run.command)
		cancel()
		exit, finished := bashExitCode(resp.Metadata)
		if err != nil || resp.StopTurn || !finished {
			continue // The audit cannot prove a disagreement; leave the claim.
		}
		if exit != 0 || resp.IsError {
			l.seq++
			l.remember(evaluatorCommand{command: run.command, failed: true, seq: l.seq})
			return a.qualitySendBack(l, fmt.Sprintf("You reported `%s` as passing, but a fresh run just failed:\n%s", run.command, strings.TrimSpace(evaluatorTruncate(resp.Content)))), true
		}
	}
	return evaluatorDecision{}, false
}
