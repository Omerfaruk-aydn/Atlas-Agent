package tools

import (
	"context"
	"encoding/json"
	"testing"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func bashMetadata(t *testing.T, resp fantasy.ToolResponse) BashResponseMetadata {
	t.Helper()
	var meta BashResponseMetadata
	require.NoError(t, json.Unmarshal([]byte(resp.Metadata), &meta))
	return meta
}

// Session 7d92f539: python was not installed (exit 127) yet the result was
// reported as a success.
func TestBashCommandNotFoundIsAnError(t *testing.T) {
	t.Parallel()
	tool := newBashToolForTest(t.TempDir())
	ctx := context.WithValue(t.Context(), SessionIDContextKey, "test-session")
	resp := runBashTool(t, tool, ctx, BashParams{Description: "missing program", Command: "definitely-not-a-real-program-xyz --version"})
	require.True(t, resp.IsError, resp.Content)
	meta := bashMetadata(t, resp)
	require.Equal(t, "command_not_found", meta.Status)
	require.True(t, meta.Executed)
	require.NotNil(t, meta.ExitCode)
	require.Equal(t, 127, *meta.ExitCode)
	require.Contains(t, resp.Content, "was not performed")
	require.Contains(t, resp.Content, "not retried or rewritten")
}
