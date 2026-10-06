package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/schema"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/stretchr/testify/require"
)

func computerBatchTestParams(groups, inputs int) ComputerParams {
	p := ComputerParams{Action: "batch", Batch: &ComputerBatchParams{}}
	for range groups {
		group := ComputerBatchGroup{Checkpoint: computer.AutomationRequest{WindowID: "11", ElementID: "result", Condition: "text", Expected: "done"}}
		for range inputs {
			group.Inputs = append(group.Inputs, ComputerBatchInput{Action: "key", Key: "enter", Automation: computer.AutomationRequest{WindowID: "11"}})
		}
		p.Batch.Groups = append(p.Batch.Groups, group)
	}
	return p
}

func TestComputerBatchExecutesLongGroupsWithReadbacksWithoutImages(t *testing.T) {
	for _, size := range [][2]int{{1, 16}, {24, 1}, {6, 16}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			inputs, assertions, enumerations := 0, 0, 0
			r, err := runComputerBatch(t.Context(), computerBatchTestParams(size[0], size[1]), fantasy.ToolCall{ID: "batch"}, func(_ context.Context, c fantasy.ToolCall) (fantasy.ToolResponse, error) {
				var p ComputerParams
				require.NoError(t, json.Unmarshal([]byte(c.Input), &p))
				require.Nil(t, p.Batch, "Children must be atomic calls")
				switch p.Action {
				case "key":
					inputs++
					return fantasy.NewTextResponse(`{}`), nil
				case "assert":
					assertions++
					return fantasy.NewTextResponse(`{"window_id":"11","passed":true,"actual":"done"}`), nil
				case "windows":
					enumerations++
					return fantasy.NewTextResponse(`{"result":[{"window_id":"11","foreground":true}]}`), nil
				default:
					t.Fatalf("Unnecessary batch action: %s", p.Action)
					return fantasy.ToolResponse{}, nil
				}
			})
			require.NoError(t, err)
			require.False(t, r.IsError, r.Content)
			require.Equal(t, size[0]*size[1], inputs)
			require.Equal(t, size[0], assertions)
			require.Equal(t, 1, enumerations)
			require.Equal(t, "text", r.Type)
		})
	}
}

func TestComputerBatchValidatesAllGroupsBeforeAnyInput(t *testing.T) {
	for _, kind := range []string{"empty", "group_limit", "input_limit", "call_budget", "bad_later_input", "wrong_checkpoint", "mixed_params", "no_dispatch"} {
		t.Run(kind, func(t *testing.T) {
			p := computerBatchTestParams(2, 2)
			var invoke ComputerDispatcher = func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
				t.Fatal("Invalid batch executed a child")
				return fantasy.ToolResponse{}, nil
			}
			switch kind {
			case "empty":
				p.Batch.Groups = nil
			case "group_limit":
				p = computerBatchTestParams(25, 1)
			case "input_limit":
				p = computerBatchTestParams(1, 17)
			case "call_budget":
				p = computerBatchTestParams(8, 16)
			case "bad_later_input":
				p.Batch.Groups[1].Inputs[1].Action = "batch"
			case "wrong_checkpoint":
				p.Batch.Groups[1].Checkpoint.WindowID = "22"
			case "mixed_params":
				p.Text = "unexpected"
			case "no_dispatch":
				invoke = nil
			}
			r, err := runComputerBatch(t.Context(), p, fantasy.ToolCall{}, invoke)
			require.NoError(t, err)
			require.True(t, r.IsError)
		})
	}
}

func TestComputerBatchStopsAfterFailedCheckpointOrDeniedChild(t *testing.T) {
	for _, kind := range []string{"checkpoint", "denial", "cancellation", "input_failure"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			r, err := runComputerBatch(ctx, computerBatchTestParams(2, 1), fantasy.ToolCall{}, func(_ context.Context, c fantasy.ToolCall) (fantasy.ToolResponse, error) {
				calls++
				var p ComputerParams
				require.NoError(t, json.Unmarshal([]byte(c.Input), &p))
				if p.Action == "assert" {
					return fantasy.NewTextResponse(`{"window_id":"11","passed":false,"actual":"wrong"}`), nil
				}
				if kind == "denial" || kind == "input_failure" {
					r := fantasy.NewTextErrorResponse("input failed")
					r.StopTurn = kind == "denial"
					return r, nil
				}
				if kind == "cancellation" {
					cancel()
				}
				return fantasy.NewTextResponse(`{}`), nil
			})
			if kind == "cancellation" {
				require.ErrorIs(t, err, context.Canceled)
				require.Equal(t, 1, calls)
			} else {
				require.NoError(t, err)
				require.True(t, r.IsError)
				require.Equal(t, kind == "denial", r.StopTurn)
				require.LessOrEqual(t, calls, 2)
			}
		})
	}
}

