package evaluation

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
)

// BenchGateConfig changes only opt-in disabled gates. True permits existing
// configured policy, never supplies a reviewer model or turns one on.
func BenchGateConfig(raw json.RawMessage, gates map[string]bool) (json.RawMessage, error) {
	if len(gates) == 0 {
		return raw, nil
	}
	object := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &object); err != nil {
			return nil, err
		}
		if object == nil {
			return nil, fmt.Errorf("variant config must be an object")
		}
	}
	child := func(parent map[string]any, key string) (map[string]any, error) {
		if parent[key] == nil {
			m := map[string]any{}
			parent[key] = m
			return m, nil
		}
		m, ok := parent[key].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s must be an object", key)
		}
		return m, nil
	}
	options, err := child(object, "options")
	if err != nil {
		return nil, err
	}
	harness, err := child(options, "harness")
	if err != nil {
		return nil, err
	}
	disabled := []string{}
	if values, ok := harness["disabled_gates"]; ok {
		list, ok := values.([]any)
		if !ok {
			return nil, fmt.Errorf("disabled_gates must be a string list")
		}
		for _, value := range list {
			name, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("disabled_gates must be a string list")
			}
			disabled = append(disabled, name)
		}
	}
	for name, enabled := range gates {
		if name == "lint_fix" || name == "mutation" {
			harness[name] = enabled
		}
		if !slices.Contains([]string{"evaluator", "contract", "advisor", "goal_judge", "escalator", "lint_fix", "mutation"}, name) {
			return nil, fmt.Errorf("unknown ablation gate %q", name)
		}
		disabled = slices.DeleteFunc(disabled, func(v string) bool { return v == name })
		if !enabled {
			disabled = append(disabled, name)
		}
	}
	sort.Strings(disabled)
	harness["disabled_gates"] = slices.Compact(disabled)
	return json.Marshal(object)
}
