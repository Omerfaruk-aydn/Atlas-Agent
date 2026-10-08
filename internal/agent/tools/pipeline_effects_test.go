package tools

import (
	"context"
	"encoding/json"
	"testing"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/outcome"
	"github.com/stretchr/testify/require"
)

func TestPipelineAggregateKeepsMutationScope(t *testing.T) {
	t.Parallel()
	invoke := func(_ context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		if call.Name == "write" {
			return withToolOutcome(fantasy.NewTextResponse("Written"), ToolOutcome{Dispatch: "sent", Mutation: true, Subjects: []string{"file:a"}}), nil
		}
		return withToolOutcome(fantasy.NewTextResponse(`{"passed":true}`), ToolOutcome{Dispatch: "none", Verified: true, Subjects: []string{"desktop:22"}, Kind: outcome.KindOutput}), nil
	}
	response, _ := runPipelineSteps(t, invoke, PipelineParams{Steps: []PipelineStep{
		{ID: "write", Tool: "write", Arguments: map[string]any{"file_path": "a", "content": "value"}},
		{ID: "read", Tool: "computer", Arguments: map[string]any{"action": "assert", "automation": map[string]any{"window_id": "22", "name": "Result", "condition": "value", "expected": "value"}}, RequirePassed: true},
	}})
	require.False(t, response.IsError, response.Content)
	native, ok := ResponseOutcome(response)
	require.True(t, ok)
	require.Equal(t, "sent", native.Dispatch)
	require.True(t, native.Mutation)
	require.Equal(t, []string{"file:a"}, native.Subjects)
	require.False(t, native.Verified, "a last read in another target must not verify the entire pipeline")
}

func TestPipelineAggregatePreservesPauseAndMedia(t *testing.T) {
	t.Parallel()
	calls := 0
	tool := NewToolPipeline(func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		calls++
		if calls == 1 {
			return withToolOutcome(fantasy.NewTextResponse("Written"), ToolOutcome{Dispatch: "sent", Mutation: true, Subjects: []string{"file:a"}}), nil
		}
		response := fantasy.NewImageResponse([]byte("image-data"), "image/png")
		response.Content, response.StopTurn = "Waiting for permission", true
		response.Metadata = `{"question":{"id":"q","options":["yes","no"]},"permission":{"id":"p"},"atlas_outcome":{"dispatch":"none","mutation":true}}`
		return response, nil
	})
	input := `{"steps":[{"id":"first","tool":"write","arguments":{"file_path":"a","content":"value"}},{"id":"second","tool":"write","arguments":{"file_path":"b","content":"value"}},{"id":"after","tool":"write","arguments":{"file_path":"c","content":"value"}}]}`
	response, err := tool.Run(t.Context(), fantasy.ToolCall{ID: "pause", Name: ToolPipelineName, Input: input})
	require.NoError(t, err)
	require.Equal(t, 2, calls, "nothing after the pause may run")
	require.True(t, response.StopTurn)
	require.Equal(t, []byte("image-data"), response.Data)
	require.Equal(t, "image/png", response.MediaType)
	require.Contains(t, response.Metadata, `"question":{"id":"q"`)
	require.Contains(t, response.Metadata, `"permission":{"id":"p"}`)
	var metadata struct {
		Child ToolOutcome `json:"pipeline_child_atlas_outcome"`
	}
	require.NoError(t, json.Unmarshal([]byte(response.Metadata), &metadata))
	require.Equal(t, "none", metadata.Child.Dispatch)
	native, ok := ResponseOutcome(response)
	require.True(t, ok)
	require.Equal(t, "sent", native.Dispatch, "the earlier write remains accounted for while the next input waits")
	require.Equal(t, []string{"file:a"}, native.Subjects)
}

func TestPipelineAggregateRefusalVersusUnknown(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, metadata string
		knownNone      bool
	}{
		{"missing_native_evidence", "", false},
		{"invalid_native_evidence", `{"atlas_outcome":{"dispatch":"invalid","mutation":false,"subjects":["untrusted"]}}`, false},
		{"refused_before_dispatch", `{"atlas_outcome":{"dispatch":"none","mutation":true}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			response, _ := runPipelineSteps(t, func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
				response := fantasy.NewTextErrorResponse("Write could not finish")
				response.Metadata = tc.metadata
				return response, nil
			}, PipelineParams{Steps: []PipelineStep{{ID: "write", Tool: "write", Arguments: map[string]any{"file_path": "a", "content": "value"}}}})
			require.True(t, response.IsError)
			native, ok := ResponseOutcome(response)
			require.True(t, ok)
			require.Equal(t, !tc.knownNone, native.Mutation)
			require.Equal(t, map[bool]string{false: "unknown", true: "none"}[tc.knownNone], native.Dispatch)
			if !tc.knownNone {
				require.Equal(t, []string{"*"}, native.Subjects)
			}
		})
	}
}
