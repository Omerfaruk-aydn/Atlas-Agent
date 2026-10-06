package tools

import (
	"context"
	"encoding/json"
	"testing"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func TestDesktopAdaptiveSelectsMethodAndVerifiesResult(t *testing.T) {
	for _, kind := range []string{"semantic", "keyboard", "skip", "ambiguous", "wrong_foreground", "mutation_error", "checkpoint_failed"} {
		t.Run(kind, func(t *testing.T) {
			var p DesktopWorkflowParams
			require.NoError(t, json.Unmarshal([]byte(`{"mode":"adaptive","observation":"auto","adaptive_steps":[{"input":{"action":"set_value","automation":{"window_id":"11","name":"Field","role":"ControlType.Edit","text":"hello"}},"checkpoint":{"window_id":"11","name":"Field","condition":"value","expected":"hello"}}]}`), &p))
			if kind == "skip" {
				require.NoError(t, json.Unmarshal([]byte(`{"mode":"adaptive","adaptive_steps":[{"when":{"window_id":"11","name":"Field","condition":"value","expected":"already"},"input":{"action":"set_value","automation":{"window_id":"11","name":"Field","text":"hello"}},"checkpoint":{"window_id":"11","name":"Field","condition":"value","expected":"hello"}}]}`), &p))
			}
			var actions []string
			r, err := runDesktopWorkflow(t.Context(), p, fantasy.ToolCall{}, func(_ context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
				var input ComputerParams
				require.NoError(t, json.Unmarshal([]byte(call.Input), &input))
				actions = append(actions, input.Action)
				switch input.Action {
				case "observe":
					patterns := []string{"Value"}
					if kind == "keyboard" {
						patterns = nil
					}
					e := desktopElement{ID: "field", Name: "Field", Role: "ControlType.Edit", Enabled: true, Patterns: patterns, ValueAvailable: true, Value: "initial", X: 10, Y: 20, Width: 100, Height: 30}
					elements := []desktopElement{e}
					if kind == "ambiguous" {
						e.ID = "other"
						elements = append(elements, e)
					}
					foreground := "11"
					if kind == "wrong_foreground" {
						foreground = "22"
					}
					data, marshalErr := json.Marshal(map[string]any{"window_id": "11", "snapshot_id": "fresh", "truncated": false, "foreground_window": map[string]any{"window_id": foreground, "foreground": true}, "elements": elements})
					require.NoError(t, marshalErr)
					return fantasy.NewTextResponse(string(data)), nil
				case "assert":
					if kind == "checkpoint_failed" {
						return fantasy.NewTextResponse(`{"window_id":"11","passed":false}`), nil
					}
					return fantasy.NewTextResponse(`{"window_id":"11","passed":true,"actual":"hello"}`), nil
				case "set_value":
					require.Equal(t, "field", input.Automation.ElementID)
					if kind == "mutation_error" {
						return fantasy.NewTextErrorResponse("unsupported_pattern: uncertain execution"), nil
					}
					return fantasy.NewTextResponse(`{}`), nil
				default:
					return fantasy.NewTextResponse(`{}`), nil
				}
			})
			require.NoError(t, err)
			switch kind {
			case "semantic":
				require.False(t, r.IsError, r.Content)
				require.Equal(t, []string{"observe", "set_value", "assert", "observe"}, actions)
				require.Contains(t, r.Content, `"method":"accessibility"`)
			case "keyboard":
				require.False(t, r.IsError, r.Content)
				require.Equal(t, []string{"observe", "click", "assert", "hotkey", "type", "assert", "observe"}, actions)
				require.Contains(t, r.Content, `"method":"keyboard"`)
			case "skip":
				require.False(t, r.IsError, r.Content)
				require.Equal(t, []string{"observe"}, actions)
				require.Contains(t, r.Content, `"status":"skipped"`)
			default:
				require.True(t, r.IsError, r.Content)
				require.NotContains(t, actions, "type")
				require.NotContains(t, actions, "hotkey")
			}
		})
	}
}
