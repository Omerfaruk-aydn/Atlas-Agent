package config

import (
	"fmt"
	"slices"
)

// GateEnabled permits existing gate policy; it never enables a model call.
func (h *Harness) GateEnabled(name string) bool {
	return h == nil || !slices.Contains(h.DisabledGates, name)
}

func (h *Harness) ValidateAblation() error {
	if h == nil {
		return nil
	}
	for _, name := range h.DisabledGates {
		if !slices.Contains([]string{"evaluator", "contract", "advisor", "goal_judge", "escalator", "lint_fix", "mutation"}, name) {
			return fmt.Errorf("unsupported harness gate ablation %q", name)
		}
	}
	if h.LintTimeoutSeconds < 0 || h.LintTimeoutSeconds > 120 || h.MutationMax < 0 || h.MutationMax > 32 || h.MutationTimeoutSeconds < 0 || h.MutationTimeoutSeconds > 600 || h.NoProgressSteps < 0 || h.NoProgressSteps > 100 {
		return fmt.Errorf("harness verification budget out of range")
	}
	return nil
}

func (h *Harness) LintFixEnabled() bool {
	return h != nil && h.LintFix != nil && *h.LintFix && h.GateEnabled("lint_fix")
}

func (h *Harness) MutationEnabled() bool {
	return h != nil && h.Mutation != nil && *h.Mutation && h.GateEnabled("mutation")
}
func (h *Harness) RedGreenEnabled() bool  { return h != nil && h.RedGreen != nil && *h.RedGreen }
func (h *Harness) FuzzHintsEnabled() bool { return h == nil || defaultOn(h.FuzzHints) }

func (h *Harness) EvaluatorRepairEnabled() bool {
	return h != nil && h.EvaluatorRepair != nil && *h.EvaluatorRepair
}
