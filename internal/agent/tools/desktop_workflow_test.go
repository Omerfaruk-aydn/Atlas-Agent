package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/schema"
	"github.com/stretchr/testify/require"
)

func TestDesktopPrepareUsesGuardedChildren(t *testing.T) {
	var actions []string
	invoke := func(_ context.Context, c fantasy.ToolCall) (fantasy.ToolResponse, error) {
		var p ComputerParams
		require.NoError(t, json.Unmarshal([]byte(c.Input), &p))
		actions = append(actions, p.Action)
		switch p.Action {
		case "windows":
			return fantasy.NewTextResponse(`{"result":[{"window_id":"11","name":"Fixture App"}]}`), nil
		case "focus":
			return fantasy.NewTextResponse(`{"result":{"focused":true}}`), nil
		case "observe":
			r := fantasy.NewImageResponse([]byte("image"), "image/png")
			r.Content = `{"snapshot_id":"s"}`
			return r, nil
		}
		t.Fatalf("Unexpected child %s", p.Action)
		return fantasy.ToolResponse{}, nil
	}
	r, err := runDesktopWorkflow(t.Context(), DesktopWorkflowParams{Mode: "prepare", Application: "Fixture App"}, fantasy.ToolCall{ID: "parent"}, invoke)
	require.NoError(t, err)
	require.False(t, r.IsError)
	require.Equal(t, []string{"windows", "focus", "observe"}, actions)
	require.Equal(t, "image", r.Type)
}

func TestDesktopPrepareAmbiguousDoesNotFocus(t *testing.T) {
	calls := 0
	r, err := runDesktopWorkflow(t.Context(), DesktopWorkflowParams{Mode: "prepare", Application: "Fixture"}, fantasy.ToolCall{}, func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		calls++
		return fantasy.NewTextResponse(`{"result":[{"window_id":"11","name":"Fixture"},{"window_id":"12","name":"Fixture"}]}`), nil
	})
	require.NoError(t, err)
	require.True(t, r.IsError)
	require.Equal(t, 1, calls)
}

func TestDesktopPrepareNotepadUsesProcessIdentity(t *testing.T) {
	for _, application := range []string{"Notepad", "Not Defteri"} {
		t.Run(application, func(t *testing.T) {
			var actions []string
			r, err := runDesktopWorkflow(t.Context(), DesktopWorkflowParams{Mode: "prepare", Application: application}, fantasy.ToolCall{}, func(_ context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
				var p ComputerParams
				require.NoError(t, json.Unmarshal([]byte(call.Input), &p))
				actions = append(actions, p.Action)
				switch p.Action {
				case "windows":
					return fantasy.NewTextResponse(`{"result":[{"window_id":"10","name":"Save As","process_name":"Notepad.exe","owner_window_id":"11","foreground":true},{"window_id":"11","name":"hesap.txt - Not Defteri","process_name":"Notepad.exe","owner_window_id":"0"},{"window_id":"12","name":"Not Defteri","process_name":"chrome.exe"}]}`), nil
				case "focus":
					require.Equal(t, "11", p.Automation.WindowID)
					return fantasy.NewTextResponse(`{"result":{"focused":true}}`), nil
				case "observe":
					return fantasy.NewTextResponse(`{"snapshot_id":"ready"}`), nil
				default:
					return fantasy.NewTextErrorResponse("Unexpected launch or action"), nil
				}
			})
			require.NoError(t, err)
			require.False(t, r.IsError, r.Content)
			require.Equal(t, []string{"windows", "focus", "observe"}, actions)
		})
	}
}

func TestDesktopPrepareNotepadPreservesAmbiguity(t *testing.T) {
	for _, test := range []struct {
		name, windows, selected string
	}{
		{
			name:    "No foreground main window",
			windows: `{"result":[{"window_id":"11","name":"one.txt - Notepad","process_name":"notepad.exe"},{"window_id":"12","name":"two.txt - Notepad","process_name":"notepad.exe"}]}`,
		},
		{
			name:     "Foreground main window",
			windows:  `{"result":[{"window_id":"11","name":"one.txt - Notepad","process_name":"notepad.exe"},{"window_id":"12","name":"two.txt - Notepad","process_name":"notepad.exe","foreground":true}]}`,
			selected: "12",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			r, err := runDesktopWorkflow(t.Context(), DesktopWorkflowParams{Mode: "prepare", Application: "Notepad"}, fantasy.ToolCall{}, func(_ context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
				calls++
				var p ComputerParams
				require.NoError(t, json.Unmarshal([]byte(call.Input), &p))
				switch p.Action {
				case "windows":
					return fantasy.NewTextResponse(test.windows), nil
				case "focus":
					require.Equal(t, test.selected, p.Automation.WindowID)
					return fantasy.NewTextResponse(`{"result":{"focused":true}}`), nil
				case "observe":
					return fantasy.NewTextResponse(`{"snapshot_id":"ready"}`), nil
				default:
					t.Fatalf("Unexpected action %s", p.Action)
					return fantasy.ToolResponse{}, nil
				}
			})
			require.NoError(t, err)
			if test.selected == "" {
				require.True(t, r.IsError)
				require.Contains(t, r.Content, "ambiguous_target")
				require.Equal(t, 1, calls)
			} else {
				require.False(t, r.IsError, r.Content)
				require.Equal(t, 3, calls)
			}
		})
	}
}

