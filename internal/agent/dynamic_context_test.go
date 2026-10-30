package agent

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func TestDynamicContextIsTransientAndDoesNotChangeStablePrompt(t *testing.T) {
	t.Parallel()
	stableBefore := "stable instructions and tool contract"
	history := []fantasy.Message{fantasy.NewUserMessage("continue")}

	first := withDynamicContext(history, "<goal>first goal</goal>")
	second := withDynamicContext(first, "<goal>changed goal</goal>")

	require.Len(t, second, 2, "the refreshed block must not accumulate")
	require.Equal(t, history[0], second[0], "tool-call and result history is untouched")
	text := second[1].Content[0].(fantasy.TextPart).Text
	require.Contains(t, text, "changed goal")
	require.NotContains(t, text, "first goal")
	require.NotContains(t, stableBefore, "changed goal")
	require.Equal(t, history, withDynamicContext(second, ""), "empty state leaves the original history")
}

func TestDynamicContextDoesNotWrapArbitrarySystemMessages(t *testing.T) {
	t.Parallel()
	stable := fantasy.NewSystemMessage("stable")
	dynamic := fantasy.NewSystemMessage(dynamicContextMarker + "not a complete marker")
	out := withDynamicContext([]fantasy.Message{stable, dynamic}, "state")
	require.Equal(t, stable, out[0])
	require.Equal(t, dynamic, out[1])
}
