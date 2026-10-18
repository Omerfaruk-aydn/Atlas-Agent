package tools

import (
	"encoding/json"
	"fmt"
)

// resolvePlanReferences lets a check's input name a step of the discovered
// plan by its position, as models do after action plan ("input": 0 or
// "input": [0]). Such an input used to reach the tool as an array and fail
// with "cannot unmarshal array", which read as the check failing.
func resolvePlanReferences(checks, plan []VerificationStep) ([]VerificationStep, error) {
	out := make([]VerificationStep, len(checks))
	for i, c := range checks {
		out[i] = c
		index, ok := planIndex(c.Input)
		if !ok {
			continue
		}
		if index < 0 || index >= len(plan) {
			return nil, fmt.Errorf("check %q refers to plan step %d, but the discovered plan has %d steps; call verify with action plan to list them", c.Name, index, len(plan))
		}
		out[i].Tool = plan[index].Tool
		out[i].Input = plan[index].Input
		if out[i].Name == "" {
			out[i].Name = plan[index].Name
		}
	}
	return out, nil
}

// planIndex reads an input that is a plan position: a number, or a list
// holding one number.
func planIndex(raw json.RawMessage) (int, bool) {
	var n int
	if json.Unmarshal(raw, &n) == nil {
		return n, true
	}
	var list []int
	if json.Unmarshal(raw, &list) == nil && len(list) == 1 {
		return list[0], true
	}
	return 0, false
}
