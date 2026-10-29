package hooks

import (
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/stretchr/testify/require"
)

func TestRequiredHookFailurePolicy(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		hook config.HookConfig
	}{
		{name: "nonzero exit", hook: config.HookConfig{Command: "exit 1", Required: true}},
		{name: "timeout", hook: config.HookConfig{Command: "sleep 10", Timeout: 1, Required: true}},
		{name: "missing agent executor", hook: config.HookConfig{Prompt: "review", Agent: "reviewer", Required: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runner := NewRunner([]config.HookConfig{tc.hook}, t.TempDir(), t.TempDir())
			result, err := runner.Run(t.Context(), EventPreToolUse, "session", "bash", `{}`)
			require.NoError(t, err)
			require.Equal(t, DecisionDeny, result.Decision)
			require.True(t, result.RequiredFailure)
			require.Contains(t, result.Reason, "Required hook failed")
		})
	}
}

func TestRequiredPolicySurvivesActionDeduplication(t *testing.T) {
	t.Parallel()
	optional := config.HookConfig{Command: "exit 1"}
	required := optional
	required.Required = true
	runner := NewRunner([]config.HookConfig{optional, required}, t.TempDir(), t.TempDir())
	start := time.Now()
	result, err := runner.Run(t.Context(), EventPreToolUse, "session", "bash", `{}`)
	require.NoError(t, err)
	require.Less(t, time.Since(start), 5*time.Second)
	require.Equal(t, 1, result.HookCount)
	require.True(t, result.RequiredFailure)
}
