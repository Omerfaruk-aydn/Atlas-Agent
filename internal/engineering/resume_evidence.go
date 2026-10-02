package engineering

import (
	"context"
	"slices"
)

// ReconcileResumeEvidence applies the same contract and finding checks to
// agent and CLI continuation previews. It starts or replays no command.
func (s *Store) ReconcileResumeEvidence(ctx context.Context, session, root string, state State, specs, statuses map[string]string, plan *ResumePlan) {
	for task, taskFP := range specs {
		if task == "" {
			continue
		}
		refs := state.TaskContractRefs[task]
		if state.Delivery != nil {
			refs = state.Delivery.TaskContractRefs[task]
		}
		err := s.ValidateContractRefs(ctx, root, task, refs)
		if statuses[task] == "completed" && err == nil {
			err = s.ValidateTaskContracts(ctx, session, root, task, taskFP, refs)
		}
		if statuses[task] == "completed" && err == nil {
			err = s.ValidateTaskFindings(ctx, session, root, task, taskFP)
		}
		if err != nil && !slices.Contains(plan.ReverifyTasks, task) {
			plan.ReverifyTasks = append(plan.ReverifyTasks, task)
		}
	}
	if len(plan.ReverifyTasks) > 0 {
		plan.ReadyTasks = nil
		slices.Sort(plan.ReverifyTasks)
	}
}
