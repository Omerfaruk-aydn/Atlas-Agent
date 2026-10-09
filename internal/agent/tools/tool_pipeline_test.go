package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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
	response, err := tool.Run(t.Context(), fantasy.ToolCall{Input: `{"steps":[{"id":"observe","tool":"computer","arguments":{"action":"screenshot"}},{"id":"input","tool":"computer","arguments":{"action":"type","text":"must not be sent","automation":{"window_id":"11"}}}]}`})
	require.NoError(t, err)
	require.True(t, response.IsError)
	require.Contains(t, response.Content, "8 MiB")
	require.Equal(t, 1, calls)
}

func TestPipelineContinueDoesNotTolerateUnknownOutcomeOrStaleOutput(t *testing.T) {
	t.Parallel()
	steps := []PipelineStep{
		{ID: "send", Tool: "mcp_x_send", Arguments: map[string]any{}, OnError: "continue"},
		{ID: "after", Tool: "mcp_x_read", Arguments: map[string]any{}},
	}
	var calls []string
	invoke := func(_ context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		calls = append(calls, call.Name)
		if call.Name == "mcp_x_send" {
			return fantasy.NewTextErrorResponse(`outcome unknown {"dispatch":"unknown"}`), nil
		}
		return fantasy.NewTextResponse(`{}`), nil
	}
	run, err := executePipeline(context.Background(), steps, nil, fantasy.ToolCall{ID: "c"}, invoke)
	require.NoError(t, err)
	require.True(t, run.failed, "unknown outcome must stop the pipeline even with on_error=continue")
	require.Equal(t, []string{"mcp_x_send"}, calls, "dependent work must not run on an unobserved state")
	require.Equal(t, StepUncertain, run.results[0].Status)

	steps = []PipelineStep{
		{ID: "item", Tool: "t", Arguments: map[string]any{"v": "$item"}, Items: []string{"a", "b"}, OnError: "continue"},
		{ID: "use", Tool: "u", Arguments: map[string]any{}, Bindings: map[string]string{"text": "item#/v"}},
	}
	n := 0
	invoke = func(_ context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		if call.Name == "t" {
			n++
			if n == 2 {
				return fantasy.NewTextErrorResponse("second iteration failed"), nil
			}
			return fantasy.NewTextResponse(`{"v":"from-first"}`), nil
		}
		return fantasy.NewTextResponse(`{}`), nil
	}
	run, err = executePipeline(context.Background(), steps, nil, fantasy.ToolCall{ID: "c"}, invoke)
	require.NoError(t, err)
	last := run.results[len(run.results)-1]
	require.Equal(t, "use", last.ID)
	require.Equal(t, StepFailed, last.Status, "binding must not read the previous iteration's output")
	require.Contains(t, last.Content, "nothing was sent", last.Content)
}

func TestPipelineUnknownOutcomeAllowsOnlyReadOnlyRecoveryAndRemainsFailed(t *testing.T) {
	t.Parallel()
	for _, recovery := range []PipelineStep{
		{Tool: ViewToolName, Arguments: map[string]any{"file_path": "receipt.json"}},
		{Tool: BrowserToolName, Arguments: map[string]any{"action": "observe"}},
		{Tool: ComputerToolName, Arguments: map[string]any{"action": "inspect"}},
		{Tool: "mcp_x_read", Arguments: map[string]any{}},
		{Tool: "bash", Arguments: map[string]any{"command": "cat receipt.json"}},
		{Tool: "edit", Arguments: map[string]any{}},
		{Tool: BrowserToolName, Arguments: map[string]any{"action": "click", "ref": "e1"}},
		{Tool: ComputerToolName, Arguments: map[string]any{"action": "key", "key": "enter"}},
	} {
		t.Run(recovery.Tool+"/"+fmt.Sprint(recovery.Arguments["action"]), func(t *testing.T) {
			t.Parallel()
			recovery.ID, recovery.IfFailed = "recover", "send"
			steps := []PipelineStep{
				{ID: "send", Tool: "mcp_x_send", Arguments: map[string]any{}},
				recovery,
				{ID: "after", Tool: "edit", Arguments: map[string]any{}},
			}
			var called []string
			run, err := executePipeline(t.Context(), steps, nil, fantasy.ToolCall{ID: "p"}, func(_ context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
				called = append(called, call.ID)
				if call.ID == "p/send/0" {
					resp := fantasy.NewTextErrorResponse("Request result was lost")
					resp.Metadata = `{"dispatch": "unknown"}`
					return resp, nil
				}
				return fantasy.NewTextResponse(`{"reconciled":true,"passed":true}`), nil
			})
			require.NoError(t, err)
			require.True(t, run.failed)
			require.Equal(t, StepUncertain, run.results[0].Status)
			require.Equal(t, StepSkipped, run.results[2].Status)
			if pipelineReadOnlyRecovery(recovery) {
				require.Equal(t, []string{"p/send/0", "p/recover/0"}, called)
				require.Equal(t, StepSucceeded, run.results[1].Status)
			} else {
				require.Equal(t, []string{"p/send/0"}, called)
				require.Contains(t, run.results[1].Reason, "only read-only recovery")
			}
			resp, err := pipelineResponse(run, nil)
			require.NoError(t, err)
			require.True(t, resp.IsError, "a successful fallback is not trusted reconciliation")
		})
	}
}

