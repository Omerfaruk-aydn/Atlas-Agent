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

func workflowTimeline(snapshot engineering.WorkflowSnapshot) []workflowTimelineEntry {
	entries := make([]workflowTimelineEntry, 0, len(snapshot.Operations)+len(snapshot.Checks))
	for _, operation := range snapshot.Operations {
		entries = append(entries, workflowTimelineEntry{At: operation.StartedAt, ID: operation.ID, Label: operation.Tool + " [" + operation.Status + "]", Details: []string{"Operation: " + operation.ID, "Call: " + operation.CallID, "Task: " + operation.TaskID, "Agent: " + operation.AgentName, "Input fingerprint: " + operation.Fingerprint, "Evidence hash: " + operation.EvidenceHash, fmt.Sprintf("Outcome observed: %t", operation.OutcomeObserved)}})
	}
	for _, check := range snapshot.Checks {
		entries = append(entries, workflowTimelineEntry{At: check.CheckedAt, ID: check.RunID + "/" + check.Name, Label: fmt.Sprintf("Check %s [passed=%t]", check.Name, check.Passed), Details: []string{"Tool: " + check.Tool, "Task: " + check.TaskID, "Run: " + check.RunID, "Evidence: " + check.Evidence}})
	}
	for _, checkpoint := range snapshot.Checkpoints {
		entries = append(entries, workflowTimelineEntry{ID: checkpoint.ID, Label: fmt.Sprintf("Checkpoint stage %d", checkpoint.Stage), Details: []string{"Checkpoint: " + checkpoint.ID, "Message: " + checkpoint.MessageID, "Source fingerprint: " + checkpoint.SourceFingerprint, "Plan fingerprint: " + checkpoint.PlanFingerprint, "Operations: " + strings.Join(checkpoint.Operations, ", "), "Timestamp unavailable in checkpoint record"}})
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
	entries := workflowTimeline(p.snapshot)
	lines := []string{"Session timeline", "t tasks | Esc close | arrows select | Enter details"}
	if p.err != "" {
		lines = append(lines, "Error: "+p.err)
	}
	if len(entries) == 0 {
		lines = append(lines, "No recorded operations, checks or checkpoints")
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
