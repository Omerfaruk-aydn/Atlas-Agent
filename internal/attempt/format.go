package attempt

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Format renders a report as plain text for a terminal or a tool result.
func Format(r *Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Attempt run %s: %d attempt(s), task: %s\n", r.ID, len(r.Attempts), clip(oneLine(r.Task), 120))
	switch r.VerifySource {
	case SourceNone, "":
		b.WriteString("Verify: none available\n")
	default:
		fmt.Fprintf(&b, "Verify (%s): %s\n", r.VerifySource, strings.Join(r.Verify, "; "))
	}
	if len(r.Lint) > 0 {
		fmt.Fprintf(&b, "Lint: %s\n", strings.Join(r.Lint, "; "))
	}
	b.WriteString("\n")

	order := append([]Attempt(nil), r.Attempts...)
	sort.SliceStable(order, func(i, j int) bool {
		a, c := order[i], order[j]
		if (a.Rank > 0) != (c.Rank > 0) {
			return a.Rank > 0
		}
		if a.Rank != c.Rank {
			return a.Rank < c.Rank
		}
		return a.Index < c.Index
	})
	for _, a := range order {
		writeAttempt(&b, r, a)
	}

	if r.Judge.Ran {
		fmt.Fprintf(&b, "Judge%s: %s\n", modelSuffix(r.Judge.Model), r.Judge.Summary)
	} else if r.Judge.Error != "" {
		fmt.Fprintf(&b, "Judge failed: %s\n", r.Judge.Error)
	} else if r.Judge.Skipped != "" {
		fmt.Fprintf(&b, "Judge skipped: %s\n", r.Judge.Skipped)
	}
	if r.TotalCost > 0 {
		fmt.Fprintf(&b, "Total cost: $%.4f\n", r.TotalCost)
	}
	for _, w := range r.Warnings {
		fmt.Fprintf(&b, "Warning: %s\n", w)
	}
	b.WriteString("\n")
	switch {
	case r.Applied != 0:
		fmt.Fprintf(&b, "Applied attempt %s at %s.\n", label(r.Applied-1), r.AppliedAt.Format(time.RFC3339))
	case r.Winner != 0:
		fmt.Fprintf(&b, "Recommended: attempt %s (confidence %s).\n", label(r.Winner-1), r.Confidence)
		for _, reason := range r.ConfidenceReasons {
			fmt.Fprintf(&b, "  - %s\n", reason)
		}
		fmt.Fprintf(&b, "Nothing has been applied. Apply with: atlas-agent attempts apply %s\n", r.ID)
	default:
		b.WriteString("No attempt produced an applicable change. Nothing was applied.\n")
	}
	return b.String()
}

func writeAttempt(b *strings.Builder, r *Report, a Attempt) {
	rank := "-"
	if a.Rank > 0 {
		rank = fmt.Sprintf("#%d", a.Rank)
	}
	marker := ""
	if a.Index == r.Winner {
		marker = " [winner]"
	}
	fmt.Fprintf(b, "%s Attempt %s%s%s - %s", rank, a.Label, modelSuffix(a.Model), marker, a.Status)
	if a.Eligible {
		fmt.Fprintf(b, " - score %.1f (checks %.1f", a.Final, a.Deterministic)
		if a.Judge != nil {
			fmt.Fprintf(b, ", judge %.1f/10", a.Judge.Score)
		}
		b.WriteString(")")
	}
	b.WriteString("\n")
	if a.Error != "" {
		fmt.Fprintf(b, "    error: %s\n", a.Error)
	}
	if a.PatchBytes > 0 {
		fmt.Fprintf(b, "    %d file(s), +%d/-%d, %s\n", len(a.Files), a.Additions, a.Deletions, (time.Duration(a.DurationMS) * time.Millisecond).Round(time.Second))
	}
	for _, c := range a.Checks {
		fmt.Fprintf(b, "    verify %s: %s\n", state(c), c.Command)
		if !c.Passed && c.Output != "" {
			fmt.Fprintf(b, "      %s\n", clip(oneLine(c.Output), 300))
		}
	}
	for _, c := range a.Lint {
		fmt.Fprintf(b, "    lint %s: %s\n", state(c), c.Command)
	}
	if a.Judge != nil {
		if a.Judge.Rationale != "" {
			fmt.Fprintf(b, "    judge: %s\n", a.Judge.Rationale)
		}
		for _, c := range a.Judge.Concerns {
			fmt.Fprintf(b, "    concern: %s\n", c)
		}
	}
	for _, n := range a.Notes {
		fmt.Fprintf(b, "    note: %s\n", n)
	}
	b.WriteString("\n")
}

func state(c CheckResult) string {
	switch {
	case c.Passed:
		return "PASS"
	case c.TimedOut:
		return "TIMEOUT"
	}
	return fmt.Sprintf("FAIL(exit %d)", c.ExitCode)
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