func TestComputerBatchPublicToolRetainsPermissionsAndAtomicDispatch(t *testing.T) {
	base := newComputerTool(&mockPermissionService{}, t.TempDir(), &fakeComputerBackend{}, func() bool { return true }, "computer", time.Second)
	calls := 0
	tool := &computerBatchDispatchTool{AgentTool: base, invoke: func(_ context.Context, c fantasy.ToolCall) (fantasy.ToolResponse, error) {
		calls++
		var input ComputerParams
		require.NoError(t, json.Unmarshal([]byte(c.Input), &input))
		if input.Action == "assert" {
			return fantasy.NewTextResponse(`{"window_id":"11","passed":true,"actual":"done"}`), nil
		}
		if input.Action == "windows" {
			return fantasy.NewTextResponse(`{"result":[{"window_id":"11","foreground":true}]}`), nil
		}
		return fantasy.NewTextResponse(`{}`), nil
	}}
	data, err := json.Marshal(computerBatchTestParams(1, 2))
	require.NoError(t, err)
	ctx := context.WithValue(t.Context(), SessionIDContextKey, "batch-public-test")
	r, err := tool.Run(ctx, fantasy.ToolCall{ID: "public", Input: string(data)})
	require.NoError(t, err)
	require.False(t, r.IsError, r.Content)
	require.Equal(t, 4, calls)
	info, err := json.Marshal(tool.Info())
	require.NoError(t, err)
	require.Contains(t, string(info), `"batch"`)
	toolInfo := tool.Info()
	spec, err := json.Marshal(map[string]any{"type": "object", "properties": toolInfo.Parameters, "required": toolInfo.Required})
	require.NoError(t, err)
	var specification schema.Schema
	require.NoError(t, json.Unmarshal(spec, &specification))
	var arguments any
	require.NoError(t, json.Unmarshal(data, &arguments))
	require.NoError(t, schema.ValidateAgainstSchema(arguments, specification), "Coordinator must accept the real public batch schema")
}

func TestPipelineSupportsExpandedBoundedCapacity(t *testing.T) {
	for _, count := range []int{64, 128, 129} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			calls := 0
			tool := NewToolPipeline(func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
				calls++
				return fantasy.NewTextResponse("ok"), nil
			})
			params := PipelineParams{Steps: []PipelineStep{{ID: "read", Tool: "view", Items: make([]string, count)}}}
			data, err := json.Marshal(params)
			require.NoError(t, err)
			r, err := tool.Run(t.Context(), fantasy.ToolCall{Input: string(data)})
			require.NoError(t, err)
			require.Equal(t, count > 128, r.IsError)
			if count > 128 {
				require.Zero(t, calls)
			} else {
				require.Equal(t, count, calls)
			}
		})
	}
}

type batchPermissionService struct {
	mockPermissionService
	actions []string
	deny    string
}

func (p *batchPermissionService) Request(_ context.Context, r permission.CreatePermissionRequest) (bool, error) {
	p.actions = append(p.actions, r.Action)
	return r.Action != p.deny, nil
}

func TestComputerBatchChildPermissionDenialStopsAtomicBackend(t *testing.T) {
	permissions := &batchPermissionService{deny: "key"}
	backend := &foregroundComputerBackend{foreground: "11"}
	base := newComputerTool(permissions, t.TempDir(), backend, func() bool { return true }, "computer", time.Second)
	tool := &computerBatchDispatchTool{AgentTool: base, invoke: base.Run}
	data, err := json.Marshal(computerBatchTestParams(2, 1))
	require.NoError(t, err)
	ctx := context.WithValue(t.Context(), SessionIDContextKey, "batch-permission-denied")
	r, err := tool.Run(ctx, fantasy.ToolCall{ID: "permission-batch", Input: string(data)})
	require.NoError(t, err)
	require.True(t, r.IsError)
	require.True(t, r.StopTurn)
	require.Equal(t, []string{"batch", "key"}, permissions.actions)
	require.Empty(t, backend.keys)
}
