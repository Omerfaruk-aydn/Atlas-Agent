package model

import (
	"fmt"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
)

// projectWork renders the task's working memory: what was asked, what the
// agent is doing and why, what it is testing, and what it waits for. Every
// row is a recorded fact; nothing here is estimated progress.
func projectWork(w *engineering.WorkView, text func(string) string) []workflowRow {
	if w == nil {
		return []workflowRow{{ID: "work-empty", Label: text("No task working memory recorded yet")}}
	}
	status := []string{
		text("Reasons:") + " " + strings.Join(w.Budget.Reasons, "; "),
		fmt.Sprintf(text("Source generation: %d"), w.Generation),
	}
	if w.LastTurn != nil {
		status = append(status, fmt.Sprintf(text("Last user turn %d (%s): %s"), w.LastTurn.Seq, w.LastTurn.Kind, w.LastTurn.Text))
	}
	if len(w.NextChecks) > 0 {
		status = append(status, text("Next checks:")+" "+strings.Join(w.NextChecks, "; "))
	}
	if w.Recovered != nil {
		status = append(status, text("State recovered from a damaged record:")+" "+w.Recovered.QuarantinedPath)
	}
	phase := w.Phase
	if phase == "" {
		phase = "-"
	}
	rows := []workflowRow{{ID: "work-status", Label: fmt.Sprintf(text("Budget: %s · phase: %s"), w.Budget.Tier, phase), Details: status}}
	if w.Waiting != "" {
		rows = append(rows, workflowRow{ID: "work-waiting", Label: text("Waiting:") + " " + w.Waiting})
	}
	items := func(kind string, list []engineering.WorkItemView) {
		for _, item := range list {
			details := []string{item.Text}
			if len(item.Turns) > 0 {
				details = append(details, fmt.Sprintf(text("User turns: %v"), item.Turns))
			}
			rows = append(rows, workflowRow{ID: "work-" + item.ID, Label: fmt.Sprintf("%s · %s · %s · %s", item.ID, text(kind), item.Status, item.Text), Details: details})
		}
	}
	items("requested", w.Requested)
	items("proposed", w.Proposed)
	items("assumption", w.Assumptions)
	if w.Method != nil {
		rows = append(rows, workflowRow{ID: "work-method", Label: fmt.Sprintf(text("Method: %s"), w.Method.Method), Details: []string{w.Method.Reason, text("Evidence: ") + strings.Join(w.Method.Evidence, ", "), text("Trigger:") + " " + w.Method.Trigger}})
	}
	for _, h := range w.Hypotheses {
		label := fmt.Sprintf(text("Hypothesis %s · %s"), h.ID, h.Status)
		if h.Limited {
			label += " · " + text("limited fixture")
		}
		rows = append(rows, workflowRow{ID: "work-" + h.ID, Label: label, Details: []string{h.Statement}})
	}
	for _, e := range w.Experiments {
		details := []string{e.Question, text("Refuted if:") + " " + e.Prediction}
		if e.Limits != "" {
			details = append(details, text("Limits:")+" "+e.Limits)
		}
		rows = append(rows, workflowRow{ID: "work-" + e.ID, Label: fmt.Sprintf(text("Experiment %s · %s · running"), e.ID, e.HypothesisID), Details: details})
	}
	total := 0
	if w.Suite != nil {
		total = len(w.Suite.Checks)
	}
	for _, c := range w.Candidates {
		isolation := text("shared checkout")
		if c.Isolated {
			isolation = text("isolated")
		}
		rows = append(rows, workflowRow{ID: "work-" + c.ID, Label: fmt.Sprintf(text("Candidate %s · %s · %d/%d checks"), c.ID, c.Status, c.Passed, total), Details: []string{c.Approach, c.Axis + " · " + isolation}})
	}
	if w.Suite != nil {
		details := append([]string{w.Suite.Rationale}, w.Suite.Revisions...)
		rows = append(rows, workflowRow{ID: "work-suite", Label: text("Discriminating checks:") + " " + strings.Join(w.Suite.Checks, ", "), Details: details})
	}
	if w.Selection != "" {
		rows = append(rows, workflowRow{ID: "work-selection", Label: text("Selection: ") + w.Selection})
	}
	for _, q := range w.Questions {
		rows = append(rows, workflowRow{ID: "work-" + q.ID, Label: fmt.Sprintf(text("Open question %s"), q.ID), Details: []string{q.Text, text("Blocks:") + " " + strings.Join(q.Blocks, ", ")}})
	}
	for i, a := range w.Attempts {
		rows = append(rows, workflowRow{ID: fmt.Sprintf("work-attempt-%d", i), Label: text("Failed attempt:") + " " + a.What, Details: []string{a.ErrorClass, a.Lesson, text("Source: ") + a.Source, text("Evidence: ") + a.Evidence}})
	}
	return rows
}
