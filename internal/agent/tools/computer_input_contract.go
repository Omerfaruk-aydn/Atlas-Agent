package tools

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

// NormalizeComputerCall exposes the effective keyboard input to pre-tool hooks.
// Other arguments and tools retain their original encoding.
func NormalizeComputerCall(call fantasy.ToolCall) (fantasy.ToolCall, error) {
	if call.Name != ComputerToolName {
		return call, nil
	}
	var p ComputerParams
	if err := json.Unmarshal([]byte(call.Input), &p); err != nil {
		return call, err
	}
	if p.Action != "key" && p.Action != "hotkey" {
		return call, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(call.Input), &fields); err != nil {
		return call, err
	}
	for name, value := range map[string]string{"action": p.Action, "key": p.Key, "modifiers": p.Modifiers} {
		fields[name], _ = json.Marshal(value)
	}
	if raw, ok := fields["automation"]; ok && string(raw) != "null" {
		var automation map[string]json.RawMessage
		if err := json.Unmarshal(raw, &automation); err != nil {
			return call, err
		}
		delete(automation, "key")
		delete(automation, "modifiers")
		fields["automation"], _ = json.Marshal(automation)
	}
	data, err := json.Marshal(fields)
	call.Input = string(data)
	return call, err
}

// automationCoordinateKeys lists coordinate-like fields a model sometimes puts
// inside automation, with the actions where the top-level meaning is
// identical. Hoisting is only done for these exact pairs.
var automationCoordinateKeys = map[string][]string{
	"x":      {"ocr", "capture_region", "move", "click", "double_click", "right_click", "drag", "scroll"},
	"y":      {"ocr", "capture_region", "move", "click", "double_click", "right_click", "drag", "scroll"},
	"width":  {"ocr", "capture_region"},
	"height": {"ocr", "capture_region"},
	"end_x":  {"drag"},
	"end_y":  {"drag"},
}

var topLevelHints = map[string]string{
	"window_id": "put it inside automation", "element_id": "put it inside automation", "name": "put it inside automation",
	"role": "put it inside automation", "condition": "put it inside automation", "expected": "put it inside automation",
	"wait_ms": "put it inside automation", "max_elements": "put it inside automation",
	"mode": "mode belongs to tool_pipeline desktop, not computer", "desktop": "desktop belongs to tool_pipeline, not computer",
	"steps": "steps belong to tool_pipeline desktop mode sequence", "inputs": "inputs belong to tool_pipeline desktop mode act",
	"input": "input belongs to tool_pipeline desktop mode act", "checkpoint": "checkpoints belong to tool_pipeline desktop sequence steps",
	"application": "applications are resolved by tool_pipeline desktop mode prepare", "focus_window": "focus_window belongs to tool_pipeline desktop act",
}

