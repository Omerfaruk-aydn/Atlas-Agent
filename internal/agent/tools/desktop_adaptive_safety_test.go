package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func adaptiveFixtureStep() DesktopAdaptiveStep {
	return DesktopAdaptiveStep{Input: ComputerParams{Action: "set_value", Automation: computer.AutomationRequest{WindowID: "11", Name: "Field", Text: "hello"}}, Checkpoint: computer.AutomationRequest{WindowID: "11", Name: "Field", Condition: "value", Expected: "hello"}}
}

func adaptiveFixtureObservation(e desktopElement, truncated bool) fantasy.ToolResponse {
	data, _ := json.Marshal(map[string]any{"window_id": "11", "snapshot_id": "fresh", "truncated": truncated, "foreground_window": map[string]any{"window_id": "11", "foreground": true}, "elements": []desktopElement{e}})
	return fantasy.NewTextResponse(string(data))
}

func TestDesktopAdaptiveRejectsWholeInvalidPlanBeforeDispatch(t *testing.T) {
	for _, kind := range []string{"wrong_checkpoint", "wrong_predicate", "both_inputs", "too_many", "budget", "predicate_wait", "cross_window", "unsupported_mode"} {
		t.Run(kind, func(t *testing.T) {
			p := DesktopWorkflowParams{Mode: "adaptive", AdaptiveSteps: []DesktopAdaptiveStep{adaptiveFixtureStep(), adaptiveFixtureStep()}}
			s := &p.AdaptiveSteps[1]
			switch kind {
			case "wrong_checkpoint":
				s.Checkpoint.WindowID = "22"
			case "wrong_predicate":
				s.When = &computer.AutomationRequest{WindowID: "22", Name: "Field", Condition: "visible"}
			case "both_inputs":
				s.Inputs = []ComputerParams{s.Input}
			case "too_many":
				for range 7 {
					p.AdaptiveSteps = append(p.AdaptiveSteps, adaptiveFixtureStep())
				}
			case "budget":
				p.AdaptiveSteps = nil
				for range 8 {
					step := adaptiveFixtureStep()
					step.Input = ComputerParams{}
					for range 8 {
						step.Inputs = append(step.Inputs, ComputerParams{Action: "key", Key: "enter", Automation: computer.AutomationRequest{WindowID: "11"}})
					}
					p.AdaptiveSteps = append(p.AdaptiveSteps, step)
				}
			case "predicate_wait":
				s.When = &computer.AutomationRequest{WindowID: "11", Name: "Field", Condition: "visible", WaitMS: 1000}
			case "cross_window":
				s.Input = ComputerParams{}
				s.Inputs = []ComputerParams{{Action: "key", Key: "enter", Automation: computer.AutomationRequest{WindowID: "11"}}, {Action: "key", Key: "enter", Automation: computer.AutomationRequest{WindowID: "22"}}}
			case "unsupported_mode":
				p.Mode = "act"
			}
			calls := 0
			r, err := runDesktopWorkflow(t.Context(), p, fantasy.ToolCall{}, func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
				calls++
				return fantasy.ToolResponse{}, nil
			})
			require.NoError(t, err)
			require.True(t, r.IsError)
			require.Zero(t, calls)
		})
	}
}

func TestDesktopAdaptiveStopsBeforeUnsafeKeyboardMutation(t *testing.T) {
	for _, kind := range []string{"password", "offscreen", "disabled", "zero_bounds", "truncated", "focus_false", "focus_denied", "wrong_assertion_window", "bad_assertion"} {
		t.Run(kind, func(t *testing.T) {
			e := desktopElement{ID: "field", Name: "Field", Role: "ControlType.Edit", Enabled: true, Width: 100, Height: 30}
			switch kind {
			case "password":
				e.Password = true
			case "offscreen":
				e.Offscreen = true
			case "disabled":
				e.Enabled = false
			case "zero_bounds":
				e.Width = 0
			}
			var actions []string
			r, err := runDesktopWorkflow(t.Context(), DesktopWorkflowParams{Mode: "adaptive", AdaptiveSteps: []DesktopAdaptiveStep{adaptiveFixtureStep()}}, fantasy.ToolCall{}, func(_ context.Context, c fantasy.ToolCall) (fantasy.ToolResponse, error) {
				var p ComputerParams
				require.NoError(t, json.Unmarshal([]byte(c.Input), &p))
				actions = append(actions, p.Action)
				if p.Action == "observe" {
					return adaptiveFixtureObservation(e, kind == "truncated"), nil
				}
				if p.Action == "assert" {
					switch kind {
					case "focus_false":
						return fantasy.NewTextResponse(`{"window_id":"11","passed":false}`), nil
					case "focus_denied":
						return fantasy.ToolResponse{Content: "permission_denied", IsError: true, StopTurn: true}, nil
					case "wrong_assertion_window":
						return fantasy.NewTextResponse(`{"window_id":"22","passed":true}`), nil
					case "bad_assertion":
						return fantasy.NewTextResponse(`{}`), nil
					}
				}
				return fantasy.NewTextResponse(`{}`), nil
			})
			require.NoError(t, err)
			require.True(t, r.IsError, r.Content)
			require.NotContains(t, actions, "type")
			require.NotContains(t, actions, "hotkey")
			require.NotContains(t, actions, "set_value")
			if kind == "focus_denied" {
				require.True(t, r.StopTurn)
			}
		})
	}
}

func TestDesktopAdaptivePreservesProgressWithoutReplaying(t *testing.T) {
	for _, kind := range []string{"checkpoint", "mutation", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			mutations := 0
			r, err := runDesktopWorkflow(ctx, DesktopWorkflowParams{Mode: "adaptive", AdaptiveSteps: []DesktopAdaptiveStep{adaptiveFixtureStep(), adaptiveFixtureStep(), adaptiveFixtureStep()}}, fantasy.ToolCall{}, func(_ context.Context, c fantasy.ToolCall) (fantasy.ToolResponse, error) {
				var p ComputerParams
				require.NoError(t, json.Unmarshal([]byte(c.Input), &p))
				switch p.Action {
				case "observe":
					return adaptiveFixtureObservation(desktopElement{ID: "field", Name: "Field", Role: "ControlType.Edit", Enabled: true, Patterns: []string{"Value"}}, false), nil
				case "set_value":
					mutations++
					if mutations == 2 && kind == "mutation" {
						return fantasy.NewTextErrorResponse("operation_failed: ambiguous execution"), nil
					}
					if mutations == 2 && kind == "cancel" {
						cancel()
						return fantasy.ToolResponse{}, context.Canceled
					}
					return fantasy.NewTextResponse(`{}`), nil
				case "assert":
					if mutations == 2 && kind == "checkpoint" {
						return fantasy.NewTextResponse(`{"window_id":"11","passed":false}`), nil
					}
					return fantasy.NewTextResponse(`{"window_id":"11","passed":true}`), nil
				default:
					t.Fatalf("Unexpected action %s", p.Action)
					return fantasy.ToolResponse{}, nil
				}
			})
			if kind == "cancel" {
				require.True(t, errors.Is(err, context.Canceled))
			} else {
				require.NoError(t, err)
				require.True(t, r.IsError)
			}
			require.Equal(t, 2, mutations)
			require.Contains(t, r.Content, `"status":"verified"`)
			require.Contains(t, r.Content, `"input_replayed":false`)
			require.Contains(t, r.Metadata, "adaptive_progress")
		})
	}
}
