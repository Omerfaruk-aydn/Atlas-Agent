package openai

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func TestSessionCacheKey(t *testing.T) {
	t.Parallel()
	key, ok := sessionCacheKey(map[string]string{"x-session-id": "a", "X-Session-Affinity": "abc123"})
	require.True(t, ok)
	require.Equal(t, "abc123", key)
	_, ok = sessionCacheKey(map[string]string{"x-session-id": "a"})
	require.False(t, ok)
	_, ok = sessionCacheKey(nil)
	require.False(t, ok)
}

func TestSupportsPromptCacheKey(t *testing.T) {
	t.Parallel()
	require.True(t, supportsPromptCacheKey("", "openai"))
	require.True(t, supportsPromptCacheKey("https://api.openai.com/v1", "custom"))
	require.True(t, supportsPromptCacheKey("https://chatgpt.com/backend-api/codex", "x"))
	require.True(t, supportsPromptCacheKey("https://example.test/v1", "chatgpt"))
	require.False(t, supportsPromptCacheKey("http://localhost:11434/v1", "ollama"))
	require.False(t, supportsPromptCacheKey("https://openrouter.ai/api/v1", "openrouter"))
}

func TestChatGPTAffinityRequestOptions(t *testing.T) {
	t.Parallel()
	headers := map[string]string{"x-session-affinity": "abc123"}
	call := fantasy.Call{Headers: headers}
	require.Len(t, chatGPTAffinityRequestOptions("https://chatgpt.com/backend-api/codex", "chatgpt", call), 2)
	// Only the ChatGPT backend gets the headers.
	require.Empty(t, chatGPTAffinityRequestOptions("https://api.openai.com/v1", "openai", call))
	// No session, no headers.
	require.Empty(t, chatGPTAffinityRequestOptions("https://chatgpt.com/backend-api/codex", "chatgpt", fantasy.Call{}))
	// A header the caller set wins.
	set := fantasy.Call{Headers: map[string]string{"x-session-affinity": "abc123", "Session-Id": "mine"}}
	require.Empty(t, chatGPTAffinityRequestOptions("https://chatgpt.com/backend-api/codex", "chatgpt", set))
}
