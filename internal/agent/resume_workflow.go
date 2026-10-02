package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/codegraph"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/google/uuid"
)

func checkpointTasks(todos []session.Todo) (map[string]string, map[string]string) {
	specs, states := map[string]string{}, map[string]string{}
	for _, task := range todos {
		specs[task.ID], states[task.ID] = session.TaskFingerprint(task), string(task.Status)
	}
	return specs, states
}

func (c *coordinator) captureCheckpoint(ctx context.Context, sess session.Session, messageID string) (engineering.Checkpoint, error) {
	id := engineering.GetScope(ctx, sess.ID).SessionID
	state, err := c.engineering.Read(ctx, id)
	if err != nil {
		return engineering.Checkpoint{}, err
	}
	if err := c.currentRecipe(ctx, id); err != nil {
		return engineering.Checkpoint{}, err
	}
	source, err := engineering.SourceFingerprint(ctx, c.cfg.WorkingDir(), c.engineering.Dir())
	if err != nil {
		return engineering.Checkpoint{}, err
	}
	specs, _ := checkpointTasks(sess.Todos)
	data, err := json.Marshal(specs)
	if err != nil {
		return engineering.Checkpoint{}, err
	}
	cp := engineering.Checkpoint{ID: uuid.NewString(), Root: c.cfg.WorkingDir(), SessionID: id, MessageID: messageID, SourceFingerprint: source, PlanFingerprint: engineering.Hash(string(data)), TaskFingerprints: specs, Workspaces: []string{}, Operations: []string{}}
	if state.Recipe != nil {
		cp.RecipeHash, cp.ParametersHash = state.Recipe.RecipeHash, state.Recipe.ParametersHash
	}
	if state.Delivery != nil {
		cp.PlanFingerprint, cp.Stage = state.Delivery.Fingerprint(), state.Delivery.CurrentStage
	}
	for _, work := range state.Workspaces {
		cp.Workspaces = append(cp.Workspaces, work.ID)
	}
	for _, op := range state.Operations {
		cp.Operations = append(cp.Operations, op.ID)
	}
	_, err = c.engineering.SaveCheckpoint(ctx, cp)
	return cp, err
}

func (c *coordinator) currentResumePlan(ctx context.Context, cp engineering.Checkpoint, sess session.Session) (engineering.ResumePlan, error) {
	if err := c.currentRecipe(ctx, cp.SessionID); err != nil {
		return engineering.ResumePlan{}, err
	}
	state, err := c.engineering.Read(ctx, cp.SessionID)
	if err != nil {
		return engineering.ResumePlan{}, err
	}
	source, err := engineering.SourceFingerprint(ctx, c.cfg.WorkingDir(), c.engineering.Dir())
	if err != nil {
		return engineering.ResumePlan{}, err
	}
	specs, states := checkpointTasks(sess.Todos)
	plan, err := engineering.PlanResume(ctx, engineering.ResumeInput{Checkpoint: cp, State: state, Source: source, TaskSpecs: specs, TaskStates: states})
	if err != nil {
		return plan, err
	}
	c.engineering.ReconcileResumeEvidence(ctx, cp.SessionID, cp.Root, state, specs, states, &plan)
	if len(plan.AmbiguousOperations) == 0 && len(plan.ReverifyTasks) == 0 {
		wave, err := c.deliveryReady(ctx, cp.SessionID, sess.Todos, 16)
		if err != nil {
			return plan, err
		}
		ready := []string{}
		for _, task := range wave {
			if slices.Contains(plan.ReadyTasks, task.ID) {
				ready = append(ready, task.ID)
			}
		}
		plan.ReadyTasks = ready
	}
	return plan, nil
}

func (c *coordinator) resumeWorkflow(ctx context.Context, p WorkflowParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	scope := engineering.GetScope(ctx, tools.GetSessionFromContext(ctx))
	if scope.TaskID != "" {
		return fantasy.NewTextErrorResponse("checkpoint and resume are session-level controls"), nil
	}
	sess, err := c.sessions.Get(ctx, tools.GetSessionFromContext(ctx))
	if err != nil {
		return fantasy.ToolResponse{}, err
	}
	fail := func(err error) (fantasy.ToolResponse, error) { return fantasy.NewTextErrorResponse(err.Error()), nil }
	if p.Action == "checkpoint" {
		cp, err := c.captureCheckpoint(ctx, sess, call.ID)
		if err != nil {
			return fail(err)
		}
		return fantasy.NewTextResponse(workflowJSON(cp)), nil
	}
	id := p.CheckpointID
	if p.ResumePlan != nil {
		id = p.ResumePlan.CheckpointID
	}
	cp, err := c.engineering.ReadCheckpoint(ctx, c.cfg.WorkingDir(), scope.SessionID, id)
	if err != nil {
		return fail(err)
	}
	root, _ := codegraph.Namespace(cp.Root)
	current, _ := codegraph.Namespace(c.cfg.WorkingDir())
	if root != current {
		return fail(fmt.Errorf("checkpoint belongs to another root"))
	}
	plan, err := c.currentResumePlan(ctx, cp, sess)
	if err != nil {
		return fail(err)
	}
	if p.Action == "resume-plan" {
		return fantasy.NewTextResponse(workflowJSON(plan)), nil
	}
	if p.ResumePlan != nil && !reflect.DeepEqual(*p.ResumePlan, plan) {
		return fail(fmt.Errorf("resume plan changed; inspect a fresh plan"))
	}
	if len(plan.AmbiguousOperations) > 0 {
		return fail(fmt.Errorf("inspect jobs or recover ambiguous operations before resume; commands will not be replayed"))
	}
	if len(plan.ReverifyTasks) > 0 {
		return fail(fmt.Errorf("changed or interrupted tasks require fresh verification and checkpoint before resume"))
	}
	allowed, err := c.permissions.Request(ctx, permission.CreatePermissionRequest{SessionID: sess.ID, ToolCallID: call.ID, ToolName: "workflow", Action: "resume", Path: cp.Root, Description: "Approve the reconciled pending-task schedule without replaying commands", Params: plan})
	if err != nil {
		return fantasy.ToolResponse{}, err
	}
	if !allowed {
		return tools.NewPermissionDeniedResponse(c.permissions), nil
	}
	latest, err := c.sessions.Get(ctx, sess.ID)
	if err != nil {
		return fantasy.ToolResponse{}, err
	}
	observed, err := c.currentResumePlan(ctx, cp, latest)
	if err != nil {
		return fail(err)
	}
	if !reflect.DeepEqual(plan, observed) {
		return fail(fmt.Errorf("resume plan changed during permission request"))
	}
	err = c.engineering.UpdateRevision(ctx, scope.SessionID, plan.StateRevision, func(st *engineering.State) error {
		fresh, err := engineering.SourceFingerprint(ctx, cp.Root, c.engineering.Dir())
		if err != nil {
			return err
		}
		if fresh != plan.SourceFingerprint {
			return fmt.Errorf("source changed before resume commit")
		}
		st.Resume = &plan
		return nil
	})
	if err != nil {
		return fail(err)
	}
	return fantasy.NewTextResponse(workflowJSON(map[string]any{"approved_resume": plan, "ready_tasks": plan.ReadyTasks, "commands_replayed": 0})), nil
}
