package computer

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"
)

// ContractError is the structured form of a rejected tool call. It is used
// for every call that is refused before anything is sent to the desktop, so
// every provider receives the same actionable, machine-readable shape.
//
// Effect is "none" for a pre-dispatch rejection. Callers that report an
// uncertain outcome must use "unknown" and must not claim InputSent=false.
type ContractError struct {
	Code    string `json:"code"`
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
	// InputSent reports whether any desktop input left the process.
	InputSent bool `json:"input_sent"`
	// Effect is none, applied or unknown.
	Effect string `json:"effect"`
	// Observed carries target/foreground facts that came from a real
	// observation. It is never filled from the model's own arguments.
	Observed map[string]any `json:"observed,omitempty"`
	NextStep string         `json:"next_step,omitempty"`
	// Example is a minimal valid shape. Identity fields use non-numeric
	// placeholders so a copied example cannot address a real window.
	Example string `json:"example,omitempty"`
	// FreshObservation is true when a retry must first read new state.
	FreshObservation bool `json:"fresh_observation_required"`
}

// Error renders "code: message" first so recovery code can still split on the
// first colon, followed by the structured document.
func (e *ContractError) Error() string {
	data, err := json.Marshal(e)
	if err != nil {
		return e.Code + ": " + e.Message
	}
	return e.Code + ": " + e.Message + "\nTool contract: " + string(data)
}

// ContractCode lets generic wrappers keep the code-first message intact.
func (e *ContractError) ContractCode() string { return e.Code }

// NewContractError builds a pre-dispatch rejection: no input was sent and the
// desktop state is unchanged.
func NewContractError(code, field, message, nextStep, example string) *ContractError {
	return &ContractError{Code: code, Field: field, Message: message, Effect: "none", NextStep: nextStep, Example: example}
}

// AsContractError extracts a ContractError from an error chain.
func AsContractError(err error) (*ContractError, bool) {
	for err != nil {
		if c, ok := err.(*ContractError); ok {
			return c, true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return nil, false
		}
		err = u.Unwrap()
	}
	return nil, false
}

// controlTypes is the complete set of UI Automation control types the native
// matcher can compare against. The wire form is "ControlType.<Name>".
var controlTypes = []string{
	"AppBar", "Button", "Calendar", "CheckBox", "ComboBox", "Custom", "DataGrid", "DataItem",
	"Document", "Edit", "Group", "Header", "HeaderItem", "Hyperlink", "Image", "List", "ListItem",
	"Menu", "MenuBar", "MenuItem", "Pane", "ProgressBar", "RadioButton", "ScrollBar", "SemanticZoom",
	"Separator", "Slider", "Spinner", "SplitButton", "StatusBar", "Tab", "TabItem", "Table", "Text",
	"Thumb", "TitleBar", "ToolBar", "ToolTip", "Tree", "TreeItem", "Window",
}

// ControlTypes returns the valid role names in canonical wire form.
func ControlTypes() []string {
	out := make([]string, len(controlTypes))
	for i, name := range controlTypes {
		out[i] = "ControlType." + name
	}
	return out
}

var roleSeparators = strings.NewReplacer(" ", "", "_", "", "-", "")

// roleSuggestions only ever feed error hints. They are never applied, because
// a plausible synonym (textbox, link) is not the same UIA control type.
var roleSuggestions = map[string]string{
	"textbox": "Edit", "input": "Edit", "textfield": "Edit", "link": "Hyperlink", "listbox": "List",
	"row": "ListItem", "item": "ListItem", "checkbox": "CheckBox", "dropdown": "ComboBox",
	"dialog": "Window", "label": "Text", "textblock": "Text",
}

// NormalizeRole maps a role to its canonical "ControlType.Name" form. Only a
// case, separator or ControlType-prefix difference is normalized; anything
// else is an unknown role and is rejected instead of guessed.
func NormalizeRole(role string) (string, error) {
	trimmed := strings.TrimSpace(role)
	if trimmed == "" {
		return "", nil
	}
	name := trimmed
	if prefix, rest, ok := strings.Cut(trimmed, "."); ok && strings.EqualFold(prefix, "ControlType") {
		name = rest
	}
	key := strings.ToLower(roleSeparators.Replace(name))
	for _, candidate := range controlTypes {
		if strings.ToLower(candidate) == key {
			return "ControlType." + candidate, nil
		}
	}
	next := "Use one of the valid roles, or omit role and select by element_id or name from a fresh observation. An unknown role is not the same as a valid role whose element was not found."
	if suggested, ok := roleSuggestions[key]; ok {
		next = fmt.Sprintf("Did you mean ControlType.%s? %s", suggested, next)
	}
	err := NewContractError("invalid_role", "automation.role", fmt.Sprintf("%q is not a UI Automation control type. Valid roles: %s. No input sent", role, strings.Join(ControlTypes(), ", ")), next, `{"action":"find","automation":{"window_id":"<window_id from windows/observe>","role":"ControlType.ListItem","name":"<observed name>"}}`)
	return "", err
}

