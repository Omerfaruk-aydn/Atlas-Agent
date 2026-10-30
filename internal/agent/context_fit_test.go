package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func toolRound(id, output string) []fantasy.Message {
	return []fantasy.Message{
		{Role: fantasy.MessageRoleAssistant, Content: []fantasy.MessagePart{fantasy.ToolCallPart{ToolCallID: id, ToolName: "view", Input: "{}"}}},
		{Role: fantasy.MessageRoleTool, Content: []fantasy.MessagePart{fantasy.ToolResultPart{ToolCallID: id, Output: fantasy.ToolResultOutputContentText{Text: output}}}},
	}
}

func conversation(rounds int, size int) fantasy.Prompt {
	prompt := fantasy.Prompt{
		{Role: fantasy.MessageRoleSystem, Content: []fantasy.MessagePart{fantasy.TextPart{Text: "system instructions"}}},
		{Role: fantasy.MessageRoleUser, Content: []fantasy.MessagePart{fantasy.TextPart{Text: "do the task"}}},
	}
	for i := range rounds {
		prompt = append(prompt, toolRound(string(rune('a'+i)), strings.Repeat("x", size))...)
	}
	return prompt
}

func outputCap(n int64) *int64 { return &n }

func TestFitLeavesAFittingRequestAlone(t *testing.T) {
	t.Parallel()

	call := fantasy.Call{Prompt: conversation(2, 1000), MaxOutputTokens: outputCap(4000)}
	fitted, report, err := fitCall(modelWithWindow(200_000, 16_000), call)
	require.NoError(t, err)
	require.False(t, report.changed())
	require.Equal(t, int64(4000), *fitted.MaxOutputTokens)
}

func TestFitCapsOutputBeforeTouchingTheConversation(t *testing.T) {
	t.Parallel()

	prompt := conversation(2, 4000)
	window := estimateMessageTokens(prompt) + 6000
	fitted, report, err := fitCall(modelWithWindow(window, 16_000), fantasy.Call{Prompt: prompt, MaxOutputTokens: outputCap(16_000)})
	require.NoError(t, err)
	require.True(t, report.OutputCapped)
	require.Zero(t, report.Elided, "no content is dropped while an output cap is enough")
	require.LessOrEqual(t, *fitted.MaxOutputTokens, int64(6000))
	require.Equal(t, prompt, fitted.Prompt)
}

// A fallback with a smaller window must see a smaller request: the oldest big
// tool outputs are replaced by a marker that says so, the recent messages stay
// whole, and the caller's own messages are not modified.
func TestFitElidesOldToolOutputVisibly(t *testing.T) {
	t.Parallel()

	prompt := conversation(6, 4_000)
	small := modelWithWindow(4_000, 200)
	fitted, report, err := fitCall(small, fantasy.Call{Prompt: prompt, MaxOutputTokens: outputCap(200)})
	require.NoError(t, err)
	require.Positive(t, report.Elided)
	require.LessOrEqual(t, estimateMessageTokens(fitted.Prompt)+*fitted.MaxOutputTokens, int64(4_000))

	first, _ := toolResultPart(fitted.Prompt[3].Content[0])
	text, _ := toolResultText(first.Output)
	require.Contains(t, text, "stale tool output were pruned", "reduction is announced, never silent")

	last, _ := toolResultPart(fitted.Prompt[len(fitted.Prompt)-1].Content[0])
	lastText, _ := toolResultText(last.Output)
	require.Len(t, lastText, 4_000, "the most recent result is untouched")

	original, _ := toolResultPart(prompt[3].Content[0])
	originalText, _ := toolResultText(original.Output)
	require.Len(t, originalText, 4_000, "the caller's prompt is not mutated")
	require.Len(t, fitted.Prompt, len(prompt), "message and tool-call pairing is preserved")
}

func TestFitRefusesWhatReductionCannotSave(t *testing.T) {
	t.Parallel()

	// Nothing reducible: one enormous user message.
	prompt := fantasy.Prompt{{Role: fantasy.MessageRoleUser, Content: []fantasy.MessagePart{fantasy.TextPart{Text: strings.Repeat("y", 200_000)}}}}
	_, _, err := fitCall(modelWithWindow(8_000, 1_000), fantasy.Call{Prompt: prompt})
	var overflow *contextOverflowError
	require.ErrorAs(t, err, &overflow)
	require.Equal(t, int64(8_000), overflow.Window)
}

func TestFitCountsToolSchemas(t *testing.T) {
	t.Parallel()

	schema := map[string]any{"type": "object", "description": strings.Repeat("schema ", 4000)}
	call := fantasy.Call{
		Prompt: conversation(0, 0),
		Tools:  []fantasy.Tool{fantasy.FunctionTool{Name: "big", Description: "big", InputSchema: schema}},
	}
	require.Greater(t, estimateCallTokens(call), estimateMessageTokens(call.Prompt)+5000)
	_, _, err := fitCall(modelWithWindow(4_000, 500), call)
	require.Error(t, err, "tool schemas count against the window")
}

type recordingLanguageModel struct {
	finishStreamModel
	seen []fantasy.Call
}

func (m *recordingLanguageModel) Stream(ctx context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	m.seen = append(m.seen, call)
	return m.finishStreamModel.Stream(ctx, call)
}

