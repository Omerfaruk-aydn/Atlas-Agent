package browser

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
)

// requestFields is Request without its JSON methods.
type requestFields Request

// requestTopLevelHints names keys that belong next to action, not inside
// advanced (or a workflow target).
var requestTopLevelHints = map[string]string{
	"ref":        "ref is a top-level field next to action (or the workflow step's ref)",
	"script":     "script is a top-level field of eval",
	"direction":  "direction is a top-level field of scroll",
	"amount":     "amount is a top-level field of scroll",
	"full":       "full is a top-level field of snapshot",
	"full_page":  "full_page is a top-level field of screenshot",
	"cdp_method": "cdp_method is a top-level field of cdp",
	"task":       "task is a top-level field of task_plan/task_update",
}

// UnmarshalJSON rejects unknown keys instead of dropping them and records
// whether value was present, so a missing value is never read as an empty
// one.
func (r *Request) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw == nil {
		*r = Request{}
		return nil
	}
	if unknown := computer.UnknownFields(raw, Request{}); len(unknown) > 0 {
		return unknownFieldError("advanced", unknown, Request{}, requestTopLevelHints)
	}
	present := false
	if value, ok := raw["value"]; ok && string(value) != "null" {
		if !IsJSONString(value) {
			return computer.NewContractError("invalid_arguments", "advanced.value", "advanced.value must be a JSON string, e.g. \"390\". No input sent", "Send the value as a string.", `{"action":"set_field","advanced":{"label":"Width","value":"390"}}`)
		}
		present = true
	}
	var out requestFields
	if err := json.Unmarshal(data, &out); err != nil {
		return err
	}
	*r = Request(out)
	r.ValueSet = present
	return nil
}

// MarshalJSON keeps an explicit empty value, which omitempty would drop: an
// encoded request must decode to the same request.
func (r Request) MarshalJSON() ([]byte, error) {
	data, err := json.Marshal(requestFields(r))
	if err != nil || !r.ValueSet || r.Value != "" {
		return data, err
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return nil, err
	}
	object["value"] = json.RawMessage(`""`)
	return json.Marshal(object)
}

type formFieldFields FormField

// UnmarshalJSON requires each fill_form field to name its value: a field
// without one would otherwise be filled with an empty string.
func (f *FormField) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw == nil {
		return computer.NewContractError("invalid_arguments", "advanced.fields", "a fill_form field must be an object. No input sent", "Give each field a target and a value.", `{"action":"fill_form","advanced":{"fields":[{"label":"Email","value":"a@example.com"}]}}`)
	}
	if unknown := computer.UnknownFields(raw, FormField{}); len(unknown) > 0 {
		return unknownFieldError("advanced.fields[]", unknown, FormField{}, nil)
	}
	value, ok := raw["value"]
	if !ok || string(value) == "null" {
		return computer.NewContractError("missing_value", "advanced.fields[].value", "every fill_form field needs value; an empty string clears the field. No input sent", "Add value to each field.", `{"action":"fill_form","advanced":{"fields":[{"label":"Email","value":"a@example.com"}]}}`)
	}
	if !IsJSONString(value) {
		return computer.NewContractError("invalid_arguments", "advanced.fields[].value", "a fill_form value must be a JSON string. No input sent", "Send the value as a string.", "")
	}
	var out formFieldFields
	if err := json.Unmarshal(data, &out); err != nil {
		return err
	}
	*f = FormField(out)
	return nil
}

// IsJSONString reports whether raw is a JSON string. Numbers are refused
// rather than converted: the schema every path is checked against types
// value as a string, and a direct call must not accept what a pipeline
// step refuses.
func IsJSONString(raw json.RawMessage) bool {
	var s string
	return strings.HasPrefix(strings.TrimSpace(string(raw)), `"`) && json.Unmarshal(raw, &s) == nil
}

func unknownFieldError(where string, unknown []string, sample any, hints map[string]string) error {
	hint := ""
	for _, key := range unknown {
		if note, ok := hints[key]; ok {
			hint += " " + key + ": " + note + "."
		}
	}
	return computer.NewContractError("unknown_field", where+"."+unknown[0],
		fmt.Sprintf("unknown %s field(s) %s; allowed: %s.%s Unknown fields are rejected instead of ignored. No input sent", where, strings.Join(unknown, ", "), strings.Join(computer.KnownFields(sample), ", "), hint),
		"Remove or move the listed fields and call again.", "")
}
