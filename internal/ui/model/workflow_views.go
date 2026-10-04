package model

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ansiext"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ansi"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/interaction"
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
	workflowContext
	workflowBatches
	workflowInteractions
	workflowAutomation
	workflowDurableBoard
	workflowSourceMemory
	workflowTabCount
)

var workflowTabs = [workflowTabCount]string{"Tasks", "Graph", "Handoffs", "Ownership", "Operations", "Queue", "Checkpoints", "Attention", "Context", "Batches", "Interactions", "Automation", "Board", "Memory"}

type workflowRow struct {
	ID, TaskID, Label string
	Details           []string
	Destination       int
}

type workflowViews [workflowTabCount][]workflowRow

// Project the graph off the UI goroutine. Ownership comparisons and evidence
// summaries are reused across redraws, keyboard movement and terminal resize.
func projectWorkflow(s engineering.WorkflowSnapshot, codes ...string) workflowViews {
	text := workflowTranslator(codes...)
	var views workflowViews
	for _, j := range s.PlatformJobs {
		views[workflowAutomation] = append(views[workflowAutomation], workflowRow{ID: j.ID, Label: fmt.Sprintf(text("%s · %s · runs %d/%d · paused %t"), j.ID, j.Kind, j.Runs, j.MaxRuns, j.Paused), Details: []string{text("p: pause; r: resume; x: recover expired attempt"), j.Prompt, fmt.Sprintf(text("Next: %s · status: %s · lease: %d"), time.Unix(j.NextAt, 0).Format(time.RFC3339), workflowStatus(text, j.LastStatus), j.LeaseUntil), j.LastResult}})
	}
	for _, t := range s.PlatformTasks {
		views[workflowDurableBoard] = append(views[workflowDurableBoard], workflowRow{ID: t.ID, Label: fmt.Sprintf(text("%s · %s · worker %s"), t.Title, workflowStatus(text, t.Status), t.Worker), Details: []string{text("a: accept review; r: retry; x: recover expired worker"), t.Prompt, text("Acceptance: ") + strings.Join(t.Acceptance, "; "), text("Evidence: ") + strings.Join(t.Evidence, ", "), fmt.Sprintf(text("Attempt: %s · lease: %d · session: %s"), t.Attempt, t.LeaseUntil, t.SessionID), t.Result}})
	}
	for _, entry := range s.SourceMemories {
		details := []string{entry.Text, text("Origin session: ") + entry.SessionID, fmt.Sprintf(text("Recorded: %d · valid until: %d"), entry.RecordedAt, entry.ValidUntil)}
		for _, source := range entry.Sources {
			details = append(details, source.Path+" · "+source.SHA256)
		}
		views[workflowSourceMemory] = append(views[workflowSourceMemory], workflowRow{ID: entry.ID, Label: entry.ID + " · " + workflowStatus(text, entry.Status), Details: details})
	}
	state := text("ready")
	if s.Interactions.Paused {
		state = text("paused")
	}
	views[workflowInteractions] = append(views[workflowInteractions], workflowRow{ID: "interaction-control", Label: fmt.Sprintf(text("Control: %s · active: %s"), state, s.Interactions.Active), Details: []string{text("p: pause; r: resume; v: capture preview; f: toggle live preview; Enter: view captured frame"), s.Interactions.Reason, text("An in-flight driver operation may finish before control is released. Trace records omit typed text and credentials."), text("Preview: ") + s.Capabilities["interaction_preview"]}})
	for i := len(s.Interactions.History) - 1; i >= 0; i-- {
		entry := s.Interactions.History[i]
		views[workflowInteractions] = append(views[workflowInteractions], workflowRow{ID: fmt.Sprintf("interaction-%d", i), Label: fmt.Sprintf("%s · %s/%s · %s · %dms", entry.Time.Format("15:04:05"), entry.Resource, entry.Action, entry.Status, entry.DurationMS), Details: []string{text("Owner: ") + entry.OwnerID, text("Target: ") + entry.Target, text("URL: ") + entry.URL, text("Before capture: ") + entry.Before, text("After capture: ") + entry.After}})
	}
	if s.Context.Truncated {
		views[workflowContext] = append(views[workflowContext], workflowRow{ID: "context-truncated", Label: text("Context metadata truncated to 2048 entries; estimate covers the entire request")})
	}
	if len(s.Batches) > 0 {
		views[workflowBatches] = append(views[workflowBatches], workflowRow{ID: "batch-help", Label: text("f queues failed-only retry of selected batch; c starts the queue"), Details: []string{text("Succeeded rows are skipped. Running rows require reconciliation before replay."), text("Retry is queued visibly, then executed through the normal agent permission and budget flow.")}})
	}
	for _, batch := range s.Batches {
		for _, row := range batch.Rows {
			views[workflowBatches] = append(views[workflowBatches], workflowRow{ID: batch.ID + "/" + row.ID, Label: fmt.Sprintf(text("%s / %s · %s · attempt %d"), batch.ID, row.ID, workflowStatus(text, row.Status), row.Attempts), Details: []string{text("Batch: ") + batch.ID, text("Item: ") + row.ID, text("Input: ") + row.Input, text("Status: ") + workflowStatus(text, row.Status), text("Output:"), row.Output}})
		}
	}
	views[workflowContext] = append(views[workflowContext], workflowRow{ID: "context-summary", Label: fmt.Sprintf(text("%s · step %d · ~%d tokens"), s.Context.Model, s.Context.Step, s.Context.EstimatedTokens), Details: []string{text("Estimates use serialized text length; image tokens and provider framing are not measured."), text("f: pin a literal project file; x: toggle selected tool output or unpin selected file."), text("Mandatory instructions and user messages cannot be excluded.")}})
	for _, entry := range s.Context.Entries {
		state := text("included")
		if entry.Excluded {
			state = text("excluded")
		}
		if entry.ParentID != "" {
			state = text("included in ") + entry.ParentID
		}
		views[workflowContext] = append(views[workflowContext], workflowRow{ID: entry.ID, Label: fmt.Sprintf(text("[%s] %s · ~%d tokens (%s)"), entry.Kind, entry.Name, entry.EstimatedTokens, state), Details: []string{text("Kind: ") + entry.Kind, text("Name: ") + entry.Name, fmt.Sprintf(text("Serialized bytes: %d"), entry.Bytes), text("Fingerprint: ") + entry.Fingerprint, text("Selection: ") + state}})
	}
	completed := map[string]bool{}
	for _, task := range s.Tasks {
		completed[task.ID] = task.Status == "completed"
	}
	attention := func(id, task, label string, destination int) {
		views[workflowAttention] = append(views[workflowAttention], workflowRow{ID: id, TaskID: task, Label: label, Destination: destination})
	}
	if s.Paused {
		attention("paused", "", text("Dispatch paused by user; r resumes the session"), workflowTasks)
	}
	if err := s.Limits.Check(s.Usage); err != nil {
		attention("budget", "", text("Session budget exhausted; b edits limits"), workflowTasks)
	}
	for _, task := range s.Tasks {
		details := []string{text("Task: ") + task.ID, text("Assignment: ") + task.Content, text("Agent: ") + task.Agent, text("State: ") + workflowStatus(text, task.Status), text("Verification: ") + task.Verification, text("Depends on: ") + strings.Join(task.DependsOn, ", "), text("Owned paths: ") + strings.Join(task.OwnedPaths, ", "), text("Acceptance criteria:")}
		details = append(details, task.AcceptanceCriteria...)
		var reasons []string
		if slices.Contains(s.Board.HeldTasks, task.ID) {
			reasons = append(reasons, text("held by user"))
		}
		for _, dep := range task.DependsOn {
			if !completed[dep] {
				reasons = append(reasons, text("waiting for ")+dep)
			}
		}
		for _, other := range s.Tasks {
			if other.ID != task.ID && other.Status == "in_progress" && session.OwnershipOverlaps(task.OwnedPaths, other.OwnedPaths) {
				reasons = append(reasons, text("writer ")+other.ID+text(" owns overlapping paths"))
			}
		}
		if task.Status == "pending" && s.Paused {
			reasons = append(reasons, text("session dispatch paused"))
		}
		if task.Status == "pending" && s.AgentLimit > 0 && len(s.Runners) >= s.AgentLimit {
			reasons = append(reasons, text("team at concurrency limit"))
		}
		if task.Status == "pending" && task.Agent == "" {
			reasons = append(reasons, text("assign a specialist role"))
		}
		state := workflowStatus(text, task.Status)
		if task.Status == "pending" {
			if len(reasons) > 0 {
				state = text("waiting: ") + strings.Join(reasons, "; ")
			} else {
				state = text("dependency-ready; delivery/contract gates apply")
			}
		}
		for _, runner := range s.Runners {
			if runner.TaskID == task.ID {
				details = append(details, text("Live agent: ")+runner.Title, text("Child session: ")+runner.SessionID, text("Started: ")+time.UnixMilli(runner.StartedAt).Format(time.RFC3339))
			}
		}
		for _, op := range s.Operations {
			if op.TaskID == task.ID && (op.Status == "running" || op.Status == "background") {
				details = append(details, text("Current operation: ")+op.Tool+" ["+workflowStatus(text, op.Status)+"]")
			}
		}
		details = append(details, text("Readiness: ")+state)
		views[workflowTasks] = append(views[workflowTasks], workflowRow{ID: task.ID, TaskID: task.ID, Label: fmt.Sprintf("%s [%s] %s: %s", task.ID, workflowStatus(text, task.Status), task.Agent, task.Content), Details: details})
		views[workflowGraph] = append(views[workflowGraph], workflowRow{ID: task.ID, TaskID: task.ID, Label: fmt.Sprintf("%s <- [%s] %s", task.ID, strings.Join(task.DependsOn, ", "), state), Details: details})
		views[workflowOwnership] = append(views[workflowOwnership], workflowRow{ID: task.ID, TaskID: task.ID, Label: task.ID + ": " + strings.Join(task.OwnedPaths, ", "), Details: append(slices.Clone(details), text("Empty ownership reserves the repository; overlapping writers are serialized."))})
		for _, other := range s.Tasks {
			if other.ID != task.ID && other.Status != "completed" && task.Status != "completed" && session.OwnershipOverlaps(task.OwnedPaths, other.OwnedPaths) {
				index := len(views[workflowOwnership]) - 1
				views[workflowOwnership][index].Details = append(views[workflowOwnership][index].Details, text("Overlap with ")+other.ID+text(": dispatch serializes these writers"))
			}
		}
		if task.Status == "pending" && len(reasons) > 0 {
			attention(task.ID, task.ID, task.ID+": "+strings.Join(reasons, "; "), workflowGraph)
		}
		if task.Status == "in_progress" && !slices.ContainsFunc(s.Runners, func(r engineering.LiveRunner) bool { return r.TaskID == task.ID }) {
			attention(task.ID, task.ID, task.ID+text(": no live runner; inspect operations and verification before retry"), workflowOperations)
		}
	}
	for _, run := range s.Executions {
		details := []string{text("Task: ") + run.TaskID, text("Implementer: ") + run.Agent, text("Execution: ") + run.ExecutionID, text("Source fingerprint: ") + run.SourceFingerprint, fmt.Sprintf(text("Recorded gate passed: %t; current source gates remain authoritative"), run.Passed)}
		label := run.TaskID + text(" [handoff pending]")
		if run.Handoff != nil {
			label = run.TaskID + text(" [reported ") + run.Handoff.Decision + "] " + run.Handoff.Summary
			details = append(details, text("Reported summary: ")+run.Handoff.Summary, text("Changed files: ")+strings.Join(run.Handoff.ChangedFiles, ", "), text("Reported risks: ")+strings.Join(run.Handoff.Risks, "; "))
			for _, check := range run.Handoff.Checks {
				details = append(details, text("Reported check: ")+check.Command+" | "+check.Evidence)
			}
		}
		if run.Error != "" {
			details = append(details, text("Execution error: ")+run.Error)
			attention(run.ExecutionID, run.TaskID, run.TaskID+": "+run.Error, workflowHandoffs)
		}
		for _, review := range run.Reviews {
			state := text("pending")
			if review.Handoff != nil {
				state = review.Handoff.Decision
			}
			details = append(details, text("Independent ")+review.Agent+": "+state+" "+review.Error)
		}
		for _, check := range run.MachineChecks {
			details = append(details, fmt.Sprintf(text("Observed check: %s passed=%t | %s"), check.Name, check.Passed, check.Evidence))
		}
		if run.RequireReview && len(run.Reviews) == 0 {
			details = append(details, text("Independent review required; not complete"))
		}
		views[workflowHandoffs] = append(views[workflowHandoffs], workflowRow{ID: run.ExecutionID, TaskID: run.TaskID, Label: label, Details: details})
	}
	for _, runner := range s.Runners {
		views[workflowTasks] = append(views[workflowTasks], workflowRow{ID: runner.SessionID, TaskID: runner.TaskID, Label: text("Live agent: ") + runner.Title + text(" | task=") + runner.TaskID, Details: []string{text("Child session: ") + runner.SessionID, text("Task: ") + runner.TaskID, text("Started: ") + time.UnixMilli(runner.StartedAt).Format(time.RFC3339), text("e directs steering to this task; x requests only this task's cancellation"), text("o opens live jobs; g opens agent history")}})
	}
	for _, op := range s.Operations {
		duration := text("exit not observed")
		if op.FinishedAt > 0 {
			duration = fmt.Sprintf("%.2fs", float64(op.FinishedAt-op.StartedAt)/1000)
		}
		details := []string{text("Operation: ") + op.ID, text("Call: ") + op.CallID, text("Task: ") + op.TaskID, text("Tool: ") + op.Tool, text("Status: ") + workflowStatus(text, op.Status), text("Duration: ") + duration, text("Background job: ") + op.BackgroundID, text("Evidence: ") + op.EvidenceHash, text("o opens jobs and output; g opens agent history")}
		views[workflowOperations] = append(views[workflowOperations], workflowRow{ID: op.ID, TaskID: op.TaskID, Label: op.Tool + " [" + workflowStatus(text, op.Status) + text("] task=") + op.TaskID + " " + duration, Details: details})
		if op.Status == "failed" || (op.Status == "running" || op.Status == "background") && len(s.Runners) == 0 && !s.Busy {
			attention(op.ID, op.TaskID, op.Tool+" ["+workflowStatus(text, op.Status)+text("] requires inspection; commands are not automatically replayed"), workflowOperations)
		}
	}
	slices.Reverse(views[workflowOperations])
	for _, directive := range s.Board.Directives {
		target := directive.TaskID
		if target == "" {
			target = text("coordinator")
		}
		details := []string{text("Instruction: ") + directive.ID, text("Target: ") + target, text("Mode: ") + directive.Mode, text("Delivery: ") + workflowStatus(text, string(directive.Status)), text("Depends on: ") + strings.Join(directive.DependsOn, ", "), directive.Text, text("Queued means pending delivery; received means recorded in agent history, not finished work.")}
		if directive.File != "" {
			details = append(details, fmt.Sprintf(text("Review location: %s:%d"), directive.File, directive.Line))
		}
		views[workflowQueue] = append(views[workflowQueue], workflowRow{ID: directive.ID, TaskID: directive.TaskID, Label: fmt.Sprintf("[%s/%s] %s: %s", directive.Mode, directive.Status, target, directive.Text), Details: details})
		if directive.Status == "claimed" {
			attention(directive.ID, directive.TaskID, text("Uncertain instruction delivery; inspect history before explicit retry"), workflowQueue)
		}
	}
	for _, cp := range s.Checkpoints {
		details := []string{text("Checkpoint: ") + cp.ID, text("Source: ") + cp.SourceFingerprint, text("Plan: ") + cp.PlanFingerprint, fmt.Sprintf(text("Stage: %d"), cp.Stage), text("i inspects current recovery plan; R applies an unchanged reconciled plan")}
		if plan := s.Board.ResumePreview; plan != nil && plan.CheckpointID == cp.ID {
			details = append(details, text("Ready tasks: ")+strings.Join(plan.ReadyTasks, ", "), text("Reverify tasks: ")+strings.Join(plan.ReverifyTasks, ", "), text("Ambiguous operations: ")+strings.Join(plan.AmbiguousOperations, ", "), text("Source inspected: ")+plan.SourceFingerprint)
		}
		views[workflowCheckpoints] = append(views[workflowCheckpoints], workflowRow{ID: cp.ID, Label: fmt.Sprintf(text("Stage %d | %s"), cp.Stage, cp.ID), Details: details})
	}
	for _, finding := range s.Findings {
		if finding.Status != "verified" && finding.Status != "waived" {
			attention(finding.ID, finding.TaskID, fmt.Sprintf(text("Finding %s:%d %s [%s]"), finding.Path, finding.StartLine, finding.Issue, finding.Status), workflowHandoffs)
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
	if p.tab == workflowInteractions && p.details && p.selected == 0 && p.snapshot.Interactions.Preview != nil {
		return renderInteractionPreview(p.snapshot.Interactions.Preview, width, height, p.locale.Code())
	}
	state := p.locale.Text("idle")
	if p.snapshot.Busy {
		state = p.locale.Text("working")
	}
	if p.snapshot.Paused {
		state = p.locale.Text("dispatch paused")
	}
	teamLimit := p.locale.Text("configured")
	if p.snapshot.AgentLimit > 0 {
		teamLimit = fmt.Sprint(p.snapshot.AgentLimit)
	}
	header := []string{p.locale.Text("Agent controls | ") + p.locale.Text(workflowTabs[p.tab]), p.locale.Text("Esc close | s stop | p pause | r resume | 1-9/0 views | i interactions | t timeline"), p.locale.Text("1 Tasks  2 Graph  3 Handoffs  4 Ownership  5 Operations  6 Queue  7 Checkpoints  8 Attention  9 Context  0 Batches"), fmt.Sprintf(p.locale.Text("%s | agents %d/%s | tokens %d/%d | $%.2f/$%.2f | calls %d/%d"), state, len(p.snapshot.Runners), teamLimit, p.snapshot.Usage.Tokens, p.snapshot.Limits.MaxTokens, p.snapshot.Usage.Cost, p.snapshot.Limits.MaxCost, p.snapshot.Usage.ToolCalls, p.snapshot.Limits.MaxToolCalls)}
	if p.err != "" {
		header = append(header, p.locale.Text("Error: ")+p.err)
	}
	if p.loading && p.snapshot.Revision == "" {
		header = append(header, p.locale.Text("Loading workflow…"))
	}
	if p.input.mode != "" {
		header = append(header, p.input.label(p.locale.Code())+p.locale.Text(" | Enter apply, Esc cancel"), p.input.visible(width))
	}
	if len(p.snapshot.Operations) > 0 && height >= 18 {
		op := p.snapshot.Operations[len(p.snapshot.Operations)-1]
		header = append(header, p.locale.Text("Latest operation: ")+op.Tool+" ["+workflowStatus(p.locale.Text, op.Status)+p.locale.Text("] task=")+op.TaskID+p.locale.Text(" | 5 details/output"))
	}
	rows := p.rows()
	if len(rows) == 0 {
		header = append(header, p.locale.Text("No records in this view"))
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
	right := []string{p.locale.Text("Details | Enter expands | h hold | u unhold | x cancel task"), p.locale.Text("e steer task | E steer coordinator | n queue | a assign"), p.locale.Text("d diff | o jobs/output | g agent history | l team limit | b budget")}
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
	header = append(header, p.locale.Text("Arrows select | Enter details | e steer | n queue | h hold | u unhold | x cancel | v replan | w scope | f retry"), p.locale.Text("Queue: +/- reorder | Delete cancel | y retry uncertain | z remove received | c continue queue"))
	return workflowFit(header, width, height)
}

func renderInteractionPreview(preview *interaction.Preview, width, height int, codes ...string) string {
	text := workflowTranslator(codes...)
	if width < 1 || height < 1 {
		return ""
	}
	if preview.Width <= 0 || preview.Width > 120 || preview.Height <= 0 || preview.Height > 80 || len(preview.Pixels) != preview.Width*preview.Height {
		return workflowFit([]string{text("Invalid preview dimensions")}, width, height)
	}
	header := workflowFit([]string{text("Captured interaction | Enter closes | v refresh | f live preview | p take control | r resume"), preview.Path}, width, min(2, height))
	var b strings.Builder
	b.WriteString(header)
	for y := 0; y+1 < preview.Height && y/2 < height-2; y += 2 {
		b.WriteByte('\n')
		for x := 0; x < preview.Width && x < width; x++ {
			top, bottom := preview.Pixels[y*preview.Width+x], preview.Pixels[(y+1)*preview.Width+x]
			fmt.Fprintf(&b, "\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm▀", top>>16&255, top>>8&255, top&255, bottom>>16&255, bottom>>8&255, bottom&255)
		}
		b.WriteString("\x1b[0m")
	}
	return b.String()
}
