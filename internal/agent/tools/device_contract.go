package tools

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

// DeviceToolError is the machine-readable failure shared by the device
// tools (remote_node, isolated_desktop, android, screen_mark, app_loop).
// Every provider receives the same shape, so recovery does not depend on
// the model family.
type DeviceToolError struct {
	Code    string `json:"code"`
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
	// InputSent is whether anything left Atlas toward a device or process.
	InputSent bool `json:"input_sent"`
	// Effect is none, applied or unknown.
	Effect   string `json:"effect"`
	NextStep string `json:"next_step,omitempty"`
	Example  string `json:"example,omitempty"`
}

func (e *DeviceToolError) Error() string { return e.Code + ": " + e.Message }

func deviceErr(code, field, message, next string) *DeviceToolError {
	return &DeviceToolError{Code: code, Field: field, Message: message, Effect: "none", NextStep: next}
}

// deviceErrorResponse renders err as a JSON error result.
func deviceErrorResponse(err error) fantasy.ToolResponse {
	var de *DeviceToolError
	if !errors.As(err, &de) {
		de = &DeviceToolError{Code: "error", Message: err.Error(), Effect: "unknown"}
	}
	data, _ := json.Marshal(map[string]any{"ok": false, "error": de})
	return fantasy.NewTextErrorResponse(string(data))
}

// deviceResponse renders a successful structured result.
func deviceResponse(v any) fantasy.ToolResponse {
	data, err := json.Marshal(v)
	if err != nil {
		return deviceErrorResponse(err)
	}
	return fantasy.NewTextResponse(string(data))
}

// strictDecode decodes input into v and rejects unknown or duplicated
// top-level fields with a structured error instead of ignoring them.
func strictDecode(input string, v any, example string) error {
	if len(input) > 1<<20 {
		return deviceErr("invalid_request", "", "tool input exceeds 1 MiB", "Send a smaller request.")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(input), &raw); err != nil {
		return &DeviceToolError{Code: "invalid_json", Message: err.Error(), Effect: "none", Example: example}
	}
	dec := json.NewDecoder(bytes.NewReader([]byte(input)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		msg := err.Error()
		field := ""
		if name, ok := strings.CutPrefix(msg, "json: unknown field "); ok {
			field = strings.Trim(name, `"`)
			msg = fmt.Sprintf("unknown field %q; unknown fields are rejected, nothing was run", field)
		}
		return &DeviceToolError{Code: "invalid_request", Field: field, Message: msg, Effect: "none", NextStep: "Fix the arguments using the tool schema and call again.", Example: example}
	}
	return nil
}
