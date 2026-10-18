package agent

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/providers/anthropic"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	"github.com/stretchr/testify/require"
)

func reasoningParts(msgs []fantasy.Message) []fantasy.MessagePart {
	var out []fantasy.MessagePart
	for _, msg := range msgs {
		for _, part := range msg.Content {
			if part.GetType() == fantasy.ContentTypeReasoning {
				out = append(out, part)
			}
		}
	}
	return out
}

func TestPrepareHistoryReplaysReasoningOnlyToItsProducer(t *testing.T) {
	t.Parallel()

	a := &sessionAgent{isSubAgent: true}
	claudeTurn := message.Message{Role: message.Assistant, Provider: "claude", Model: "claude-a", Parts: []message.ContentPart{
		message.ReasoningContent{Thinking: "private plan", Signature: "sig-a", Provider: "claude", Model: "claude-a"},
		message.TextContent{Text: "visible answer"},
		message.ToolCall{ID: "c1", Name: "view", Input: `{}`, Finished: true},
	}}
	toolResult := message.Message{Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "c1", Name: "view", Content: "ok"}}}
	msgs := []message.Message{{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "q"}}}, claudeTurn, toolResult}

	same, _ := a.prepareHistory(msgs, true, reasoningOrigin{Provider: "claude", Model: "claude-a"})
	parts := reasoningParts(same)
	require.Len(t, parts, 1)
	require.Equal(t, "sig-a", anthropic.GetReasoningMetadata(parts[0].Options()).Signature)

	for _, target := range []reasoningOrigin{
		{Provider: "claude", Model: "claude-b"},   // Same provider, other model.
		{Provider: "deepseek", Model: "claude-a"}, // Other provider.
	} {
		other, _ := a.prepareHistory(msgs, true, target)
		require.Empty(t, reasoningParts(other), "reasoning leaked to %v", target)
		// The conversation itself is preserved.
		var text, calls int
		for _, msg := range other {
			for _, part := range msg.Content {
				switch part.GetType() {
				case fantasy.ContentTypeText:
					text++
				case fantasy.ContentTypeToolCall:
					calls++
				}
			}
		}
		require.Equal(t, 1, calls)
		require.GreaterOrEqual(t, text, 2)
	}
}

// Reasoning recorded by a rate-limit fallback carries its own origin, which
// overrides the message-level model the step was created under.
func TestReasoningOriginPrefersBlockProducer(t *testing.T) {
	t.Parallel()

	m := message.Message{Role: message.Assistant, Provider: "primary", Model: "p", Parts: []message.ContentPart{
		message.ReasoningContent{Signature: "s", Provider: "fallback", Model: "f"},
	}}
	require.False(t, reasoningOrigin{Provider: "primary", Model: "p"}.accepts(m))
	require.True(t, reasoningOrigin{Provider: "fallback", Model: "f"}.accepts(m))

	legacy := message.Message{Role: message.Assistant, Provider: "primary", Model: "p", Parts: []message.ContentPart{message.ReasoningContent{Signature: "s"}}}
	require.True(t, reasoningOrigin{Provider: "primary", Model: "p"}.accepts(legacy))
	require.False(t, reasoningOrigin{Provider: "other", Model: "p"}.accepts(legacy))

	unknown := message.Message{Role: message.Assistant, Parts: []message.ContentPart{message.ReasoningContent{Signature: "s"}}}
	require.False(t, reasoningOrigin{Provider: "x", Model: "y"}.accepts(unknown))
	require.True(t, reasoningOrigin{}.accepts(unknown))
}

func TestFailoverRetainsFallbacksOwnReasoning(t *testing.T) {
	t.Parallel()
	a := reasoningOrigin{Provider: "a", Model: "one"}
	b := reasoningOrigin{Provider: "b", Model: "two"}
	first := fantasy.Message{Role: fantasy.MessageRoleAssistant, Content: []fantasy.MessagePart{fantasy.ReasoningPart{Text: "first"}, fantasy.ToolCallPart{ToolCallID: "c1", ToolName: "view", Input: "{}"}}}
	second := fantasy.Message{Role: fantasy.MessageRoleAssistant, Content: []fantasy.MessagePart{fantasy.ReasoningPart{Text: "fallback"}}}
	origins := map[string]reasoningOrigin{}
	rememberReasoning(origins, []fantasy.Message{first}, a)
	rememberReasoning(origins, []fantasy.Message{second}, b)
	in := []fantasy.Message{first, second}
	out := filterTurnReasoning(in, origins, b)
	parts := reasoningParts(out)
	require.Len(t, parts, 1)
	require.Equal(t, "fallback", parts[0].(fantasy.ReasoningPart).Text)
	require.Equal(t, fantasy.ContentTypeToolCall, out[0].Content[0].GetType())
	require.Len(t, in[0].Content, 2)
}

func TestStripReasoningPartsKeepsToolProtocol(t *testing.T) {
	t.Parallel()

	in := []fantasy.Message{{Role: fantasy.MessageRoleAssistant, Content: []fantasy.MessagePart{
		fantasy.ReasoningPart{Text: "r"},
		fantasy.ToolCallPart{ToolCallID: "1", ToolName: "view", Input: "{}"},
	}}}
	out := stripReasoningParts(in)
	require.Len(t, out[0].Content, 1)
	require.Equal(t, fantasy.ContentTypeToolCall, out[0].Content[0].GetType())
	// The input slice is not mutated.
	require.Len(t, in[0].Content, 2)
}
