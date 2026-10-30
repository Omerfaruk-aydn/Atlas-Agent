package agent

import (
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"
)

type architectPlan struct {
	Changes []struct {
		File   string `json:"file"`
		Intent string `json:"intent"`
	} `json:"changes"`
	Invariants   []string `json:"invariants"`
	Verification []string `json:"verification"`
}

func parseArchitectPlan(text string, ownedPaths []string) (architectPlan, error) {
	var plan architectPlan
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&plan); err != nil {
		return plan, fmt.Errorf("architect must return one JSON plan: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return plan, fmt.Errorf("architect response must contain exactly one JSON object")
	}
	if len(plan.Changes) == 0 || len(plan.Changes) > 64 || len(plan.Verification) == 0 || len(plan.Invariants) > 32 {
		return plan, fmt.Errorf("architect plan requires 1–64 changes, bounded invariants and verification")
	}
	seen := map[string]bool{}
	for _, change := range plan.Changes {
		file := path.Clean(strings.ReplaceAll(change.File, `\`, "/"))
		if file == "." || path.IsAbs(file) || strings.HasPrefix(file, "../") || seen[file] || strings.TrimSpace(change.Intent) == "" {
			return plan, fmt.Errorf("architect plan contains an invalid or duplicate file change")
		}
		allowed := false
		for _, owned := range ownedPaths {
			owned = path.Clean(strings.ReplaceAll(owned, `\`, "/"))
			allowed = allowed || owned == "." || file == owned || strings.HasPrefix(file, owned+"/")
		}
		if !allowed {
			return plan, fmt.Errorf("architect plan file %q is outside owned_paths", file)
		}
		seen[file] = true
	}
	return plan, nil
}
