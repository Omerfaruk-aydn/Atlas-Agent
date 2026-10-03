package model

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ansiext"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ansi"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
)

const (
	workflowTasks = iota
	workflowGraph
	workflowHandoffs
	workflowOwnership
	workflowOperations
	workflowQueue
	workflowCheckpoints
	workflowAttention
	workflowTabCount
)

var workflowTabs = [workflowTabCount]string{"Tasks", "Graph", "Handoffs", "Ownership", "Operations", "Queue", "Checkpoints", "Attention"}

type workflowRow struct {
	ID, TaskID, Label string
	Details           []string
	Destination       int
}

type workflowViews [workflowTabCount][]workflowRow

// Project the graph off the UI goroutine. Ownership comparisons and evidence
// summaries are reused across redraws, keyboard movement and terminal resize.
func projectWorkflow(s engineering.WorkflowSnapshot) workflowViews {
	var views workflowViews
	completed := map[string]bool{}
	for _, task := range s.Tasks {
		completed[task.ID] = task.Status == "completed"
	}
	attention := func(id, task, label string, destination int) {
		views[workflowAttention] = append(views[workflowAttention], workflowRow{ID: id, TaskID: task, Label: label, Destination: destination})
	}
	if s.Paused {
		attention("paused", "", "Dispatch paused by user; r resumes the session", workflowTasks)
	}
	if err := s.Limits.Check(s.Usage); err != nil {
		attention("budget", "", "Session budget exhausted; b edits limits", workflowTasks)
	}
	for _, task := range s.Tasks {
		details := []string{"Task: " + task.ID, "Assignment: " + task.Content, "Agent: " + task.Agent, "State: " + task.Status, "Verification: " + task.Verification, "Depends on: " + strings.Join(task.DependsOn, ", "), "Owned paths: " + strings.Join(task.OwnedPaths, ", "), "Acceptance criteria:"}
		details = append(details, task.AcceptanceCriteria...)
		var reasons []string
		if slices.Contains(s.Board.HeldTasks, task.ID) {
			reasons = append(reasons, "held by user")
		}
		for _, dep := range task.DependsOn {
			if !completed[dep] {
				reasons = append(reasons, "waiting for "+dep)
			}
		}
		for _, other := range s.Tasks {
			if other.ID != task.ID && other.Status == "in_progress" && session.OwnershipOverlaps(task.OwnedPaths, other.OwnedPaths) {
				reasons = append(reasons, "writer "+other.ID+" owns overlapping paths")
			}
		}
		if task.Status == "pending" && s.Paused {
			reasons = append(reasons, "session dispatch paused")
		}
		if task.Status == "pending" && s.AgentLimit > 0 && len(s.Runners) >= s.AgentLimit {
			reasons = append(reasons, "team at concurrency limit")
		}
		if task.Status == "pending" && task.Agent == "" {
			reasons = append(reasons, "assign a specialist role")
		}
		state := task.Status
		if task.Status == "pending" {
			if len(reasons) > 0 {
				state = "waiting: " + strings.Join(reasons, "; ")
			} else {
				state = "dependency-ready; delivery/contract gates apply"
			}
		}
		for _, runner := range s.Runners {
			if runner.TaskID == task.ID {
				details = append(details, "Live agent: "+runner.Title, "Child session: "+runner.SessionID, "Started: "+time.UnixMilli(runner.StartedAt).Format(time.RFC3339))
			}
		}
		for _, op := range s.Operations {
			if op.TaskID == task.ID && (op.Status == "running" || op.Status == "background") {
				details = append(details, "Current operation: "+op.Tool+" ["+op.Status+"]")
			}
		}
		details = append(details, "Readiness: "+state)
		views[workflowTasks] = append(views[workflowTasks], workflowRow{ID: task.ID, TaskID: task.ID, Label: fmt.Sprintf("%s [%s] %s: %s", task.ID, task.Status, task.Agent, task.Content), Details: details})
		views[workflowGraph] = append(views[workflowGraph], workflowRow{ID: task.ID, TaskID: task.ID, Label: fmt.Sprintf("%s <- [%s] %s", task.ID, strings.Join(task.DependsOn, ", "), state), Details: details})
		views[workflowOwnership] = append(views[workflowOwnership], workflowRow{ID: task.ID, TaskID: task.ID, Label: task.ID + ": " + strings.Join(task.OwnedPaths, ", "), Details: append(slices.Clone(details), "Empty ownership reserves the repository; overlapping writers are serialized.")})
		for _, other := range s.Tasks {
			if other.ID != task.ID && other.Status != "completed" && task.Status != "completed" && session.OwnershipOverlaps(task.OwnedPaths, other.OwnedPaths) {
				index := len(views[workflowOwnership]) - 1
				views[workflowOwnership][index].Details = append(views[workflowOwnership][index].Details, "Overlap with "+other.ID+": dispatch serializes these writers")
			}
		}
		if task.Status == "pending" && len(reasons) > 0 {
			attention(task.ID, task.ID, task.ID+": "+strings.Join(reasons, "; "), workflowGraph)
		}
		if task.Status == "in_progress" && !slices.ContainsFunc(s.Runners, func(r engineering.LiveRunner) bool { return r.TaskID == task.ID }) {
			attention(task.ID, task.ID, task.ID+": no live runner; inspect operations and verification before retry", workflowOperations)
		}
	}
	for _, run := range s.Executions {
		details := []string{"Task: " + run.TaskID, "Implementer: " + run.Agent, "Execution: " + run.ExecutionID, "Source fingerprint: " + run.SourceFingerprint, fmt.Sprintf("Recorded gate passed: %t; current source gates remain authoritative", run.Passed)}
		label := run.TaskID + " [handoff pending]"
		if run.Handoff != nil {
			label = run.TaskID + " [reported " + run.Handoff.Decision + "] " + run.Handoff.Summary
			details = append(details, "Reported summary: "+run.Handoff.Summary, "Changed files: "+strings.Join(run.Handoff.ChangedFiles, ", "), "Reported risks: "+strings.Join(run.Handoff.Risks, "; "))
			for _, check := range run.Handoff.Checks {
				details = append(details, "Reported check: "+check.Command+" | "+check.Evidence)
			}
		}
		if run.Error != "" {
			details = append(details, "Execution error: "+run.Error)
			attention(run.ExecutionID, run.TaskID, run.TaskID+": "+run.Error, workflowHandoffs)
		}
		for _, review := range run.Reviews {
			state := "pending"
			if review.Handoff != nil {
				state = review.Handoff.Decision
			}
			details = append(details, "Independent "+review.Agent+": "+state+" "+review.Error)
		}
		for _, check := range run.MachineChecks {
			details = append(details, fmt.Sprintf("Observed check: %s passed=%t | %s", check.Name, check.Passed, check.Evidence))
		}
		if run.RequireReview && len(run.Reviews) == 0 {
			details = append(details, "Independent review required; not complete")
		}
		views[workflowHandoffs] = append(views[workflowHandoffs], workflowRow{ID: run.ExecutionID, TaskID: run.TaskID, Label: label, Details: details})
	}
	for _, runner := range s.Runners {
		views[workflowTasks] = append(views[workflowTasks], workflowRow{ID: runner.SessionID, TaskID: runner.TaskID, Label: "Live agent: " + runner.Title + " | task=" + runner.TaskID, Details: []string{"Child session: " + runner.SessionID, "Task: " + runner.TaskID, "Started: " + time.UnixMilli(runner.StartedAt).Format(time.RFC3339), "e directs steering to this task; x requests only this task's cancellation", "o opens live jobs; g opens agent history"}})
	}
	for _, op := range s.Operations {
		duration := "exit not observed"
		if op.FinishedAt > 0 {
			duration = fmt.Sprintf("%.2fs", float64(op.FinishedAt-op.StartedAt)/1000)
		}
		details := []string{"Operation: " + op.ID, "Call: " + op.CallID, "Task: " + op.TaskID, "Tool: " + op.Tool, "Status: " + op.Status, "Duration: " + duration, "Background job: " + op.BackgroundID, "Evidence: " + op.EvidenceHash, "o opens jobs and output; g opens agent history"}
		views[workflowOperations] = append(views[workflowOperations], workflowRow{ID: op.ID, TaskID: op.TaskID, Label: op.Tool + " [" + op.Status + "] task=" + op.TaskID + " " + duration, Details: details})
		if op.Status == "failed" || (op.Status == "running" || op.Status == "background") && len(s.Runners) == 0 && !s.Busy {
			attention(op.ID, op.TaskID, op.Tool+" ["+op.Status+"] requires inspection; commands are not automatically replayed", workflowOperations)
		}
	}
	slices.Reverse(views[workflowOperations])
	for _, directive := range s.Board.Directives {
		target := directive.TaskID
		if target == "" {
			target = "coordinator"
		}
		details := []string{"Instruction: " + directive.ID, "Target: " + target, "Mode: " + directive.Mode, "Delivery: " + directive.Status, "Depends on: " + strings.Join(directive.DependsOn, ", "), directive.Text, "Queued means pending delivery; received means recorded in agent history, not finished work."}
		if directive.File != "" {
			details = append(details, fmt.Sprintf("Review location: %s:%d", directive.File, directive.Line))
		}
		views[workflowQueue] = append(views[workflowQueue], workflowRow{ID: directive.ID, TaskID: directive.TaskID, Label: fmt.Sprintf("[%s/%s] %s: %s", directive.Mode, directive.Status, target, directive.Text), Details: details})
		if directive.Status == "claimed" {
			attention(directive.ID, directive.TaskID, "Uncertain instruction delivery; inspect history before explicit retry", workflowQueue)
		}
	}
	for _, cp := range s.Checkpoints {
		details := []string{"Checkpoint: " + cp.ID, "Source: " + cp.SourceFingerprint, "Plan: " + cp.PlanFingerprint, fmt.Sprintf("Stage: %d", cp.Stage), "i inspects current recovery plan; R applies an unchanged reconciled plan"}
		if plan := s.Board.ResumePreview; plan != nil && plan.CheckpointID == cp.ID {
			details = append(details, "Ready tasks: "+strings.Join(plan.ReadyTasks, ", "), "Reverify tasks: "+strings.Join(plan.ReverifyTasks, ", "), "Ambiguous operations: "+strings.Join(plan.AmbiguousOperations, ", "), "Source inspected: "+plan.SourceFingerprint)
		}
		views[workflowCheckpoints] = append(views[workflowCheckpoints], workflowRow{ID: cp.ID, Label: fmt.Sprintf("Stage %d | %s", cp.Stage, cp.ID), Details: details})
	}
	for _, finding := range s.Findings {
		if finding.Status != "verified" && finding.Status != "waived" {
			attention(finding.ID, finding.TaskID, fmt.Sprintf("Finding %s:%d %s [%s]", finding.Path, finding.StartLine, finding.Issue, finding.Status), workflowHandoffs)
		}
	}
	return views
}

