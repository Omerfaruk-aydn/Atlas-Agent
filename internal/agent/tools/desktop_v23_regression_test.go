package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func TestDesktopRenameAcceptsSemanticHint(t *testing.T) {
	t.Parallel()
	p := renameParams()
	p.Observation = "semantic"
	invoke, _ := renameFixture(t, nil)
	r, err := runDesktopWorkflow(t.Context(), p, fantasy.ToolCall{}, invoke)
	require.NoError(t, err)
	require.False(t, r.IsError, r.Content)
}

func TestDesktopTransitionAcceptsMatchingOuterWindow(t *testing.T) {
	t.Parallel()
	for _, outer := range []string{"11", "22"} {
		keys := 0
		p := DesktopWorkflowParams{Mode: "transition", WindowID: outer, Input: ComputerParams{Action: "hotkey", Key: "s", Modifiers: "ctrl", Automation: computer.AutomationRequest{WindowID: "11"}}, Transition: &DesktopTransitionParams{ExpectedTitle: "Save", WaitMS: 1000}}
		r, err := runDesktopWorkflow(t.Context(), p, fantasy.ToolCall{}, func(_ context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			var input ComputerParams
			require.NoError(t, json.Unmarshal([]byte(call.Input), &input))
			switch input.Action {
			case "windows":
				if keys == 0 {
					return fantasy.NewTextResponse(`{"result":[{"window_id":"11","foreground":true,"process_id":1,"class_name":"Notepad"}]}`), nil
				}
				return fantasy.NewTextResponse(`{"result":[{"window_id":"12","foreground":true,"process_id":1,"owner_window_id":"11","class_name":"#32770","name":"Save"}]}`), nil
			case "hotkey":
				keys++
			case "observe":
				return fantasy.NewTextResponse(`{"window_id":"12","elements":[]}`), nil
			}
			return fantasy.NewTextResponse(`{}`), nil
		})
		require.NoError(t, err)
		if outer == "22" {
			require.True(t, r.IsError)
			require.Zero(t, keys)
		} else {
			require.False(t, r.IsError, r.Content)
			require.Equal(t, 1, keys)
		}
	}
}

func TestDesktopCheckpointGuidanceUsesRuntimeIdentity(t *testing.T) {
	t.Parallel()
	observed := map[string]any{"elements": []any{map[string]any{"automation_id": "CalculatorResults", "element_id": "fresh-result", "name": "Ekran değeri 69.104", "enabled": true}}}
	desktopCheckpointGuidance(observed, "11")
	guidance := observed["checkpoint_guidance"].(map[string]any)
	selector := guidance["selector"].(computer.AutomationRequest)
	require.Equal(t, "fresh-result", selector.ElementID)
	require.Empty(t, selector.Name)
	require.Equal(t, "Ekran değeri 69.104", guidance["actual"])
	require.Empty(t, selector.Expected)
}

func TestDesktopSequenceRejectsChangingNameCheckpointBeforeInput(t *testing.T) {
	t.Parallel()
	p := DesktopWorkflowParams{Mode: "sequence", Steps: []DesktopWorkflowStep{{Input: ComputerParams{Action: "type", Text: "1234*56", Automation: computer.AutomationRequest{WindowID: "11"}}, Checkpoint: computer.AutomationRequest{WindowID: "11", Name: "Ekran değeri 0", Condition: "text", Expected: "69104"}}}}
	r, err := runDesktopWorkflow(t.Context(), p, fantasy.ToolCall{}, func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		t.Fatal("Impossible checkpoint dispatched input")
		return fantasy.ToolResponse{}, nil
	})
	require.NoError(t, err)
	require.True(t, r.IsError)
	require.Contains(t, r.Content, "inconsistent_text_checkpoint")
}

