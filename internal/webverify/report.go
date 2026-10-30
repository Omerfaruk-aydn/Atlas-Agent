package webverify

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// keepRuns is how many evidence directories are kept under .atlas/verify.
const keepRuns = 5

// Blocking lists the findings that must be fixed before the work is done.
func (r *Report) Blocking(threshold Severity) []Finding {
	var out []Finding
	for _, f := range r.Findings {
		if f.Blocks(threshold) {
			out = append(out, f)
		}
	}
	return out
}

// Summary is a one-line outcome.
func (r *Report) Summary(threshold Severity) string {
	blocking := len(r.Blocking(threshold))
	flowsPassed := 0
	for _, f := range r.Flows {
		if f.Passed {
			flowsPassed++
		}
	}
	state := "passed"
	if blocking > 0 {
		state = fmt.Sprintf("found %d blocking problem(s)", blocking)
	}
	return fmt.Sprintf("Product verification %s at %s: %d viewport(s), %d page(s), %d/%d flow(s) passed, %d finding(s) in total, accessibility by %s.",
		state, r.URL, len(r.Viewports), len(r.Pages), flowsPassed, len(r.Flows), len(r.Findings), r.A11yEngine)
}

func describeEvidence(e Evidence) string {
	var parts []string
	if e.Screenshot != "" {
		parts = append(parts, "screenshot "+e.Screenshot)
	}
	if e.Network != nil {
		n := e.Network
		desc := "request"
		if n.URL != "" {
			desc += " " + n.URL
		}
		if n.Failed {
			desc += " failed"
		} else if n.Status != 0 {
			desc += fmt.Sprintf(" status %d", n.Status)
		}
		if n.RequestID != "" {
			desc += " (id " + n.RequestID + ")"
		}
		parts = append(parts, desc)
	}
	if e.Console != "" {
		parts = append(parts, "console `"+oneLine(clip(e.Console, 200))+"`")
	}
	if e.DOM != "" {
		parts = append(parts, "DOM `"+oneLine(clip(e.DOM, 240))+"`")
	}
	if e.Measure != "" {
		parts = append(parts, "measurement "+clip(e.Measure, 240))
	}
	if e.Error != "" {
		parts = append(parts, "error "+oneLine(clip(e.Error, 200)))
	}
	return strings.Join(parts, "; ")
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// FixPrompt is the instruction that sends blocking findings back to the
// agent as fix tasks. It returns an empty string when nothing blocks.
func (r *Report) FixPrompt(threshold Severity, round, limit int) string {
	blocking := r.Blocking(threshold)
	if len(blocking) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Product verification opened the running app at %s in the browser (%s) and found %d problem(s) that block completion (check %d of %d). Each carries evidence:\n\n",
		r.URL, strings.Join(r.Viewports, ", "), len(blocking), round, limit)
	shown := blocking[:min(len(blocking), 10)]
	for i, f := range shown {
		where := f.Page
		if len(f.Viewports) > 0 {
			where += " @ " + strings.Join(f.Viewports, ", ")
		}
		fmt.Fprintf(&b, "%d. [%s/%s] %s (%s)\n", i+1, f.Severity, f.Kind, f.Title, strings.TrimSpace(where))
		if f.Detail != "" && f.Detail != f.Title {
			fmt.Fprintf(&b, "   %s\n", oneLine(f.Detail))
		}
		fmt.Fprintf(&b, "   evidence: %s\n", describeEvidence(f.Evidence))
	}
	if len(blocking) > len(shown) {
		fmt.Fprintf(&b, "... and %d more in the full report.\n", len(blocking)-len(shown))
	}
	if r.Dir != "" {
		fmt.Fprintf(&b, "\nFull report with screenshots: %s/report.md\n", r.Dir)
	}
	b.WriteString("\nFix each finding that holds and make sure the dev server serves the fix, then give your final report again. " +
		"If a finding is wrong, show concrete evidence (the screenshot, DOM or request) instead of changing code.")
	return b.String()
}

