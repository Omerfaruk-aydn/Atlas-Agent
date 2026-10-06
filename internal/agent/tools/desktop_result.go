package tools

import (
	"encoding/json"
	"maps"
)

// Put grounded decision state before control trees without changing JSON fields.
func marshalDesktopResult(observed map[string]any) ([]byte, error) {
	if observed == nil {
		return json.Marshal(observed)
	}
	rest := maps.Clone(observed)
	result := []byte{'{'}
	for _, key := range []string{"window_id", "source_window_id", "snapshot_id", "foreground_window", "desktop_state", "explorer_location", "focused_element", "workflow"} {
		value, ok := rest[key]
		if !ok {
			continue
		}
		field, err := json.Marshal(map[string]any{key: value})
		if err != nil {
			return nil, err
		}
		if len(result) > 1 {
			result = append(result, ',')
		}
		result = append(result, field[1:len(field)-1]...)
		delete(rest, key)
	}
	if len(rest) > 0 {
		fields, err := json.Marshal(rest)
		if err != nil {
			return nil, err
		}
		if len(result) > 1 {
			result = append(result, ',')
		}
		result = append(result, fields[1:len(fields)-1]...)
	}
	return append(result, '}'), nil
}
