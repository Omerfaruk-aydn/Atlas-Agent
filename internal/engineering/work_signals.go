package engineering

import (
	"fmt"
	"strings"
)

// The prompt sets the starting tier (ClassifyBudget). What the task has done
// since says more about its risk than its wording: how much source it has
// changed, whether earlier proof went stale, whether effects reached outside
// the tree, and whether an outcome is unknown. These signals only raise the
// tier; a simple task stays on the short path, and nothing here lowers one.

const (
	// signalManyChanges is the number of source changes after which even a
	// small request is no longer a one-step job.
	signalManyChanges = 6
	// signalWideChanges is the number after which independent verification
	// is warranted regardless of how the task was phrased.
	signalWideChanges = 20
)

// RaiseForRuntimeSignals returns the budget raised by what the session has
// observed. The reasons name each signal so the change is explainable.
func RaiseForRuntimeSignals(b WorkBudget, w *WorkState, st State) WorkBudget {
	if b.Override != "" || w == nil {
		return b
	}
	var toStandard, toDeep []string

	stale, unverified := 0, 0
	for _, item := range w.Items {
		if item.Origin != OriginUser || item.Status == ItemSuperseded || item.Status == ItemDeclined {
			continue
		}
		switch status := EffectiveStatus(item, st.Generation); {
		case strings.HasSuffix(status, "_stale"):
			stale++
		case status == ItemOpen || status == ItemImplemented:
			unverified++
		}
	}
	external := 0
	for _, op := range st.Operations {
		if op.Status == "completed" && (strings.HasPrefix(op.Tool, "mcp_") || op.Tool == "browser" || op.Tool == "computer" || op.Tool == "remote_support") {
			external++
		}
	}

	switch {
	case len(st.Outcomes.Unresolved()) > 0:
		toDeep = append(toDeep, fmt.Sprintf("%d mutation(s) with an unobserved outcome", len(st.Outcomes.Unresolved())))
	case stale >= 2:
		toDeep = append(toDeep, fmt.Sprintf("%d requirements whose proof went stale after later changes", stale))
	case st.Generation >= signalWideChanges:
		toDeep = append(toDeep, fmt.Sprintf("%d source changes", st.Generation))
	}
	if st.Generation >= signalManyChanges && unverified > 0 {
		toStandard = append(toStandard, fmt.Sprintf("%d source changes with %d requirement(s) still unverified", st.Generation, unverified))
	}
	if external > 0 && st.Generation > 0 {
		toStandard = append(toStandard, "effects outside the source tree were produced")
	}
	for _, q := range w.Questions {
		if q.Status == "open" && len(q.Blocks) > 0 {
			toStandard = append(toStandard, "an open question blocks a requirement")
			break
		}
	}

	raised := func(to string, reasons []string) {
		if rank(b.Tier) < rank(to) {
			b.Tier = to
		}
		b.Reasons = append(b.Reasons, reasons...)
	}
	if len(toStandard) > 0 && rank(b.Tier) < rank(TierStandard) {
		raised(TierStandard, toStandard)
	}
	if len(toDeep) > 0 {
		raised(TierDeep, toDeep)
	}
	return b
}
