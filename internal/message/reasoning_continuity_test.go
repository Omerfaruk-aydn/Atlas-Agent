package message

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/providers/anthropic"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/providers/google"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/providers/openai"
	"github.com/stretchr/testify/require"
)

// The streaming sequence the agent performs at the end of a reasoning block
// must keep every protocol field. FinishThinking used to rebuild the part
// and silently drop Google thought signatures and encrypted Responses state.
func TestReasoningEndSequencePreservesProtocolState(t *testing.T) {
	t.Parallel()

	m := Message{Role: Assistant}
	m.AppendReasoningContent("step one")
	m.AppendReasoningContent(" and two")
	m.SetReasoningOrigin("openai", "gpt-test")
	m.AppendThoughtSignature("gsig", "call-1")
	m.AppendReasoningSignature("asig")
	data := &openai.ResponsesReasoningMetadata{ItemID: "rs_1", EncryptedContent: new("enc")}
	m.SetReasoningResponsesData(data)
	m.FinishThinking()

	r := m.ReasoningContent()
	require.Equal(t, "step one and two", r.Thinking)
	require.Equal(t, map[string]string{"call-1": "gsig"}, r.ToolSignatures)
	require.Equal(t, "asig", r.Signature)
	require.Same(t, data, r.ResponsesData)
	require.Equal(t, "openai", r.Provider)
	require.Equal(t, "gpt-test", r.Model)
	require.NotZero(t, r.StartedAt)
	require.NotZero(t, r.FinishedAt)
}

// A signature-only reasoning block (models that omit thinking text) must be
// echoed back, otherwise prior-turn reasoning state is lost.
func TestSignatureOnlyReasoningIsReplayed(t *testing.T) {
	t.Parallel()

	m := Message{Role: Assistant, Parts: []ContentPart{
		ReasoningContent{Signature: "sig"},
		TextContent{Text: "answer"},
	}}
	msgs := m.ToAIMessage()
	require.Len(t, msgs, 1)
	var found bool
	for _, part := range msgs[0].Content {
		if part.GetType() != fantasy.ContentTypeReasoning {
			continue
		}
		found = true
		meta := anthropic.GetReasoningMetadata(part.Options())
		require.NotNil(t, meta)
		require.Equal(t, "sig", meta.Signature)
	}
	require.True(t, found)

	g := Message{Role: Assistant, Parts: []ContentPart{ReasoningContent{ThoughtSignature: "g"}, TextContent{Text: "x"}}}
	part := g.ToAIMessage()[0].Content[1]
	require.Equal(t, fantasy.ContentTypeReasoning, part.GetType())
	require.IsType(t, &google.ReasoningMetadata{}, part.Options()[google.Name])
}

func TestReasoningOriginRoundTripsThroughStorage(t *testing.T) {
	t.Parallel()

	data, err := marshalParts([]ContentPart{ReasoningContent{Thinking: "t", Provider: "claude", Model: "m"}})
	require.NoError(t, err)
	parts, err := unmarshalParts(data)
	require.NoError(t, err)
	require.Equal(t, ReasoningContent{Thinking: "t", Provider: "claude", Model: "m"}, parts[0])
}
