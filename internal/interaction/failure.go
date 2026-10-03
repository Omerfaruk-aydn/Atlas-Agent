package interaction

import "strings"

// Failure provides bounded recovery instructions without repeating mutations.
func Failure(message string) (string, string) {
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "interaction_paused"):
		return "interaction_paused", "Wait for the user's explicit resume; do not continue automation."
	case strings.Contains(lower, "wrong_window"):
		return "wrong_window", "Inspect windows, focus the intended target, then observe again."
	case strings.Contains(lower, "ambiguous"):
		return "ambiguous_target", "Refine role, name or selector until exactly one target matches."
	case strings.Contains(lower, "accessibility_unavailable"):
		return "provider_unavailable", "Inspect OCR or a screenshot; use visual input only with an observed target."
	case strings.Contains(lower, "timeout") || strings.Contains(lower, "timed out") || strings.Contains(lower, "deadline"):
		return "timeout", "Observe current state and verify whether the action already completed before any retry."
	case strings.Contains(lower, "target_missing") || strings.Contains(lower, "no element") || strings.Contains(lower, "gone"):
		return "target_missing", "Refresh the snapshot or window tree and resolve the target again."
	case strings.Contains(lower, "disabled") || strings.Contains(lower, "offscreen") || strings.Contains(lower, "overlay"):
		return "target_not_actionable", "Observe the obstruction or disabled state; wait for the required state or request user help."
	default:
		return "driver_error", "Inspect current state before proceeding; do not assume a failed submission is safe to repeat."
	}
}
