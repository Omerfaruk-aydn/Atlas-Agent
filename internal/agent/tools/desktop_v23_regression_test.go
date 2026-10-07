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
