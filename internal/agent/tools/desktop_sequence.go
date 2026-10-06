package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

const (
	desktopInputLimit        = 16
	desktopSequenceStepLimit = 24
	desktopSequenceCallLimit = 128
)

// DesktopWorkflowStep performs one input followed by verified fresh readback.
type DesktopWorkflowStep struct {
	FocusWindow bool                       `json:"focus_window,omitempty" description:"Activate this explicit already-observed window before inputs. Enables known multi-window sequences; focus must be confirmed and never redirects to another window."`
	Input       ComputerParams             `json:"input,omitempty" description:"One known mutation with explicit window_id; alternative to inputs."`
	Inputs      []ComputerParams           `json:"inputs,omitempty" description:"1-16 known inputs for one logical operation, all in the same window, followed by the checkpoint."`
	Checkpoint  computer.AutomationRequest `json:"checkpoint" description:"Required expected state with explicit window_id, target and condition; wait_ms maximum 15000."`
}

func desktopKnownInputs(single ComputerParams, multiple []ComputerParams) ([]ComputerParams, error) {
	if len(multiple) > 0 && single.Action != "" {
		return nil, fmt.Errorf("use input or inputs, not both")
	}
	inputs := multiple
	if len(inputs) == 0 {
		inputs = []ComputerParams{single}
	}
	if len(inputs) > desktopInputLimit {
		return nil, fmt.Errorf("a logical desktop operation requires 1-16 inputs")
	}
	for _, input := range inputs {
		if err := validateDesktopInput(input); err != nil {
			return nil, err
		}
		if input.Automation.WindowID != inputs[0].Automation.WindowID {
			return nil, fmt.Errorf("logical inputs must target the same window")
		}
	}
	return inputs, nil
}

func validateDesktopChild(p ComputerParams) error {
	if p.Batch != nil || p.Action == "batch" {
		return fmt.Errorf("nested computer batches are prohibited")
	}
	if err := computer.ValidateAutomationRequest(p.Automation); err != nil {
		return err
	}
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if len(data) > 64*1024 {
		return fmt.Errorf("desktop child arguments exceed limit")
	}
	return nil
}

func validateDesktopInput(p ComputerParams) error {
	if err := validateDesktopChild(p); err != nil {
		return err
	}
	if strings.TrimSpace(p.Automation.WindowID) == "" {
		return fmt.Errorf("desktop input requires explicit window_id")
	}
	if p.Automation.Action != "" && p.Automation.Action != p.Action {
		return fmt.Errorf("input automation.action must match action")
	}
	if p.Element != 0 || p.SnapshotID != "" {
		return fmt.Errorf("recipe inputs require explicit fresh selectors, not snapshot references")
	}
	switch p.Action {
	case "click", "double_click", "right_click", "move", "drag":
		if err := computer.ValidatePoint(p.X, p.Y); err != nil {
			return err
		}
		if p.Action == "click" {
			if _, err := computer.ParseButton(p.Button); err != nil {
				return err
			}
		}
		if p.Action == "drag" {
			return computer.ValidatePoint(p.EndX, p.EndY)
		}
	case "key", "hotkey":
		key := strings.ToLower(strings.TrimSpace(p.Key))
		code, ok := computer.ResolveKey(key)
		if !ok {
			return fmt.Errorf("unknown desktop key")
		}
		if p.Action == "hotkey" {
			asciiKey := len(key) == 1 && ((key[0] >= 'a' && key[0] <= 'z') || (key[0] >= '0' && key[0] <= '9'))
			if code == 0 && !asciiKey {
				return fmt.Errorf("hotkey requires a named key or ASCII letter/digit")
			}
			mods, err := computer.ParseModifiers(splitModifiers(p.Modifiers))
			if err != nil {
				return err
			}
			if len(mods) == 0 {
				return fmt.Errorf("hotkey requires modifiers")
			}
		}
	case "type":
		if p.Text == "" {
			return fmt.Errorf("type requires text")
		}
	case "invoke", "set_value":
		if p.Automation.ElementID == "" && p.Automation.Name == "" && p.Automation.Role == "" {
			return fmt.Errorf("accessibility input requires explicit target")
		}
	case "scroll":
	default:
		return fmt.Errorf("unsupported desktop recipe input %q", p.Action)
	}
	return nil
}

