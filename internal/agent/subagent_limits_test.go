package agent

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSubAgentTimeoutPrefersTheDefinitionThenTheConfiguration(t *testing.T) {
	t.Parallel()
	require.Equal(t, 90*time.Second, subAgentTimeout(600, 90))
	require.Equal(t, 10*time.Minute, subAgentTimeout(600, 0))
	require.Equal(t, defaultSubAgentTimeout, subAgentTimeout(0, 0))
	require.Zero(t, subAgentTimeout(-1, 0), "a negative setting removes the limit")
	require.Equal(t, 45*time.Second, subAgentTimeout(-1, 45), "a definition's own limit still applies")
}

func TestSubagentStepCapOnlyTightens(t *testing.T) {
	t.Parallel()
	require.Equal(t, 40, subagentStepCap(40, 0))
	require.Equal(t, 25, subagentStepCap(0, 25))
	require.Equal(t, 25, subagentStepCap(40, 25))
	require.Equal(t, 40, subagentStepCap(40, 90), "it never loosens the global cap")
	require.Zero(t, subagentStepCap(0, 0))
}
