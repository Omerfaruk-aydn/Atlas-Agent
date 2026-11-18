package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSubAgentDepthCountsEachLevel(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	require.Zero(t, subAgentDepth(ctx))
	ctx = withDeeperSubAgent(withDeeperSubAgent(ctx))
	require.Equal(t, 2, subAgentDepth(ctx))
}

func TestSpawningToolsIncludeVibe(t *testing.T) {
	t.Parallel()
	require.Contains(t, subagentSpawningTools, VibeToolName)
}
