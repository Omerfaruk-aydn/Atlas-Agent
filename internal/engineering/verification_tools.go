package engineering

// IsVerificationTool identifies tools with machine-readable observed checks.
func IsVerificationTool(name string) bool {
	switch name {
	case "bash", "test_run", "lint_run", "api_probe", "migration_rehearse", "visual_diff", "mutation_test", "stress_journey", "handover", "time_lab", "assumption_check", "data_boundary", "cli_bridge", "browser", "computer":
		return true
	default:
		return false
	}
}
