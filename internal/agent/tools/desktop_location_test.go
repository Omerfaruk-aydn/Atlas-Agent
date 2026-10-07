package tools

import (
	"testing"

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
