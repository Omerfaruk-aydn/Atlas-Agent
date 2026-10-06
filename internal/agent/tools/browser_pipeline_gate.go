package tools

func pipelineAssertionStep(s PipelineStep) bool {
	if s.Tool == ComputerToolName {
		return s.Arguments["action"] == "assert"
	}
	if s.Tool != BrowserToolName {
		return false
	}
	switch s.Arguments["action"] {
	case "assert", "wait_for", "download_wait", "popup_wait", "dialog_wait":
		return true
	}
	return false
}