// UnmarshalJSON accepts equivalent encodings across all providers and rejects
// everything else before any desktop input is dispatched: unknown keys,
// conflicting keyboard or coordinate representations and unscoped OCR regions.
func (p *ComputerParams) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	var action string
	if raw, ok := fields["action"]; ok {
		_ = json.Unmarshal(raw, &action)
	}
	action = strings.ToLower(strings.TrimSpace(action))
	if err := wrongToolError(action, action); err != nil {
		return err
	}
	if unknown := computer.UnknownFields(fields, ComputerParams{}); len(unknown) > 0 {
		hint := ""
		for _, key := range unknown {
			if note, ok := topLevelHints[key]; ok {
				hint += " " + key + ": " + note + "."
			}
		}
		return computer.NewContractError("unknown_field", unknown[0], fmt.Sprintf("unknown computer field(s) %s; allowed: %s.%s Unknown fields are rejected instead of ignored. No input sent", strings.Join(unknown, ", "), strings.Join(computer.KnownFields(ComputerParams{}), ", "), hint), "Remove or move the listed fields and call again.", `{"action":"observe","observation":"semantic","automation":{"window_id":"<window_id from windows/observe>"}}`)
	}
	coerceScalars(fields, []string{"x", "y", "width", "height", "end_x", "end_y", "scroll_x", "scroll_y", "element"}, []string{"full_res"})
	var automation map[string]json.RawMessage
	if raw, ok := fields["automation"]; ok && string(raw) != "null" {
		if err := json.Unmarshal(raw, &automation); err != nil {
			return err
		}
		coerceScalars(automation, []string{"x", "y", "width", "height", "end_x", "end_y", "max_elements", "wait_ms"}, []string{"focus"})
	}
	keyboard := map[string]string{}
	hoisted := map[string]json.RawMessage{}
	for name, raw := range automation {
		switch name {
		case "key", "modifiers":
			if action != "key" && action != "hotkey" {
				return fmt.Errorf("automation.%s requires key/hotkey action; keyboard fields belong at the top level", name)
			}
			var value string
			if err := json.Unmarshal(raw, &value); err != nil || string(raw) == "null" {
				return fmt.Errorf("automation.%s must be a string", name)
			}
			keyboard[name] = value
			delete(automation, name)
		default:
			actions, ok := automationCoordinateKeys[name]
			if !ok {
				continue
			}
			if !slices.Contains(actions, action) {
				return computer.NewContractError("misplaced_field", "automation."+name, fmt.Sprintf("automation.%s is not used by action %q; coordinates and region size are top-level fields of pointer/ocr/capture_region actions. No input sent", name, action), "Remove it, or use the action that takes it.", "")
			}
			var number float64
			if err := json.Unmarshal(raw, &number); err != nil || number != float64(int64(number)) {
				return computer.NewContractError("invalid_region", "automation."+name, fmt.Sprintf("automation.%s must be an integer pixel value. No input sent", name), "Send integer native pixel values at the top level.", "")
			}
			if top, exists := fields[name]; exists && string(top) != string(raw) {
				return computer.NewContractError("conflicting_fields", name, fmt.Sprintf("top-level %s and automation.%s differ; no coordinate was guessed and no input was sent", name, name), "Send each value once, at the top level.", "")
			}
			hoisted[name] = raw
			delete(automation, name)
		}
	}
	if len(hoisted) > 0 || len(keyboard) > 0 || action != "" || automation != nil {
		canonical := make(map[string]json.RawMessage, len(fields))
		for name, raw := range fields {
			canonical[name] = raw
		}
		for name, raw := range hoisted {
			canonical[name] = raw
		}
		if automation != nil {
			canonical["automation"], _ = json.Marshal(automation)
		}
		canonical["action"], _ = json.Marshal(action)
		var err error
		if data, err = json.Marshal(canonical); err != nil {
			return err
		}
	}
	type plain ComputerParams
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	for name, target := range map[string]*string{"key": &decoded.Key, "modifiers": &decoded.Modifiers} {
		value, exists := keyboard[name]
		if !exists {
			continue
		}
		if *target != "" && *target != value {
			return fmt.Errorf("conflicting top-level and automation.%s; no input sent", name)
		}
		*target = value
	}
	if decoded.Action == "key" || decoded.Action == "hotkey" {
		key := strings.ToLower(strings.TrimSpace(decoded.Key))
		if key == "escape" {
			key = "esc"
		}
		if len(key) > 1 && strings.Contains(key, "+") {
			parts := strings.Split(key, "+")
			mods := strings.Join(parts[:len(parts)-1], "+")
			key = parts[len(parts)-1]
			if key == "escape" {
				key = "esc"
			}
			if decoded.Modifiers != "" && decoded.Modifiers != mods {
				return fmt.Errorf("conflicting combined key and modifiers; no input sent")
			}
			if parsed, err := computer.ParseModifiers(splitModifiers(mods)); err != nil || len(parsed) == 0 {
				return fmt.Errorf("invalid combined keyboard modifiers; no input sent")
			}
			if _, ok := computer.ResolveKey(key); !ok {
				return fmt.Errorf("invalid combined keyboard key; no input sent")
			}
			decoded.Modifiers = mods
		}
		decoded.Key = key
		if decoded.Modifiers != "" {
			decoded.Action = "hotkey"
		}
	}
	if decoded.Action == "ocr" {
		if err := validateOCRScope(decoded.X, decoded.Y, decoded.Width, decoded.Height); err != nil {
			return err
		}
	}
	*p = ComputerParams(decoded)
	return nil
}

// validateOCRScope guarantees that a successful OCR response covers exactly the
// requested area: either an explicit complete region or an explicit full
// screen. A partial region would otherwise silently read the whole display.
func validateOCRScope(x, y, width, height int) error {
	example := `{"action":"ocr","x":100,"y":200,"width":400,"height":120}`
	switch {
	case x < 0 || y < 0 || width < 0 || height < 0:
		return computer.NewContractError("invalid_region", "width", "ocr region values must not be negative. No input sent", "Send a non-negative region, or omit x/y/width/height for the full screen.", example)
	case width == 0 && height == 0 && x == 0 && y == 0:
		return nil
	case width == 0 || height == 0:
		return computer.NewContractError("invalid_region", "width", "ocr region needs both width and height greater than zero; x/y alone or one dimension would silently read the whole screen. No input sent", "Send x, y, width and height together at the top level, or omit all four for the full screen.", example)
	}
	return nil
}

// UnmarshalJSON gives batch inputs the same contract as direct desktop calls.
func (p *ComputerBatchInput) UnmarshalJSON(data []byte) error {
	var input ComputerParams
	if err := json.Unmarshal(data, &input); err != nil {
		return err
	}
	canonical, err := json.Marshal(input)
	if err != nil {
		return err
	}
	type plain ComputerBatchInput
	var decoded plain
	if err := json.Unmarshal(canonical, &decoded); err != nil {
		return err
	}
	*p = ComputerBatchInput(decoded)
	return nil
}

// coerceScalars converts quoted numbers and booleans ("100", "true") to their
// JSON scalar form for the listed fields. The two spellings mean exactly the
// same thing, and several providers emit the quoted one for integer
// parameters. Anything that is not an unambiguous scalar is left untouched
// and fails normally.
func coerceScalars(fields map[string]json.RawMessage, ints, bools []string) {
	for _, name := range ints {
		var quoted string
		if raw, ok := fields[name]; ok && json.Unmarshal(raw, &quoted) == nil {
			if n, err := strconv.ParseInt(strings.TrimSpace(quoted), 10, 64); err == nil {
				fields[name], _ = json.Marshal(n)
			}
		}
	}
	for _, name := range bools {
		var quoted string
		if raw, ok := fields[name]; ok && json.Unmarshal(raw, &quoted) == nil {
			if b, err := strconv.ParseBool(strings.TrimSpace(quoted)); err == nil {
				fields[name], _ = json.Marshal(b)
			}
		}
	}
}
