package tools

import (
	"fmt"
	"math"
	"slices"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
)

func desktopAdaptiveTarget(o desktopObservation, q computer.AutomationRequest) (*desktopElement, error) {
	if o.Truncated {
		return nil, fmt.Errorf("observation_incomplete: target resolution requires a complete observation")
	}
	var matches []desktopElement
	for _, e := range o.Elements {
		if (q.ElementID == "" || e.ID == q.ElementID) && (q.Name == "" || e.Name == q.Name) && (q.Role == "" || e.Role == q.Role) {
			matches = append(matches, e)
		}
	}
	if o.Focused != nil && !slices.ContainsFunc(matches, func(e desktopElement) bool { return e.ID == o.Focused.ID }) {
		e := *o.Focused
		if (q.ElementID == "" || e.ID == q.ElementID) && (q.Name == "" || e.Name == q.Name) && (q.Role == "" || e.Role == q.Role) {
			matches = append(matches, e)
		}
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("ambiguous_target: multiple adaptive controls match")
	}
	if len(matches) == 0 {
		return nil, nil
	}
	if matches[0].Password {
		return nil, fmt.Errorf("target_not_actionable: adaptive password controls are excluded")
	}
	return &matches[0], nil
}

func desktopAdaptiveCondition(o desktopObservation, q computer.AutomationRequest) (bool, error) {
	e, err := desktopAdaptiveTarget(o, q)
	if err != nil {
		return false, err
	}
	if e == nil {
		if q.Condition == "visible" {
			return false, nil
		}
		if q.Condition == "hidden" {
			return true, nil
		}
		return false, fmt.Errorf("target_missing: adaptive condition control was not observed")
	}
	switch q.Condition {
	case "visible":
		return !e.Offscreen, nil
	case "hidden":
		return e.Offscreen, nil
	case "enabled":
		return e.Enabled, nil
	case "keyboard_focused":
		return e.KeyboardFocused, nil
	case "text":
		return e.Name == q.Expected, nil
	case "value":
		if e.ValueAvailable && !e.ValueTruncated {
			return e.Value == q.Expected, nil
		}
	case "document_text":
		if e.TextAvailable && !e.TextTruncated {
			return e.Text == q.Expected, nil
		}
	}
	return false, fmt.Errorf("observation_incomplete: condition needs actual readable, untruncated content")
}

func desktopAdaptiveMethod(o desktopObservation, input ComputerParams) ([]ComputerParams, string, error) {
	e, err := desktopAdaptiveTarget(o, input.Automation)
	if err != nil {
		return nil, "", err
	}
	if e == nil || e.ID == "" {
		return nil, "", fmt.Errorf("target_missing: adaptive control not resolved")
	}
	if !e.Enabled || e.Offscreen {
		return nil, "", fmt.Errorf("target_not_actionable: adaptive control is disabled or offscreen")
	}
	input.Automation.ElementID = e.ID
	pattern := "Invoke"
	if input.Action == "set_value" {
		pattern = "Value"
	}
	if slices.Contains(e.Patterns, pattern) {
		return []ComputerParams{input}, "accessibility", nil
	}
	if (input.Action == "invoke" && e.Role != "ControlType.Button") || (input.Action == "set_value" && e.Role != "ControlType.Edit") {
		return nil, "", fmt.Errorf("unsupported_pattern: no verified keyboard method for this control role")
	}
	q := computer.AutomationRequest{WindowID: input.Automation.WindowID}
	focus := ComputerParams{Action: "assert", Automation: computer.AutomationRequest{WindowID: q.WindowID, ElementID: e.ID, Condition: "keyboard_focused", WaitMS: 500}}
	if input.Action == "invoke" && e.KeyboardFocused {
		return []ComputerParams{focus, {Action: "key", Key: "enter", Automation: q}}, "keyboard", nil
	}
	x, y := e.X+e.Width/2, e.Y+e.Height/2
	if e.Width <= 0 || e.Height <= 0 || math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) {
		return nil, "", fmt.Errorf("invalid_target: adaptive control has no usable bounds")
	}
	if err := computer.ValidatePoint(int(math.Round(x)), int(math.Round(y))); err != nil {
		return nil, "", err
	}
	click := ComputerParams{Action: "click", X: int(math.Round(x)), Y: int(math.Round(y)), Automation: q}
	if input.Action == "invoke" {
		// Clicking a button already invokes it; never follow it with Enter.
		return []ComputerParams{click}, "pointer", nil
	}
	steps := []ComputerParams{click, focus}
	steps = append(steps, ComputerParams{Action: "hotkey", Modifiers: "ctrl", Key: "a", Automation: q})
	if input.Automation.Text == "" {
		steps = append(steps, ComputerParams{Action: "key", Key: "backspace", Automation: q})
	} else {
		steps = append(steps, ComputerParams{Action: "type", Text: input.Automation.Text, Automation: q})
	}
	return steps, "keyboard", nil
}
