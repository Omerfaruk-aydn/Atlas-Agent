package tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

// Pipeline argument type contract.
//
// The advertised schema describes desktop, browser, flow and every
// steps[].arguments as JSON objects, and every provider receives them as
// objects. Models nevertheless sometimes send such a value as a JSON string
// that contains the object (session 69361c0a: `{"desktop":"{\"mode\":...}}"}`
// with a stray trailing brace). The plain decoder then failed with a Go type
// error that named neither the field nor the expected shape.
//
// The decoder below accepts exactly one encoding layer for those known object
// fields, and only when the string holds one complete JSON object and nothing
// else. Everything else - free text, arrays, null inside the string, trailing
// content, a second encoding layer or an oversized string - is rejected with a
// structured invalid_argument_type contract error before any step runs. No
// other field is ever interpreted as JSON, and the decoded object then goes
// through the ordinary unknown-field, schema and runtime validation.

// pipelineObjectFields are the top-level PipelineParams fields whose value is
// always a JSON object.
var pipelineObjectFields = []string{"desktop", "browser", "flow"}

// maxEncodedObjectBytes bounds a string-encoded object. Larger strings are
// rejected rather than decoded; the same plan sent as an object is limited by
// the ordinary plan bounds.
const maxEncodedObjectBytes = 256 * 1024

// pipelineObjectExamples are minimal valid call shapes for each field.
var pipelineObjectExamples = map[string]string{
	"desktop":   `{"desktop":{"mode":"observe","window_id":"<window_id from prepare/windows>","observation":"semantic"}}`,
	"browser":   `{"browser":{"steps":[{"id":"look","action":"observe"}]}}`,
	"flow":      `{"flow":{"name":"<flow name>","items":[{"<input>":"<value>"}]}}`,
	"arguments": `{"steps":[{"id":"list","tool":"computer","arguments":{"action":"windows"}}]}`,
}

// desktopModeExamples are minimal valid desktop calls, one per recipe mode.
// A type refusal returns the one matching the mode the model was building,
// so the repair keeps its plan's shape instead of switching to observe.
// Placeholders are non-numeric so a copied example cannot address a window.
var desktopModeExamples = map[string]string{
	"observe":     `{"desktop":{"mode":"observe","window_id":"<window_id>","observation":"semantic"}}`,
	"prepare":     `{"desktop":{"mode":"prepare","application":"<installed application name>"}}`,
	"act":         `{"desktop":{"mode":"act","input":{"action":"type","text":"<text>","automation":{"window_id":"<window_id>"}}}}`,
	"fill_submit": `{"desktop":{"mode":"fill_submit","input":{"action":"set_value","automation":{"window_id":"<window_id>","name":"<field name>","role":"ControlType.Edit","text":"<value>"}}}}`,
	"sequence":    `{"desktop":{"mode":"sequence","result":"checkpoints","steps":[{"input":{"action":"type","text":"<text>","automation":{"window_id":"<window_id>"}},"checkpoint":{"window_id":"<window_id>","element_id":"<element_id>","condition":"text","expected":"<expected text>","wait_ms":3000}}]}}`,
	"transition":  `{"desktop":{"mode":"transition","input":{"action":"hotkey","modifiers":"ctrl","key":"s","automation":{"window_id":"<window_id>"}},"transition":{"expected_title":"<dialog title>"}}}`,
	"adaptive":    `{"desktop":{"mode":"adaptive","adaptive_steps":[{"input":{"action":"set_value","automation":{"window_id":"<window_id>","name":"<field name>","role":"ControlType.Edit","text":"<value>"}},"checkpoint":{"window_id":"<window_id>","name":"<field name>","role":"ControlType.Edit","condition":"value","expected":"<value>"}}]}}`,
	"flow":        `{"desktop":{"mode":"flow","flow":{"nodes":[{"id":"app","kind":"resolve","application":"<application>","next":"check"},{"id":"check","kind":"verify","window_ref":"app","checkpoint":{"name":"<control name>","role":"ControlType.Text","condition":"visible"}}]}}}`,
	"rename":      `{"desktop":{"mode":"rename","window_id":"<explorer_window_id>","rename":{"old_name":"<displayed old name>","new_name":"<new name>"}}}`,
}

