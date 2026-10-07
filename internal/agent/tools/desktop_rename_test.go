package tools

import (
	"context"
	"encoding/json"
	"testing"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func renameFixture(t *testing.T, change func(ComputerParams, fantasy.ToolResponse) fantasy.ToolResponse) (func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error), *[]ComputerParams) {
	t.Helper()
	calls := []ComputerParams{}
	inspections := 0
	invoke := func(_ context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		var p ComputerParams
		require.NoError(t, json.Unmarshal([]byte(call.Input), &p))
		calls = append(calls, p)
		require.Equal(t, ComputerToolName, call.Name)
		var result any = map[string]any{}
		item := desktopElement{ID: "file", Name: "hesap", Role: "ControlType.ListItem", ProcessID: 7, Enabled: true, X: 100, Y: 200, Width: 400, Height: 30}
		switch p.Action {
		case "windows":
			result = []desktopWindowInfo{{ID: "11", Name: "deneme", ProcessID: 7, ProcessName: "explorer.exe", WindowClass: "CabinetWClass", Foreground: true}}
		case "find":
			items := []desktopElement{}
			if p.Automation.Name == "hesap" {
				items = append(items, item)
			}
			result = map[string]any{"matches": items, "truncated": false}
		case "select":
			result = map[string]any{"selected": true, "selection_count": 1, "keyboard_focused": true, "element_id": "file", "window_id": "11"}
		case "inspect":
			inspections++
			e := desktopElement{ID: "editor", Name: "hesap", Role: "ControlType.Pane", ProcessID: 7, Enabled: true, KeyboardFocused: true, X: 110, Y: 202, Width: 90, Height: 25}
			if inspections == 2 {
				e.Name = "sonuc"
			}
			if inspections == 3 {
				e = item
				e.Name, e.KeyboardFocused = "sonuc", true
			}
			result = map[string]any{"window_id": "11", "focused_element": e}
		case "assert":
			actual := any(p.Automation.Expected)
			if p.Automation.Condition == "hidden" {
				actual = true
			}
			data, err := json.Marshal(map[string]any{"window_id": "11", "passed": true, "actual": actual})
			require.NoError(t, err)
			r := fantasy.NewTextResponse(string(data))
			if change != nil {
				r = change(p, r)
			}
			return r, nil
		}
		data, err := json.Marshal(map[string]any{"result": result})
		require.NoError(t, err)
		r := fantasy.NewTextResponse(string(data))
		if change != nil {
			r = change(p, r)
		}
		return r, nil
	}
	return invoke, &calls
}

func renameParams() DesktopWorkflowParams {
	return DesktopWorkflowParams{Mode: "rename", WindowID: "11", Rename: &DesktopRenameParams{OldName: "hesap", NewName: "sonuc"}}
}

func TestDesktopRenameVerifiesSelectionEditorAndCommit(t *testing.T) {
	t.Parallel()
	invoke, calls := renameFixture(t, nil)
	r, err := runDesktopWorkflow(t.Context(), renameParams(), fantasy.ToolCall{ID: "rename"}, invoke)
	require.NoError(t, err)
	require.False(t, r.IsError, r.Content)
	require.Contains(t, r.Content, `"actual_name":"sonuc"`)
	var actions []string
	var keys []string
	for _, c := range *calls {
		actions = append(actions, c.Action)
		if c.Action == "key" {
			keys = append(keys, c.Key)
		}
	}
	require.Equal(t, []string{"windows", "find", "find", "windows", "select", "key", "inspect", "hotkey", "type", "inspect", "key", "inspect", "assert", "assert", "windows"}, actions)
	require.Equal(t, []string{"f2", "enter"}, keys)
}

func TestDesktopRenameStopsAtEveryChildFailure(t *testing.T) {
	for fail := 1; fail <= 15; fail++ {
		t.Run(string(rune('A'+fail)), func(t *testing.T) {
			count := 0
			invoke, calls := renameFixture(t, func(_ ComputerParams, r fantasy.ToolResponse) fantasy.ToolResponse {
				count++
				if count == fail {
					r = fantasy.NewTextErrorResponse("permission_denied: fixture")
					r.StopTurn = true
				}
				return r
			})
			r, err := runDesktopWorkflow(t.Context(), renameParams(), fantasy.ToolCall{}, invoke)
			require.NoError(t, err)
			require.True(t, r.IsError)
			require.True(t, r.StopTurn)
			require.Len(t, *calls, fail)
		})
	}
}

func TestDesktopRenameRejectsUnverifiedSelectionAndEditor(t *testing.T) {
	for _, action := range []string{"select", "inspect", "assert", "windows"} {
		t.Run(action, func(t *testing.T) {
			invoke, calls := renameFixture(t, func(p ComputerParams, r fantasy.ToolResponse) fantasy.ToolResponse {
				if p.Action == action {
					return fantasy.NewTextResponse(`{"result":{}}`)
				}
				return r
			})
			r, err := runDesktopWorkflow(t.Context(), renameParams(), fantasy.ToolCall{}, invoke)
			require.NoError(t, err)
			require.True(t, r.IsError, r.Content)
			require.Equal(t, action, (*calls)[len(*calls)-1].Action)
		})
	}
}

func TestDesktopRenamePreflightRejectsUnsafeNamesAndMixedModes(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"", "../sonuc", "a\\b", "CON.txt", "LPT².txt", "sonuc.", "sonuc ", "a\nb"} {
		p := renameParams()
		p.Rename.NewName = name
		r, err := runDesktopWorkflow(t.Context(), p, fantasy.ToolCall{}, func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
			t.Fatal("Invalid arguments dispatched input")
			return fantasy.ToolResponse{}, nil
		})
		require.NoError(t, err)
		require.True(t, r.IsError, name)
	}
	for _, mode := range []string{"prepare", "sequence", "flow"} {
		p := renameParams()
		p.Mode = mode
		r, err := runDesktopWorkflow(t.Context(), p, fantasy.ToolCall{}, nil)
		require.NoError(t, err)
		require.True(t, r.IsError)
	}
}
