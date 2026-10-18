package agent

import (
	"fmt"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

// A model cannot see how full its context is or how long the current turn
// has run, so it starts a broad exploration just before a compaction or
// keeps going long after it should have stepped back. The budget line tells
// it, as a short trailing message refreshed every step. It is appended after
// the cache breakpoints are placed and replaced each step, so it never
// invalidates the cached conversation. It appears once the context is a
// third full or the turn has taken some steps, so short turns pay nothing.

const (
	budgetLineMarker   = "<context_budget>"
	budgetLineMinRatio = 0.33
	budgetLineMinSteps = 8
)

// compactionPoint is the token count at which a session on a window of
// contextWindow tokens is summarized, mirroring shouldAutoSummarize; 0 when
// the window is unknown.
func compactionPoint(contextWindow int64, usedRatio float64) int64 {
	switch {
	case contextWindow <= 0:
		return 0
	case validUsedRatio(usedRatio):
		return int64(float64(contextWindow) * usedRatio)
	case contextWindow > largeContextWindowThreshold:
		return min(int64(float64(contextWindow)*largeContextWindowCompactAt), contextWindow-largeContextWindowBuffer)
	default:
		return contextWindow - int64(float64(contextWindow)*smallContextWindowRatio)
	}
}

// budgetLine describes the context use and the turn's length, or "" while
// both are small.
func budgetLine(contextWindow, tokens int64, usedRatio float64, step int) string {
	point := compactionPoint(contextWindow, usedRatio)
	full := contextWindow > 0 && float64(tokens) >= float64(contextWindow)*budgetLineMinRatio
	if !full && step < budgetLineMinSteps {
		return ""
	}
	text := budgetLineMarker + "\n"
	if contextWindow > 0 {
		text += fmt.Sprintf("Context: %s of %s tokens used (%.0f%%)", kTokens(tokens), kTokens(contextWindow), 100*float64(tokens)/float64(contextWindow))
		if point > 0 && tokens < point {
			text += fmt.Sprintf("; the conversation is summarized at about %s, %s from now", kTokens(point), kTokens(point-tokens))
		}
		text += ". "
	}
	text += fmt.Sprintf("This turn has taken %d steps.", step)
	if point > 0 && float64(tokens) >= float64(point)*0.8 {
		text += " A summary is close: write the current state and the next step to your notes or plan before starting anything long."
	} else if step >= 3*budgetLineMinSteps {
		text += " If the last steps did not move the work forward, step back and change approach."
	}
	return text + "\n</context_budget>"
}

func kTokens(n int64) string {
	if n >= 1000 {
		return fmt.Sprintf("%dk", n/1000)
	}
	return fmt.Sprint(n)
}

// withBudgetLine replaces the previous step's budget line with line.
func withBudgetLine(messages []fantasy.Message, line string) []fantasy.Message {
	out := make([]fantasy.Message, 0, len(messages)+1)
	for _, msg := range messages {
		if msg.Role == fantasy.MessageRoleSystem && len(msg.Content) == 1 {
			if text, ok := msg.Content[0].(fantasy.TextPart); ok && len(text.Text) >= len(budgetLineMarker) && text.Text[:len(budgetLineMarker)] == budgetLineMarker {
				continue
			}
		}
		out = append(out, msg)
	}
	if line != "" {
		out = append(out, fantasy.NewSystemMessage(line))
	}
	return out
}
