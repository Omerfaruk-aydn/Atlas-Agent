package agent

import (
	"context"
	"log/slog"
)

// buildStageModels resolves only explicitly configured phase roles. Returning
// nil preserves the historical single-model session and avoids resolving
// providers that are not needed.
func (c *coordinator) buildStageModels(ctx context.Context, isSubAgent bool) func(string) *Model {
	if isSubAgent || c.cfg.Config().Options == nil {
		return nil
	}
	models := map[string]Model{}
	for _, phase := range []string{"exploration", "planning", "implementation", "verification", "review"} {
		role, ok := c.cfg.Config().ResolveRole(phase)
		if !ok {
			continue
		}
		resolved, err := c.resolveModel(ctx, role, true)
		if err != nil {
			slog.Warn("Phase model role failed to build; using the session model", "phase", phase, "error", err)
			continue
		}
		models[phase] = resolved
	}
	if len(models) == 0 {
		return nil
	}
	return func(phase string) *Model {
		if model, ok := models[phase]; ok {
			copy := model
			return &copy
		}
		return nil
	}
}