var numericWindowID = regexp.MustCompile(`^\d+$`)

// ValidateWindowID rejects values that cannot be a native window handle. A
// numeric ID is still verified against the live window by the native layer;
// this only stops names such as "shell" from ever being treated as targets.
func ValidateWindowID(id string) error {
	if id == "" || numericWindowID.MatchString(id) {
		return nil
	}
	err := NewContractError("invalid_target", "window_id", fmt.Sprintf("window_id %q is not a native window handle. A window_id is the numeric value returned by windows, observe or prepare; names, application names and guesses are rejected. No input sent", id), "Call tool_pipeline desktop prepare (application or window_id) or computer windows, choose the matching entry, and use its window_id.", `{"action":"windows"}`)
	err.FreshObservation = true
	return err
}

// fieldNames lists the JSON property names of a struct type.
func fieldNames(t reflect.Type) []string {
	var names []string
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	for i := 0; i < t.NumField(); i++ {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if name == "" {
			name = t.Field(i).Name
		}
		names = append(names, name)
	}
	return names
}

// UnknownFields returns the keys of raw that are not JSON properties of the
// struct type of sample, in sorted order. The standard decoder drops unknown
// keys silently, which hides misplaced parameters from the model.
func UnknownFields(raw map[string]json.RawMessage, sample any) []string {
	known := fieldNames(reflect.TypeOf(sample))
	var unknown []string
	for key := range raw {
		if !slices.Contains(known, key) {
			unknown = append(unknown, key)
		}
	}
	slices.Sort(unknown)
	return unknown
}

// KnownFields returns the sorted JSON property names of the sample's type.
func KnownFields(sample any) []string {
	names := fieldNames(reflect.TypeOf(sample))
	slices.Sort(names)
	return names
}

// UnmarshalJSON enforces the shared automation contract for every provider
// and every path (direct, batch, pipeline, flow): no unknown keys, canonical
// roles and numeric window handles.
func (p *AutomationRequest) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw == nil {
		return nil
	}
	// An explicit nested automation checkpoint is accepted without guessing:
	// duplicate fields must agree, and one wrapper is the maximum.
	if nestedRaw, exists := raw["automation"]; exists {
		var nested map[string]json.RawMessage
		if err := json.Unmarshal(nestedRaw, &nested); err != nil || nested == nil {
			return fmt.Errorf("nested automation checkpoint must be an object")
		}
		if _, exists := nested["automation"]; exists {
			return fmt.Errorf("nested automation checkpoint exceeds one wrapper")
		}
		for key, value := range nested {
			if prior, exists := raw[key]; exists {
				var a, b any
				if json.Unmarshal(prior, &a) != nil || json.Unmarshal(value, &b) != nil || !reflect.DeepEqual(a, b) {
					return fmt.Errorf("conflicting checkpoint %s; no input sent", key)
				}
			} else {
				raw[key] = value
			}
		}
		delete(raw, "automation")
		canonical, err := json.Marshal(raw)
		if err != nil {
			return err
		}
		data = canonical
	}
	if unknown := UnknownFields(raw, AutomationRequest{}); len(unknown) > 0 {
		return unknownAutomationFields(unknown)
	}
	type plain AutomationRequest
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	if err := ValidateWindowID(strings.TrimSpace(decoded.WindowID)); err != nil {
		return err
	}
	role, err := NormalizeRole(decoded.Role)
	if err != nil {
		return err
	}
	decoded.Role = role
	*p = AutomationRequest(decoded)
	return nil
}

func unknownAutomationFields(unknown []string) error {
	hints := map[string]string{
		"x": "coordinates belong at the top level of the computer call", "y": "coordinates belong at the top level of the computer call",
		"width": "region size belongs at the top level of the computer call", "height": "region size belongs at the top level of the computer call",
		"application":  "applications are resolved by tool_pipeline desktop mode prepare or computer launch_app with automation.name",
		"image_origin": "image_origin is an output field; do not send it back", "outer": "there is no outer wrapper; put fields directly in automation",
		"key": "keyboard fields belong at the top level", "modifiers": "keyboard fields belong at the top level",
		"mode": "mode belongs to tool_pipeline desktop, not to computer automation", "desktop": "desktop belongs to tool_pipeline, not to computer automation",
	}
	var notes []string
	for _, key := range unknown {
		if hint, ok := hints[key]; ok {
			notes = append(notes, key+": "+hint)
		}
	}
	message := fmt.Sprintf("unknown automation field(s) %s; allowed: %s. Unknown fields are rejected instead of ignored. No input sent", strings.Join(unknown, ", "), strings.Join(KnownFields(AutomationRequest{}), ", "))
	if len(notes) > 0 {
		message += ". " + strings.Join(notes, "; ")
	}
	return NewContractError("unknown_field", "automation."+unknown[0], message, "Remove or move the listed fields and call again.", `{"action":"inspect","automation":{"window_id":"<window_id from windows/observe>","max_elements":80}}`)
}
