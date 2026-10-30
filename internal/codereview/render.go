package codereview

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// WriteJSON writes the report as indented JSON.
func WriteJSON(w io.Writer, r *Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if r.Findings == nil {
		r.Findings = []Finding{}
	}
	return enc.Encode(r)
}

// WriteText writes the report for a terminal reader.
func WriteText(w io.Writer, r *Report) {
	fmt.Fprintf(w, "Code review: %s (effort %s)\n\n", r.Target, r.Effort)
	if r.Summary != "" {
		fmt.Fprintf(w, "%s\n\n", r.Summary)
	}
	if r.Truncated {
		fmt.Fprintln(w, "Note: the patch was truncated for size; files beyond the limit were only reachable through the reviewers' tools.")
		fmt.Fprintln(w)
	}
	if len(r.Findings) == 0 {
		fmt.Fprintln(w, "No findings.")
	}
	for _, f := range r.Findings {
		check := "unverified"
		if f.Verified {
			check = "verified"
		}
		fmt.Fprintf(w, "%s [%s] %s:%d  %s\n", f.ID, strings.ToUpper(string(f.Severity)), f.Path, f.Line, f.Title)
		fmt.Fprintf(w, "    Scenario:   %s\n", f.Scenario)
		if f.Suggestion != "" {
			fmt.Fprintf(w, "    Suggestion: %s\n", f.Suggestion)
		}
		fmt.Fprintf(w, "    Confidence: %.2f (%s, %d pass(es))\n\n", f.Confidence, check, f.Passes)
	}
	counts := map[Severity]int{}
	for _, f := range r.Findings {
		counts[f.Severity]++
	}
	fmt.Fprintf(w, "%d finding(s): %d critical, %d high, %d medium, %d low, %d nit. ", len(r.Findings),
		counts[SeverityCritical], counts[SeverityHigh], counts[SeverityMedium], counts[SeverityLow], counts[SeverityNit])
	fmt.Fprintf(w, "%d dropped; %d review pass(es), %d verification request(s).\n", len(r.Dropped), r.Passes, r.VerifyCalls)
	for _, warning := range r.Warnings {
		fmt.Fprintf(w, "Warning: %s\n", warning)
	}
}