// Markdown renders the evidence report.
func (r *Report) Markdown(threshold Severity) string {
	var b strings.Builder
	b.WriteString("# Product verification\n\n")
	fmt.Fprintf(&b, "- Address: %s\n- Run: %s (%d ms)\n- Pages: %s\n- Viewports: %s\n- Accessibility engine: %s\n- Blocks at: %s and above (evidenced, non-advisory findings only)\n\n",
		r.URL, r.RunID, r.DurationMS, strings.Join(r.Pages, ", "), strings.Join(r.Viewports, ", "), r.A11yEngine, threshold)
	fmt.Fprintf(&b, "%s\n\n", r.Summary(threshold))
	if len(r.Flows) > 0 {
		b.WriteString("## Flows\n\n")
		for _, f := range r.Flows {
			status := "passed"
			if !f.Passed {
				status = fmt.Sprintf("FAILED at step %d: %s", f.FailedStep, oneLine(f.Detail))
			}
			soft := ""
			if f.Soft {
				soft = " (derived from the request, advisory)"
			}
			fmt.Fprintf(&b, "- %s%s: %s\n", f.Name, soft, status)
		}
		b.WriteString("\n")
	}
	b.WriteString("## Findings\n\n")
	if len(r.Findings) == 0 {
		b.WriteString("None.\n")
	}
	for i, f := range r.Findings {
		status := "blocking"
		switch {
		case f.Advisory:
			status = "advisory"
		case !f.Evidenced():
			status = "no evidence, not blocking"
		case !f.Blocks(threshold):
			status = "below threshold"
		}
		fmt.Fprintf(&b, "### %d. %s\n\n- Severity: %s (%s, %s)\n- Where: %s %s\n", i+1, f.Title, f.Severity, f.Kind, status, f.Page, strings.Join(f.Viewports, ", "))
		if f.Detail != "" {
			fmt.Fprintf(&b, "- Detail: %s\n", oneLine(f.Detail))
		}
		if f.Evidence.Screenshot != "" {
			fmt.Fprintf(&b, "- Screenshot: `%s` (relative to the project root)\n", f.Evidence.Screenshot)
		}
		if d := describeEvidence(Evidence{DOM: f.Evidence.DOM, Console: f.Evidence.Console, Network: f.Evidence.Network, Measure: f.Evidence.Measure, Error: f.Evidence.Error}); d != "" {
			fmt.Fprintf(&b, "- Evidence: %s\n", d)
		}
		b.WriteString("\n")
	}
	if len(r.Skipped) > 0 {
		b.WriteString("## Not checked\n\n")
		for _, s := range r.Skipped {
			fmt.Fprintf(&b, "- %s\n", s)
		}
	}
	return b.String()
}

// Save writes report.json and report.md next to the screenshots and prunes
// old runs. It does nothing without a project root.
func (r *Report) Save(root string, threshold Severity) error {
	if root == "" || r.Dir == "" {
		return nil
	}
	dir := filepath.Join(root, filepath.FromSlash(r.Dir))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "report.json"), data, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "report.md"), []byte(r.Markdown(threshold)), 0o644); err != nil {
		return err
	}
	// latest.md lets the final report and the user find the newest run
	// without knowing its id.
	latest := filepath.Join(root, ".atlas", "verify", "latest.md")
	pointer := fmt.Sprintf("Latest run: %s\n\n%s", path.Join(r.Dir, "report.md"), r.Summary(threshold))
	if err := os.WriteFile(latest, []byte(pointer+"\n"), 0o644); err != nil {
		return err
	}
	return pruneRuns(filepath.Join(root, ".atlas", "verify"), keepRuns)
}

// pruneRuns removes all but the newest keep run directories. Run ids are
// timestamps, so name order is age order.
func pruneRuns(dir string, keep int) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var runs []string
	for _, e := range entries {
		if e.IsDir() {
			runs = append(runs, e.Name())
		}
	}
	slices.Sort(runs)
	for len(runs) > keep {
		if err := os.RemoveAll(filepath.Join(dir, runs[0])); err != nil {
			return err
		}
		runs = runs[1:]
	}
	return nil
}
