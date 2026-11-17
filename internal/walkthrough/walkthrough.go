// Package walkthrough builds the end-of-task report from recorded facts: the
// approved plan, the files that actually changed, the checks that passed and
// the red-to-green evidence. It contains no model prose, so what it says can
// be trusted to match what happened.
package walkthrough

import (
	"fmt"
	"sort"
	"strings"
)

// Check is one verification command.
type Check struct {
	Command string
	// Passed is true when the last run exited 0.
	Passed bool
}

// RedGreen is a check that failed before the change and passed after it.
type RedGreen struct {
	Command string
}

// Input is everything the report is built from.
type Input struct {
	Goal string
	// PlanDigest and PlannedPaths come from the approved plan; both may be
	// empty when the work had no structured plan.
	PlanDigest   string
	PlannedPaths []string
	// ChangedPaths are the files the working tree actually differs in.
	ChangedPaths []string
	Checks       []Check
	RedGreen     []RedGreen
	// OpenIssues are problems recorded and not yet closed with evidence.
	OpenIssues  []string
	Screenshots []string
}

// Deviations compares the plan with reality: files changed that the plan
// did not name, and planned files that were never touched.
func Deviations(planned, changed []string) (unplanned, untouched []string) {
	p := set(planned)
	c := set(changed)
	for _, f := range changed {
		if !p[clean(f)] {
			unplanned = append(unplanned, f)
		}
	}
	for _, f := range planned {
		if !c[clean(f)] {
			untouched = append(untouched, f)
		}
	}
	sort.Strings(unplanned)
	sort.Strings(untouched)
	return unplanned, untouched
}

func clean(p string) string { return strings.ReplaceAll(strings.TrimPrefix(p, "./"), `\`, "/") }

func set(items []string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, i := range items {
		m[clean(i)] = true
	}
	return m
}

// Render writes the walkthrough as markdown.
func Render(in Input) string {
	var b strings.Builder
	b.WriteString("# Walkthrough\n")
	if g := strings.TrimSpace(in.Goal); g != "" {
		fmt.Fprintf(&b, "\n**Goal:** %s\n", g)
	}

	b.WriteString("\n## Changes\n")
	if len(in.ChangedPaths) == 0 {
		b.WriteString("\nNo files changed.\n")
	} else {
		b.WriteString("\n")
		changed := append([]string(nil), in.ChangedPaths...)
		sort.Strings(changed)
		for _, f := range changed {
			fmt.Fprintf(&b, "- `%s`\n", clean(f))
		}
	}

	if len(in.PlannedPaths) > 0 {
		unplanned, untouched := Deviations(in.PlannedPaths, in.ChangedPaths)
		b.WriteString("\n## Plan vs. what happened")
		if in.PlanDigest != "" {
			fmt.Fprintf(&b, " (plan %s)", in.PlanDigest)
		}
		b.WriteString("\n")
		if len(unplanned) == 0 && len(untouched) == 0 {
			b.WriteString("\nThe changes match the approved plan.\n")
		}
		if len(unplanned) > 0 {
			b.WriteString("\nChanged but not in the plan:\n")
			for _, f := range unplanned {
				fmt.Fprintf(&b, "- `%s`\n", clean(f))
			}
		}
		if len(untouched) > 0 {
			b.WriteString("\nIn the plan but not changed:\n")
			for _, f := range untouched {
				fmt.Fprintf(&b, "- `%s`\n", clean(f))
			}
		}
	}

	b.WriteString("\n## Verification\n")
	if len(in.Checks) == 0 {
		b.WriteString("\nNo checks were recorded. Nothing here was verified by the harness.\n")
	} else {
		b.WriteString("\n")
		for _, c := range in.Checks {
			mark := "FAIL"
			if c.Passed {
				mark = "pass"
			}
			fmt.Fprintf(&b, "- %s `%s`\n", mark, c.Command)
		}
	}
	if len(in.RedGreen) > 0 {
		b.WriteString("\nFailed before the change and passed after it:\n")
		for _, r := range in.RedGreen {
			fmt.Fprintf(&b, "- `%s`\n", r.Command)
		}
	}

	if len(in.OpenIssues) > 0 {
		b.WriteString("\n## Open issues\n\n")
		for _, i := range in.OpenIssues {
			fmt.Fprintf(&b, "- %s\n", i)
		}
	}
	if len(in.Screenshots) > 0 {
		b.WriteString("\n## Screenshots\n\n")
		for _, s := range in.Screenshots {
			fmt.Fprintf(&b, "- %s\n", s)
		}
	}
	return b.String()
}