func validateDesktopCheckpoint(p computer.AutomationRequest, id string) error {
	if p.WindowID != id || strings.TrimSpace(p.WindowID) == "" {
		err := computer.NewContractError("checkpoint_target_missing", "checkpoint.window_id", "checkpoint must explicitly target the input window: checkpoint.window_id is missing or differs from the input window. No input sent", "Copy the observed window_id from prepare/observe into both input.automation.window_id and checkpoint.window_id.", `{"checkpoint":{"window_id":"<window_id from prepare/observe>","name":"<observed element name>","condition":"visible"},"input":{"action":"key","key":"enter","automation":{"window_id":"<same window_id>"}}}`)
		return err
	}
	if p.Action != "" && p.Action != "assert" {
		return fmt.Errorf("checkpoint action must be assert with an observed selector, condition and expected result; capture_window/screenshot is diagnostic evidence, not a batch checkpoint. No batch inputs were sent")
	}
	if p.ElementID == "" && p.Name == "" && p.Role == "" {
		return fmt.Errorf("checkpoint requires explicit expected-state target")
	}
	switch p.Condition {
	case "visible", "hidden", "enabled", "text", "value", "document_text", "keyboard_focused":
	default:
		return fmt.Errorf("unsupported checkpoint condition")
	}
	return validateDesktopChild(ComputerParams{Action: "assert", Automation: p})
}

func desktopRecipeChild(parent fantasy.ToolCall, invoke func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error)) func(context.Context, ComputerParams) (fantasy.ToolResponse, error) {
	calls := 0
	return func(ctx context.Context, p ComputerParams) (fantasy.ToolResponse, error) {
		if err := ctx.Err(); err != nil {
			return fantasy.ToolResponse{}, err
		}
		if err := validateDesktopChild(p); err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		data, err := json.Marshal(p)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		calls++
		return invoke(ctx, fantasy.ToolCall{ID: fmt.Sprintf("%s/desktop/%d", parent.ID, calls), Name: ComputerToolName, Input: string(data)})
	}
}

func desktopRecipeStopped(r fantasy.ToolResponse, err error) bool {
	return err != nil || r.IsError || r.StopTurn
}

func desktopRecipeObservation(p DesktopWorkflowParams, id string) ComputerParams {
	limit := p.MaxElements
	if limit == 0 {
		limit = 80
	}
	return ComputerParams{Action: "observe", Observation: p.Observation, Automation: computer.AutomationRequest{WindowID: id, MaxElements: limit}}
}

func desktopRecipeResult(r fantasy.ToolResponse, mode, id string, completed int) (fantasy.ToolResponse, error) {
	var observed map[string]any
	if json.Unmarshal([]byte(r.Content), &observed) != nil || observed == nil {
		return fantasy.NewTextErrorResponse("Invalid desktop observation; do not replay the input"), nil
	}
	observed["workflow"] = map[string]any{"mode": mode, "window_id": id, "completed_steps": completed, "condition_verified": mode == "sequence", "task_completion_requires_verification": true}
	data, err := marshalDesktopResult(observed)
	if err != nil {
		return fantasy.ToolResponse{}, err
	}
	if len(data) > 64*1024 {
		return fantasy.NewTextErrorResponse("Desktop observation exceeds 64 KiB; request a smaller observation without replaying input"), nil
	}
	r.Content = string(data)
	return r, nil
}

// Stable keyboard steps can reuse an exact assertion without another tree read.
// Visual requests and changed targets retain their explicit observation boundary.
func desktopStableReadback(p DesktopWorkflowParams, i int, content string) bool {
	if (p.Observation != "auto" && p.Observation != "semantic") || i+1 >= len(p.Steps) {
		return false
	}
	current, next := p.Steps[i].Checkpoint, p.Steps[i+1]
	if current.ElementID == "" || current.WindowID != next.Checkpoint.WindowID || current.ElementID != next.Checkpoint.ElementID || current.Name != next.Checkpoint.Name || current.Role != next.Checkpoint.Role {
		return false
	}
	switch current.Condition {
	case "text", "value", "document_text":
	default:
		return false
	}
	for _, input := range next.Inputs {
		if input.Action != "type" && input.Action != "key" && input.Action != "hotkey" {
			return false
		}
	}
	var state struct {
		WindowID  string  `json:"window_id"`
		Actual    *string `json:"actual"`
		Truncated bool    `json:"actual_truncated"`
	}
	return json.Unmarshal([]byte(content), &state) == nil && state.WindowID == current.WindowID && state.Actual != nil && *state.Actual == current.Expected && len([]rune(*state.Actual)) <= 4096 && !state.Truncated
}

