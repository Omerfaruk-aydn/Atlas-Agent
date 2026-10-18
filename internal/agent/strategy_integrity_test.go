package agent

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStructuredFailureClassesStayDistinct(t *testing.T) {
	t.Parallel()
	require.NotEqual(t, errorClass(`{"code":"stale_ref"}`), errorClass(`{"code":"missing_target"}`))
	require.Equal(t, "stale_ref", errorClass(`{"error_code":"stale_ref","target":"123"}`))
	require.Equal(t, "stale_ref", errorClass("Browser action failed", `{"error_code":"stale_ref"}`))
	require.NotEqual(t, errorClass("Action failed", `{"code":"missing_target"}`), errorClass("Action failed", `{"code":"stale_ref"}`))
}

func TestAsynchronousPollingIsNotStrategyFailure(t *testing.T) {
	t.Parallel()
	require.True(t, expectedPolling(toolInteraction{tool: "job_output"}))
	require.True(t, expectedPolling(toolInteraction{tool: "browser", input: `{"action":"wait_for"}`}))
	require.False(t, expectedPolling(toolInteraction{tool: "browser", input: `{"action":"click"}`}))
}
