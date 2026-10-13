package interaction

// ResumePrompt is the message that continues a task after the user returns
// browser and desktop control. It never asserts that the manual step
// succeeded: the user only said control is back, so the agent must verify
// the step from a fresh observation. text translates each sentence.
func ResumePrompt(text func(string) string, reason string) string {
	if text == nil {
		text = func(s string) string { return s }
	}
	prompt := text("I returned browser and desktop control to Atlas. Continue the previous task from a fresh observation of the current page or window.")
	if reason != "" {
		prompt += " " + text("The step that was left to me:") + " " + reason
	}
	return prompt + " " + text("Check whether that step is actually complete before relying on it; if it is not, tell me exactly what is still needed instead of assuming it succeeded.")
}
