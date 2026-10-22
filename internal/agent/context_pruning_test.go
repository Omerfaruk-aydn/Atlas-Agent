package agent

import (
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func TestAdaptivePruningRunsBeforeOverflowAndPreservesDiagnostics(t *testing.T) {
	t.Parallel()
	failure := "test output\n--- FAIL: TestParser\nparser.go:42: error: unexpected token\n" + strings.Repeat("noise\n", 3000)
	prompt := conversation(5, 100)
	prompt = append(prompt[:3], append(toolRound("failure", failure), prompt[3:]...)...)
	window := int64(float64(estimateMessageTokens(prompt)) / 0.85)

	fitted, report, err := fitCall(modelWithWindow(window, 100), fantasy.Call{Prompt: prompt, MaxOutputTokens: outputCap(100)})
	require.NoError(t, err)
	require.Positive(t, report.Elided)
	require.False(t, report.OutputCapped)

	var reduced string
	for _, msg := range fitted.Prompt {
		for _, part := range msg.Content {
			if result, ok := toolResultPart(part); ok && result.ToolCallID == "failure" {
				reduced, _ = toolResultText(result.Output)
			}
		}
	}
	require.Contains(t, reduced, "--- FAIL: TestParser")
	require.Contains(t, reduced, "parser.go:42: error: unexpected token")
	require.NotContains(t, reduced, strings.Repeat("noise\n", 100))
	require.Len(t, fitted.Prompt, len(prompt))
}

func TestAdaptivePruningLeavesSmallAndRecentRequestsAlone(t *testing.T) {
	t.Parallel()
	prompt := conversation(2, 3000)
	fitted, report, err := fitCall(modelWithWindow(200_000, 1000), fantasy.Call{Prompt: prompt, MaxOutputTokens: outputCap(1000)})
	require.NoError(t, err)
	require.Zero(t, report.Elided)
	require.Equal(t, prompt, fitted.Prompt)
}