func TestDesktopPrepareSelectsLocalizedForegroundWindow(t *testing.T) {
	var focused string
	r, err := runDesktopWorkflow(t.Context(), DesktopWorkflowParams{Mode: "prepare", Application: "Calculator"}, fantasy.ToolCall{}, func(_ context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		var p ComputerParams
		require.NoError(t, json.Unmarshal([]byte(call.Input), &p))
		switch p.Action {
		case "windows":
			return fantasy.NewTextResponse(`{"result":[{"window_id":"11","name":"Hesap Makinesi"},{"window_id":"12","name":"Hesap Makinesi","foreground":true}]}`), nil
		case "focus":
			focused = p.Automation.WindowID
			return fantasy.NewTextResponse(`{"result":{"focused":true}}`), nil
		case "observe":
			return fantasy.NewTextResponse(`{"snapshot_id":"fresh"}`), nil
		default:
			t.Errorf("Unexpected action: %s", p.Action)
			return fantasy.NewTextErrorResponse("unexpected"), nil
		}
	})
	require.NoError(t, err)
	require.False(t, r.IsError, r.Content)
	require.Equal(t, "12", focused)
}

func TestDesktopPrepareRefreshesShellOnlyObservation(t *testing.T) {
	observations := 0
	r, err := runDesktopWorkflow(t.Context(), DesktopWorkflowParams{Mode: "prepare", Application: "Fixture"}, fantasy.ToolCall{}, func(_ context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		var p ComputerParams
		require.NoError(t, json.Unmarshal([]byte(call.Input), &p))
		switch p.Action {
		case "windows":
			return fantasy.NewTextResponse(`{"result":[{"window_id":"11","name":"Fixture","foreground":true}]}`), nil
		case "focus":
			return fantasy.NewTextResponse(`{"result":{"focused":true}}`), nil
		case "observe":
			observations++
			if observations == 1 {
				return fantasy.NewTextResponse(`{"elements":[{"role":"ControlType.Window"}]}`), nil
			}
			return fantasy.NewTextResponse(`{"elements":[{"role":"ControlType.Edit","automation_id":"Search"}]}`), nil
		default:
			t.Fatalf("Unexpected action %s", p.Action)
			return fantasy.ToolResponse{}, nil
		}
	})
	require.NoError(t, err)
	require.False(t, r.IsError, r.Content)
	require.Equal(t, 2, observations)
	require.Contains(t, r.Content, "Search")
}

func TestDesktopPrepareRecoversChangedForegroundHandle(t *testing.T) {
	lists := 0
	r, err := runDesktopWorkflow(t.Context(), DesktopWorkflowParams{Mode: "prepare", Application: "Fixture"}, fantasy.ToolCall{}, func(_ context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		var p ComputerParams
		require.NoError(t, json.Unmarshal([]byte(call.Input), &p))
		switch p.Action {
		case "windows":
			lists++
			if lists == 1 {
				return fantasy.NewTextResponse(`{"result":[{"window_id":"11","name":"Fixture"}]}`), nil
			}
			return fantasy.NewTextResponse(`{"result":[{"window_id":"12","name":"Fixture","foreground":true}]}`), nil
		case "focus":
			if p.Automation.WindowID == "11" {
				return fantasy.NewTextErrorResponse("focus_denied: foreground=12"), nil
			}
			return fantasy.NewTextResponse(`{"result":{"focused":true}}`), nil
		case "observe":
			require.Equal(t, "12", p.Automation.WindowID)
			return fantasy.NewTextResponse(`{"snapshot_id":"fresh"}`), nil
		default:
			t.Fatalf("Unexpected action %s", p.Action)
			return fantasy.ToolResponse{}, nil
		}
	})
	require.NoError(t, err)
	require.False(t, r.IsError, r.Content)
	require.Equal(t, 2, lists)
}

