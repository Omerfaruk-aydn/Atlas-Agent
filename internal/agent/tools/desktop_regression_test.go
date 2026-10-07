package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func TestDesktopSequenceAvoidsDuplicateStableReadback(t *testing.T) {
	for _, kind := range []string{"stable", "visual", "truncated", "different_target", "pointer", "missing_actual", "wrong_window"} {
		t.Run(kind, func(t *testing.T) {
			steps := calculationSteps()
			mode := "auto"
			if kind == "visual" {
				mode = "visual"
			}
			if kind == "different_target" {
				steps[1].Checkpoint.ElementID = "other"
			}
			if kind == "pointer" {
				steps[1].Input = ComputerParams{Action: "click", X: 10, Y: 20, Automation: computer.AutomationRequest{WindowID: "11"}}
			}
			asserts, observations := 0, 0
			r, err := runDesktopWorkflow(t.Context(), DesktopWorkflowParams{Mode: "sequence", Steps: steps, Observation: mode}, fantasy.ToolCall{}, func(_ context.Context, c fantasy.ToolCall) (fantasy.ToolResponse, error) {
				var p ComputerParams
				require.NoError(t, json.Unmarshal([]byte(c.Input), &p))
				if p.Action == "assert" {
					actual := map[string]any{"passed": true, "actual": steps[asserts].Checkpoint.Expected, "window_id": "11", "actual_truncated": kind == "truncated"}
					if kind == "missing_actual" {
						delete(actual, "actual")
					}
					if kind == "wrong_window" {
						actual["window_id"] = "22"
					}
					asserts++
					data, marshalErr := json.Marshal(actual)
					require.NoError(t, marshalErr)
					return fantasy.NewTextResponse(string(data)), nil
				}
				if p.Action == "observe" {
					observations++
					return fantasy.NewTextResponse(`{"window_id":"11","snapshot_id":"final","elements":[]}`), nil
				}
				return fantasy.NewTextResponse(`{}`), nil
			})
			require.NoError(t, err)
			require.False(t, r.IsError, r.Content)
			require.Equal(t, 3, asserts)
			want := 3
			switch kind {
			case "stable":
				want = 1
			case "pointer":
				want = 2
			}
			require.Equal(t, want, observations)
			require.Contains(t, r.Content, `"snapshot_id":"final"`)
			var result struct {
				Checkpoints []json.RawMessage `json:"checkpoints"`
			}
			require.NoError(t, json.Unmarshal([]byte(r.Content), &result))
			require.Len(t, result.Checkpoints, 3)
		})
	}
}

func TestDesktopActObservesOnlyDirectOwnedForegroundDialog(t *testing.T) {
	for _, kind := range []string{"owned", "foreign", "not_dialog", "not_foreground", "minimized", "wrong_identity", "denied"} {
		t.Run(kind, func(t *testing.T) {
			observations, inputs := 0, 0
			r, err := runDesktopWorkflow(t.Context(), DesktopWorkflowParams{Mode: "act", Observation: "semantic", Input: ComputerParams{Action: "hotkey", Key: "s", Modifiers: "ctrl", Automation: computer.AutomationRequest{WindowID: "11"}}}, fantasy.ToolCall{}, func(_ context.Context, c fantasy.ToolCall) (fantasy.ToolResponse, error) {
				var p ComputerParams
				require.NoError(t, json.Unmarshal([]byte(c.Input), &p))
				if p.Action == "hotkey" {
					inputs++
					return fantasy.NewTextResponse(`{}`), nil
				}
				require.Equal(t, "observe", p.Action)
				observations++
				if observations == 1 {
					w := desktopWindowInfo{ID: "22", Owner: "11", Foreground: true, WindowClass: "#32770"}
					switch kind {
					case "foreign":
						w.Owner = "99"
					case "not_dialog":
						w.WindowClass = "CabinetWClass"
					case "not_foreground":
						w.Foreground = false
					case "minimized":
						w.Minimized = true
					}
					data, marshalErr := json.Marshal(map[string]any{"window_id": "11", "snapshot_id": "source", "foreground_window": w, "elements": []any{}})
					require.NoError(t, marshalErr)
					return fantasy.NewTextResponse(string(data)), nil
				}
				require.Equal(t, "22", p.Automation.WindowID)
				if kind == "denied" {
					return fantasy.NewTextErrorResponse("permission_denied: observe dialog"), nil
				}
				if kind == "wrong_identity" {
					return fantasy.NewTextResponse(`{"window_id":"99","snapshot_id":"wrong","elements":[]}`), nil
				}
				image := fantasy.NewImageResponse([]byte("dialog image"), "image/png")
				image.Content = `{"window_id":"22","snapshot_id":"dialog","elements":[]}`
				return image, nil
			})
			require.NoError(t, err)
			require.Equal(t, 1, inputs)
			if kind == "denied" || kind == "wrong_identity" {
				// A denied read must stop without replaying the preceding mutation.
				require.True(t, r.IsError)
				require.Equal(t, 2, observations)
				return
			}
			require.False(t, r.IsError, r.Content)
			if kind == "owned" {
				require.Equal(t, 2, observations)
				require.Equal(t, "image", r.Type)
				require.Contains(t, r.Content, `"source_window_id":"11"`)
				require.Contains(t, r.Content, `"snapshot_id":"dialog"`)
				require.NotContains(t, r.Content, `"snapshot_id":"source"`)
			} else {
				require.Equal(t, 1, observations)
				require.Contains(t, r.Content, `"snapshot_id":"source"`)
			}
		})
	}
}

