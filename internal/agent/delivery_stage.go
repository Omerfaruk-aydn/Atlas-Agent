package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
)

func (c *coordinator) advanceDelivery(ctx context.Context, p WorkflowParams, call fantasy.ToolCall, sess session.Session, st engineering.State, invoke tools.ToolInvoker) (fantasy.ToolResponse, error) {
	fail := func(message string) (fantasy.ToolResponse, error) { return fantasy.NewTextErrorResponse(message), nil }
	if err := c.engineering.SemanticEditsReady(ctx, engineering.GetScope(ctx, sess.ID).SessionID); err != nil {
		return fail(err.Error())
	}
	plan := st.Delivery
	if plan == nil || plan.CurrentStage >= len(plan.Stages) {
		return fail("no incomplete registered stage")
	}
	if filepath.Clean(plan.Root) != filepath.Clean(c.cfg.WorkingDir()) {
		return fail("delivery belongs to another project root; register a revised plan")
	}
	stage := plan.Stages[plan.CurrentStage]
	declared, err := c.recipeChecks(ctx, engineering.GetScope(ctx, sess.ID).SessionID, stage.TaskIDs)
	if err != nil {
		return fail(err.Error())
	}
	if len(declared) > 0 {
		p.Checks = declared
	}
	byID := map[string]session.Todo{}
	for _, task := range sess.Todos {
		byID[task.ID] = task
	}
	for _, id := range stage.TaskIDs {
		task, ok := byID[id]
		if !ok || session.TaskFingerprint(task) != plan.TaskFingerprints[id] || task.Status != session.TodoStatusCompleted || task.Verification != "passed" && task.Verification != "user_confirmed" {
			return fail("stage requires unchanged completed tasks with passing evidence: " + id)
		}
		if run, exists := st.RoleExecutions[id]; exists && run.RequireReview && (!run.Passed || run.TaskFingerprint != plan.TaskFingerprints[id] || run.Error != "") {
			return fail("stage task requires independent review: " + id)
		}
		if err := c.engineering.ValidateTaskContracts(ctx, engineering.GetScope(ctx, sess.ID).SessionID, plan.Root, id, plan.TaskFingerprints[id], plan.TaskContractRefs[id]); err != nil {
			return fail(err.Error())
		}
		if err := c.engineering.ValidateTaskFindings(ctx, engineering.GetScope(ctx, sess.ID).SessionID, plan.Root, id, plan.TaskFingerprints[id]); err != nil {
			return fail(err.Error())
		}
	}
	before, err := engineering.SourceFingerprint(ctx, plan.Root, c.engineering.Dir())
	if err != nil {
		return fail(err.Error())
	}
	if err := plan.DesignReady(ctx, st, before, stage); err != nil {
		return fail(err.Error())
	}
	id := engineering.GetScope(ctx, sess.ID).SessionID
	taskID := "stage:" + stage.ID
	verifyID := call.ID + "-stage-verify"
	verifyCtx := engineering.WithScope(ctx, id, taskID)
	response, err := invoke(verifyCtx, fantasy.ToolCall{ID: verifyID, Name: "verify", Input: workflowJSON(tools.VerifyParams{Action: "run", Checks: p.Checks})})
	var result struct {
		Passed bool `json:"passed"`
	}
	if err != nil || response.IsError || response.StopTurn || json.Unmarshal([]byte(response.Content), &result) != nil || !result.Passed {
		return fail("stage machine verification failed: " + boundedHandoff(response.Content))
	}
	observed, err := c.engineering.Read(ctx, id)
	if err != nil {
		return fantasy.ToolResponse{}, err
	}
	stage.Checks = nil
	for _, check := range observed.Checks {
		if check.RunID == verifyID && check.TaskID == taskID {
			if slices.Contains(st.Checks, check) {
				continue
			}
			if !check.Passed || check.Evidence == "" {
				return fail("stage journal contains invalid checks")
			}
			stage.Checks = append(stage.Checks, check)
		}
	}
	if len(stage.Checks) == 0 {
		return fail("stage has no matching machine-observed checks")
	}
	after, err := engineering.SourceFingerprint(ctx, plan.Root, c.engineering.Dir())
	if err != nil {
		return fail(err.Error())
	}
	if before != after {
		return fail("source changed during stage verification; inspect and rerun")
	}
	// Re-read task reports after verification to detect concurrent steering.
	latest, err := c.sessions.Get(ctx, sess.ID)
	if err != nil {
		return fantasy.ToolResponse{}, err
	}
	for _, id := range stage.TaskIDs {
		found := false
		for _, task := range latest.Todos {
			if task.ID == id && task.Status == session.TodoStatusCompleted && (task.Verification == "passed" || task.Verification == "user_confirmed") && session.TaskFingerprint(task) == plan.TaskFingerprints[id] {
				found = true
			}
		}
		if !found {
			return fail("stage tasks changed during verification")
		}
		if err := c.engineering.ValidateTaskContracts(ctx, engineering.GetScope(ctx, sess.ID).SessionID, plan.Root, id, plan.TaskFingerprints[id], plan.TaskContractRefs[id]); err != nil {
			return fail(err.Error())
		}
	}
	stage.Passed = true
	if err := c.engineering.SemanticEditsReady(ctx, engineering.GetScope(ctx, sess.ID).SessionID); err != nil {
		return fail(err.Error())
	}
	for _, id := range stage.TaskIDs {
		if err := c.engineering.ValidateTaskFindings(ctx, engineering.GetScope(ctx, sess.ID).SessionID, plan.Root, id, plan.TaskFingerprints[id]); err != nil {
			return fail(err.Error())
		}
	}
	stage.SourceFingerprint = after
	err = c.engineering.Update(ctx, id, func(next *engineering.State) error {
		if next.Delivery == nil || next.Delivery.Fingerprint() != plan.Fingerprint() {
			return fmt.Errorf("delivery plan changed during verification")
		}
		next.Delivery.Stages[plan.CurrentStage] = stage
		next.Delivery.CurrentStage++
		return nil
	})
	if err != nil {
		return fail(err.Error())
	}
	cp, err := c.captureCheckpoint(ctx, latest, call.ID)
	if err != nil {
		return fail("stage passed, but saving its recovery checkpoint failed: " + err.Error())
	}
	return fantasy.NewTextResponse(workflowJSON(map[string]any{"passed_stage": stage, "next_stage": plan.CurrentStage + 1, "all_stages_passed": plan.CurrentStage+1 == len(plan.Stages), "checkpoint": cp})), nil
}
