package tools

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/browser"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
)

// browserParamsFields is BrowserParams without its JSON method.
type browserParamsFields BrowserParams

// browserAdvancedOnly are request keys a model sometimes sends next to
// action; they are read only inside advanced.
var browserAdvancedOnly = []string{
	"role", "name", "label", "within", "within_role", "match", "scope", "mode", "commit", "settle_ms",
	"condition", "expected", "timeout_ms", "viewport_id", "x", "y", "to_x", "to_y", "button", "click_count",
	"modifiers", "hold_ms", "steps", "tab_id", "frame_id", "document_id", "expected_tab_id", "expected_origin",
	"fields", "paths", "urls", "cursor", "row_selector", "only_invalid", "allow_resubmit",
}

// browserValueActions read a value to apply.
var browserValueActions = map[string]bool{"set_field": true, "select_option": true}

// UnmarshalJSON is the single decoding contract for the browser tool. A
// direct call, a tool_pipeline step and a compiled workflow step all pass
// through it, so the same arguments mean the same thing on every path and
// for every provider. Unknown keys are refused instead of dropped. The only
// accepted aliases are explicit: a top-level value and advanced.ref, which
// must agree with their canonical field when both are given.
func (p *BrowserParams) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if fields == nil {
		*p = BrowserParams{}
		return nil
	}
	var action string
	if raw, ok := fields["action"]; ok {
		_ = json.Unmarshal(raw, &action)
	}
	action = strings.ToLower(strings.TrimSpace(action))

	topValue, hasTopValue := fields["value"]
	delete(fields, "value")
	if hasTopValue && string(topValue) == "null" {
		hasTopValue = false
	}
	_, hasTopText := fields["text"]

	var advanced map[string]json.RawMessage
	if raw, ok := fields["advanced"]; ok && string(raw) != "null" {
		if err := json.Unmarshal(raw, &advanced); err != nil {
			return computer.NewContractError("invalid_arguments", "advanced", "advanced must be an object. No input sent", "Send advanced as a JSON object.", `{"action":"find","advanced":{"role":"button","name":"Share"}}`)
		}
	}
	if ref, ok := advanced["ref"]; ok {
		if top, exists := fields["ref"]; exists && string(top) != string(ref) {
			return computer.NewContractError("conflicting_fields", "ref", "ref and advanced.ref differ; no target was guessed. No input sent", "Send ref once, at the top level.", `{"action":"read_field","ref":"e12"}`)
		}
		fields["ref"] = ref
		delete(advanced, "ref")
	}
	_, hasAdvancedText := advanced["text"]
	if advanced != nil {
		fields["advanced"], _ = json.Marshal(advanced)
	}

	if unknown := computer.UnknownFields(fields, BrowserParams{}); len(unknown) > 0 {
		hint := ""
		for _, key := range unknown {
			if slices.Contains(browserAdvancedOnly, key) {
				hint += " " + key + ": put it inside advanced."
			}
		}
		return computer.NewContractError("unknown_field", unknown[0],
			fmt.Sprintf("unknown browser field(s) %s; allowed: %s.%s Unknown fields are rejected instead of ignored. No input sent", strings.Join(unknown, ", "), strings.Join(computer.KnownFields(BrowserParams{}), ", "), hint),
			"Remove or move the listed fields and call again.", `{"action":"set_field","ref":"e12","value":"390"}`)
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	var out browserParamsFields
	if err := json.Unmarshal(encoded, &out); err != nil {
		return err
	}
	*p = BrowserParams(out)

	if hasTopValue && !browserValueActions[action] {
		return computer.NewContractError("unknown_field", "value", fmt.Sprintf("value is read only by set_field and select_option, not %s. No input sent", action), browserTextHint(action), "")
	}
	if p.Advanced.ValueSet && !browserValueActions[action] && action != "" {
		return computer.NewContractError("unknown_field", "advanced.value", fmt.Sprintf("advanced.value is read only by set_field and select_option, not %s. No input sent", action), browserTextHint(action), "")
	}
	switch {
	case browserValueActions[action]:
		var top *string
		if hasTopValue {
			if !browser.IsJSONString(topValue) {
				return computer.NewContractError("invalid_arguments", "value", "value must be a JSON string, e.g. \"390\". No input sent", "Send the value as a string.", `{"action":"set_field","ref":"e12","value":"390"}`)
			}
			var s string
			_ = json.Unmarshal(topValue, &s)
			top = &s
		}
		return normalizeBrowserValue(action, p, top, hasTopText, hasAdvancedText)
	case action == "semantic_type":
		return normalizeBrowserText(action, p, hasTopText, hasAdvancedText, true)
	case action == "type":
		if !hasTopText && !hasAdvancedText {
			return computer.NewContractError("missing_value", "text", "type needs text (it replaces the field's contents; an empty string clears it). No input sent", "Add text, or use set_field to apply and read back a value.", `{"action":"type","ref":"e12","text":"Hello"}`)
		}
		return normalizeBrowserText(action, p, hasTopText, hasAdvancedText, false)
	case action == browser.InputInsertText:
		return normalizeBrowserText(action, p, hasTopText, hasAdvancedText, true)
	}
	return nil
}

