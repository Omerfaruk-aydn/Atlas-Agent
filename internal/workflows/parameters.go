package workflows

import (
	"encoding/json"
	"fmt"
)

// ParseParameters accepts dialog text as typed data, never shell source.
func ParseParameters(recipe Recipe, values map[string]string) (map[string]json.RawMessage, error) {
	result := map[string]json.RawMessage{}
	for _, parameter := range recipe.Parameters {
		value, exists := values[parameter.Name]
		if !exists || value == "" && !parameter.Required {
			continue
		}
		var raw json.RawMessage
		if parameter.Type == "string" {
			data, err := json.Marshal(value)
			if err != nil {
				return nil, err
			}
			raw = data
		} else {
			raw = json.RawMessage(value)
		}
		if _, _, err := parameterValue(parameter, raw); err != nil {
			return nil, err
		}
		result[parameter.Name] = raw
	}
	for name := range values {
		found := false
		for _, parameter := range recipe.Parameters {
			if parameter.Name == name {
				found = true
			}
		}
		if !found {
			return nil, fmt.Errorf("unknown recipe parameter %s", name)
		}
	}
	return result, nil
}
