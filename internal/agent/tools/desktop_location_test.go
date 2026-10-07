package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	"github.com/stretchr/testify/require"
)

func TestExplorerLocationRequiresMatchingNativeIdentity(t *testing.T) {
	for _, kind := range []string{"valid", "unc", "label", "foreign", "process", "background", "class", "device", "control"} {
		t.Run(kind, func(t *testing.T) {
			w := desktopWindowInfo{ID: "11", ProcessID: 7, ProcessName: "explorer.exe", WindowClass: "CabinetWClass", Foreground: true}
			l := DesktopExplorerLocation{WindowID: "11", ProcessID: 7, Path: `C:\Users\Ömer&Ceylin\deneme`, Source: "shell_current_folder", Verified: true}
			switch kind {
			case "unc":
				l.Path = `\\server\share\deneme`
			case "label":
				l.Path = "Masaüstü > deneme"
			case "foreign":
				l.WindowID = "22"
			case "process":
				l.ProcessID = 8
			case "background":
				w.Foreground = false
			case "class":
				w.WindowClass = "Shell_TrayWnd"
			case "device":
				l.Path = `\\?\C:\deneme`
			case "control":
				l.Path += "\x00"
			}
			got := verifiedDesktopLocation(&l, &w, "11")
			if kind == "valid" || kind == "unc" {
				require.NotNil(t, got)
				require.Equal(t, l.Path, got.Path)
			} else {
				require.Nil(t, got)
			}
		})
	}
}

func TestExplorerObservationReturnsExactPathWithoutExtraInputOrImage(t *testing.T) {
	b := &observationModeBackend{}
	var actions []string
	b.call = func(_ context.Context, p computer.AutomationRequest) (json.RawMessage, error) {
		actions = append(actions, p.Action)
		if p.Action == "windows" {
			return json.RawMessage(`[{"window_id":"11","process_id":7,"process_name":"explorer.exe","class_name":"CabinetWClass","foreground":true}]`), nil
		}
		return json.RawMessage(`{"window_id":"11","elements":[{"element_id":"folder","name":"deneme","role":"ControlType.ListItem","enabled":true,"width":100,"height":20}],"explorer_location":{"window_id":"11","process_id":7,"path":"C:\\Users\\Ömer&Ceylin\\deneme","verified":true,"source":"shell_current_folder"}}`), nil
	}
	s := &computerToolState{backend: b}
	r, err := s.observe(t.Context(), ComputerParams{Observation: "auto", Automation: computer.AutomationRequest{WindowID: "11"}})
	require.NoError(t, err)
	require.False(t, r.IsError, r.Content)
	require.Equal(t, "text", r.Type)
	require.Zero(t, b.captures)
	require.Equal(t, []string{"inspect", "windows"}, actions)
	var o desktopObservation
	require.NoError(t, json.Unmarshal([]byte(r.Content), &o))
	require.NotNil(t, o.ExplorerLocation)
	require.Equal(t, `C:\Users\Ömer&Ceylin\deneme`, o.ExplorerLocation.Path)
	require.Nil(t, s.observations.entries[""].ExplorerLocation)
}
