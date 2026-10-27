package fantasy

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

type validatedInput struct {
	Mode  string `json:"mode" enum:"fast,safe" description:"how to run"`
	Count int    `json:"count" description:"how many"`
	Tags  []struct {
		Name string `json:"name" description:"tag"`
	} `json:"tags,omitempty" description:"tags"`
}

func TestToolCallInputFollowsTheAdvertisedSchema(t *testing.T) {
	t.Parallel()

	tool := NewAgentTool("run", "run it", func(context.Context, validatedInput, ToolCall) (ToolResponse, error) {
		return NewTextResponse("ok"), nil
	})
	a := &agent{}
	call := func(input string) error {
		return a.validateToolCall(ToolCallContent{ToolName: "run", Input: input}, []AgentTool{tool}, nil)
	}

	require.NoError(t, call(`{"mode":"fast","count":2}`))
	require.NoError(t, call(`{"mode":"safe","count":2,"extra":"allowed here"}`), "unknown fields are left to the tool's own contract")

	require.ErrorContains(t, call(`{"mode":"turbo","count":2}`), "mode", "an enum violation names the field")
	require.ErrorContains(t, call(`{"mode":"fast","count":"two"}`), "count", "a wrong type names the field")
	require.ErrorContains(t, call(`{"mode":"fast","count":2,"tags":[{"name":7}]}`), "name", "nested types are checked")
	require.ErrorContains(t, call(`{"count":2}`), "missing required parameter: mode")
}

type rawInput struct {
	Name  string          `json:"name" description:"name"`
	Input json.RawMessage `json:"input" description:"anything"`
	Extra any             `json:"extra,omitempty" description:"anything"`
}

// Generation describes RawMessage and interface fields with placeholder
// types; a correct value for them must never be refused.
func TestPlaceholderTypesAcceptAnyValue(t *testing.T) {
	t.Parallel()

	tool := NewAgentTool("raw", "raw", func(context.Context, rawInput, ToolCall) (ToolResponse, error) {
		return NewTextResponse("ok"), nil
	})
	a := &agent{}
	for _, input := range []string{
		`{"name":"a","input":{"action":"type","text":"x"}}`,
		`{"name":"a","input":[1,2],"extra":"text"}`,
		`{"name":"a","input":"plain","extra":7}`,
	} {
		require.NoError(t, a.validateToolCall(ToolCallContent{ToolName: "raw", Input: input}, []AgentTool{tool}, nil), input)
	}
	require.ErrorContains(t, a.validateToolCall(ToolCallContent{ToolName: "raw", Input: `{"name":5,"input":{}}`}, []AgentTool{tool}, nil), "name")
}
