package model

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ansiext"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ansi"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
)

type workflowTimelineEntry struct {
	At      int64
	ID      string
	Label   string
	Details []string
}

func workflowTimeline(snapshot engineering.WorkflowSnapshot, codes ...string) []workflowTimelineEntry {
	text := workflowTranslator(codes...)
	entries := make([]workflowTimelineEntry, 0, len(snapshot.Operations)+len(snapshot.Checks))
	for _, operation := range snapshot.Operations {
		entries = append(entries, workflowTimelineEntry{At: operation.StartedAt, ID: operation.ID, Label: operation.Tool + " [" + workflowStatus(text, operation.Status) + "]", Details: []string{text("Operation: ") + operation.ID, text("Call: ") + operation.CallID, text("Task: ") + operation.TaskID, text("Agent: ") + operation.AgentName, text("Input fingerprint: ") + operation.Fingerprint, text("Evidence hash: ") + operation.EvidenceHash, fmt.Sprintf(text("Outcome observed: %t"), operation.OutcomeObserved)}})
	}
	for _, check := range snapshot.Checks {
		entries = append(entries, workflowTimelineEntry{At: check.CheckedAt, ID: check.RunID + "/" + check.Name, Label: fmt.Sprintf(text("Check %s [passed=%t]"), check.Name, check.Passed), Details: []string{text("Tool: ") + check.Tool, text("Task: ") + check.TaskID, text("Run: ") + check.RunID, text("Evidence: ") + check.Evidence}})
	}
	for _, checkpoint := range snapshot.Checkpoints {
		entries = append(entries, workflowTimelineEntry{ID: checkpoint.ID, Label: fmt.Sprintf(text("Checkpoint stage %d"), checkpoint.Stage), Details: []string{text("Checkpoint: ") + checkpoint.ID, text("Message: ") + checkpoint.MessageID, text("Source fingerprint: ") + checkpoint.SourceFingerprint, text("Plan fingerprint: ") + checkpoint.PlanFingerprint, text("Operations: ") + strings.Join(checkpoint.Operations, ", "), text("Timestamp unavailable in checkpoint record")}})
	}
	slices.SortFunc(entries, func(a, b workflowTimelineEntry) int {
		if a.At != b.At {
			return cmp.Compare(b.At, a.At)
		}
		return strings.Compare(a.ID, b.ID)
	})
	return entries
}

func (p *workflowPanel) renderTimeline(width, height int) string {
	entries := workflowTimeline(p.snapshot, p.locale.Code())
	lines := []string{p.locale.Text("Session timeline"), p.locale.Text("t tasks | Esc close | arrows select | Enter details")}
	if p.err != "" {
		lines = append(lines, p.locale.Text("Error: ")+p.err)
	}
	if len(entries) == 0 {
		lines = append(lines, p.locale.Text("No recorded operations, checks or checkpoints"))
	}
	selected := min(max(0, p.selected), max(0, len(entries)-1))
	if p.details && len(entries) > 0 {
		entry := entries[selected]
		lines = append(lines, entry.Label)
		lines = append(lines, entry.Details...)
	} else {
		available := max(0, height-len(lines))
		start := max(0, selected-available+1)
		for i := start; i < len(entries) && len(lines) < height; i++ {
			pointer := " "
			if i == selected {
				pointer = ">"
			}
			entry := entries[i]
			stamp := "--:--:--"
			if entry.At > 0 {
				stamp = time.UnixMilli(entry.At).Format("15:04:05")
			}
			lines = append(lines, fmt.Sprintf("%s %s %s", pointer, stamp, entry.Label))
		}
	}
	lines = lines[:min(len(lines), height)]
	for i, line := range lines {
		lines[i] = ansi.Truncate(ansiext.Escape(strings.ReplaceAll(strings.ReplaceAll(line, "\n", " "), "\r", " ")), width, "")
	}
	return strings.Join(lines, "\n")
}
