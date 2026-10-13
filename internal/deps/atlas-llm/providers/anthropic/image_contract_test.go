package anthropic

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"testing"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

// A tool-result image at Atlas's 2 MiB raw delivery limit reaches the
// Anthropic request as one base64 image block, byte for byte.
func TestToolResultImageAtDeliveryLimitIsSentIntact(t *testing.T) {
	t.Parallel()
	raw := bytes.Repeat([]byte{0x89, 'P', 'N', 'G'}, 2*1024*1024/4)
	b64 := base64.StdEncoding.EncodeToString(raw)
	require.Len(t, b64, 2_796_204)
	prompt := fantasy.Prompt{
		fantasy.NewUserMessage("show it"),
		{Role: fantasy.MessageRoleAssistant, Content: []fantasy.MessagePart{fantasy.ToolCallPart{ToolCallID: "c1", ToolName: "view", Input: `{}`}}},
		{Role: fantasy.MessageRoleTool, Content: []fantasy.MessagePart{fantasy.ToolResultPart{ToolCallID: "c1", Output: fantasy.ToolResultOutputContentMedia{Data: b64, MediaType: "image/png", Text: "note"}}}},
	}
	_, messages, warnings := toPrompt(prompt, false)
	require.Empty(t, warnings)
	encoded, err := json.Marshal(messages)
	require.NoError(t, err)
	require.Equal(t, 1, bytes.Count(encoded, []byte(b64)))
	require.Contains(t, string(encoded), `"media_type":"image/png"`)
}
