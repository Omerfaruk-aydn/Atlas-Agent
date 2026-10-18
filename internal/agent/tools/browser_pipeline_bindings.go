package tools

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
)

// validPipelineBindingPath lists the argument paths an earlier result may
// fill. Each one is an identity the model would otherwise have to copy or
// guess: an observed tab, frame, document, download, element ref or the
// viewport grant of a screenshot taken earlier in the same run.
func validPipelineBindingPath(path string) bool {
	switch path {
	case "advanced.tab_id", "advanced.expected_tab_id", "advanced.download_id", "advanced.newer_than", "advanced.document_id", "advanced.frame_id",
		"advanced.viewport_id", "ref", "advanced.value", "advanced.expected", "text",
		"automation.window_id", "automation.element_id", "automation.expected":
		return true
	}
	return false
}

// pipelinePointer reads one scalar of an earlier step's JSON result. Arrays
// are only indexed when they hold exactly one element: choosing among
// several matches by position is guessing.
func pipelinePointer(outputs map[string]json.RawMessage, source string) (any, error) {
	id, pointer, ok := strings.Cut(source, "#/")
	if !ok {
		return nil, fmt.Errorf("invalid source %q: use step#/field", source)
	}
	var value any
	if err := decodeTransformValue(outputs[id], &value); err != nil {
		return nil, fmt.Errorf("binding source %s has no JSON result", id)
	}
	for _, field := range strings.Split(pointer, "/") {
		field = strings.ReplaceAll(strings.ReplaceAll(field, "~1", "/"), "~0", "~")
		switch container := value.(type) {
		case map[string]any:
			next, ok := container[field]
			if !ok {
				return nil, fmt.Errorf("binding field %q is missing in %s", field, id)
			}
			value = next
		case []any:
			index, err := strconv.Atoi(field)
			if err != nil || index < 0 || index >= len(container) {
				return nil, fmt.Errorf("binding index %q is outside the %d results of %s", field, len(container), id)
			}
			if len(container) != 1 {
				return nil, fmt.Errorf("ambiguous_target: %s returned %d results; refine it until exactly one matches before binding", id, len(container))
			}
			value = container[index]
		default:
			return nil, fmt.Errorf("binding source is not an object")
		}
	}
	return value, nil
}

func bindPipelineArguments(arguments any, bindings map[string]string, outputs map[string]json.RawMessage) error {
	if len(bindings) == 0 {
		return nil
	}
	args, ok := arguments.(map[string]any)
	if !ok {
		return fmt.Errorf("binding requires object arguments")
	}
	for path, source := range bindings {
		if !validPipelineBindingPath(path) {
			return fmt.Errorf("invalid binding")
		}
		value, err := pipelinePointer(outputs, source)
		if err != nil {
			return err
		}
		text, ok := value.(string)
		if number, isNumber := value.(float64); isNumber && (path == "advanced.value" || path == "advanced.expected" || path == "text" || path == "automation.expected") {
			// A read value such as a count binds as its decimal text.
			text, ok = strconv.FormatFloat(number, 'f', -1, 64), true
		}
		if number, isNumber := value.(json.Number); isNumber && (path == "advanced.value" || path == "advanced.expected" || path == "text" || path == "automation.expected") {
			text, ok = number.String(), true
		}
		if !ok || (text == "" && path != "advanced.value") || len(text) > 4096 {
			return fmt.Errorf("binding requires a nonempty bounded string")
		}
		if path == "automation.window_id" {
			// A window binding must be a native handle; a title, a count or
			// a list position never addresses a window.
			if err := computer.ValidateWindowID(text); err != nil {
				return fmt.Errorf("binding automation.window_id from %s is not a native window handle: %w", source, err)
			}
		}
		if path == "ref" || path == "text" {
			args[path] = text
			continue
		}
		if field, desktop := strings.CutPrefix(path, "automation."); desktop {
			// A desktop identity read from an earlier native result, such
			// as the window a launch or open produced.
			automation, ok := args["automation"].(map[string]any)
			if !ok {
				automation = map[string]any{}
				args["automation"] = automation
			}
			automation[field] = text
			continue
		}
		advanced, ok := args["advanced"].(map[string]any)
		if !ok {
			advanced = map[string]any{}
			args["advanced"] = advanced
		}
		advanced[strings.TrimPrefix(path, "advanced.")] = text
	}
	return nil
}
