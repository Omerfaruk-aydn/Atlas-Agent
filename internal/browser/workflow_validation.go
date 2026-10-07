package browser

import "fmt"

// ValidateWorkflowRequest rejects invalid later steps before a workflow can
// dispatch its first mutation. Filesystem checks still run in the tool.
func ValidateWorkflowRequest(p Request) error {
	if p.TimeoutMS < 0 || p.TimeoutMS > 30000 {
		return fmt.Errorf("timeout_ms must be 0-30000")
	}
	if p.Match != "" && p.Match != "exact" && p.Match != "contains" && p.Match != "case_insensitive" {
		return fmt.Errorf("unsupported semantic match mode")
	}
	if len(p.Selector) > 4096 || len(p.Scope) > 4096 || len(p.Name) > 4096 || len(p.Label) > 4096 || len(p.Expected) > 16384 || len(p.Paths) > 32 || len(p.ExpectedOrigin) > 2048 || len(p.Text) > 1024*1024 {
		return fmt.Errorf("target parameters exceed limits")
	}
	if p.ExpectedOrigin != "" {
		if _, err := browserOrigin(p.ExpectedOrigin); err != nil {
			return err
		}
	}
	switch p.Action {
	case "assert", "wait_for":
		if !validCondition(p.Condition) && (p.Action != "wait_for" || (p.Condition != "ready" && p.Condition != "network_idle")) {
			return fmt.Errorf("unsupported condition %q", p.Condition)
		}
		if p.Condition == "url" || p.Condition == "title" || p.Condition == "ready" || p.Condition == "network_idle" {
			return nil
		}
		fallthrough
	case "find", "semantic_click", "semantic_type":
		if p.Selector == "" && p.Name == "" && p.Label == "" && p.Role == "" {
			return fmt.Errorf("semantic target is required")
		}
	case "upload":
		if p.Selector == "" || len(p.Paths) == 0 {
			return fmt.Errorf("upload requires a selector and paths")
		}
	case "download_start":
		if len(p.Paths) != 1 {
			return fmt.Errorf("download_start requires one directory")
		}
	case "download_wait":
		if len(p.Paths) != 1 || p.NewerThan.IsZero() {
			return fmt.Errorf("download_wait requires one file and newer_than")
		}
	case "tab_select", "tab_close":
		if p.TabID == "" {
			return fmt.Errorf("tab_id is required")
		}
	}
	return nil
}
