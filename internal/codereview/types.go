// Package codereview runs a multi-pass, verified code review over a local
// diff, a git range or branch, or a GitHub pull request.
//
// The package does not talk to a model itself. A [Runner] executes one
// reviewer or verifier request and returns its text; the pipeline builds
// the prompts, parses the structured answers, merges duplicate findings,
// drops anything a verification pass refutes, and ranks what is left.
// Keeping the model behind that seam makes the whole pipeline testable
// offline and lets the CLI and the TUI share one implementation.
package codereview

import (
	"fmt"
	"strings"
)

// Effort controls how much work a review does.
type Effort string

// The supported effort levels.
const (
	EffortLow    Effort = "low"
	EffortMedium Effort = "medium"
	EffortHigh   Effort = "high"
	EffortMax    Effort = "max"
)

// ParseEffort reads an effort level. The empty string means medium.
func ParseEffort(s string) (Effort, error) {
	switch e := Effort(strings.ToLower(strings.TrimSpace(s))); e {
	case "":
		return EffortMedium, nil
	case EffortLow, EffortMedium, EffortHigh, EffortMax:
		return e, nil
	default:
		return "", fmt.Errorf("unknown effort %q: use low, medium, high or max", s)
	}
}

// Plan is the amount of work an effort level buys.
type Plan struct {
	// Passes is the number of independent reviewer passes, each with its
	// own focus.
	Passes int
	// Verifiers is the number of independent verification votes per
	// finding. Zero skips verification.
	Verifiers int
}

// PlanFor returns the plan for an effort level.
func PlanFor(e Effort) Plan {
	switch e {
	case EffortLow:
		return Plan{Passes: 1}
	case EffortHigh:
		return Plan{Passes: 3, Verifiers: 1}
	case EffortMax:
		return Plan{Passes: 4, Verifiers: 2}
	default:
		return Plan{Passes: 2, Verifiers: 1}
	}
}

// Severity ranks how bad a finding is.
type Severity string

// The severities, most serious first.
const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
	SeverityLow      Severity = "low"
	SeverityNit      Severity = "nit"
)

// Rank orders severities; a larger number is more serious.
func (s Severity) Rank() int {
	switch s {
	case SeverityCritical:
		return 5
	case SeverityHigh:
		return 4
	case SeverityMedium:
		return 3
	case SeverityLow:
		return 2
	default:
		return 1
	}
}

// normalizeSeverity maps the words models actually use onto the fixed set.
func normalizeSeverity(s string) Severity {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "critical", "blocker", "severe", "p0":
		return SeverityCritical
	case "high", "major", "error", "p1":
		return SeverityHigh
	case "low", "minor", "p3":
		return SeverityLow
	case "nit", "nitpick", "info", "style", "p4":
		return SeverityNit
	default:
		return SeverityMedium
	}
}

// Finding is one reported defect.
type Finding struct {
	ID       string   `json:"id"`
	Path     string   `json:"path"`
	Line     int      `json:"line"`
	EndLine  int      `json:"end_line,omitempty"`
	Severity Severity `json:"severity"`
	Category string   `json:"category,omitempty"`
	Title    string   `json:"title"`
	// Scenario is the concrete input or sequence that makes the defect
	// show up, and what goes wrong.
	Scenario   string  `json:"scenario"`
	Suggestion string  `json:"suggestion,omitempty"`
	Confidence float64 `json:"confidence"`
	// Passes is how many independent reviewer passes reported it.
	Passes int `json:"passes"`
	// Verified is true when a verification pass confirmed the finding.
	Verified   bool   `json:"verified"`
	VerifyNote string `json:"verify_note,omitempty"`
}

// Dropped records a finding that did not make it into the report.
type Dropped struct {
	Path   string `json:"path,omitempty"`
	Line   int    `json:"line,omitempty"`
	Title  string `json:"title"`
	Reason string `json:"reason"`
}

// Report is the outcome of a review.
type Report struct {
	Target   string    `json:"target"`
	Kind     Kind      `json:"kind"`
	Effort   Effort    `json:"effort"`
	Summary  string    `json:"summary"`
	Findings []Finding `json:"findings"`
	Dropped  []Dropped `json:"dropped,omitempty"`
	Warnings []string  `json:"warnings,omitempty"`
	// Passes and VerifyCalls count the model requests made.
	Passes      int  `json:"passes"`
	VerifyCalls int  `json:"verify_calls"`
	Truncated   bool `json:"truncated,omitempty"`
}
