package tools

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/nodes"
)

// RunNodePlan executes a cross-device plan outside a model turn (the
// `atlas node plan` command) through exactly the same step runner, journal
// identities and reconciliation the remote_node tool uses. runID makes the
// operation ids stable, so re-running the same plan id is deduplicated by
// the nodes instead of repeating side effects.
func RunNodePlan(ctx context.Context, workingDir, devicesDir, runID string, steps []nodes.PlanStep) (nodes.PlanResult, error) {
	if err := nodes.ValidatePlan(steps); err != nil {
		return nodes.PlanResult{}, err
	}
	t := &remoteNodeTool{workingDir: workingDir, registry: nodes.OpenRegistry(devicesDir), runsDir: filepath.Join(devicesDir, "runs")}
	for _, s := range steps {
		if s.Node == "local" {
			continue
		}
		if _, _, err := t.registry.Get(s.Node); err != nil {
			return nodes.PlanResult{}, fmt.Errorf("step %s: %w", s.ID, err)
		}
	}
	return nodes.RunPlan(ctx, steps, 4, func(ctx context.Context, s nodes.PlanStep) nodes.StepResult {
		return t.runStep(ctx, "cli", runID, s)
	}, nil), nil
}
