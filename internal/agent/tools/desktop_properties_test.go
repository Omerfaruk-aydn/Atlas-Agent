package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func TestDesktopActObservesVerifiedExplorerProperties(t *testing.T) {
	for _, kind := range []string{"valid", "existing", "foreign", "visible_owner", "missing_owner", "denied", "changed_source", "stale_foreground", "invalid_list"} {
		t.Run(kind, func(t *testing.T) {
			lists, mutations, dialogReads := 0, 0, 0
			dialog := desktopWindowInfo{ID: "22", ProcessID: 5, Owner: "33", OwnerProcessID: 5, WindowClass: "#32770", Foreground: true}
			if kind == "foreign" {
				dialog.ProcessID = 6
			}
			if kind == "missing_owner" {
				dialog.OwnerProcessID = 0
			}
			r, err := runDesktopWorkflow(t.Context(), DesktopWorkflowParams{Mode: "act", Input: ComputerParams{Action: "hotkey", Key: "enter", Modifiers: "alt", Automation: computer.AutomationRequest{WindowID: "11"}}}, fantasy.ToolCall{}, func(_ context.Context, c fantasy.ToolCall) (fantasy.ToolResponse, error) {
				var p ComputerParams
				require.NoError(t, json.Unmarshal([]byte(c.Input), &p))
				switch p.Action {
				case "windows":
					lists++
					if lists > 1 && kind == "invalid_list" {
						return fantasy.NewTextResponse(`{"result":null}`), nil
					}
					windows := []desktopWindowInfo{{ID: "11", ProcessID: 5, ProcessName: "explorer.exe", WindowClass: "CabinetWClass"}}
					if lists > 1 && kind == "changed_source" {
						windows[0].ProcessID = 7
					}
					if lists > 1 || kind == "existing" {
						windows = append(windows, dialog)
					}
					if lists > 1 && kind == "stale_foreground" {
						windows[len(windows)-1].Foreground = false
					}
					if kind == "visible_owner" {
						windows = append(windows, desktopWindowInfo{ID: "33", ProcessID: 5})
					}
					data, marshalErr := json.Marshal(map[string]any{"result": windows})
					require.NoError(t, marshalErr)
					return fantasy.NewTextResponse(string(data)), nil
				case "hotkey":
					mutations++
					return fantasy.NewTextResponse(`{}`), nil
				case "observe":
					if p.Automation.WindowID == "22" {
						dialogReads++
						if kind == "denied" {
							return fantasy.NewTextErrorResponse("permission_denied"), nil
						}
						return fantasy.NewTextResponse(`{"window_id":"22","snapshot_id":"properties","elements":[]}`), nil
					}
					data, marshalErr := json.Marshal(map[string]any{"window_id": "11", "snapshot_id": "source", "foreground_window": dialog, "elements": []any{}})
					require.NoError(t, marshalErr)
					return fantasy.NewTextResponse(string(data)), nil
				default:
					t.Fatalf("Unexpected action %s", p.Action)
					return fantasy.ToolResponse{}, nil
				}
			})
			require.NoError(t, err)
			require.Equal(t, 1, mutations)
			if kind == "denied" {
				require.True(t, r.IsError)
				require.Contains(t, r.Content, "do not replay")
			} else {
				require.False(t, r.IsError, r.Content)
				if kind == "valid" {
					require.Equal(t, 1, dialogReads)
					require.Contains(t, r.Content, `"snapshot_id":"properties"`)
				} else {
					require.Zero(t, dialogReads)
				}
			}
		})
	}
}