func workflowFit(lines []string, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	lines = lines[:min(len(lines), height)]
	for i, line := range lines {
		lines[i] = ansi.Truncate(ansiext.Escape(strings.NewReplacer("\r", " ", "\n", " ", "\t", " ").Replace(line)), width, "")
	}
	return strings.Join(lines, "\n")
}

func (p *workflowPanel) rows() []workflowRow {
	return p.views[p.tab]
}

func (p *workflowPanel) selectedRow() (workflowRow, bool) {
	rows := p.rows()
	if p.selected < 0 || p.selected >= len(rows) {
		return workflowRow{}, false
	}
	return rows[p.selected], true
}

func (p *workflowPanel) renderWorkspace(width, height int) string {
	state := "idle"
	if p.snapshot.Busy {
		state = "working"
	}
	if p.snapshot.Paused {
		state = "dispatch paused"
	}
	teamLimit := "configured"
	if p.snapshot.AgentLimit > 0 {
		teamLimit = fmt.Sprint(p.snapshot.AgentLimit)
	}
	header := []string{"Agent controls | " + workflowTabs[p.tab], "Esc close | s stop | p pause | r resume | 1-8 views | t timeline", "1 Tasks  2 Graph  3 Handoffs  4 Ownership  5 Operations  6 Queue  7 Checkpoints  8 Attention", fmt.Sprintf("%s | agents %d/%s | tokens %d/%d | $%.2f/$%.2f | calls %d/%d", state, len(p.snapshot.Runners), teamLimit, p.snapshot.Usage.Tokens, p.snapshot.Limits.MaxTokens, p.snapshot.Usage.Cost, p.snapshot.Limits.MaxCost, p.snapshot.Usage.ToolCalls, p.snapshot.Limits.MaxToolCalls)}
	if p.err != "" {
		header = append(header, "Error: "+p.err)
	}
	if p.loading && p.snapshot.Revision == "" {
		header = append(header, "Loading workflow…")
	}
	if p.input.mode != "" {
		header = append(header, p.input.label()+" | Enter apply, Esc cancel", p.input.visible(width))
	}
	if len(p.snapshot.Operations) > 0 && height >= 18 {
		op := p.snapshot.Operations[len(p.snapshot.Operations)-1]
		header = append(header, "Latest operation: "+op.Tool+" ["+op.Status+"] task="+op.TaskID+" | 5 details/output")
	}
	rows := p.rows()
	if len(rows) == 0 {
		header = append(header, "No records in this view")
	}
	bodyHeight := max(0, height-len(header)-2)
	left := []string{}
	start := max(0, p.selected-bodyHeight+1)
	for i := start; i < len(rows) && len(left) < bodyHeight; i++ {
		pointer := "  "
		if i == p.selected {
			pointer = "> "
		}
		left = append(left, pointer+rows[i].Label)
	}
	right := []string{"Details | Enter expands | h hold | u unhold | x cancel task", "e steer task | E steer coordinator | n queue | a assign", "d diff | o jobs/output | g agent history | l team limit | b budget"}
	if row, ok := p.selectedRow(); ok {
		right = append(right, row.Details...)
		if p.tab == workflowOperations && p.outputID == row.ID {
			right = append(right, p.output...)
		}
	}
	if p.details {
		pOffset := min(max(0, p.detailOffset), max(0, len(right)-bodyHeight))
		right = right[pOffset:]
	}
	if width >= 110 && !p.details {
		leftWidth := width * 2 / 5
		leftLines := strings.Split(workflowFit(left, leftWidth, bodyHeight), "\n")
		rightLines := strings.Split(workflowFit(right, width-leftWidth-3, bodyHeight), "\n")
		for i := range bodyHeight {
			a, b := "", ""
			if i < len(leftLines) {
				a = leftLines[i]
			}
			if i < len(rightLines) {
				b = rightLines[i]
			}
			header = append(header, a+strings.Repeat(" ", max(0, leftWidth-ansi.StringWidth(a)))+" | "+b)
		}
	} else if p.details {
		header = append(header, right[:min(len(right), bodyHeight)]...)
	} else {
		header = append(header, left...)
	}
	header = append(header, "Arrows select | Enter details | e steer | n queue | h hold | u unhold | x cancel | v replan | w scope | f retry", "Queue: +/- reorder | Delete cancel | y retry uncertain | z remove received | c continue queue")
	return workflowFit(header, width, height)
}
