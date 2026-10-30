package agent

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
)

func (a *sessionAgent) observeStrategy(ctx context.Context, m *strategyMonitor, id string, steps []fantasy.StepResult) bool {
	l := a.evaluatorLedgerFor(id)
	l.mu.Lock()
	if !l.changed {
		l.mu.Unlock()
		return m.observe(ctx, a.engineering, engineering.GetScope(ctx, id).SessionID, steps)
	}
	passed := []string{}
	for _, r := range l.runs {
		if !r.failed {
			passed = append(passed, runKey(r))
		}
	}
	slices.Sort(passed)
	mark := fmt.Sprintf("%d|%s|%s|%d|%d", len(l.files), strings.Join(passed, "\n"), strings.Join(l.qualityErrors, "\n"), len(l.redGreen), len(l.passingTests))
	l.mu.Unlock()
	if a.engineering != nil {
		if state, err := a.engineering.Read(ctx, engineering.GetScope(ctx, id).SessionID); err == nil && state.Work != nil {
			for _, item := range state.Work.Items {
				mark += fmt.Sprintf("|%s:%s", item.ID, item.Status)
			}
		}
	}
	return m.observe(ctx, a.engineering, engineering.GetScope(ctx, id).SessionID, steps, mark)
}

var namedPassingTest = regexp.MustCompile(`(?m)(?:--- PASS: ([^\s]+)|^PASSED ([^\s]+))`)

func (l *evaluatorLedger) recordPassingTests(output string) {
	if l.passingTests == nil {
		l.passingTests = map[string]bool{}
	}
	for _, match := range namedPassingTest.FindAllStringSubmatch(output[:min(len(output), 64<<10)], 128) {
		if len(l.passingTests) >= 1024 {
			return
		}
		l.passingTests[match[1]+match[2]] = true
	}
}

func (a *sessionAgent) consultStallOracle(ctx context.Context, id, review string) string {
	if !strings.Contains(review, "<stall_oracle_request>") {
		return review
	}
	ctx, gate := startGate(ctx, "stall_oracle")
	opts := a.harnessOptions()
	model := a.escalateModel.Get().v
	if opts == nil || opts.StallOracle == nil || !*opts.StallOracle || model == nil || !opts.GateEnabled("escalator") {
		gate.finish("skipped", "no_opt_in_oracle")
		return review + "\nNo opt-in oracle is available. Do not claim it reviewed this approach."
	}
	advice, err := a.runReviewer(ctx, id, *model, "You are a read-only troubleshooting oracle. Propose one discriminating experiment and a different approach grounded in observed evidence. Do not claim work complete.", review, nil, a.escalateTools.Copy(), 8)
	if err != nil {
		gate.finish("fail_open", "oracle_unavailable")
		slog.Warn("Stall oracle unavailable", "session_id", id, "error", err)
		return review + "\nThe oracle could not run; do not treat it as verification."
	}
	gate.finish("pass", "advice_returned")
	return review + "\nOracle advice (verify before relying on it):\n" + evaluatorTruncate(advice)
}
