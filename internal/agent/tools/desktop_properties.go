package tools

import (
	"encoding/json"
	"strings"
)

func desktopPropertiesShortcut(p ComputerParams) bool {
	return p.Action == "hotkey" && strings.EqualFold(p.Modifiers, "alt") && strings.EqualFold(p.Key, "enter")
}

func desktopPropertiesWindows(content string) []desktopWindowInfo {
	var envelope struct {
		Result []desktopWindowInfo `json:"result"`
	}
	if json.Unmarshal([]byte(content), &envelope) != nil || len(envelope.Result) >= 500 {
		return nil
	}
	return envelope.Result
}

// DesktopPropertiesRelated verifies a new shell dialog with a hidden helper owner.
// This permits a read only; it never changes focus or redirects input.
func desktopPropertiesRelated(sourceID string, dialog desktopWindowInfo, before, after []desktopWindowInfo) bool {
	if before == nil || after == nil || dialog.Owner == "" || dialog.Owner == "0" || dialog.ProcessID == 0 || dialog.OwnerProcessID != dialog.ProcessID {
		return false
	}
	var source desktopWindowInfo
	for _, w := range before {
		if w.ID == dialog.ID {
			return false
		}
		if w.ID == sourceID {
			source = w
		}
	}
	if source.ProcessID != dialog.ProcessID || !strings.EqualFold(source.ProcessName, "explorer.exe") || source.WindowClass != "CabinetWClass" {
		return false
	}
	foundSource, foundDialog := false, false
	for _, w := range after {
		if w.ID == dialog.Owner {
			return false
		}
		if w.ID == sourceID {
			foundSource = w.ProcessID == source.ProcessID && w.ProcessName == source.ProcessName && w.WindowClass == source.WindowClass
		}
		if w.ID == dialog.ID {
			foundDialog = w.Foreground && !w.Minimized && w.WindowClass == "#32770" && w.ProcessID == dialog.ProcessID && w.Owner == dialog.Owner && w.OwnerProcessID == dialog.OwnerProcessID
		}
	}
	return foundSource && foundDialog
}
