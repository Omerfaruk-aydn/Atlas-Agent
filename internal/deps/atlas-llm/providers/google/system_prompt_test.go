package google

import (
	"testing"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func TestConsecutiveSystemMessagesPreserveToolProtocols(t *testing.T) {
	prompt := fantasy.Prompt{
		fantasy.NewSystemMessage("base instructions"),
		fantasy.NewSystemMessage("tool routing protocol"),
		fantasy.NewUserMessage("task"),
		fantasy.NewSystemMessage("late system message"),
	}
	system, contents, _ := toGooglePrompt(prompt, false)
	require.NotNil(t, system)
	var text string
	for _, part := range system.Parts {
		text += part.Text
	}
	require.Contains(t, text, "base instructions")
	require.Contains(t, text, "tool routing protocol")
	require.NotContains(t, text, "late system message")
	require.Len(t, contents, 1)
}
