package tools

import (
	"context"
	"encoding/json"

	"testing"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func TestDesktopFlowPrepareWriteAndCloseInOneCall(t *testing.T) {
	p := desktopFlowTestPlan()
	p.Flow.Nodes[0].Kind = "prepare"
	p.Flow.Nodes[1].Next = "close"
	p.Flow.Nodes = append(p.Flow.Nodes, DesktopFlowNode{ID: "close", Kind: "close", WindowRef: "app"})
	value, mutations, closed := "empty", 0, false
	base := desktopFlowFixture(t, &value, &mutations)
	r, err := runDesktopWorkflow(t.Context(), p, fantasy.ToolCall{}, func(ctx context.Context, c fantasy.ToolCall) (fantasy.ToolResponse, error) {
		var input ComputerParams
		require.NoError(t, json.Unmarshal([]byte(c.Input), &input))
		switch input.Action {
		case "observe":
			return fantasy.NewTextResponse(`{"window_id":"11","elements":[{"role":"ControlType.Edit","name":"Field"}]}`), nil
		case "hotkey":
			require.Equal(t, "11", input.Automation.WindowID)
			require.Equal(t, "alt", input.Modifiers)
			require.Equal(t, "f4", input.Key)
			closed = true
			return fantasy.NewTextResponse("Closed"), nil
		case "windows":
			if closed {
				return fantasy.NewTextResponse(`{"result":[]}`), nil
			}
		}
		return base(ctx, c)
	})
	require.NoError(t, err)
	require.False(t, r.IsError, r.Content)
	require.True(t, closed)
	require.Equal(t, 1, mutations)
	require.Contains(t, r.Content, `"absence_verified":true`)
	require.Contains(t, r.Content, `"kind":"prepare"`)
}

func TestDesktopFlowCloseActivatesEachWindowAndProvesAbsence(t *testing.T) {
	p := DesktopWorkflowParams{Mode: "flow", Flow: &DesktopFlowParams{Nodes: []DesktopFlowNode{
		{ID: "note", Kind: "resolve", Application: "Notepad", Next: "calc"},
		{ID: "calc", Kind: "resolve", Application: "Calculator", Next: "close_note"},
		{ID: "close_note", Kind: "close", WindowRef: "note", Next: "close_calc"},
		{ID: "close_calc", Kind: "close", WindowRef: "calc"},
	}}}
	windows := []desktopWindowInfo{{ID: "11", Name: "Saved - Notepad", ProcessID: 1, ProcessName: "notepad.exe", WindowClass: "Notepad"}, {ID: "12", Name: "Calculator", ProcessID: 2, ProcessName: "CalculatorApp.exe", WindowClass: "ApplicationFrameWindow", Foreground: true}}
	var closed []string
	r, err := runDesktopWorkflow(t.Context(), p, fantasy.ToolCall{}, func(_ context.Context, c fantasy.ToolCall) (fantasy.ToolResponse, error) {
		var input ComputerParams
		require.NoError(t, json.Unmarshal([]byte(c.Input), &input))
		switch input.Action {
		case "windows":
			data, marshalErr := json.Marshal(map[string]any{"result": windows})
			require.NoError(t, marshalErr)
			return fantasy.NewTextResponse(string(data)), nil
		case "focus":
			for i := range windows {
				windows[i].Foreground = windows[i].ID == input.Automation.WindowID
			}
			return fantasy.NewTextResponse(`{"result":{"focused":true}}`), nil
		case "hotkey":
			for i, w := range windows {
				if w.ID == input.Automation.WindowID {
					require.True(t, w.Foreground, "Must focus before closing")
					closed = append(closed, w.ID)
					windows = append(windows[:i], windows[i+1:]...)
					break
				}
			}
			return fantasy.NewTextResponse("Closed"), nil
		default:
			t.Fatalf("Unexpected action %s", input.Action)
			return fantasy.ToolResponse{}, nil
		}
	})
	require.NoError(t, err)
	require.False(t, r.IsError, r.Content)
	require.Equal(t, []string{"11", "12"}, closed)
}
