package openai

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"testing"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

// OpenAI chat tool messages cannot carry images, so a tool-result image is
// re-sent as a user data URL; at the 2 MiB raw limit it must arrive intact.
func TestToolResultImageAtDeliveryLimitBecomesUserDataURL(t *testing.T) {
	t.Parallel()
	raw := bytes.Repeat([]byte{0xff, 0xd8, 0xff, 0xe0}, 2*1024*1024/4)
	b64 := base64.StdEncoding.EncodeToString(raw)
	prompt := fantasy.Prompt{
		fantasy.NewUserMessage("show it"),
		{Role: fantasy.MessageRoleAssistant, Content: []fantasy.MessagePart{fantasy.ToolCallPart{ToolCallID: "c1", ToolName: "view", Input: `{}`}}},
		{Role: fantasy.MessageRoleTool, Content: []fantasy.MessagePart{fantasy.ToolResultPart{ToolCallID: "c1", Output: fantasy.ToolResultOutputContentMedia{Data: b64, MediaType: "image/jpeg", Text: "note"}}}},
	}
	messages, warnings := DefaultToPrompt(prompt, "", "")
	require.Empty(t, warnings)
	encoded, err := json.Marshal(messages)
	require.NoError(t, err)
	require.Contains(t, string(encoded), "data:image/jpeg;base64,"+b64)
	// The tool message itself carries only the text.
	require.Contains(t, string(encoded), `"tool_call_id":"c1"`)
}
