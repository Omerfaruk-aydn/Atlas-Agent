package tools

// desktopTargetState separates two facts models routinely conflate: the
// window that was observed, and the window that currently receives input.
// Observation never changes focus.
type desktopTargetState struct {
	WindowID string `json:"window_id"`
	// Foreground is yes, no or unknown (the native layer reported no
	// foreground window).
	Foreground         string `json:"foreground"`
	ForegroundWindowID string `json:"foreground_window_id,omitempty"`
	// FocusChangedByObservation is always false; it is stated so the model
	// does not assume that observing a background window brought it forward.
	FocusChangedByObservation bool `json:"focus_changed_by_observation"`
	// InputReady is true only when the observed window is the verified
	// foreground window at observation time. Keyboard input to any other
	// window is refused with wrong_window.
	InputReady    bool     `json:"input_ready"`
	Note          string   `json:"note,omitempty"`
	InvalidatedBy []string `json:"evidence_invalidated_by"`
}

func desktopTargetFor(windowID string, foreground *desktopWindowInfo) desktopTargetState {
	state := desktopTargetState{
		WindowID:      windowID,
		Foreground:    "unknown",
		InvalidatedBy: []string{"focus or foreground change", "any input to this window", "window handle or process replacement", "application exit", "a newer observation of this window"},
	}
	switch {
	case foreground == nil:
		state.Note = "No foreground window was reported; verify focus before keyboard input."
	case foreground.ID == windowID:
		state.Foreground, state.ForegroundWindowID, state.InputReady = "yes", foreground.ID, true
	default:
		state.Foreground, state.ForegroundWindowID = "no", foreground.ID
		state.Note = "Observing a window does not focus it. This window is NOT the foreground window, so keyboard input is rejected until it is focused: use computer focus (or tool_pipeline prepare / focus_window), then observe again."
	}
	return state
}