// The same request goes to the primary and, after a failover, to a smaller
// fallback. Each attempt is measured against the model that receives it.
func TestAttemptsUseTheBudgetOfTheModelThatReceivesThem(t *testing.T) {
	t.Parallel()

	a := &sessionAgent{}
	request := fantasy.Call{Prompt: conversation(6, 4_000), MaxOutputTokens: outputCap(200)}

	attempt := func(model Model, inner *recordingLanguageModel) error {
		wrapped := &attemptModel{
			LanguageModel: inner,
			prepare:       func(c fantasy.Call) fantasy.Call { return c },
			fit:           func(c fantasy.Call) (fantasy.Call, error) { return a.fitAttempt(c, model, "test") },
		}
		_, err := wrapped.Stream(t.Context(), request)
		return err
	}

	primary, fallback := &recordingLanguageModel{}, &recordingLanguageModel{}
	require.NoError(t, attempt(modelWithWindow(1_000_000, 16_000), primary))
	require.NoError(t, attempt(modelWithWindow(4_000, 200), fallback))

	require.Equal(t, request.Prompt, primary.seen[0].Prompt, "a large window sends the conversation as it is")
	require.NotEqual(t, request.Prompt, fallback.seen[0].Prompt, "the smaller fallback gets a reduced request")
	require.LessOrEqual(t, estimateCallTokens(fallback.seen[0])+*fallback.seen[0].MaxOutputTokens, int64(4_000))

	// A request that cannot fit is not sent at all.
	tiny := &recordingLanguageModel{}
	huge := fantasy.Prompt{{Role: fantasy.MessageRoleUser, Content: []fantasy.MessagePart{fantasy.TextPart{Text: strings.Repeat("z", 100_000)}}}}
	wrapped := &attemptModel{LanguageModel: tiny, prepare: func(c fantasy.Call) fantasy.Call { return c }, fit: func(c fantasy.Call) (fantasy.Call, error) {
		return a.fitAttempt(c, modelWithWindow(4_000, 500), "test")
	}}
	_, err := wrapped.Stream(t.Context(), fantasy.Call{Prompt: huge})
	var overflow *contextOverflowError
	require.True(t, errors.As(err, &overflow))
	require.Empty(t, tiny.seen)
}

// Shrinking the history must not break the pairing the providers enforce: every
// tool call keeps exactly one result with the same id, in the same place, and
// nothing but the text of old large results changes.
func TestFitKeepsToolCallsPairedWithTheirResults(t *testing.T) {
	t.Parallel()

	prompt := fantasy.Prompt{{Role: fantasy.MessageRoleSystem, Content: []fantasy.MessagePart{fantasy.TextPart{Text: "system"}}}}
	// Parallel calls answered in one tool message, one of them an error and
	// one of them not text at all.
	prompt = append(prompt,
		fantasy.Message{Role: fantasy.MessageRoleAssistant, Content: []fantasy.MessagePart{
			fantasy.ToolCallPart{ToolCallID: "p1", ToolName: "view", Input: "{}"},
			fantasy.ToolCallPart{ToolCallID: "p2", ToolName: "bash", Input: "{}"},
			fantasy.ToolCallPart{ToolCallID: "p3", ToolName: "view", Input: "{}"},
		}},
		fantasy.Message{Role: fantasy.MessageRoleTool, Content: []fantasy.MessagePart{
			fantasy.ToolResultPart{ToolCallID: "p1", Output: fantasy.ToolResultOutputContentText{Text: strings.Repeat("a", 30_000)}},
			fantasy.ToolResultPart{ToolCallID: "p2", Output: fantasy.ToolResultOutputContentError{Error: errors.New(strings.Repeat("e", 30_000))}},
			fantasy.ToolResultPart{ToolCallID: "p3", Output: fantasy.ToolResultOutputContentText{Text: strings.Repeat("c", 30_000)}},
		}},
	)
	for i := range 4 {
		prompt = append(prompt, toolRound(string(rune('k'+i)), strings.Repeat("x", 20_000))...)
	}
	before := len(prompt)
	original := strings.Repeat("a", 30_000)

	fitted, report, err := fitCall(modelWithWindow(30_000, 2_000), fantasy.Call{Prompt: prompt, MaxOutputTokens: outputCap(2_000)})
	require.NoError(t, err)
	require.Positive(t, report.Elided)
	require.Len(t, fitted.Prompt, before, "no message is dropped or merged")

	calls, results := map[string]int{}, map[string]int{}
	for i, m := range fitted.Prompt {
		require.Equal(t, prompt[i].Role, m.Role)
		require.Len(t, m.Content, len(prompt[i].Content), "no part is dropped")
		for _, part := range m.Content {
			switch p := part.(type) {
			case fantasy.ToolCallPart:
				calls[p.ToolCallID]++
			case fantasy.ToolResultPart:
				results[p.ToolCallID]++
			}
		}
	}
	require.NotEmpty(t, calls)
	require.Equal(t, calls, results, "every call has exactly one result and no result is orphaned")

	// The error result is not text and is passed through unchanged.
	second, _ := toolResultPart(fitted.Prompt[2].Content[1])
	_, isError := second.Output.(fantasy.ToolResultOutputContentError)
	require.True(t, isError)
	// The caller's own conversation is not modified.
	untouched, _ := toolResultPart(prompt[2].Content[0])
	text, _ := toolResultText(untouched.Output)
	require.Equal(t, original, text)
}