func TestDesktopActGroupsKnownInputsBeforeOneObservation(t *testing.T) {
	var actions []string
	r, err := runDesktopWorkflow(t.Context(), DesktopWorkflowParams{Mode: "act", Observation: "semantic", Inputs: []ComputerParams{
		{Action: "key", Key: "f2", Automation: computer.AutomationRequest{WindowID: "11"}},
		{Action: "type", Text: "sonuc.txt", Automation: computer.AutomationRequest{WindowID: "11"}},
		{Action: "key", Key: "enter", Automation: computer.AutomationRequest{WindowID: "11"}},
	}, WaitFor: computer.AutomationRequest{WindowID: "11", ElementID: "file", Condition: "text", Expected: "sonuc.txt"}}, fantasy.ToolCall{}, func(_ context.Context, c fantasy.ToolCall) (fantasy.ToolResponse, error) {
		var p ComputerParams
		require.NoError(t, json.Unmarshal([]byte(c.Input), &p))
		actions = append(actions, p.Action)
		if p.Action == "assert" {
			return fantasy.NewTextResponse(`{"passed":true,"actual":"sonuc.txt"}`), nil
		}
		return fantasy.NewTextResponse(`{"window_id":"11","elements":[]}`), nil
	})
	require.NoError(t, err)
	require.False(t, r.IsError, r.Content)
	require.Equal(t, []string{"key", "type", "key", "assert", "observe"}, actions)
}

func TestDesktopActValidatesWholeInputGroup(t *testing.T) {
	inputs := []ComputerParams{{Action: "key", Key: "f2", Automation: computer.AutomationRequest{WindowID: "11"}}, {Action: "type", Text: "name", Automation: computer.AutomationRequest{WindowID: "22"}}}
	calls := 0
	r, err := runDesktopWorkflow(t.Context(), DesktopWorkflowParams{Mode: "act", Inputs: inputs}, fantasy.ToolCall{}, func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		calls++
		return fantasy.ToolResponse{}, nil
	})
	require.NoError(t, err)
	require.True(t, r.IsError)
	require.Zero(t, calls)
}

func TestDesktopActStopsGroupAtDeniedInput(t *testing.T) {
	var actions []string
	r, err := runDesktopWorkflow(t.Context(), DesktopWorkflowParams{Mode: "act", Inputs: []ComputerParams{{Action: "key", Key: "f2", Automation: computer.AutomationRequest{WindowID: "11"}}, {Action: "type", Text: "name", Automation: computer.AutomationRequest{WindowID: "11"}}, {Action: "key", Key: "enter", Automation: computer.AutomationRequest{WindowID: "11"}}}}, fantasy.ToolCall{}, func(_ context.Context, c fantasy.ToolCall) (fantasy.ToolResponse, error) {
		var p ComputerParams
		require.NoError(t, json.Unmarshal([]byte(c.Input), &p))
		actions = append(actions, p.Action)
		if p.Action == "type" {
			return fantasy.NewTextErrorResponse("permission_denied: fixture"), nil
		}
		return fantasy.NewTextResponse(`{}`), nil
	})
	require.NoError(t, err)
	require.True(t, r.IsError)
	require.Equal(t, []string{"key", "type"}, actions)
}
