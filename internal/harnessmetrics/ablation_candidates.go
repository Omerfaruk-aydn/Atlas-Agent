package harnessmetrics

import (
	"fmt"
	"sort"
	"strings"
)

// MinAblationSample is how many times a gate must have triggered before its
// record says anything about whether it is worth keeping.
const MinAblationSample = 20

// AblationCandidate is a gate that has run often enough to judge and did not
// change an outcome.
type AblationCandidate struct {
	Name   string
	Reason string
}

// AblationCandidates lists the gates that have not earned their cost: they
// triggered at least MinAblationSample times and never sent a turn back,
// failed, or left it unverified, so removing them would have changed nothing
// that was observed. It is a prompt to run the gate-ablation comparison
// (disable it with harness.disabled_gates and compare outcomes), not a
// verdict: a gate that only ever passes may be what keeps the rate at zero.
func AblationCandidates(gates []GateSummary) []AblationCandidate {
	var out []AblationCandidate
	for _, g := range gates {
		if g.Triggered < MinAblationSample {
			continue
		}
		if g.SentBack > 0 || g.Failed > 0 || g.Unverified > 0 {
			continue
		}
		out = append(out, AblationCandidate{
			Name: g.Name,
			Reason: fmt.Sprintf("ran %d times, never changed an outcome; cost %d ms, %d tokens, $%.4f",
				g.Triggered, g.DurationMS, g.Tokens, g.Cost),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// FormatAblation renders the candidates, or "" when there are none.
func FormatAblation(gates []GateSummary) string {
	c := AblationCandidates(gates)
	if len(c) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\nGates that have not yet earned their cost (candidates to ablate with harness.disabled_gates and compare):\n")
	for _, x := range c {
		fmt.Fprintf(&b, "  %s: %s\n", x.Name, x.Reason)
	}
	return b.String()
}
