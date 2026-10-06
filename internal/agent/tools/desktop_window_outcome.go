package tools

import (
	"context"
	"encoding/json"
	"strings"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

// Observe window disappearance after inputs that may normally dismiss a window.
// Absence is transition evidence, not proof that a save or submission succeeded.
func desktopWindowOutcome(ctx context.Context, id string, input ComputerParams, mode string, child func(ComputerParams) (fantasy.ToolResponse, error)) (fantasy.ToolResponse, bool, error) {
	if !desktopMayDismiss(input) {
		return fantasy.ToolResponse{}, false, nil
	}
	if err := ctx.Err(); err != nil {
		return fantasy.ToolResponse{}, true, err
	}
	r, err := child(ComputerParams{Action: "windows"})
	if desktopRecipeStopped(r, err) {
		return r, true, err
	}
	return desktopWindowOutcomeFromList(id, input, mode, r)
}

func desktopMayDismiss(input ComputerParams) bool {
	key := strings.ToLower(strings.TrimSpace(input.Key))
	return input.Action == "invoke" || (input.Action == "key" && (key == "esc" || key == "escape" || key == "enter")) || (input.Action == "hotkey" && strings.EqualFold(strings.TrimSpace(input.Modifiers), "alt") && key == "f4")
}

func desktopWindowOutcomeFromList(id string, input ComputerParams, mode string, r fantasy.ToolResponse) (fantasy.ToolResponse, bool, error) {
	if !desktopMayDismiss(input) {
		return fantasy.ToolResponse{}, false, nil
	}
	closed := input.Action == "hotkey" && strings.EqualFold(strings.TrimSpace(input.Modifiers), "alt") && strings.EqualFold(strings.TrimSpace(input.Key), "f4")
	var envelope struct {
		Result []desktopWindowInfo `json:"result"`
	}
	if json.Unmarshal([]byte(r.Content), &envelope) != nil || envelope.Result == nil || len(envelope.Result) >= 500 {
		return fantasy.NewTextErrorResponse("observation_incomplete: window outcome could not be established; do not replay input"), true, nil
	}
	var foreground *desktopWindowInfo
	for _, w := range envelope.Result {
		if w.ID == "" {
			return fantasy.NewTextErrorResponse("Invalid window outcome identity; do not replay input"), true, nil
		}
		if w.ID == id {
			return fantasy.ToolResponse{}, false, nil
		}
		if w.Foreground {
			foreground = &w
		}
	}
	closing := []string(nil)
	if closed {
		closing = []string{id}
	}
	state := summarizeDesktopEvidence("windows", r.Content, closing)
	data, marshalErr := marshalDesktopResult(map[string]any{"source_window_id": id, "window_status": "absent", "input_accepted": true, "foreground_window": foreground, "desktop_state": state, "workflow": map[string]any{"mode": mode, "condition_verified": false, "task_completion_requires_verification": true}})
	if marshalErr != nil {
		return fantasy.ToolResponse{}, true, marshalErr
	}
	return fantasy.NewTextResponse(string(data)), true, nil
}
