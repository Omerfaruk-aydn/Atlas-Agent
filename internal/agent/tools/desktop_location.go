package tools

import (
	"strings"
	"unicode"
)

// DesktopExplorerLocation is read-only shell identity, not a display label.
type DesktopExplorerLocation struct {
	WindowID  string `json:"window_id"`
	ProcessID uint32 `json:"process_id"`
	Path      string `json:"path"`
	Source    string `json:"source"`
	Verified  bool   `json:"verified"`
}

func verifiedDesktopLocation(location *DesktopExplorerLocation, window *desktopWindowInfo, id string) *DesktopExplorerLocation {
	if location == nil || window == nil || !window.Foreground || window.ID != id || window.Minimized || window.WindowClass != "CabinetWClass" || !strings.EqualFold(window.ProcessName, "explorer.exe") || location.WindowID != id || location.ProcessID == 0 || location.ProcessID != window.ProcessID || location.Source != "shell_current_folder" || !location.Verified {
		return nil
	}
	path := location.Path
	if len([]rune(path)) > 4096 || strings.ContainsFunc(path, unicode.IsControl) {
		return nil
	}
	drive := len(path) >= 3 && ((path[0] >= 'a' && path[0] <= 'z') || (path[0] >= 'A' && path[0] <= 'Z')) && path[1:3] == `:\`
	unc := false
	if strings.HasPrefix(path, `\\`) && !strings.HasPrefix(path, `\\?\`) && !strings.HasPrefix(path, `\\.\`) {
		parts := strings.SplitN(path[2:], `\`, 3)
		unc = len(parts) >= 2 && parts[0] != "" && parts[1] != ""
	}
	if !drive && !unc {
		return nil
	}
	copy := *location
	return &copy
}
