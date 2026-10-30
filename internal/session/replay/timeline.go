// Package replay turns a stored session into a step timeline, finds the
// points where it can be safely branched, and compares two sessions. It is
// pure logic over message and session values, so it needs no database.
package replay

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
)

// summaryWidth bounds the one-line summary of a step.
const summaryWidth = 80

// Step is one message of a session, described for the timeline. Numbers are
// 1-based so they match what users type into --at.
type Step struct {
	Number    int    `json:"step"`
	MessageID string `json:"message_id"`
	Role      string `json:"role"`
	Model     string `json:"model,omitempty"`
	Summary   string `json:"summary"`
	// Tools are the names of the tools this step called, in order.
	Tools []string `json:"tools,omitempty"`
	// Errors counts tool results in this step that came back as errors.
	Errors int `json:"errors,omitempty"`
	// Finish is the finish reason of an assistant step, when it has one.
	Finish string `json:"finish,omitempty"`
	// CreatedAt is Unix seconds.
	CreatedAt int64 `json:"created_at"`
	// Branchable reports whether a branch can end at this step without
	// leaving a tool call unanswered, which providers reject.
	Branchable bool `json:"branchable"`
}

// BuildTimeline describes msgs in order.
func BuildTimeline(msgs []message.Message) []Step {
	open := map[string]struct{}{}
	steps := make([]Step, 0, len(msgs))
	for i := range msgs {
		m := &msgs[i]
		step := Step{
			Number:    i + 1,
			MessageID: m.ID,
			Role:      string(m.Role),
			Model:     m.Model,
			Summary:   summarize(m),
			CreatedAt: m.CreatedAt,
		}
		for _, c := range m.ToolCalls() {
			step.Tools = append(step.Tools, c.Name)
			open[c.ID] = struct{}{}
		}
		for _, r := range m.ToolResults() {
			delete(open, r.ToolCallID)
			if r.IsError {
				step.Errors++
			}
		}
		if m.Role == message.Assistant {
			if f := m.FinishReason(); f != "" {
				step.Finish = string(f)
			}
		}
		step.Branchable = len(open) == 0
		steps = append(steps, step)
	}
	return steps
}

// summarize is the one-line description of a message: its text, or for a
// message without any, what it did.
func summarize(m *message.Message) string {
	text := strings.Join(strings.Fields(m.Content().Text), " ")
	if text == "" {
		switch {
		case len(m.ToolCalls()) > 0:
			names := make([]string, 0, len(m.ToolCalls()))
			for _, c := range m.ToolCalls() {
				names = append(names, c.Name)
			}
			text = "calls " + strings.Join(names, ", ")
		case len(m.ToolResults()) > 0:
			text = fmt.Sprintf("%d tool result(s)", len(m.ToolResults()))
		}
	}
	return truncate(text, summaryWidth)
}

func truncate(s string, width int) string {
	if utf8.RuneCountInString(s) <= width {
		return s
	}
	runes := []rune(s)
	return string(runes[:width-1]) + "…"
}

// ResolveStep finds the index into msgs that spec names. spec is a 1-based
// step number or a message ID (or a unique prefix of one).
func ResolveStep(msgs []message.Message, spec string) (int, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return 0, fmt.Errorf("no step given")
	}
	if n, err := strconv.Atoi(spec); err == nil {
		if n < 1 || n > len(msgs) {
			return 0, fmt.Errorf("step %d is out of range: the session has %d step(s)", n, len(msgs))
		}
		return n - 1, nil
	}
	found := -1
	for i := range msgs {
		if msgs[i].ID == spec {
			return i, nil
		}
		if strings.HasPrefix(msgs[i].ID, spec) {
			if found != -1 {
				return 0, fmt.Errorf("message prefix %q is ambiguous", spec)
			}
			found = i
		}
	}
	if found == -1 {
		return 0, fmt.Errorf("no step %q in this session", spec)
	}
	return found, nil
}

// NextBranchable returns the index of the first branchable step at or after
// idx, or -1 when there is none.
func NextBranchable(steps []Step, idx int) int {
	for i := idx; i < len(steps); i++ {
		if steps[i].Branchable {
			return i
		}
	}
	return -1
}

// CheckBranchable returns an error naming the nearest usable step when a
// branch ending at msgs[idx] would leave a tool call without its result.
func CheckBranchable(msgs []message.Message, idx int) error {
	steps := BuildTimeline(msgs)
	if idx < 0 || idx >= len(steps) {
		return fmt.Errorf("step out of range")
	}
	if steps[idx].Branchable {
		return nil
	}
	if next := NextBranchable(steps, idx); next != -1 {
		return fmt.Errorf("step %d leaves a tool call unanswered; branch at step %d instead (or an earlier one)",
			idx+1, next+1)
	}
	return fmt.Errorf("step %d leaves a tool call unanswered; branch at an earlier step", idx+1)
}