func TestDesktopActFocusWindowStopsBeforeInputOnDenial(t *testing.T) {
	t.Parallel()
	p := DesktopWorkflowParams{Mode: "act", FocusWindow: true, Input: ComputerParams{Action: "type", Text: "hello", Automation: computer.AutomationRequest{WindowID: "11"}}}
	var actions []string
	r, err := runDesktopWorkflow(t.Context(), p, fantasy.ToolCall{}, func(_ context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		var input ComputerParams
		require.NoError(t, json.Unmarshal([]byte(call.Input), &input))
		actions = append(actions, input.Action)
		if input.Action == "windows" {
			return fantasy.NewTextResponse(`{"result":[{"window_id":"11","process_id":1,"class_name":"Notepad","process_name":"notepad.exe"}]}`), nil
		}
		return fantasy.NewTextErrorResponse("focus_denied: fixture"), nil
	})
	require.NoError(t, err)
	require.True(t, r.IsError)
	require.NotContains(t, actions, "type")
}

func TestDesktopActFocusWindowValidatesIdentityBeforeInput(t *testing.T) {
	t.Parallel()
	for _, changed := range []bool{false, true} {
		typed, lists := 0, 0
		p := DesktopWorkflowParams{Mode: "act", FocusWindow: true, Input: ComputerParams{Action: "type", Text: "hello", Automation: computer.AutomationRequest{WindowID: "11"}}, Observation: "semantic"}
		r, err := runDesktopWorkflow(t.Context(), p, fantasy.ToolCall{}, func(_ context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			var input ComputerParams
			require.NoError(t, json.Unmarshal([]byte(call.Input), &input))
			switch input.Action {
			case "windows":
				lists++
				pid := uint32(1)
				if changed && lists > 1 {
					pid = 2
				}
				data, marshalErr := json.Marshal(map[string]any{"result": []desktopWindowInfo{{ID: "11", ProcessID: pid, WindowClass: "Notepad", ProcessName: "notepad.exe", Foreground: lists > 1}}})
				require.NoError(t, marshalErr)
				return fantasy.NewTextResponse(string(data)), nil
			case "focus":
				return fantasy.NewTextResponse(`{"result":{"focused":true}}`), nil
			case "type":
				typed++
			case "observe":
				return fantasy.NewTextResponse(`{"window_id":"11","elements":[]}`), nil
			default:
				t.Fatalf("Unexpected action %s", input.Action)
			}
			return fantasy.NewTextResponse(`{}`), nil
		})
		require.NoError(t, err)
		if changed {
			require.True(t, r.IsError)
			require.Zero(t, typed)
		} else {
			require.False(t, r.IsError, r.Content)
			require.Equal(t, 1, typed)
		}
	}
}

func TestDesktopActHandlesDisappearingDialogUIAWithoutReplay(t *testing.T) {
	t.Parallel()
	keys := 0
	disappeared := false
	p := DesktopWorkflowParams{Mode: "act", Input: ComputerParams{Action: "key", Key: "enter", Automation: computer.AutomationRequest{WindowID: "11"}}}
	r, err := runDesktopWorkflow(t.Context(), p, fantasy.ToolCall{}, func(_ context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		var input ComputerParams
		require.NoError(t, json.Unmarshal([]byte(call.Input), &input))
		switch input.Action {
		case "key":
			keys++
		case "windows":
			if disappeared {
				return fantasy.NewTextResponse(`{"result":[{"window_id":"22","foreground":true}]}`), nil
			}
			return fantasy.NewTextResponse(`{"result":[{"window_id":"11","foreground":true}]}`), nil
		case "observe":
			disappeared = true
			r := fantasy.NewTextErrorResponse("accessibility_unavailable: dialog closed during observation")
			r.Metadata = `{"desktop_recovery":{"fresh_windows":[{"window_id":"22","foreground":true}]}}`
			return r, nil
		}
		return fantasy.NewTextResponse(`{}`), nil
	})
	require.NoError(t, err)
	require.False(t, r.IsError, r.Content)
	require.Contains(t, r.Content, `"window_status":"absent"`)
	require.Contains(t, r.Content, `"condition_verified":false`)
	require.Equal(t, 1, keys)
}
