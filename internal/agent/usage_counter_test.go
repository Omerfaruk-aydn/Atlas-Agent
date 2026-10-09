package agent

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func openAIUsageText(text string, prompt, cached, completion int) string {
	chunk, _ := json.Marshal(map[string]any{"id": "c1", "object": "chat.completion.chunk", "created": 1, "model": "mock-model", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "content": text}}}})
	done, _ := json.Marshal(map[string]any{"id": "c1", "object": "chat.completion.chunk", "created": 1, "model": "mock-model", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}}, "usage": map[string]any{"prompt_tokens": prompt, "completion_tokens": completion, "total_tokens": prompt + completion, "prompt_tokens_details": map[string]any{"cached_tokens": cached}}})
	return "data: " + string(chunk) + "\n\ndata: " + string(done) + "\n\ndata: [DONE]\n\n"
}

// The sidebar's context figure is PromptTokens + CompletionTokens of the
// latest conversation request. Cached tokens count once and the separate
// title request never adds to it.
func TestContextCounterReflectsLatestConversationRequest(t *testing.T) {
	h := newPayloadCoordinator(t, wireOpenAI, "", "", openAIUsageText("Merhaba", 5000, 4000, 7))
	h.recorder.untooled = openAIUsageText("Greeting", 777, 0, 3)
	h.recorder.untooledDelay = 300 * time.Millisecond
	session := newSession(t, h)
	_, err := h.coord.Run(t.Context(), session, "merhaba")
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		s, err := h.env.sessions.Get(t.Context(), session)
		return err == nil && s.Title == "Greeting"
	}, 5*time.Second, 20*time.Millisecond)
	s, err := h.env.sessions.Get(t.Context(), session)
	require.NoError(t, err)
	require.EqualValues(t, 5000, s.PromptTokens, "prompt_tokens includes the cached 4000 exactly once; title tokens are not added")
	require.EqualValues(t, 7, s.CompletionTokens)
	require.False(t, s.EstimatedUsage)
	require.Positive(t, s.Cost+1, fmt.Sprint(s.Cost))
}

// Anthropic reports cache writes outside input_tokens; they are still part
// of the prompt the context window holds.
func TestContextCounterIncludesAnthropicCacheWrites(t *testing.T) {
	reply := sse(
		map[string]any{"type": "message_start", "message": map[string]any{"id": "msg_1", "type": "message", "role": "assistant", "content": []any{}, "model": "mock-model", "usage": map[string]any{"input_tokens": 10, "cache_creation_input_tokens": 30000, "cache_read_input_tokens": 2000, "output_tokens": 1}}},
		map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "text", "text": ""}},
		map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": "Merhaba"}},
		map[string]any{"type": "content_block_stop", "index": 0},
		map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn"}, "usage": map[string]any{"output_tokens": 4}},
		map[string]any{"type": "message_stop"},
	)
	h := newPayloadCoordinator(t, wireAnthropic, "", "", reply)
	session := newSession(t, h)
	_, err := h.coord.Run(t.Context(), session, "merhaba")
	require.NoError(t, err)
	s, err := h.env.sessions.Get(t.Context(), session)
	require.NoError(t, err)
	require.EqualValues(t, 32010, s.PromptTokens)
}

// Gemini streams cumulative usage snapshots; the final one is the total.
func TestGeminiStreamUsageIsNotSummed(t *testing.T) {
	reply := sse(
		map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"role": "model", "parts": []any{map[string]any{"text": "Mer"}}}}}, "usageMetadata": map[string]any{"promptTokenCount": 1000, "cachedContentTokenCount": 800, "candidatesTokenCount": 1, "totalTokenCount": 1001}},
		map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"role": "model", "parts": []any{map[string]any{"text": "haba"}}}, "finishReason": "STOP"}}, "usageMetadata": map[string]any{"promptTokenCount": 1000, "cachedContentTokenCount": 800, "candidatesTokenCount": 3, "totalTokenCount": 1003}},
	)
	h := newPayloadCoordinator(t, wireGemini, "", "", reply)
	session := newSession(t, h)
	_, err := h.coord.Run(t.Context(), session, "merhaba")
	require.NoError(t, err)
	s, err := h.env.sessions.Get(t.Context(), session)
	require.NoError(t, err)
	require.EqualValues(t, 1000, s.PromptTokens)
	require.EqualValues(t, 3, s.CompletionTokens)
}
