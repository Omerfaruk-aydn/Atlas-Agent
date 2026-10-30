package securityaudit

import (
	"encoding/json"
	"fmt"
	"strings"
)

// RenderJSON returns the report as indented JSON.
func RenderJSON(r Report) ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

// RenderMarkdown renders the report, ranked by severity. Confirmed and
// unconfirmed findings are in separate sections with different headings
// so a skimming reader cannot mistake a lead for a demonstrated defect.
func RenderMarkdown(r Report) string {
	var b strings.Builder
	b.WriteString("# Security audit\n\n")
	scope := r.Scope
	if scope == "" {
		scope = "whole project"
	}
	fmt.Fprintf(&b, "Scope: %s\n\n", scope)
	fmt.Fprintf(&b, "Summary: %d confirmed, %d fixed, %d fix failed, %d unconfirmed.\n",
		r.Count(StatusConfirmed), r.Count(StatusFixed), r.Count(StatusFixFailed), r.Count(StatusUnconfirmed))
	b.WriteString("\"Confirmed\" means a proof test in this project fails repeatably and names the finding. Anything else is unconfirmed.\n\n")

	section := func(title string, include func(Finding) bool, intro string) {
		var items []Finding
		for _, f := range r.Findings {
			if include(f) {
				items = append(items, f)
			}
		}
		if len(items) == 0 {
			return
		}
		fmt.Fprintf(&b, "## %s (%d)\n\n", title, len(items))
		if intro != "" {
			b.WriteString(intro + "\n\n")
		}
		for _, f := range items {
			writeFinding(&b, f)
		}
	}
	section("Confirmed, open", func(f Finding) bool { return f.Status == StatusConfirmed }, "")
	section("Fix attempted and failed", func(f Finding) bool { return f.Status == StatusFixFailed }, "")
	section("Fixed", func(f Finding) bool { return f.Status == StatusFixed },
		"The proof test now passes and the rest of the suite did not regress. Keep the proof tests as regression tests.")
	section("Unconfirmed (NOT demonstrated)", func(f Finding) bool { return f.Status == StatusUnconfirmed },
		"These have no failing proof test. They are leads to review, not established vulnerabilities.")

	if len(r.Regressions) > 0 {
		b.WriteString("## Regressions after fix\n\n")
		for _, name := range r.Regressions {
			fmt.Fprintf(&b, "- %s\n", name)
		}
		b.WriteString("\n")
	}
	m := r.ThreatModel
	if len(m.Boundaries) > 0 {
		b.WriteString("## Trust boundaries\n\n")
		for _, t := range m.Boundaries {
			fmt.Fprintf(&b, "- %s\n", t)
		}
		b.WriteString("\n")
	}
	if len(r.Notes) > 0 {
		b.WriteString("## Limits of this audit\n\n")
		for _, n := range r.Notes {
			fmt.Fprintf(&b, "- %s\n", n)
		}
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

func writeFinding(b *strings.Builder, f Finding) {
	fmt.Fprintf(b, "### [%s] %s\n\n", f.ID, f.Title)
	loc := f.File
	if loc != "" && f.Line > 0 {
		loc = fmt.Sprintf("%s:%d", f.File, f.Line)
	}
	fmt.Fprintf(b, "- Severity: %s | %s | status: %s | source: %s\n", f.Severity, f.CWE, f.Status, f.Source)
	if loc != "" {
		fmt.Fprintf(b, "- Location: %s\n", loc)
	}
	if f.Description != "" {
		fmt.Fprintf(b, "- Issue: %s\n", f.Description)
	}
	if f.AttackPath != "" {
		fmt.Fprintf(b, "- Attack path: %s\n", f.AttackPath)
	}
	if f.Proof != nil {
		fmt.Fprintf(b, "- Proof: %s in %s\n", f.Proof.Test, f.Proof.File)
	}
	if f.Reason != "" {
		fmt.Fprintf(b, "- Evidence status: %s\n", f.Reason)
	}
	if f.Evidence != "" {
		fmt.Fprintf(b, "- Failure output:\n\n```\n%s\n```\n", f.Evidence)
	}
	if f.Remediation != "" {
		fmt.Fprintf(b, "- Fix: %s\n", f.Remediation)
	}
	b.WriteString("\n")
}
