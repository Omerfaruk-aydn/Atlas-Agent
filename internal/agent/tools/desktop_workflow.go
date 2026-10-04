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

// DesktopWorkflowParams groups known operations while preserving child controls.
type DesktopWorkflowParams struct {
	Mode        string                     `json:"mode" description:"prepare: find/launch/focus/observe an app; act: one input and fresh observation; fill_submit: verify a focused field, submit and observe."`
	Application string                     `json:"application,omitempty" description:"Exact installed application name for prepare; open window titles must match it exactly, case-insensitively. Use window_id for dynamic document titles."`
	WindowID    string                     `json:"window_id,omitempty" description:"Explicit observed window ID for prepare, avoiding title ambiguity."`
	Input       ComputerParams             `json:"input,omitempty" description:"Known input for act; set_value with text and a fresh field selector for fill_submit. window_id required."`
	WaitFor     computer.AutomationRequest `json:"wait_for,omitempty" description:"Optional expected-state assertion after input, before observation. Same window only."`
	WaitMS      int                        `json:"wait_ms,omitempty" description:"prepare launch wait, default 8000, maximum 15000."`
	MaxElements int                        `json:"max_elements,omitempty" description:"Final observation limit, default 80, maximum 500."`
}

func runDesktopWorkflow(ctx context.Context, p DesktopWorkflowParams, parent fantasy.ToolCall, invoke func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error)) (fantasy.ToolResponse, error) {
	p.Application = strings.TrimSpace(p.Application)
	if p.WaitMS < 0 || p.WaitMS > 15000 || p.MaxElements < 0 || p.MaxElements > 500 {
		return fantasy.NewTextErrorResponse("invalid desktop workflow bounds"), nil
	}
	if p.Mode != "prepare" && p.Mode != "act" && p.Mode != "fill_submit" {
		return fantasy.NewTextErrorResponse("desktop mode must be prepare/act/fill_submit"), nil
	}
	if p.WaitFor.WindowID != "" && p.WaitFor.WindowID != p.Input.Automation.WindowID {
		return fantasy.NewTextErrorResponse("wait_for must target the input window"), nil
	}
	if err := computer.ValidateAutomationRequest(p.WaitFor); err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	if p.WaitFor.Condition != "" {
		if p.WaitFor.ElementID == "" && p.WaitFor.Name == "" && p.WaitFor.Role == "" {
			return fantasy.NewTextErrorResponse("wait_for requires an explicit expected-state target"), nil
		}
		switch p.WaitFor.Condition {
		case "visible", "hidden", "enabled", "text", "value":
		default:
			return fantasy.NewTextErrorResponse("unsupported wait_for condition"), nil
		}
	}
	if p.Mode == "prepare" && (p.WaitFor.Condition != "" || p.Input.Action != "") {
		return fantasy.NewTextErrorResponse("prepare cannot include input or wait_for"), nil
	}
	if p.Mode != "prepare" && (p.Input.Automation.WindowID == "" || !desktopMutation(p.Input.Action) || p.Input.Action == "focus" || p.Input.Action == "launch_app") {
		return fantasy.NewTextErrorResponse("desktop input requires a mutation and explicit window_id"), nil
	}
	if p.Mode == "fill_submit" && (p.Input.Action != "set_value" || p.Input.Automation.Text == "") {
		return fantasy.NewTextErrorResponse("fill_submit requires set_value with automation.text"), nil
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	actions := []string{}
	operationCtx := ctx
	child := func(input ComputerParams) (fantasy.ToolResponse, error) {
		if err := operationCtx.Err(); err != nil {
			return fantasy.ToolResponse{}, err
		}
		data, err := json.Marshal(input)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		if len(data) > 64*1024 {
			return fantasy.NewTextErrorResponse("desktop child arguments exceed limit"), nil
		}
		actions = append(actions, input.Action)
		return invoke(operationCtx, fantasy.ToolCall{ID: fmt.Sprintf("%s/desktop/%d", parent.ID, len(actions)), Name: ComputerToolName, Input: string(data)})
	}
	stopped := func(r fantasy.ToolResponse, err error) bool { return err != nil || r.IsError || r.StopTurn }
	id := p.Input.Automation.WindowID
	if p.Mode == "prepare" {
		if strings.TrimSpace(p.Application) == "" && p.WindowID == "" {
			return fantasy.NewTextErrorResponse("prepare requires application or explicit window_id"), nil
		}
		find := func() (string, fantasy.ToolResponse, error) {
			r, err := child(ComputerParams{Action: "windows"})
			if stopped(r, err) {
				return "", r, err
			}
			var envelope struct {
				Result []struct {
					ID   string `json:"window_id"`
					Name string `json:"name"`
				} `json:"result"`
			}
			if json.Unmarshal([]byte(r.Content), &envelope) != nil {
				return "", fantasy.NewTextErrorResponse("Invalid window list"), nil
			}
			matches := []string{}
			for _, w := range envelope.Result {
				if (p.WindowID != "" && w.ID == p.WindowID) || (p.WindowID == "" && strings.EqualFold(strings.TrimSpace(w.Name), p.Application)) {
					matches = append(matches, w.ID)
				}
			}
			if len(matches) > 1 {
				return "", fantasy.NewTextErrorResponse("ambiguous_target: multiple application windows; select window_id"), nil
			}
			if len(matches) == 1 {
				return matches[0], r, nil
			}
			return "", r, nil
		}
		var r fantasy.ToolResponse
		var err error
		id, r, err = find()
		if stopped(r, err) {
			return r, err
		}
		if id == "" {
			if p.WindowID != "" {
				return fantasy.NewTextErrorResponse("target_missing: supplied window disappeared"), nil
			}
			r, err = child(ComputerParams{Action: "launch_app", Automation: computer.AutomationRequest{Name: p.Application}})
			if stopped(r, err) {
				return r, err
			}
			wait := p.WaitMS
			if wait == 0 {
				wait = 8000
			}
			waitCtx, stop := context.WithTimeout(ctx, time.Duration(wait)*time.Millisecond)
			defer stop()
			operationCtx = waitCtx
			for id == "" {
				select {
				case <-waitCtx.Done():
					return fantasy.NewTextErrorResponse("condition_timeout: application window did not appear; inspect windows before retrying launch"), nil
				case <-time.After(300 * time.Millisecond):
				}
				id, r, err = find()
				if stopped(r, err) {
					return r, err
				}
			}
			operationCtx = ctx
		}
		r, err = child(ComputerParams{Action: "focus", Automation: computer.AutomationRequest{WindowID: id}})
		if stopped(r, err) {
			return r, err
		}
		var focus struct {
			Result struct {
				Focused bool `json:"focused"`
			} `json:"result"`
		}
		if json.Unmarshal([]byte(r.Content), &focus) != nil || !focus.Result.Focused {
			return fantasy.NewTextErrorResponse("focus_denied: application focus was not confirmed"), nil
		}
	} else {
		input := p.Input
		if p.Mode == "fill_submit" {
			input.Automation.Focus = true
		}
		r, err := child(input)
		if stopped(r, err) {
			return r, err
		}
		if p.Mode == "fill_submit" {
			var field struct {
				Result struct {
					ElementID       string `json:"element_id"`
					ValueVerified   bool   `json:"value_verified"`
					KeyboardFocused bool   `json:"keyboard_focused"`
				} `json:"result"`
			}
			if json.Unmarshal([]byte(r.Content), &field) != nil || !field.Result.ValueVerified || !field.Result.KeyboardFocused || field.Result.ElementID == "" {
				return fantasy.NewTextErrorResponse("field_not_ready: text or keyboard focus unverified; Enter was not sent"), nil
			}
			r, err = child(ComputerParams{Action: "key", Key: "enter", Automation: computer.AutomationRequest{WindowID: id, ElementID: field.Result.ElementID}})
			if stopped(r, err) {
				return r, err
			}
		}
		if p.WaitFor.Condition != "" {
			assertion := p.WaitFor
			assertion.WindowID = id
			r, err = child(ComputerParams{Action: "assert", Automation: assertion})
			if stopped(r, err) {
				return r, err
			}
			var state struct {
				Passed bool `json:"passed"`
			}
			if json.Unmarshal([]byte(r.Content), &state) != nil || !state.Passed {
				return fantasy.NewTextErrorResponse("gate_failed: expected application state not confirmed"), nil
			}
		} else {
			// A small settle interval does not certify that the application is ready.
			select {
			case <-ctx.Done():
				return fantasy.ToolResponse{}, ctx.Err()
			case <-time.After(250 * time.Millisecond):
			}
		}
	}
	limit := p.MaxElements
	if limit == 0 {
		limit = 80
	}
	r, err := child(ComputerParams{Action: "observe", Automation: computer.AutomationRequest{WindowID: id, MaxElements: limit}})
	if stopped(r, err) {
		return r, err
	}
	var observed map[string]any
	if json.Unmarshal([]byte(r.Content), &observed) != nil {
		return fantasy.NewTextErrorResponse("Invalid final desktop observation; do not replay the input"), nil
	}
	observed["workflow"] = map[string]any{"mode": p.Mode, "window_id": id, "actions": actions, "condition_verified": p.WaitFor.Condition != "", "task_completion_requires_verification": true}
	data, err := json.Marshal(observed)
	if err != nil {
		return fantasy.ToolResponse{}, err
	}
	if len(data) > 64*1024 {
		return fantasy.NewTextErrorResponse("Desktop observation exceeds 64 KiB; request a smaller observation without replaying input"), nil
	}
	r.Content = string(data)
	return r, nil
}
