package google

import (
	"bytes"
	"testing"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

// Gemini receives tool images as user inline data (Atlas moves them out of
// the tool result); a 2 MiB image must arrive as the exact bytes.
func TestUserImageAtDeliveryLimitIsInlineData(t *testing.T) {
	t.Parallel()
	raw := bytes.Repeat([]byte{0x89, 'P', 'N', 'G'}, 2*1024*1024/4)
	prompt := fantasy.Prompt{{Role: fantasy.MessageRoleUser, Content: []fantasy.MessagePart{
		fantasy.TextPart{Text: "tool image"},
		fantasy.FilePart{Data: raw, MediaType: "image/png", Filename: "tool-result-c1"},
	}}}
	_, contents, warnings := toGooglePrompt(prompt, false)
	require.Empty(t, warnings)
	require.Len(t, contents, 1)
	var found bool
	for _, part := range contents[0].Parts {
		if part.InlineData != nil {
			found = true
			require.Equal(t, "image/png", part.InlineData.MIMEType)
			require.Equal(t, raw, part.InlineData.Data)
		}
	}
	require.True(t, found)
}
