package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"

	"github.com/stretchr/testify/require"
)

func calculationSteps() []DesktopWorkflowStep {
	steps := []DesktopWorkflowStep{}
	for _, value := range []string{"69104", "8638", "9000"} {
		steps = append(steps, DesktopWorkflowStep{
			Input:      ComputerParams{Action: "key", Key: "enter", Automation: computer.AutomationRequest{WindowID: "11"}},
			Checkpoint: computer.AutomationRequest{WindowID: "11", ElementID: "result", Condition: "value", Expected: value},
		})
	}
	return steps
}

func TestDesktopTransitionGroupedSaveReturnsApplicationReadback(t *testing.T) {
	var actions []string
	lists := 0
	r, err := runDesktopWorkflow(t.Context(), DesktopWorkflowParams{Mode: "transition", Inputs: []ComputerParams{{Action: "type", Text: "hesap.txt", Automation: computer.AutomationRequest{WindowID: "11"}}, {Action: "key", Key: "enter", Automation: computer.AutomationRequest{WindowID: "11"}}}, Transition: &DesktopTransitionParams{ExpectedApplication: "Notepad"}}, fantasy.ToolCall{}, func(_ context.Context, c fantasy.ToolCall) (fantasy.ToolResponse, error) {
		var p ComputerParams
		require.NoError(t, json.Unmarshal([]byte(c.Input), &p))
		actions = append(actions, p.Action)
		if p.Action == "windows" {
			lists++
			if lists == 1 {
				return fantasy.NewTextResponse(`{"result":[{"window_id":"11","foreground":true},{"window_id":"22","name":"document","process_name":"notepad.exe"}]}`), nil
			}
			return fantasy.NewTextResponse(`{"result":[{"window_id":"22","name":"hesap - Notepad","process_name":"notepad.exe","foreground":true}]}`), nil
		}
		if p.Action == "observe" {
			require.Equal(t, "22", p.Automation.WindowID)
			return fantasy.NewTextResponse(`{"window_id":"22","elements":[],"document_text":"verified"}`), nil
		}
		return fantasy.NewTextResponse(`{}`), nil
	})
	require.NoError(t, err)
	require.False(t, r.IsError, r.Content)
	require.Equal(t, []string{"windows", "type", "key", "windows", "observe"}, actions)
	require.Contains(t, r.Content, "verified")
}

func TestDesktopTransitionGroupRejectsBadScopeAndStopsOnDenial(t *testing.T) {
	for _, badScope := range []bool{false, true} {
		inputs := []ComputerParams{{Action: "type", Text: "file", Automation: computer.AutomationRequest{WindowID: "11"}}, {Action: "key", Key: "enter", Automation: computer.AutomationRequest{WindowID: "11"}}}
		if badScope {
			inputs[1].Automation.WindowID = "22"
		}
		var actions []string
		r, err := runDesktopWorkflow(t.Context(), DesktopWorkflowParams{Mode: "transition", Inputs: inputs, Transition: &DesktopTransitionParams{ExpectedApplication: "Notepad"}}, fantasy.ToolCall{}, func(_ context.Context, c fantasy.ToolCall) (fantasy.ToolResponse, error) {
			var p ComputerParams
			require.NoError(t, json.Unmarshal([]byte(c.Input), &p))
			actions = append(actions, p.Action)
			if p.Action == "windows" {
				return fantasy.NewTextResponse(`{"result":[{"window_id":"11","foreground":true}]}`), nil
			}
			return fantasy.NewTextErrorResponse("permission_denied: fixture"), nil
		})
		require.NoError(t, err)
		require.True(t, r.IsError)
		if badScope {
			require.Empty(t, actions)
		} else {
			require.Equal(t, []string{"windows", "type"}, actions)
		}
	}
}
