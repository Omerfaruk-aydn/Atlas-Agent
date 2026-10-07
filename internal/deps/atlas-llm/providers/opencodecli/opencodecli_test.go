package opencodecli

import (
	"testing"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func testCall() fantasy.Call {
	return fantasy.Call{Prompt: fantasy.Prompt{fantasy.NewUserMessage("Find a file")}, Tools: []fantasy.Tool{fantasy.FunctionTool{Name: "view", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}, "required": []string{"path"}, "additionalProperties": false}}}}
}

func TestReplyValidation(t *testing.T) {
	t.Parallel()
	call := testCall()
	r, err := decodeReply(`{"text":"Inspecting","tool_calls":[{"name":"view","arguments":{"path":"a.go"}}]}`, call)
	require.NoError(t, err)
	require.Equal(t, fantasy.FinishReasonToolCalls, r.FinishReason)
	require.False(t, r.Content.ToolCalls()[0].ProviderExecuted)
	for _, reply := range []string{
		`{"text":"","tool_calls":[{"name":"bash","arguments":{}}]}`,
		`{"text":"","tool_calls":[{"name":"view","arguments":{}}]}`,
		`{"text":"","tool_calls":[{"name":"view","arguments":{"path":123}}]}`,
		`{"text":"","tool_calls":[{"name":"view","arguments":"{}"}]}`,
		`{"text":"OK","tool_calls":[]} {"text":"other"}`,
		`{"text":"","tool_calls":[]}`,
		`{"text":"OK","tool_calls":[],"execute":true}`,
	} {
		_, err := decodeReply(reply, call)
		require.Error(t, err, reply)
	}
	none := fantasy.ToolChoiceNone
	call.ToolChoice = &none
	_, err = decodeReply(`{"text":"","tool_calls":[{"name":"view","arguments":{"path":"a"}}]}`, call)
	require.Error(t, err)
	required := fantasy.ToolChoiceRequired
	call.ToolChoice = &required
	_, err = decodeReply(`{"text":"Done","tool_calls":[]}`, call)
	require.Error(t, err)
}
