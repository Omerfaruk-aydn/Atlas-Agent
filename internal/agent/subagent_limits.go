package agent

import "time"

// defaultSubAgentTimeout is how long one sub-agent run may last when neither
// the configuration nor its definition says otherwise. It is generous on
// purpose: it exists to end a run that hangs, not to rush honest work.
const defaultSubAgentTimeout = 30 * time.Minute

// subAgentTimeout resolves the wall-clock limit of one run. A definition's
// own limit wins; then the configured one, where a negative value removes the
// limit; then the default. Zero means no limit.
func subAgentTimeout(configured, own int) time.Duration {
	switch {
	case own > 0:
		return time.Duration(own) * time.Second
	case configured < 0:
		return 0
	case configured > 0:
		return time.Duration(configured) * time.Second
	default:
		return defaultSubAgentTimeout
	}
}

// subagentStepCap combines the global step cap with a definition's own. A
// definition can tighten the global cap but never loosen it. Zero means no
// cap.
func subagentStepCap(global, own int) int {
	switch {
	case own <= 0:
		return global
	case global <= 0:
		return own
	default:
		return min(global, own)
	}
}
