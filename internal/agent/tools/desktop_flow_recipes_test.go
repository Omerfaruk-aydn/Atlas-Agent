package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
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

func TestDesktopFlowCloseStopsAtOwnedUnsavedDialog(t *testing.T) {
	p := desktopFlowTestPlan()
	p.Flow.Nodes[1] = DesktopFlowNode{ID: "close", Kind: "close", WindowRef: "app"}
	p.Flow.Nodes[0].Next = "close"
	value, mutations, keys := "empty", 0, 0
	base := desktopFlowFixture(t, &value, &mutations)
	r, err := runDesktopWorkflow(t.Context(), p, fantasy.ToolCall{}, func(ctx context.Context, c fantasy.ToolCall) (fantasy.ToolResponse, error) {
		var input ComputerParams
		require.NoError(t, json.Unmarshal([]byte(c.Input), &input))
		if input.Action == "hotkey" {
			keys++
			return fantasy.NewTextResponse("Sent"), nil
		}
		if keys > 0 && input.Action == "windows" {
			return fantasy.NewTextResponse(`{"result":[{"window_id":"11","process_id":17},{"window_id":"22","name":"Save changes","process_id":17,"owner_window_id":"11","foreground":true}]}`), nil
		}
		return base(ctx, c)
	})
	require.NoError(t, err)
	require.True(t, r.IsError)
	require.Contains(t, r.Content, "close_blocked")
	require.Equal(t, 1, keys)
}

func TestDesktopFlowSupports64NodesAnd16Inputs(t *testing.T) {
	p := desktopFlowTestPlan()
	p.Flow.Nodes = p.Flow.Nodes[:1]
	for i := 1; i < 64; i++ {
		id := fmt.Sprintf("verify%d", i)
		p.Flow.Nodes[len(p.Flow.Nodes)-1].Next = id
		p.Flow.Nodes = append(p.Flow.Nodes, DesktopFlowNode{ID: id, Kind: "verify", WindowRef: "app", Checkpoint: computer.AutomationRequest{Name: "Field", Condition: "value", Expected: "empty"}})
	}
	value, mutations := "empty", 0
	r, err := runDesktopWorkflow(t.Context(), p, fantasy.ToolCall{}, desktopFlowFixture(t, &value, &mutations))
	require.NoError(t, err)
	require.False(t, r.IsError, r.Content)
	require.Contains(t, r.Content, "verify63")
	p = desktopFlowTestPlan()
	p.Flow.Nodes[1].Input = ComputerParams{}
	for range 16 {
		p.Flow.Nodes[1].Inputs = append(p.Flow.Nodes[1].Inputs, ComputerParams{Action: "key", Key: "enter"})
	}
	p.Flow.Nodes[1].Checkpoint.Expected = "empty"
	keys := 0
	base := desktopFlowFixture(t, &value, &mutations)
	r, err = runDesktopWorkflow(t.Context(), p, fantasy.ToolCall{}, func(ctx context.Context, c fantasy.ToolCall) (fantasy.ToolResponse, error) {
		var input ComputerParams
		require.NoError(t, json.Unmarshal([]byte(c.Input), &input))
		if input.Action == "key" {
			require.Equal(t, "11", input.Automation.WindowID)
			keys++
			return fantasy.NewTextResponse("Sent"), nil
		}
		return base(ctx, c)
	})
	require.NoError(t, err)
	require.False(t, r.IsError, r.Content)
	require.Equal(t, 16, keys)
}

func TestDesktopFlowNewRecipeJournalRejectsInterruptedReplay(t *testing.T) {
	for _, kind := range []string{"prepare", "rename", "close"} {
		t.Run(kind, func(t *testing.T) {
			p := desktopFlowTestPlan()
			if kind == "prepare" {
				p.Flow.Nodes = p.Flow.Nodes[:1]
				p.Flow.Nodes[0].Kind, p.Flow.Nodes[0].Next = "prepare", ""
			} else {
				p.Flow.Nodes[1] = DesktopFlowNode{ID: "write", Kind: kind, WindowRef: "app"}
				if kind == "rename" {
					p.Flow.Nodes[1].Rename = &DesktopRenameParams{OldName: "hesap", NewName: "sonuc"}
				}
			}
			p.Flow.RunID = "interrupted"
			ctx := context.WithValue(t.Context(), desktopFlowStoreKey{}, t.TempDir())
			ctx = context.WithValue(ctx, SessionIDContextKey, "session")
			state, save, release, err := openDesktopFlow(ctx, *p.Flow)
			require.NoError(t, err)
			id := p.Flow.Nodes[len(p.Flow.Nodes)-1].ID
			if kind != "prepare" {
				state.Completed = []desktopFlowRecord{{ID: "app", Next: "write"}}
			}
			state.Next, state.Pending, state.Attempts[id] = id, id, 1
			require.NoError(t, save(state))
			release()
			p.Flow.Resume = true
			r, err := runDesktopWorkflow(ctx, p, fantasy.ToolCall{}, func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
				t.Fatal("Interrupted effect dispatched a child")
				return fantasy.ToolResponse{}, nil
			})
			require.NoError(t, err)
			require.True(t, r.IsError)
			require.Contains(t, r.Content, "uncertain_effect")
		})
	}
}

func TestDesktopFlowRenameUsesVerifiedRecipe(t *testing.T) {
	p := DesktopWorkflowParams{Mode: "flow", Flow: &DesktopFlowParams{Nodes: []DesktopFlowNode{
		{ID: "folder", Kind: "resolve", Application: "File Explorer", Next: "rename"},
		{ID: "rename", Kind: "rename", WindowRef: "folder", Rename: &DesktopRenameParams{OldName: "hesap", NewName: "sonuc"}},
	}}}
	invoke, calls := renameFixture(t, nil)
	r, err := runDesktopWorkflow(t.Context(), p, fantasy.ToolCall{}, func(ctx context.Context, c fantasy.ToolCall) (fantasy.ToolResponse, error) {
		var input ComputerParams
		require.NoError(t, json.Unmarshal([]byte(c.Input), &input))
		if input.Action == "focus" {
			return fantasy.NewTextResponse(`{"result":{"focused":true}}`), nil
		}
		return invoke(ctx, c)
	})
	require.NoError(t, err)
	require.False(t, r.IsError, r.Content)
	require.Contains(t, r.Content, `"actual_name":"sonuc"`)
	keys := 0
	for _, c := range *calls {
		if c.Key == "f2" {
			keys++
		}
	}
	require.Equal(t, 1, keys)
}
