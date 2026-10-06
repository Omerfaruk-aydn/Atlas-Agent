package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func TestDesktopFlowBindsNewOwnedDialogBeforeFollowingOperation(t *testing.T) {
	p := desktopFlowTestPlan()
	p.Flow.Nodes[1].Next = "dialog"
	p.Flow.Nodes = append(p.Flow.Nodes,
		DesktopFlowNode{ID: "dialog", Kind: "resolve", Title: "Save As", OwnerRef: "app", Next: "filename"},
		DesktopFlowNode{ID: "filename", Kind: "operation", WindowRef: "dialog", Input: ComputerParams{Action: "set_value", Automation: computer.AutomationRequest{Name: "File name", Text: "result.txt"}}, Checkpoint: computer.AutomationRequest{Name: "File name", Condition: "value", Expected: "result.txt"}})
	value, mutations, stage := "empty", 0, 0
	base := desktopFlowFixture(t, &value, &mutations)
	r, err := runDesktopWorkflow(t.Context(), p, fantasy.ToolCall{}, func(ctx context.Context, c fantasy.ToolCall) (fantasy.ToolResponse, error) {
		var input ComputerParams
		require.NoError(t, json.Unmarshal([]byte(c.Input), &input))
		if stage == 1 {
			switch input.Action {
			case "windows":
				return fantasy.NewTextResponse(`{"result":[{"window_id":"11","name":"Untitled - Notepad","process_id":17,"process_name":"Notepad.exe","class_name":"Notepad"},{"window_id":"22","name":"Save As","process_id":17,"process_name":"Notepad.exe","class_name":"#32770","owner_window_id":"11","foreground":true}]}`), nil
			case "find":
				require.Equal(t, "22", input.Automation.WindowID)
				return fantasy.NewTextResponse(`{"result":{"matches":[{"element_id":"dialog-field","name":"File name","role":"ControlType.Edit","enabled":true,"value_available":true,"value":"","supported_patterns":["Value"]}],"truncated":false}}`), nil
			case "set_value":
				require.Equal(t, "22", input.Automation.WindowID)
				require.Equal(t, "dialog-field", input.Automation.ElementID)
				mutations++
				return fantasy.NewTextResponse(`{}`), nil
			case "assert":
				return fantasy.NewTextResponse(`{"window_id":"22","passed":true,"actual":"result.txt"}`), nil
			}
		}
		r, err := base(ctx, c)
		if input.Action == "assert" {
			stage = 1
		}
		return r, err
	})
	require.NoError(t, err)
	require.False(t, r.IsError, r.Content)
	require.Equal(t, 2, mutations)
	require.Contains(t, r.Content, `"window_id":"22"`)
}

func TestDesktopFlowStopsOnUnsafeFreshTarget(t *testing.T) {
	for _, kind := range []string{"ambiguous", "truncated", "password", "offscreen", "wrong_foreground", "wrong_process", "read_denied"} {
		t.Run(kind, func(t *testing.T) {
			p := desktopFlowTestPlan()
			value, mutations, lists := "empty", 0, 0
			base := desktopFlowFixture(t, &value, &mutations)
			r, err := runDesktopWorkflow(t.Context(), p, fantasy.ToolCall{}, func(ctx context.Context, c fantasy.ToolCall) (fantasy.ToolResponse, error) {
				var input ComputerParams
				require.NoError(t, json.Unmarshal([]byte(c.Input), &input))
				if input.Action == "windows" {
					lists++
					if lists == 2 && (kind == "wrong_foreground" || kind == "wrong_process") {
						return fantasy.NewTextResponse(`{"result":[{"window_id":"11","process_id":99,"process_name":"other.exe","class_name":"Notepad"}]}`), nil
					}
				}
				if input.Action == "find" {
					if kind == "read_denied" {
						r := fantasy.NewTextErrorResponse("denied")
						r.StopTurn = true
						return r, nil
					}
					e := desktopElement{ID: "fresh", Name: "Field", Role: "ControlType.Edit", Enabled: true, ValueAvailable: true}
					e.Password, e.Offscreen = kind == "password", kind == "offscreen"
					matches := []desktopElement{e}
					if kind == "ambiguous" {
						matches = append(matches, e)
					}
					data, _ := json.Marshal(map[string]any{"result": map[string]any{"matches": matches, "truncated": kind == "truncated"}})
					return fantasy.NewTextResponse(string(data)), nil
				}
				return base(ctx, c)
			})
			require.NoError(t, err)
			require.True(t, r.IsError, r.Content)
			require.Zero(t, mutations)
			require.Equal(t, kind == "read_denied", r.StopTurn)
		})
	}
}
