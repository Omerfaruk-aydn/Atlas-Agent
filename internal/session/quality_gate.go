package session

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
)

type CompletionGate func(context.Context, Session, Session) error

func TaskFingerprint(t Todo) string {
	data, _ := json.Marshal(struct {
		ID, Content, Agent              string
		DependsOn, OwnedPaths, Criteria []string
	}{t.ID, t.Content, t.Agent, t.DependsOn, t.OwnedPaths, t.AcceptanceCriteria})
	return engineering.Hash(string(data))
}

// EngineeringCompletionGate validates newly completed dispatched role tasks.
// Existing completed tasks remain historical records when later work changes files.
func EngineeringCompletionGate(store *engineering.Store) CompletionGate {
	return func(ctx context.Context, previous, next Session) error {
		old := map[string]Todo{}
		for _, task := range previous.Todos {
			old[task.ID] = task
		}
		var state *engineering.State
		for _, task := range next.Todos {
			before := old[task.ID]
			if task.ID == "" || task.Status == TodoStatusPending || before.Status == task.Status && TaskFingerprint(before) == TaskFingerprint(task) {
				continue
			}
			if state == nil {
				st, err := store.Read(ctx, next.ID)
				if err != nil {
					return err
				}
				state = &st
			}
			if state.Delivery != nil {
				if !state.Delivery.AllowsTask(task.ID) || state.Delivery.TaskFingerprints[task.ID] != TaskFingerprint(task) {
					return fmt.Errorf("task %s is outside the current delivery stage or changed; revise the plan before execution", task.ID)
				}
			}
		}
		for _, task := range next.Todos {
			if task.ID == "" || task.Status != TodoStatusCompleted {
				continue
			}
			if before, ok := old[task.ID]; ok && before.Status == TodoStatusCompleted && TaskFingerprint(before) == TaskFingerprint(task) {
				continue
			}
			if state == nil {
				st, err := store.Read(ctx, next.ID)
				if err != nil {
					return err
				}
				state = &st
			}
			run, ok := state.RoleExecutions[task.ID]
			if err := store.SemanticEditsReady(ctx, next.ID); err != nil {
				return fmt.Errorf("task %s semantic edit gate: %w", task.ID, err)
			}
			root, refs := state.ContractRoot, state.TaskContractRefs[task.ID]
			if state.Delivery != nil {
				root, refs = state.Delivery.Root, state.Delivery.TaskContractRefs[task.ID]
			}
			if root == "" && ok {
				root = run.Root
			}
			if root == "" {
				findings, err := store.TaskFindings(ctx, next.ID, task.ID)
				if err != nil {
					return err
				}
				if len(findings) > 0 {
					root = findings[0].Root
				}
			}
			if root != "" {
				if err := store.ValidateTaskContracts(ctx, next.ID, root, task.ID, TaskFingerprint(task), refs); err != nil {
					return fmt.Errorf("task %s contract gate: %w", task.ID, err)
				}
				if err := store.ValidateTaskFindings(ctx, next.ID, root, task.ID, TaskFingerprint(task)); err != nil {
					return fmt.Errorf("task %s finding gate: %w", task.ID, err)
				}
			}
			if !ok || !run.RequireReview {
				continue
			}
			if !run.Passed || run.Handoff == nil || run.Error != "" || run.TaskFingerprint != TaskFingerprint(task) || len(run.Reviews) != 2 {
				return fmt.Errorf("task %s requires independent test and review via workflow review", task.ID)
			}
			if task.Verification != "passed" && task.Verification != "user_confirmed" {
				return fmt.Errorf("task %s requires passing verification", task.ID)
			}
			for i, role := range []string{"test", "review"} {
				review := run.Reviews[i]
				if review.Agent != role || review.Error != "" || review.Handoff == nil || review.Handoff.Decision != "passed" || review.Handoff.TaskID != task.ID {
					return fmt.Errorf("task %s lacks valid independent %s evidence", task.ID, role)
				}
			}
			if len(run.MachineChecks) == 0 {
				return fmt.Errorf("task %s lacks machine verification evidence", task.ID)
			}
			for _, check := range run.MachineChecks {
				if !check.Passed || check.TaskID != task.ID || check.RunID == "" || check.Evidence == "" {
					return fmt.Errorf("task %s has invalid machine verification evidence", task.ID)
				}
			}
			fingerprint, err := engineering.SourceFingerprint(ctx, run.Root, store.Dir())
			if err != nil {
				return err
			}
			if fingerprint != run.SourceFingerprint {
				return fmt.Errorf("task %s source changed after quality checks; rerun workflow review", task.ID)
			}
		}
		return nil
	}
}