func TestDesktopPrepareDoesNotRecoverExplicitHandleOrDenial(t *testing.T) {
	for _, tc := range []struct{ id, failure string }{{"11", "focus_denied: foreground=12"}, {"", "denied: permission refused"}} {
		t.Run(tc.failure, func(t *testing.T) {
			calls := 0
			r, err := runDesktopWorkflow(t.Context(), DesktopWorkflowParams{Mode: "prepare", Application: "Fixture", WindowID: tc.id}, fantasy.ToolCall{}, func(_ context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
				calls++
				var p ComputerParams
				require.NoError(t, json.Unmarshal([]byte(call.Input), &p))
				if p.Action == "windows" {
					return fantasy.NewTextResponse(`{"result":[{"window_id":"11","name":"Fixture"}]}`), nil
				}
				require.Equal(t, "focus", p.Action)
				return fantasy.NewTextErrorResponse(tc.failure), nil
			})
			require.NoError(t, err)
			require.True(t, r.IsError)
			if tc.id != "" {
				require.Equal(t, 3, calls)
			} else {
				require.Equal(t, 2, calls)
			}
		})
	}
}

func TestDesktopPrepareBoundsShellOnlyRetries(t *testing.T) {
	observations := 0
	r, err := runDesktopWorkflow(t.Context(), DesktopWorkflowParams{Mode: "prepare", WindowID: "11"}, fantasy.ToolCall{}, func(_ context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		var p ComputerParams
		require.NoError(t, json.Unmarshal([]byte(call.Input), &p))
		switch p.Action {
		case "windows":
			return fantasy.NewTextResponse(`{"result":[{"window_id":"11","name":"Fixture"}]}`), nil
		case "focus":
			return fantasy.NewTextResponse(`{"result":{"focused":true}}`), nil
		case "observe":
			observations++
			return fantasy.NewTextResponse(`{"elements":[{"role":"ControlType.Window"}]}`), nil
		default:
			t.Fatalf("Unexpected child %s", p.Action)
			return fantasy.ToolResponse{}, nil
		}
	})
	require.NoError(t, err)
	require.False(t, r.IsError)
	require.Equal(t, 3, observations)
	require.Contains(t, r.Content, `"accessibility_status":"shell_only"`)
}

func TestDesktopFillDoesNotSubmitUnverifiedField(t *testing.T) {
	for _, content := range []string{`{"result":{"value_verified":true,"keyboard_focused":false}}`, `{"result":{"value_verified":false,"keyboard_focused":true}}`, `{}`} {
		calls := 0
		r, err := runDesktopWorkflow(t.Context(), DesktopWorkflowParams{Mode: "fill_submit", Input: ComputerParams{Action: "set_value", Automation: computer.AutomationRequest{WindowID: "11", Name: "Search", Text: "query"}}}, fantasy.ToolCall{}, func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
			calls++
			return fantasy.NewTextResponse(content), nil
		})
		require.NoError(t, err)
		require.True(t, r.IsError)
		require.Equal(t, 1, calls)
	}
}

func TestDesktopActStopsAfterDeniedInput(t *testing.T) {
	calls := 0
	r, err := runDesktopWorkflow(t.Context(), DesktopWorkflowParams{Mode: "act", Input: ComputerParams{Action: "click", Automation: computer.AutomationRequest{WindowID: "11"}}}, fantasy.ToolCall{}, func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		calls++
		return fantasy.NewTextErrorResponse("denied"), nil
	})
	require.NoError(t, err)
	require.True(t, r.IsError)
	require.Equal(t, 1, calls)
}