// desktopExampleModes lists the modes with a repair example.
func desktopExampleModes() []string {
	modes := make([]string, 0, len(desktopModeExamples))
	for mode := range desktopModeExamples {
		modes = append(modes, mode)
	}
	slices.Sort(modes)
	return modes
}

// desktopModeExample returns the example for mode, or the generic desktop
// example for an unknown or missing mode.
func desktopModeExample(mode string) string {
	if example, ok := desktopModeExamples[mode]; ok {
		return example
	}
	return pipelineObjectExamples["desktop"]
}

// PipelineNormalization records one argument the decoder accepted only after
// decoding a single string-encoding layer.
type PipelineNormalization struct {
	Field string `json:"field"`
	From  string `json:"from"`
	To    string `json:"to"`
}

// pipelineTypeError is the structured rejection for a known object field that
// does not hold an object.
func pipelineTypeError(field, got, detail string) *computer.ContractError {
	return pipelineTypeErrorFor(field, got, detail, "", "")
}

// pipelineTypeErrorFor builds the refusal with the mode of the object the
// string held (when it could be read) and a repair specific to the mistake.
func pipelineTypeErrorFor(field, got, detail, mode, repair string) *computer.ContractError {
	exampleKey := field
	if strings.HasPrefix(field, "steps[") {
		exampleKey = "arguments"
	}
	example := pipelineObjectExamples[exampleKey]
	if field == "desktop" {
		example = desktopModeExample(mode)
	}
	message := fmt.Sprintf("%s must be a JSON object, got %s", field, got)
	if detail != "" {
		message += " (" + detail + ")"
	}
	message += ". No pipeline step was run and no input was sent"
	next := "Send " + field + " as a JSON object: the value itself, without surrounding quotes or backslash escapes. Do not resend the same text."
	if repair != "" {
		next = repair + " " + next
	}
	err := computer.NewContractError("invalid_argument_type", field, message, next, example)
	err.ExpectedType = "object"
	return err
}

func jsonKind(raw json.RawMessage) string {
	switch trimmed := bytes.TrimSpace(raw); {
	case len(trimmed) == 0:
		return "nothing"
	case trimmed[0] == '{':
		return "object"
	case trimmed[0] == '[':
		return "array"
	case trimmed[0] == '"':
		return "string"
	case bytes.Equal(trimmed, []byte("null")):
		return "null"
	case bytes.Equal(trimmed, []byte("true")) || bytes.Equal(trimmed, []byte("false")):
		return "boolean"
	default:
		return "number"
	}
}

// decodeEncodedObject returns raw unchanged when it is an object or null, the
// decoded object when raw is a string holding exactly one JSON object, and a
// contract error otherwise. It never decodes more than one layer.
func decodeEncodedObject(field string, raw json.RawMessage) (json.RawMessage, bool, error) {
	switch kind := jsonKind(raw); kind {
	case "object", "null":
		return raw, false, nil
	case "string":
	default:
		return nil, false, pipelineTypeError(field, kind, "")
	}
	if len(raw) > maxEncodedObjectBytes {
		return nil, false, pipelineTypeError(field, "a string", fmt.Sprintf("string-encoded object exceeds %d bytes", maxEncodedObjectBytes))
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return nil, false, pipelineTypeError(field, "an invalid string", err.Error())
	}
	inner := strings.TrimSpace(text)
	if !strings.HasPrefix(inner, "{") {
		got := "a string"
		if inner != "" {
			got = "a string containing " + jsonKind(json.RawMessage(inner))
			if !json.Valid([]byte(inner)) {
				got = "a string that is not a JSON object"
			}
		}
		return nil, false, pipelineTypeError(field, got, "only one string-encoding layer around a single object is accepted")
	}
	decoder := json.NewDecoder(strings.NewReader(inner))
	var object map[string]json.RawMessage
	if err := decoder.Decode(&object); err != nil {
		return nil, false, pipelineTypeError(field, "a string with malformed JSON", err.Error())
	}
	if end := int(decoder.InputOffset()); strings.TrimSpace(inner[end:]) != "" {
		// The leading object is never executed: which of the two parts the
		// model meant is a guess, and a guessed plan may send input.
		trailing := pipelineShort(strings.TrimSpace(inner[end:]), 16)
		mode := encodedMode(object)
		held := "a complete JSON object"
		if mode != "" {
			held = fmt.Sprintf("a complete %s object (mode %q)", field, mode)
		}
		detail := fmt.Sprintf("the string holds %s that ends at byte %d of %d, followed by %q", held, end, len(inner), trailing)
		repair := fmt.Sprintf("The object inside the string was complete; the extra %q after it made the call invalid. Rebuild the call from your plan with balanced braces.", trailing)
		return nil, false, pipelineTypeErrorFor(field, "a string with trailing content", detail, mode, repair)
	}
	return json.RawMessage(inner[:decoder.InputOffset()]), true, nil
}

