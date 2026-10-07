package fantasy

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenCodeFreeTierRestrictionIsActionableAndNeverRetried(t *testing.T) {
	t.Parallel()
	err := &ProviderError{URL: "https://opencode.ai/zen/v1/chat/completions", StatusCode: 403, Message: "OpenCode's free tier can only be used from within OpenCode", ResponseHeaders: map[string]string{"x-should-retry": "true"}, TransientError: true}
	require.False(t, err.IsRetryable())
	require.Contains(t, err.Error(), "server restricts")
	require.False(t, err.AuthError)
	for _, endpoint := range []string{"https://api.meta.ai/v1/messages", "https://opencode.ai.example/zen/v1/responses"} {
		copyErr := *err
		copyErr.URL = endpoint
		require.True(t, copyErr.IsRetryable())
		require.Equal(t, err.Message, copyErr.Error())
	}
	authErr := &ProviderError{URL: err.URL, StatusCode: 403, Message: "Invalid key"}
	require.Equal(t, "Invalid key", authErr.Error())
}