func TestDesktopRecipeSchemaAndFinalObservation(t *testing.T) {
	calls := 0
	tool := NewToolPipeline(func(_ context.Context, c fantasy.ToolCall) (fantasy.ToolResponse, error) {
		calls++
		var p ComputerParams
		require.NoError(t, json.Unmarshal([]byte(c.Input), &p))
		switch p.Action {
		case "set_value":
			require.True(t, p.Automation.Focus)
			return fantasy.NewTextResponse(`{"result":{"value_verified":true,"keyboard_focused":true,"element_id":"field"}}`), nil
		case "key":
			require.Equal(t, "11", p.Automation.WindowID)
			require.Equal(t, "field", p.Automation.ElementID)
			return fantasy.NewTextResponse("sent"), nil
		case "assert":
			return fantasy.NewTextResponse(`{"passed":true}`), nil
		case "observe":
			r := fantasy.NewImageResponse([]byte("frame"), "image/png")
			r.Content = `{"snapshot_id":"final","elements":[]}`
			return r, nil
		}
		t.Fatal("Unexpected child")
		return fantasy.ToolResponse{}, nil
	})
	info := tool.Info()
	encoded, _ := json.Marshal(map[string]any{"type": "object", "properties": info.Parameters, "required": info.Required})
	var spec schema.Schema
	require.NoError(t, json.Unmarshal(encoded, &spec))
	var args any
	input := `{"desktop":{"mode":"fill_submit","input":{"action":"set_value","automation":{"action":"set_value","window_id":"11","name":"Search","text":"query"}},"wait_for":{"action":"assert","name":"Result","condition":"visible"}}}`
	require.NoError(t, json.Unmarshal([]byte(input), &args))
	require.NoError(t, schema.ValidateAgainstSchema(args, spec))
	r, err := tool.Run(t.Context(), fantasy.ToolCall{ID: "recipe", Input: input})
	require.NoError(t, err)
	require.False(t, r.IsError)
	require.Equal(t, 4, calls)
	require.Equal(t, "image", r.Type)
	require.Contains(t, r.Content, `"condition_verified":true`)
	require.NoError(t, json.Unmarshal([]byte(`{"desktop":{"mode":"prepare","application":"Fixture App"}}`), &args))
	require.NoError(t, schema.ValidateAgainstSchema(args, spec))
}

func TestDesktopPrepareLaunchesOnlyOnce(t *testing.T) {
	actions := []string{}
	lists := 0
	r, err := runDesktopWorkflow(t.Context(), DesktopWorkflowParams{Mode: "prepare", Application: "Fixture App"}, fantasy.ToolCall{}, func(_ context.Context, c fantasy.ToolCall) (fantasy.ToolResponse, error) {
		var p ComputerParams
		require.NoError(t, json.Unmarshal([]byte(c.Input), &p))
		actions = append(actions, p.Action)
		switch p.Action {
		case "windows":
			lists++
			if lists == 1 {
				return fantasy.NewTextResponse(`{"result":[]}`), nil
			}
			return fantasy.NewTextResponse(`{"result":[{"window_id":"11","name":"Fixture App"}]}`), nil
		case "launch_app":
			require.Equal(t, "Fixture App", p.Automation.Name)
			return fantasy.NewTextResponse(`{"result":{"launch_requested":true}}`), nil
		case "focus":
			return fantasy.NewTextResponse(`{"result":{"focused":true}}`), nil
		case "observe":
			return fantasy.NewTextResponse(`{"snapshot_id":"s"}`), nil
		}
		t.Fatal("Unexpected child")
		return fantasy.ToolResponse{}, nil
	})
	require.NoError(t, err)
	require.False(t, r.IsError)
	require.Equal(t, []string{"windows", "launch_app", "windows", "focus", "observe"}, actions)
}

func TestDesktopCancelledAndFailedAssertionStop(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	calls := 0
	_, err := runDesktopWorkflow(ctx, DesktopWorkflowParams{Mode: "prepare", Application: "Fixture"}, fantasy.ToolCall{}, func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		calls++
		return fantasy.ToolResponse{}, nil
	})
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, calls)
	r, err := runDesktopWorkflow(t.Context(), DesktopWorkflowParams{Mode: "act", Input: ComputerParams{Action: "click", Automation: computer.AutomationRequest{WindowID: "11"}}, WaitFor: computer.AutomationRequest{Name: "Ready", Condition: "visible"}}, fantasy.ToolCall{}, func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		calls++
		if calls == 1 {
			return fantasy.NewTextResponse("sent"), nil
		}
		return fantasy.NewTextResponse(`{"passed":false}`), nil
	})
	require.NoError(t, err)
	require.True(t, r.IsError)
	require.Equal(t, 2, calls)
}

func TestDesktopSubmissionRejectsChangedFieldFocus(t *testing.T) {
	b := &efficientDesktopBackend{call: func(_ context.Context, p computer.AutomationRequest) (json.RawMessage, error) {
		require.Equal(t, "keyboard_focused", p.Condition)
		return json.RawMessage(`{"passed":false}`), nil
	}}
	s := &computerToolState{backend: b}
	r, err := s.runComputerAction(t.Context(), "key", ComputerParams{Key: "enter", Automation: computer.AutomationRequest{WindowID: "11", ElementID: "field"}})
	require.NoError(t, err)
	require.True(t, r.IsError)
	require.Empty(t, b.keys)
}