// encodedMode reads the recipe mode of a decoded desktop object, if any.
func encodedMode(object map[string]json.RawMessage) string {
	var mode string
	if json.Unmarshal(object["mode"], &mode) != nil {
		return ""
	}
	return strings.TrimSpace(mode)
}

// UnmarshalJSON applies the narrow object-field contract described above and
// then decodes the call exactly as before.
func (p *PipelineParams) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return computer.NewContractError("invalid_arguments", "", "tool_pipeline arguments must be one JSON object. No pipeline step was run and no input was sent", "Send the call as a JSON object.", pipelineObjectExamples["arguments"])
	}
	var notes []PipelineNormalization
	for _, name := range pipelineObjectFields {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		decoded, normalized, err := decodeEncodedObject(name, raw)
		if err != nil {
			return err
		}
		if normalized {
			fields[name] = decoded
			notes = append(notes, PipelineNormalization{Field: name, From: "json_string", To: "object"})
		}
	}
	if raw, ok := fields["steps"]; ok && jsonKind(raw) == "array" {
		var steps []map[string]json.RawMessage
		if json.Unmarshal(raw, &steps) == nil {
			changed := false
			for i, step := range steps {
				arguments, ok := step["arguments"]
				if !ok {
					continue
				}
				field := fmt.Sprintf("steps[%d].arguments", i)
				decoded, normalized, err := decodeEncodedObject(field, arguments)
				if err != nil {
					return err
				}
				if normalized {
					step["arguments"], changed = decoded, true
					notes = append(notes, PipelineNormalization{Field: field, From: "json_string", To: "object"})
				}
			}
			if changed {
				encoded, err := json.Marshal(steps)
				if err != nil {
					return err
				}
				fields["steps"] = encoded
			}
		}
	}
	if len(notes) > 0 {
		encoded, err := json.Marshal(fields)
		if err != nil {
			return err
		}
		data = encoded
	}
	type plain PipelineParams
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*p = PipelineParams(decoded)
	p.normalized = notes
	return nil
}

// annotateNormalization tells the model which arguments were accepted only
// after decoding a string layer, so it can send objects next time. JSON
// objects gain a leading field; other content gains a final line.
func annotateNormalization(resp fantasy.ToolResponse, notes []PipelineNormalization) fantasy.ToolResponse {
	if len(notes) == 0 {
		return resp
	}
	data, err := json.Marshal(notes)
	if err != nil {
		return resp
	}
	content := strings.TrimLeft(resp.Content, " \t\r\n")
	if strings.HasPrefix(content, "{") && json.Valid([]byte(content)) {
		rest := strings.TrimLeft(content[1:], " \t\r\n")
		separator := ","
		if strings.HasPrefix(rest, "}") {
			separator = ""
		}
		resp.Content = `{"argument_normalization":` + string(data) + separator + rest
		return resp
	}
	fields := make([]string, 0, len(notes))
	for _, note := range notes {
		fields = append(fields, note.Field)
	}
	line := "Argument normalization: " + strings.Join(fields, ", ") + " arrived as a JSON string holding one object; it was decoded once and validated normally. Send objects directly."
	if strings.HasPrefix(content, "[") && json.Valid([]byte(content)) {
		// A step ledger keeps its JSON shape for callers that parse it.
		resp.Content = `{"argument_normalization":` + string(data) + `,"results":` + content + `}`
		return resp
	}
	if resp.Content == "" {
		resp.Content = line
	} else {
		resp.Content += "\n" + line
	}
	return resp
}
