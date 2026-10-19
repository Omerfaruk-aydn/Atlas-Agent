package message

import (
	"testing"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/providers/google"
	"github.com/stretchr/testify/require"
)

// A step's thinking signature and its tool call signatures are kept apart
// and survive saving the message, so each goes back with its own part.
func TestThoughtSignaturesStaySeparate(t *testing.T) {
	t.Parallel()

	thinking := google.EncodeSignature([]byte{0x0a, 0xff, 0x01})
	first := google.EncodeSignature([]byte{0x0b, 0xfe, 0x02})
	m := Message{Role: Assistant, Parts: []ContentPart{
		ToolCall{ID: "call-1", Name: "read", Input: "{}", Finished: true},
		ToolCall{ID: "call-2", Name: "read", Input: "{}", Finished: true},
	}}
	m.AppendReasoningContent("thinking")
	m.AppendThoughtSignature(thinking, "")
	m.AppendThoughtSignature(first, "call-1")

	data, err := marshalParts(m.Parts)
	require.NoError(t, err)
	m.Parts, err = unmarshalParts(data)
	require.NoError(t, err)

	reasoning := m.ReasoningContent()
	require.Equal(t, thinking, reasoning.ThoughtSignature)
	require.Equal(t, map[string]string{"call-1": first}, reasoning.ToolSignatures)

	sent := map[string]string{}
	for _, msg := range m.ToAIMessage() {
		for _, part := range msg.Content {
			if r, ok := fantasy.AsMessagePart[fantasy.ReasoningPart](part); ok {
				metadata := google.GetReasoningMetadata(r.ProviderOptions)
				require.NotNil(t, metadata)
				sent[metadata.ToolID] = metadata.Signature
			}
		}
	}
	require.Equal(t, map[string]string{"": thinking, "call-1": first}, sent)
}
