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

func TestPipelineExposesDesktopEvidenceWithoutExtraCalls(t *testing.T) {
	t.Parallel()
	calls := 0
	tool := NewToolPipeline(func(_ context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		calls++
		var p ComputerParams
		require.NoError(t, json.Unmarshal([]byte(call.Input), &p))
		if p.Action == "hotkey" {
			return fantasy.NewTextResponse("Sent shortcut"), nil
		}
		return fantasy.NewTextResponse(`{"result":[{"window_id":"22","foreground":true}]}`), nil
	})
	p := PipelineParams{Steps: []PipelineStep{
		{ID: "close", Tool: "computer", Arguments: map[string]any{"action": "hotkey", "modifiers": "alt", "key": "f4", "automation": map[string]any{"window_id": "11"}}},
		{ID: "verify", Tool: "computer", Arguments: map[string]any{"action": "windows"}},
	}, Return: []string{"verify"}}
	data, err := json.Marshal(p)
	require.NoError(t, err)
	r, err := tool.Run(t.Context(), fantasy.ToolCall{Input: string(data)})
	require.NoError(t, err)
	var output []struct {
		State struct {
			Foreground string   `json:"foreground_window_id"`
			Absent     []string `json:"absent_closed_window_ids"`
		} `json:"desktop_state"`
	}
	require.NoError(t, json.Unmarshal([]byte(r.Content), &output))
	require.Len(t, output, 1)
	require.Equal(t, "22", output[0].State.Foreground)
	require.Equal(t, []string{"11"}, output[0].State.Absent)
	require.Equal(t, 2, calls, "Reuse the requested verification; do not add enumeration")
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

func TestPipelinePreservesSelectedImageAndTextResults(t *testing.T) {
	t.Parallel()
	for _, selected := range []string{"first", "last"} {
		tool := NewToolPipeline(func(_ context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if call.Name == "view" {
				return fantasy.NewTextResponse("observed target"), nil
			}
			return fantasy.NewImageResponse([]byte(call.Input), "image/png"), nil
		})
		p := PipelineParams{Steps: []PipelineStep{
			{ID: "first", Tool: "computer", Arguments: map[string]any{"action": "screenshot", "full_res": true}},
			{ID: "info", Tool: "view"},
			{ID: "last", Tool: "computer", Arguments: map[string]any{"action": "screenshot"}},
		}, Return: []string{selected, "info"}}
		input, err := json.Marshal(p)
		require.NoError(t, err)
		response, err := tool.Run(t.Context(), fantasy.ToolCall{Input: string(input)})
		require.NoError(t, err)
		require.Equal(t, "image", response.Type)
		require.Equal(t, "image/png", response.MediaType)
		require.NotEmpty(t, response.Data)
		require.Contains(t, response.Content, "observed target")
		require.Contains(t, response.Content, `"image_attached":true`)
		if selected == "first" {
			require.Contains(t, string(response.Data), "full_res")
		} else {
			require.NotContains(t, string(response.Data), "full_res")
		}
	}
}

func TestPipelineDefaultImageReturnKeepsLatestAndLabelsEarlierImage(t *testing.T) {
	t.Parallel()
	tool := NewToolPipeline(func(_ context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		return fantasy.NewImageResponse([]byte(call.Input), "image/png"), nil
	})
	input := `{"steps":[{"id":"before","tool":"computer","arguments":{"action":"screenshot"}},{"id":"after","tool":"computer","arguments":{"action":"capture_window"}}]}`
	response, err := tool.Run(t.Context(), fantasy.ToolCall{Input: input})
	require.NoError(t, err)
	require.Equal(t, "image", response.Type)
	require.Contains(t, string(response.Data), "capture_window")
	require.Contains(t, response.Content, `"image_omitted":true`)
	require.Contains(t, response.Content, `"image_attached":true`)
}

func TestPipelineDesktopBatchStopsOnWrongWindow(t *testing.T) {
	t.Parallel()
	b := &foregroundComputerBackend{foreground: "22"}
	s := &computerToolState{backend: b}
	calls := 0
	tool := NewToolPipeline(func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		calls++
		var params ComputerParams
		require.NoError(t, json.Unmarshal([]byte(call.Input), &params))
		return s.runComputerAction(ctx, params.Action, params)
	})
	input := `{"steps":[{"id":"key","tool":"computer","arguments":{"action":"key","key":"escape","automation":{"window_id":"11"}}},{"id":"text","tool":"computer","arguments":{"action":"type","text":"must not be sent","automation":{"window_id":"11"}}}]}`
	response, err := tool.Run(t.Context(), fantasy.ToolCall{Input: input})
	require.NoError(t, err)
	require.True(t, response.IsError)
	require.Contains(t, response.Content, "wrong_window")
	require.Equal(t, 1, calls)
	require.Empty(t, b.keys)
	require.Empty(t, b.typed)
}

func TestPipelineRejectsOversizedSelectedImageBeforeNextAction(t *testing.T) {
	t.Parallel()
	calls := 0
	tool := NewToolPipeline(func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		calls++
		return fantasy.NewImageResponse(make([]byte, 8*1024*1024+1), "image/png"), nil
	})
	response, err := tool.Run(t.Context(), fantasy.ToolCall{Input: `{"steps":[{"id":"observe","tool":"computer"},{"id":"input","tool":"computer"}]}`})
	require.NoError(t, err)
	require.True(t, response.IsError)
	require.Contains(t, response.Content, "8 MiB")
	require.Equal(t, 1, calls)
}
