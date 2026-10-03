package engineering

// IsVerificationTool identifies tools with machine-readable observed checks.
func IsVerificationTool(name string) bool {
	switch name {
	case "bash", "test_run", "lint_run", "api_probe", "migration_rehearse", "visual_diff", "mutation_test":
		return true
	default:
		return false
	}
}
