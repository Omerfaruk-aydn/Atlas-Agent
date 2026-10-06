package tools

import "strings"

type desktopWindowInfo struct {
	Minimized      bool    `json:"minimized,omitempty"`
	ID             string  `json:"window_id"`
	Name           string  `json:"name"`
	ProcessID      uint32  `json:"process_id"`
	ProcessName    string  `json:"process_name,omitempty"`
	WindowClass    string  `json:"class_name,omitempty"`
	Owner          string  `json:"owner_window_id,omitempty"`
	OwnerProcessID uint32  `json:"owner_process_id,omitempty"`
	Foreground     bool    `json:"foreground"`
	X              float64 `json:"x"`
	Y              float64 `json:"y"`
	Width          float64 `json:"width"`
	Height         float64 `json:"height"`
}

// Notepad document titles vary; match the observed process and exclude dialogs.
func desktopApplicationWindowMatches(w desktopWindowInfo, application string) bool {
	for _, adapter := range desktopApplicationAdapters() {
		for _, alias := range adapter.Aliases {
			if !strings.EqualFold(strings.TrimSpace(application), alias) {
				continue
			}
			switch adapter.ID {
			case "explorer":
				return strings.EqualFold(w.ProcessName, "explorer.exe") && w.WindowClass == "CabinetWClass" && w.Name != "" && (w.Owner == "" || w.Owner == "0")
			case "notepad":
				return strings.EqualFold(w.ProcessName, "notepad.exe") && w.Name != "" && (w.Owner == "" || w.Owner == "0")
			}
		}
	}
	return desktopApplicationMatches(w.Name, application)
}

// Match exact observed titles, including known localized Calculator titles.
// Arbitrary document titles and unrelated foreground windows never qualify.
func desktopApplicationMatches(title, application string) bool {
	title, application = strings.TrimSpace(title), strings.TrimSpace(application)
	if strings.EqualFold(title, application) {
		return true
	}
	calculator := func(name string) bool {
		for _, alias := range []string{"Calculator", "Hesap Makinesi", "Rechner", "Calculatrice", "Calcolatrice", "الحاسبة"} {
			if strings.EqualFold(name, alias) {
				return true
			}
		}
		return false
	}
	return calculator(title) && calculator(application)
}

// A small frame-only tree is evidence of missing controls, not a failed app.
func desktopShellOnly(observed map[string]any) bool {
	elements, ok := observed["elements"].([]any)
	if !ok {
		return false
	}
	if len(elements) > 8 {
		return false
	}
	for _, item := range elements {
		element, ok := item.(map[string]any)
		if !ok {
			return false
		}
		role, _ := element["role"].(string)
		switch role {
		case "ControlType.Pane":
			if focused, _ := element["keyboard_focused"].(bool); focused {
				return false
			}
		case "ControlType.Window", "ControlType.TitleBar", "ControlType.MenuBar":
		case "ControlType.Button":
			id, _ := element["automation_id"].(string)
			if id != "Minimize" && id != "Maximize" && id != "Close" {
				return false
			}
		default:
			return false
		}
	}
	return true
}
