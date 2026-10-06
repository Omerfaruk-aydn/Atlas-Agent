package tools

import (
	"context"
	"encoding/json"
	"testing"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func TestDesktopAdaptersRequireObservedIdentity(t *testing.T) {
	for _, tc := range []struct {
		window   desktopWindowInfo
		expected string
	}{
		{desktopWindowInfo{Name: "any document", ProcessName: "notepad.exe"}, "notepad"},
		{desktopWindowInfo{Name: "Notepad", ProcessName: "unknown.exe"}, "generic"},
		{desktopWindowInfo{ProcessName: "explorer.exe", WindowClass: "CabinetWClass"}, "explorer"},
		{desktopWindowInfo{ProcessName: "explorer.exe", WindowClass: "Progman"}, "generic"},
		{desktopWindowInfo{ProcessName: "notepad.exe", Owner: "33"}, "generic"},
		{desktopWindowInfo{Name: "Hesap Makinesi", ProcessName: "ApplicationFrameHost.exe"}, "calculator"},
		{desktopWindowInfo{Name: "Hesap Makinesi", ProcessName: "unknown.exe"}, "generic"},
	} {
		require.Equal(t, tc.expected, desktopAdapterForWindow(tc.window).ID)
	}
}

func TestDesktopPrepareReturnsVerifiedApplicationCapabilities(t *testing.T) {
	r, err := runDesktopWorkflow(t.Context(), DesktopWorkflowParams{Mode: "prepare", Application: "Not Defteri"}, fantasy.ToolCall{}, func(_ context.Context, c fantasy.ToolCall) (fantasy.ToolResponse, error) {
		var p ComputerParams
		require.NoError(t, json.Unmarshal([]byte(c.Input), &p))
		switch p.Action {
		case "windows":
			return fantasy.NewTextResponse(`{"result":[{"window_id":"11","name":"Document","process_name":"notepad.exe","foreground":true}]}`), nil
		case "focus":
			return fantasy.NewTextResponse(`{"result":{"focused":true}}`), nil
		case "observe":
			return fantasy.NewTextResponse(`{"window_id":"11","foreground_window":{"window_id":"11","process_name":"notepad.exe","foreground":true},"elements":[{"role":"ControlType.Edit"}]}`), nil
		default:
			t.Fatalf("Unexpected action %s", p.Action)
			return fantasy.ToolResponse{}, nil
		}
	})
	require.NoError(t, err)
	require.False(t, r.IsError, r.Content)
	require.Contains(t, r.Content, `"application_adapter":{"id":"notepad"`)
}

func TestDesktopAdapterCapabilitiesAreNotSharedMutableState(t *testing.T) {
	w := desktopWindowInfo{ProcessName: "notepad.exe"}
	a := desktopAdapterForWindow(w)
	a.Methods[0] = "modified"
	require.NotEqual(t, "modified", desktopAdapterForWindow(w).Methods[0])
}