// normalizeBrowserValue resolves the one value of set_field/select_option
// from value, advanced.value, text or advanced.text. Several different
// values are refused; none is refused unless the mode clears the field. An
// explicit empty value is a clear, never a silent no-op.
func normalizeBrowserValue(action string, p *BrowserParams, top *string, hasTopText, hasAdvancedText bool) error {
	type source struct{ name, value string }
	var given []source
	if top != nil {
		given = append(given, source{"value", *top})
	}
	if p.Advanced.ValueSet {
		given = append(given, source{"advanced.value", p.Advanced.Value})
	}
	if hasTopText {
		given = append(given, source{"text", p.Text})
	}
	if hasAdvancedText {
		given = append(given, source{"advanced.text", p.Advanced.Text})
	}
	example := `{"action":"set_field","ref":"e12","value":"390"}`
	if action == "select_option" {
		example = `{"action":"select_option","advanced":{"label":"Country","value":"Turkey"}}`
	}
	for _, g := range given[min(1, len(given)):] {
		if g.value != given[0].value {
			return computer.NewContractError("conflicting_fields", g.name, fmt.Sprintf("%s and %s give different values (%q, %q); no value was guessed. No input sent", given[0].name, g.name, given[0].value, g.value), "Send the value once, as advanced.value or value.", example)
		}
	}
	mode := strings.ToLower(strings.TrimSpace(p.Advanced.Mode))
	if len(given) == 0 {
		if action == "set_field" && mode == "clear" {
			return nil
		}
		return computer.NewContractError("missing_value", "advanced.value", action+" needs value (the text or option to apply). A missing value is not an empty one: to empty a field send value \"\" or mode clear. No input sent", "Add value and call again.", example)
	}
	value := given[0].value
	if action == "select_option" && value == "" {
		return computer.NewContractError("missing_value", "advanced.value", "select_option needs a non-empty value naming the option. No input sent", "Name the option to choose.", example)
	}
	if action == "set_field" {
		switch {
		case mode == "clear" && value != "":
			return computer.NewContractError("conflicting_fields", "advanced.mode", fmt.Sprintf("mode clear empties the field but value %q was also given. No input sent", value), "Send either mode clear or a value.", example)
		case value == "" && mode == "append":
			return computer.NewContractError("missing_value", "advanced.value", "mode append with an empty value would change nothing. No input sent", "Give the text to append.", example)
		case value == "" && (mode == "" || mode == "replace"):
			p.Advanced.Mode = "clear"
		}
	}
	p.Advanced.Value, p.Advanced.ValueSet = value, true
	p.Text, p.Advanced.Text = "", ""
	return nil
}

// normalizeBrowserText resolves the text of type, semantic_type and
// insert_text, which accept it at the top level or in advanced.
func normalizeBrowserText(action string, p *BrowserParams, hasTopText, hasAdvancedText, required bool) error {
	if hasTopText && hasAdvancedText && p.Text != p.Advanced.Text {
		return computer.NewContractError("conflicting_fields", "advanced.text", fmt.Sprintf("text and advanced.text differ (%q, %q); no text was guessed. No input sent", p.Text, p.Advanced.Text), "Send the text once.", fmt.Sprintf(`{"action":%q,"text":"Hello"}`, action))
	}
	text := firstNonEmpty(p.Text, p.Advanced.Text)
	if required && text == "" {
		return computer.NewContractError("missing_value", "text", action+" needs non-empty text. No input sent", "Add the text to enter.", fmt.Sprintf(`{"action":%q,"text":"Hello"}`, action))
	}
	p.Text, p.Advanced.Text = text, text
	return nil
}

func browserTextHint(action string) string {
	switch action {
	case "type", "insert_text", "semantic_type":
		return "Send the text as text."
	case "assert", "wait_for":
		return "Compare with advanced.expected."
	case "fill_form":
		return "Give each field its own value in advanced.fields."
	}
	return "Remove value, or use set_field to apply a value."
}
