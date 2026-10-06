package tools

import (
	"encoding/json"
	"fmt"
	"strings"
)

func validPipelineBindingPath(path string) bool {
	switch path {
	case "advanced.tab_id", "advanced.expected_tab_id", "advanced.download_id", "advanced.newer_than", "advanced.document_id", "advanced.frame_id":
		return true
	}
	return false
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
		id, pointer, ok := strings.Cut(source, "#/")
		if !ok || !validPipelineBindingPath(path) {
			return fmt.Errorf("invalid binding")
		}
		var value any
		if err := json.Unmarshal(outputs[id], &value); err != nil {
			return fmt.Errorf("binding source %s has no JSON result", id)
		}
		for _, field := range strings.Split(pointer, "/") {
			object, ok := value.(map[string]any)
			if !ok {
				return fmt.Errorf("binding source is not an object")
			}
			field = strings.ReplaceAll(strings.ReplaceAll(field, "~1", "/"), "~0", "~")
			value, ok = object[field]
			if !ok {
				return fmt.Errorf("binding field is missing")
			}
		}
		text, ok := value.(string)
		if !ok || text == "" || len(text) > 4096 {
			return fmt.Errorf("binding requires a nonempty bounded string")
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
