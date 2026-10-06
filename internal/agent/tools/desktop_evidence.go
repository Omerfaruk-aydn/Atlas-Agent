package tools

import (
	"encoding/json"
	"time"
)

// DesktopEvidence exposes fresh existing evidence without issuing more actions.
type DesktopEvidence struct {
	ObservedAt   time.Time `json:"observed_at"`
	WindowID     string    `json:"window_id,omitempty"`
	Foreground   string    `json:"foreground_window_id,omitempty"`
	AbsentClosed []string  `json:"absent_closed_window_ids,omitempty"`
}

func summarizeDesktopEvidence(action, content string, closing []string) *DesktopEvidence {
	var observed struct {
		WindowID   string             `json:"window_id"`
		Foreground *desktopWindowInfo `json:"foreground_window"`
		Result     json.RawMessage    `json:"result"`
	}
	if json.Unmarshal([]byte(content), &observed) != nil {
		return nil
	}
	e := &DesktopEvidence{ObservedAt: time.Now().UTC()}
	switch action {
	case "observe":
		if observed.WindowID == "" {
			return nil
		}
		e.WindowID = observed.WindowID
		if observed.Foreground != nil {
			e.Foreground = observed.Foreground.ID
		}
	case "windows":
		var windows []desktopWindowInfo
		if len(observed.Result) == 0 || observed.Result[0] != '[' || json.Unmarshal(observed.Result, &windows) != nil {
			return nil
		}
		present := make(map[string]bool, len(windows))
		for _, window := range windows {
			if window.ID == "" {
				return nil
			}
			present[window.ID] = true
			if window.Foreground {
				e.Foreground = window.ID
			}
		}
		// Native enumeration is capped at 500; a capped list cannot prove absence.
		if len(windows) < 500 {
			for _, id := range closing {
				if !present[id] {
					e.AbsentClosed = append(e.AbsentClosed, id)
				}
			}
		}
	default:
		return nil
	}
	return e
}
