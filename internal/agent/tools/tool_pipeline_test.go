package tools

import (
	"context"
	"encoding/json"
	"testing"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func TestPipelineItemsAndSelectiveReturn(t *testing.T) {
	t.Parallel()
	calls := []fantasy.ToolCall{}
	tool := NewToolPipeline(func(_ context.Context, c fantasy.ToolCall) (fantasy.ToolResponse, error) {
		calls = append(calls, c)
		return fantasy.NewTextResponse(c.Input), nil
	})
	params := PipelineParams{Steps: []PipelineStep{{ID: "read", Tool: "view", Items: []string{"a.go", "b.go"}, Arguments: map[string]any{"file_path": "$item"}}, {ID: "verify", Tool: "test_run", IfSuccess: "read", Arguments: map[string]any{"filter": "safe"}}}, Return: []string{"verify"}}
	data, err := json.Marshal(params)
	require.NoError(t, err)
	response, err := tool.Run(t.Context(), fantasy.ToolCall{ID: "pipeline", Name: "tool_pipeline", Input: string(data)})
	require.NoError(t, err)
	require.False(t, response.IsError)
	require.Len(t, calls, 3)
	require.Contains(t, calls[0].Input, "a.go")
	require.Contains(t, calls[1].Input, "b.go")
	require.NotContains(t, response.Content, "a.go")
}

func TestPipelineStopsAtDenialAndRefusesRecursion(t *testing.T) {
	t.Parallel()
	calls := 0
	tool := NewToolPipeline(func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		calls++
		r := fantasy.NewTextErrorResponse("denied")
		r.StopTurn = true
		return r, nil
	})
	p := PipelineParams{Steps: []PipelineStep{{ID: "a", Tool: "edit"}, {ID: "b", Tool: "edit"}}}
	data, _ := json.Marshal(p)
	r, err := tool.Run(t.Context(), fantasy.ToolCall{Input: string(data)})
	require.NoError(t, err)
	require.True(t, r.StopTurn)
	require.Equal(t, 1, calls)
	p.Steps[0].Tool = "tool_pipeline"
	data, _ = json.Marshal(p)
	r, err = tool.Run(t.Context(), fantasy.ToolCall{Input: string(data)})
	require.NoError(t, err)
	require.True(t, r.IsError)
	require.Equal(t, 1, calls)
}
