package tools

import (
	"testing"

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