func TestPipelineKnownRefusalStillResumesAfterSuccessfulFallback(t *testing.T) {
	t.Parallel()
	steps := []PipelineStep{
		{ID: "primary", Tool: "edit", Arguments: map[string]any{}},
		{ID: "fallback", Tool: "edit", IfFailed: "primary", Arguments: map[string]any{}},
		{ID: "after", Tool: "edit", Arguments: map[string]any{}},
	}
	var called []string
	run, err := executePipeline(t.Context(), steps, nil, fantasy.ToolCall{ID: "p"}, func(_ context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		called = append(called, call.ID)
		if call.ID == "p/primary/0" {
			return fantasy.NewTextErrorResponse(`refused {"dispatch":"none"}`), nil
		}
		return fantasy.NewTextResponse("done"), nil
	})
	require.NoError(t, err)
	require.False(t, run.failed)
	require.Equal(t, []string{"p/primary/0", "p/fallback/0", "p/after/0"}, called)
}

func TestPipelineSuccessfulNonJSONIterationClearsBindingsAndConditions(t *testing.T) {
	t.Parallel()
	for _, latest := range []fantasy.ToolResponse{
		fantasy.NewTextResponse("plain text"),
		fantasy.NewImageResponse([]byte("image"), "image/png"),
		fantasy.NewTextResponse(`{"value":"` + strings.Repeat("a", 64*1024) + `"}`),
	} {
		steps := []PipelineStep{
			{ID: "item", Tool: "view", Arguments: map[string]any{"file_path": "$item"}, Items: []string{"first", "last"}},
			{ID: "condition", Tool: "edit", Arguments: map[string]any{}, When: &PipelineCondition{Source: "item#/value", Equals: new("stale")}},
			{ID: "bound", Tool: "edit", Arguments: map[string]any{}, Bindings: map[string]string{"text": "item#/value"}},
		}
		var called []string
		run, err := executePipeline(t.Context(), steps, nil, fantasy.ToolCall{ID: "p"}, func(_ context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			called = append(called, call.ID)
			if call.ID == "p/item/0" {
				return fantasy.NewTextResponse(`{"value":"stale"}`), nil
			}
			return latest, nil
		})
		require.NoError(t, err)
		require.Equal(t, []string{"p/item/0", "p/item/1"}, called)
		require.Equal(t, StepSkipped, run.results[2].Status)
		require.Contains(t, run.results[2].Reason, "unreadable")
		require.Equal(t, StepFailed, run.results[3].Status)
		require.Equal(t, "none", run.results[3].Dispatch)
		require.Contains(t, run.results[3].Content, "has no JSON result")
	}
}

func TestPipelineResumePreservesPartialItemProgressAndDispatch(t *testing.T) {
	t.Parallel()
	for _, dispatch := range []string{"none", "sent", "partial", "unknown", ""} {
		t.Run(dispatch, func(t *testing.T) {
			t.Parallel()
			steps := []PipelineStep{
				{ID: "batch", Tool: "mcp_x_send", Arguments: map[string]any{"value": "$item"}, Items: []string{"a", "b", "c"}},
				{ID: "after", Tool: "edit", Arguments: map[string]any{}},
			}
			var called []string
			run, err := executePipeline(t.Context(), steps, nil, fantasy.ToolCall{ID: "p"}, func(_ context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
				called = append(called, call.ID)
				if call.ID == "p/batch/0" {
					return fantasy.NewTextResponse("done"), nil
				}
				resp := fantasy.NewTextErrorResponse(strings.Repeat("failure ", 1200))
				if dispatch != "" {
					resp.Content += fmt.Sprintf(`{"dispatch":%q}`, dispatch)
				}
				return resp, nil
			})
			require.NoError(t, err)
			require.Equal(t, []string{"p/batch/0", "p/batch/1"}, called)
			require.Len(t, run.results, 4)
			require.Equal(t, 2, run.results[2].Iteration)
			require.Equal(t, StepSkipped, run.results[2].Status)
			resp, err := pipelineResponse(run, nil)
			require.NoError(t, err)
			require.True(t, resp.IsError)
			var results []PipelineResult
			require.NoError(t, json.Unmarshal([]byte(resp.Content), &results))
			resume := results[1].Resume
			require.Contains(t, resume, "Already applied, do not repeat: batch iteration 0")
			require.Contains(t, resume, "Step batch iteration 1")
			require.Contains(t, resume, "Not run: batch iteration 2, after")
			require.Contains(t, resume, "never replay the full items list")
			require.NotContains(t, resume, "did not take effect")
			require.NotContains(t, resume, "new call holding batch")
			switch dispatch {
			case "none":
				require.Contains(t, resume, "refused before dispatch")
			case "sent", "partial":
				require.Contains(t, resume, "sent input and may have partial effects")
			case "unknown":
				require.Contains(t, resume, "unknown effect")
			default:
				require.Contains(t, resume, "without dispatch evidence")
			}
		})
	}
}

func TestPipelineCancellationStopsUnknownRecovery(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	steps := []PipelineStep{
		{ID: "send", Tool: "mcp_x_send", Arguments: map[string]any{}},
		{ID: "read", Tool: ViewToolName, IfFailed: "send", Arguments: map[string]any{}},
		{ID: "after", Tool: "edit", Arguments: map[string]any{}},
	}
	calls := 0
	_, err := executePipeline(ctx, steps, nil, fantasy.ToolCall{ID: "p"}, func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		calls++
		cancel()
		return fantasy.NewTextErrorResponse(`{"dispatch":"unknown"}`), nil
	})
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, calls)
}
