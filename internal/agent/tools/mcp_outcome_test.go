package tools

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMCPServerErrorIsNotSuccess(t *testing.T) {
	t.Parallel()
	resp := mcpServerError("quota exceeded")
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "quota exceeded")
	require.False(t, ToolSucceeded("mcp_x_y", resp, nil))
	require.False(t, pipelineStepUncertain(resp.Content), "a delivered refusal is a failure, not an unknown outcome")
}

func TestMCPTransportFailureIsUnknownNotRetryable(t *testing.T) {
	t.Parallel()
	resp := mcpOutcomeUnknown(errors.New("connection reset"))
	require.True(t, resp.IsError)
	require.True(t, pipelineStepUncertain(resp.Content))
	require.Contains(t, resp.Content, "do not resend")
	require.False(t, ToolSucceeded("mcp_x_y", resp, nil))
}