func runDesktopSequence(ctx context.Context, p DesktopWorkflowParams, parent fantasy.ToolCall, invoke func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error)) (fantasy.ToolResponse, error) {
	if len(p.Steps) == 0 || len(p.Steps) > desktopSequenceStepLimit || p.Transition != nil || p.Input.Action != "" || len(p.Inputs) > 0 || p.WaitFor.Condition != "" || p.Application != "" || p.WindowID != "" || p.WaitMS != 0 {
		return fantasy.NewTextErrorResponse("sequence requires 1-24 steps without other recipe parameters"), nil
	}
	p.Steps = append([]DesktopWorkflowStep(nil), p.Steps...)
	inputCount := 0
	for i := range p.Steps {
		step := &p.Steps[i]
		inputs, err := desktopKnownInputs(step.Input, step.Inputs)
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		step.Input = inputs[0]
		step.Inputs = inputs
		inputCount += len(inputs)
		if step.FocusWindow {
			inputCount++
		}
		if inputCount+2*len(p.Steps) > desktopSequenceCallLimit {
			return fantasy.NewTextErrorResponse("sequence exceeds 128 normal child operations; split at a verified logical boundary"), nil
		}
		if err := validateDesktopCheckpoint(step.Checkpoint, step.Input.Automation.WindowID); err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		if err := validateDesktopChild(desktopRecipeObservation(p, step.Input.Automation.WindowID)); err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	child := desktopRecipeChild(parent, invoke)
	var result fantasy.ToolResponse
	checkpoints := []map[string]any{}
	progress := func(r fantasy.ToolResponse) fantasy.ToolResponse {
		metadata := map[string]any{}
		if r.Metadata != "" {
			_ = json.Unmarshal([]byte(r.Metadata), &metadata)
		}
		if metadata == nil {
			metadata = map[string]any{}
		}
		metadata["completed_checkpoints"] = checkpoints
		data, err := json.Marshal(metadata)
		if err == nil && len(data) <= 64*1024 {
			r.Metadata = string(data)
		}
		if len(checkpoints) > 0 {
			evidence, marshalErr := json.Marshal(checkpoints)
			if marshalErr == nil && len(r.Content)+len(evidence)+32 <= 64*1024 {
				r.Content += "\nCompleted checkpoints: " + string(evidence)
			}
		}
		return r
	}
	for i, step := range p.Steps {
		var r fantasy.ToolResponse
		var err error
		if step.FocusWindow {
			r, err = child(ctx, ComputerParams{Action: "focus", Automation: computer.AutomationRequest{WindowID: step.Input.Automation.WindowID}})
			if desktopRecipeStopped(r, err) {
				return progress(r), err
			}
			var state struct {
				Result struct {
					Focused  bool   `json:"focused"`
					WindowID string `json:"window_id"`
				} `json:"result"`
			}
			if json.Unmarshal([]byte(r.Content), &state) != nil || !state.Result.Focused || (state.Result.WindowID != "" && state.Result.WindowID != step.Input.Automation.WindowID) {
				return progress(fantasy.NewTextErrorResponse("focus_denied: sequence window activation was not confirmed")), nil
			}
		}
		for _, input := range step.Inputs {
			r, err = child(ctx, input)
			if desktopRecipeStopped(r, err) {
				return progress(r), err
			}
		}
		checkpointCtx, stop := context.WithTimeout(ctx, 15*time.Second)
		r, err = child(checkpointCtx, ComputerParams{Action: "assert", Automation: step.Checkpoint})
		if desktopRecipeStopped(r, err) {
			stop()
			return progress(r), err
		}
		var state struct {
			Passed bool `json:"passed"`
		}
		if json.Unmarshal([]byte(r.Content), &state) != nil || !state.Passed {
			stop()
			failed := fantasy.NewTextErrorResponse("gate_failed: checkpoint state not confirmed; do not replay input")
			if json.Valid([]byte(r.Content)) && len(r.Content) <= 8*1024 {
				failed.Content += "\nFailed checkpoint: " + r.Content
			}
			return progress(failed), nil
		}
		if len(r.Content) > 8*1024 {
			stop()
			return progress(fantasy.NewTextErrorResponse("Checkpoint evidence exceeds limit; input was not replayed")), nil
		}
		assertion := json.RawMessage(r.Content)
		if p.Result == "checkpoints" {
			if !desktopCheckpointReadback(r.Content, step.Checkpoint) {
				stop()
				return progress(fantasy.NewTextErrorResponse("observation_incomplete: checkpoints result requires actual untruncated native readback; input was not replayed")), nil
			}
			stop()
			checkpoints = append(checkpoints, map[string]any{"step": i + 1, "window_id": step.Checkpoint.WindowID, "assertion": assertion, "readback_source": "assertion", "observation_omitted": true})
			continue
		}
		if desktopStableReadback(p, i, r.Content) {
			stop()
			checkpoints = append(checkpoints, map[string]any{"step": i + 1, "window_id": step.Checkpoint.WindowID, "assertion": assertion, "readback_source": "assertion", "observed_at": time.Now().UTC(), "observation_omitted": true})
			continue
		}
		r, err = child(checkpointCtx, desktopRecipeObservation(p, step.Input.Automation.WindowID))
		stop()
		if desktopRecipeStopped(r, err) {
			return progress(r), err
		}
		result, err = desktopRecipeResult(r, "sequence", step.Input.Automation.WindowID, i+1)
		if desktopRecipeStopped(result, err) {
			return progress(result), err
		}
		var observation desktopObservation
		if json.Unmarshal([]byte(r.Content), &observation) != nil || observation.WindowID != step.Input.Automation.WindowID {
			return progress(fantasy.NewTextErrorResponse("Invalid checkpoint observation identity; do not replay input")), nil
		}
		readback := []desktopElement{}
		for _, e := range observation.Elements {
			q := step.Checkpoint
			if (q.ElementID == "" || q.ElementID == e.ID) && (q.Name == "" || q.Name == e.Name) && (q.Role == "" || q.Role == e.Role) {
				readback = append(readback, e)
			}
		}
		checkpoints = append(checkpoints, map[string]any{"step": i + 1, "window_id": observation.WindowID, "snapshot_id": observation.ID, "assertion": assertion, "readback": readback, "observation_truncated": observation.Truncated, "image_omitted": i < len(p.Steps)-1 && r.Type == "image"})
		var final map[string]any
		_ = json.Unmarshal([]byte(result.Content), &final)
		final["checkpoints"] = checkpoints
		data, marshalErr := marshalDesktopResult(final)
		if marshalErr != nil {
			return progress(result), marshalErr
		}
		if len(data) > 64*1024 {
			return progress(fantasy.NewTextErrorResponse("Sequence evidence exceeds 64 KiB; completed inputs were not replayed")), nil
		}
		result.Content = string(data)
	}
	if p.Result == "checkpoints" {
		r, err := child(ctx, ComputerParams{Action: "windows"})
		if desktopRecipeStopped(r, err) {
			return progress(r), err
		}
		var listed struct {
			Windows []desktopWindowInfo `json:"result"`
		}
		if json.Unmarshal([]byte(r.Content), &listed) != nil {
			return progress(fantasy.NewTextErrorResponse("Invalid final sequence window evidence")), nil
		}
		id := p.Steps[len(p.Steps)-1].Checkpoint.WindowID
		for _, w := range listed.Windows {
			if w.ID == id && w.Foreground && !w.Minimized {
				data, err := marshalDesktopResult(map[string]any{"window_id": id, "foreground_window": w, "checkpoints": checkpoints, "workflow": map[string]any{"mode": "sequence", "result": "checkpoints", "completed_steps": len(checkpoints), "condition_verified": true, "task_completion_requires_verification": true, "observation_omitted": true}})
				if err != nil {
					return progress(r), err
				}
				if len(data) > 64*1024 {
					return progress(fantasy.NewTextErrorResponse("Checkpoint result exceeds limit")), nil
				}
				return fantasy.NewTextResponse(string(data)), nil
			}
		}
		return progress(fantasy.NewTextErrorResponse("wrong_window: checkpoints verified but final foreground changed; observe before further input")), nil
	}
	return result, nil
}

func desktopCheckpointReadback(content string, q computer.AutomationRequest) bool {
	if !desktopAdaptiveAssertion(content, q.WindowID) {
		return false
	}
	var state struct {
		Actual json.RawMessage `json:"actual"`
	}
	if json.Unmarshal([]byte(content), &state) != nil || len(state.Actual) == 0 || string(state.Actual) == "null" {
		return false
	}
	if q.Condition == "text" || q.Condition == "value" || q.Condition == "document_text" {
		var actual string
		return json.Unmarshal(state.Actual, &actual) == nil && actual == q.Expected && len([]rune(actual)) <= 4096
	}
	var actual bool
	if json.Unmarshal(state.Actual, &actual) != nil {
		return false
	}
	if q.Condition == "hidden" {
		// Hidden providers may report visibility or offscreen state as actual.
		// The native passed flag establishes the requested condition.
		return true
	}
	return actual
}
