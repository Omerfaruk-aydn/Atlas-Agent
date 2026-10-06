package agent

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenCodeHeadersPreserveConfiguredSession(t *testing.T) {
	t.Parallel()
	headers := openCodeHeaders(map[string]string{"X-OpenCode-Session": "configured-session", "user-agent": "generic-sdk", "X-Proxy": "preserved"})
	require.Equal(t, "configured-session", headers["x-opencode-session"])
	require.Equal(t, userAgent, headers["User-Agent"])
	require.Equal(t, "preserved", headers["X-Proxy"])
	require.NotContains(t, headers, "X-OpenCode-Session")
	require.NotContains(t, headers, "user-agent")
	require.NotEqual(t, openCodeHeaders(map[string]string{})["x-opencode-session"], openCodeHeaders(map[string]string{})["x-opencode-session"])
}
