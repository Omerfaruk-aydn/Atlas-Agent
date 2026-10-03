package tools

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func TestMeasuredChecksCannotPassWithoutObservedMetadata(t *testing.T) {
	t.Parallel()
	for _, tool := range []string{"api_probe", "migration_rehearse", "visual_diff", "mutation_test"} {
		response := fantasy.NewTextResponse(`{"passed":true}`)
		require.False(t, ToolSucceeded(tool, response, nil), tool)
		require.False(t, ToolOutcomeObserved(tool, response, nil), tool)
		response = fantasy.WithResponseMetadata(response, MeasuredCheckMetadata{Observed: true, Passed: true})
		require.True(t, ToolSucceeded(tool, response, nil), tool)
		require.True(t, ToolOutcomeObserved(tool, response, nil), tool)
		response = fantasy.WithResponseMetadata(response, MeasuredCheckMetadata{Observed: true})
		require.False(t, ToolSucceeded(tool, response, nil), tool)
		require.True(t, ToolOutcomeObserved(tool, response, nil), tool)
	}
}
