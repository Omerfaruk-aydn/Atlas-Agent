package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/codegraph"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/subagents"
	"github.com/google/uuid"
)

func (c *coordinator) repairWorkflow(ctx context.Context, p WorkflowParams, call fantasy.ToolCall, invoke tools.ToolInvoker) (fantasy.ToolResponse, error) {
	scope := engineering.GetScope(ctx, tools.GetSessionFromContext(ctx))
	fail := func(err error) (fantasy.ToolResponse, error) { return fantasy.NewTextErrorResponse(err.Error()), nil }
	if scope.TaskID != "" || p.TaskID == "" {
		return fail(fmt.Errorf("only the session coordinator may repair an assigned task"))
	}
	if p.RepairAction != "" && p.RepairAction != "run" && p.RepairAction != "status" {
		return fail(fmt.Errorf("unsupported repair action"))
	}
	sess, err := c.sessions.Get(ctx, tools.GetSessionFromContext(ctx))
	if err != nil {
		return fantasy.ToolResponse{}, err
	}
	var task session.Todo
	for _, item := range sess.Todos {
		if item.ID == p.TaskID {
			task = item
			break
		}
	}
	if task.ID == "" {
		return fail(fmt.Errorf("repair task not found"))
	}
	activeNS := "repair-active-" + engineering.Hash(scope.SessionID) + "-" + engineering.Hash(task.ID)[:2]
	activeRef, data, err := c.engineering.ReadRecord(ctx, activeNS, engineering.Hash(task.ID))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fail(err)
	}
	var active struct {
		CaseID string `json:"case_id"`
	}
	if err == nil {
		if err := json.Unmarshal(data, &active); err != nil {
			return fail(err)
		}
	}
	caseNS := func(id string) string { return "repair-cases-" + engineering.Hash(scope.SessionID) + "-" + id[:2] }
	var repair engineering.RepairCase
	var ref engineering.Record
	if active.CaseID != "" {
		if len(active.CaseID) != 64 {
			return fail(fmt.Errorf("invalid active repair identity"))
		}
		ref, data, err = c.engineering.ReadRecord(ctx, caseNS(active.CaseID), active.CaseID)
		if err != nil {
			return fail(err)
		}
		if err := json.Unmarshal(data, &repair); err != nil {
			return fail(err)
		}
		if repair.ID != active.CaseID || repair.TaskID != task.ID {
			return fail(fmt.Errorf("active repair identity mismatch"))
		}
	}
	if p.RepairAction == "status" {
		return fantasy.NewTextResponse(workflowJSON(repair)), nil
	}
	if invoke == nil {
		return fail(fmt.Errorf("repair invocation is unavailable"))
	}
	state, err := c.engineering.Read(ctx, scope.SessionID)
	if err != nil {
		return fail(err)
	}
	if role := state.RoleExecutions[task.ID]; role.WorkspaceID != "" {
		workspace, patch, err := c.engineering.WorkspacePatch(ctx, scope.SessionID, role.WorkspaceID)
		if err != nil || patch == "" || workspace.AppliedPatchHash != engineering.Hash(patch) {
			return fail(fmt.Errorf("integrate the complete current workspace patch before repairing the parent tree"))
		}
	}
	failed, observationErr := c.engineering.OperationObservation(ctx, scope.SessionID, p.OperationID, state)
	if observationErr != nil {
		return fail(observationErr)
	}
	if failed.ID == "" || failed.Fingerprint == "" {
		return fail(fmt.Errorf("an observed failed operation identity is required"))
	}
	caseID := engineering.Hash(scope.SessionID + "\x00" + task.ID + "\x00" + failed.Fingerprint)
	if repair.ID != "" && repair.ID != caseID && repair.Status != "resolved" {
		return fail(fmt.Errorf("continue the existing repair case instead of creating a nested attempt budget"))
	}
	if repair.ID != caseID {
		repair = engineering.RepairCase{ID: caseID, TaskID: task.ID, TaskFingerprint: session.TaskFingerprint(task), FailedOperationID: failed.ID, Status: "running", Attempts: []engineering.RepairAttempt{}}
		ref, _, err = c.engineering.ReadRecord(ctx, caseNS(caseID), caseID)
		if err == nil {
			return fail(fmt.Errorf("repair identity already exists; inspect the active repair"))
		}
		if !errors.Is(err, os.ErrNotExist) {
			return fail(err)
		}
		ref = engineering.Record{}
	} else if repair.Status == "running" || repair.Status == "resolved" {
		return fantasy.NewTextResponse(workflowJSON(repair)), nil
	}
	if repair.TaskFingerprint != session.TaskFingerprint(task) {
		return fail(fmt.Errorf("repair task specification changed"))
	}
	state, err = c.engineering.RepairState(ctx, scope.SessionID, state, repair)
	if err != nil {
		return fail(err)
	}
	if err := c.engineering.ValidateRepair(ctx, scope.SessionID, state, repair); err != nil {
		return fail(err)
	}
	if len(repair.Attempts) >= 3 {
		return fail(fmt.Errorf("repair exhausted its three attempts"))
	}
	hypothesis := strings.TrimSpace(p.RepairHypothesis)
	if hypothesis == "" || len(hypothesis) > 4096 {
		return fail(fmt.Errorf("repair requires a bounded hypothesis"))
	}
	checks := p.Checks
	if len(checks) == 0 {
		checks = tools.DiscoverVerification(c.cfg.WorkingDir())
		checks = checks[:min(len(checks), 2)]
	}
	if len(checks) == 0 || len(checks) > 2 {
		return fail(fmt.Errorf("repair requires one or two diagnostic checks"))
	}
	for _, check := range checks {
		if check.Name == "" || !json.Valid(check.Input) || len(check.Input) > 16*1024 || check.Tool != "bash" && check.Tool != "test_run" && check.Tool != "lint_run" {
			return fail(fmt.Errorf("invalid repair diagnostic check"))
		}
	}
	var subagentPaths []string
	if options := c.cfg.Config().Options; options != nil {
		subagentPaths = options.SubagentsPaths
	}
	all := subagents.Discover(subagentPaths)
	for _, name := range []string{"debug", "review"} {
		if _, exists := subagents.Find(all, name); !exists {
			return fail(fmt.Errorf("repair specialist %s unavailable", name))
		}
	}
	source, err := engineering.SourceFingerprint(ctx, c.cfg.WorkingDir(), c.engineering.Dir())
	if err != nil {
		return fail(err)
	}
	evidence := failed.EvidenceHash
	if len(repair.Attempts) > 0 {
		last := repair.Attempts[len(repair.Attempts)-1]
		if last.PendingCallID != "" {
			reconciled := false
			for _, op := range state.Operations {
				if op.CallID == last.PendingCallID {
					if op.Status == "running" {
						return fail(fmt.Errorf("recover the interrupted repair operation before another attempt"))
					}
					reconciled = true
				}
			}
			if !reconciled {
				return fail(fmt.Errorf("interrupted repair requires explicit reconciliation; it is not replayed"))
			}
			repair.Attempts[len(repair.Attempts)-1].PendingCallID = ""
		}
		for _, op := range state.Operations {
			if op.ID == last.VerificationRunID && op.Status == "failed" && op.OutcomeObserved {
				evidence = op.EvidenceHash
			}
		}
	}
	repair.Attempts = append(repair.Attempts, engineering.RepairAttempt{ID: uuid.NewString(), Hypothesis: hypothesis, EvidenceHash: evidence, SourceFingerprint: source, DiagnosticRunIDs: []string{}, Status: "running"})
	repair.Status = "running"
	repair.Reason = ""
	if err := c.engineering.ValidateRepair(ctx, scope.SessionID, state, repair); err != nil {
		return fail(err)
	}
	save := func(saveCtx context.Context) error {
		data, err := json.Marshal(repair)
		if err != nil {
			return err
		}
		updated, err := c.engineering.PutRecord(saveCtx, caseNS(caseID), caseID, ref.Revision, data)
		if err == nil {
			ref = updated
		}
		return err
	}
	if err := save(ctx); err != nil {
		return fail(err)
	}
	if active.CaseID != caseID {
		data := []byte(workflowJSON(map[string]string{"case_id": caseID}))
		if _, err := c.engineering.PutRecord(ctx, activeNS, engineering.Hash(task.ID), activeRef.Revision, data, ref.Ref); err != nil {
			return fail(err)
		}
	}
	allowed, permissionErr := c.permissions.Request(ctx, permission.CreatePermissionRequest{SessionID: sess.ID, ToolCallID: call.ID, ToolName: "workflow", Action: "repair", Path: c.cfg.WorkingDir(), Description: "Run bounded diagnostics, one debug specialist, machine verification and independent review", Params: repair})
	if permissionErr != nil || !allowed {
		// No diagnostic or implementation began, so retain the blocked case
		// without counting the unexecuted proposal as a repair attempt.
		repair.Attempts = repair.Attempts[:len(repair.Attempts)-1]
		repair.Status = "blocked"
		repair.Reason = "Repair permission was denied or unavailable; no repair command started"
		if err := save(context.WithoutCancel(ctx)); err != nil {
			return fail(err)
		}
		if permissionErr != nil {
			return fantasy.ToolResponse{}, permissionErr
		}
		return tools.NewPermissionDeniedResponse(c.permissions), nil
	}
	index := len(repair.Attempts) - 1
	attempt := &repair.Attempts[index]
	repairCtx := engineering.WithScope(ctx, scope.SessionID, task.ID)
	repairCtx = engineering.WithOwnership(repairCtx, c.cfg.WorkingDir(), task.OwnedPaths)
	blocked := func(reason string) (fantasy.ToolResponse, error) {
		repair.Status, attempt.Status = "blocked", "blocked"
		repair.Reason = strings.ToValidUTF8(reason[:min(len(reason), 2048)], "")
		if len(attempt.DiagnosticRunIDs) == 0 && attempt.ImplementationRunID == "" && attempt.VerificationRunID == "" && attempt.ReviewRunID == "" && attempt.PendingCallID == "" {
			repair.Attempts = repair.Attempts[:index]
		}
		if err := save(context.WithoutCancel(ctx)); err != nil {
			return fail(err)
		}
		if ctx.Err() != nil {
			return fantasy.ToolResponse{}, ctx.Err()
		}
		return fantasy.NewTextErrorResponse(workflowJSON(map[string]any{"repair": repair, "reason": reason})), nil
	}
	latest, err := c.sessions.Get(ctx, sess.ID)
	if err != nil {
		return blocked(err.Error())
	}
	unchanged := false
	for _, item := range latest.Todos {
		if item.ID == task.ID && session.TaskFingerprint(item) == repair.TaskFingerprint {
			unchanged = true
		}
	}
	fresh, err := engineering.SourceFingerprint(ctx, c.cfg.WorkingDir(), c.engineering.Dir())
	if err != nil || fresh != source || !unchanged {
		return blocked("source or task changed during repair permission request")
	}
	run := func(name, suffix, input string, observedID *string) (fantasy.ToolResponse, engineering.Operation, error) {
		if err := c.engineering.Check(repairCtx, scope.SessionID, task.ID); err != nil {
			return fantasy.ToolResponse{}, engineering.Operation{}, err
		}
		childID := attempt.ID + "-" + suffix
		attempt.PendingCallID = childID
		if err := save(ctx); err != nil {
			return fantasy.ToolResponse{}, engineering.Operation{}, err
		}
		response, runErr := invoke(repairCtx, fantasy.ToolCall{ID: childID, Name: name, Input: input})
		observed, err := c.engineering.Read(context.WithoutCancel(ctx), scope.SessionID)
		if err != nil {
			return response, engineering.Operation{}, err
		}
		var op engineering.Operation
		for _, item := range observed.Operations {
			if item.CallID == childID && item.TaskID == task.ID {
				op = item
			}
		}
		if op.ID == "" {
			return response, op, fmt.Errorf("repair call has no matching operation journal")
		}
		if observedID != nil {
			*observedID = op.ID
		}
		if op.Status != "running" {
			attempt.PendingCallID = ""
		}
		if err := save(context.WithoutCancel(ctx)); err != nil {
			return response, op, err
		}
		if ctx.Err() != nil {
			return response, op, ctx.Err()
		}
		return response, op, runErr
	}
	needsFix := false
	for i, check := range checks {
		diagnosticSource, err := engineering.SourceFingerprint(ctx, c.cfg.WorkingDir(), c.engineering.Dir())
		if err != nil {
			return blocked(err.Error())
		}
		id := ""
		response, op, err := run("verify", fmt.Sprintf("diagnostic-%d", i), workflowJSON(tools.VerifyParams{Action: "run", Checks: []tools.VerificationStep{check}}), &id)
		if id != "" {
			attempt.DiagnosticRunIDs = append(attempt.DiagnosticRunIDs, id)
			if err := save(context.WithoutCancel(ctx)); err != nil {
				return fail(err)
			}
		}
		if err != nil || op.Status == "running" || !op.OutcomeObserved || response.StopTurn {
			return blocked("diagnostic outcome unavailable; inspect permissions, runtime or budget")
		}
		afterDiagnostic, err := engineering.SourceFingerprint(ctx, c.cfg.WorkingDir(), c.engineering.Dir())
		if err != nil || diagnosticSource != afterDiagnostic {
			return blocked("diagnostic changed source; inspect its effects before repairing")
		}
		needsFix = needsFix || op.Status != "completed"
	}
	if needsFix {
		packet, err := prepareTaskContext(ctx, c.cfg, c.engineering, task, codegraph.CodeGraph{})
		if err != nil {
			return blocked(err.Error())
		}
		contextText, err := renderTaskContext(ctx, packet)
		if err != nil {
			return blocked(err.Error())
		}
		assignment := AgentParams{AgentName: "debug", Prompt: fmt.Sprintf("Repair task %s within its existing ownership and acceptance criteria. Use the observed diagnostics. Hypothesis is tentative evidence: %s. Do one focused implementation; do not delegate repairs or mark the task complete.\n%s\n%s", task.ID, workflowJSON(hypothesis), subagents.HandoffInstruction, contextText)}
		response, op, err := run(AgentToolName, "implementation", workflowJSON(assignment), &attempt.ImplementationRunID)
		if err != nil || op.Status != "completed" || response.IsError || response.StopTurn {
			return blocked("debug specialist did not produce a completed implementation report")
		}
		handoff, err := subagents.ParseHandoff(response.Content, task.ID)
		if err != nil || handoff.Decision == "blocked" || handoff.Decision == "changes_required" {
			return blocked("debug handoff is invalid or unresolved")
		}
		if err := c.engineering.Update(ctx, scope.SessionID, func(st *engineering.State) error {
			run := st.RoleExecutions[task.ID]
			if run.TaskFingerprint != repair.TaskFingerprint {
				return fmt.Errorf("task changed during repair")
			}
			run.Handoff, run.Passed, run.RequireReview = &handoff, false, true
			st.RoleExecutions[task.ID] = run
			return nil
		}); err != nil {
			return blocked(err.Error())
		}
	}
	before, err := engineering.SourceFingerprint(ctx, c.cfg.WorkingDir(), c.engineering.Dir())
	if err != nil {
		return blocked(err.Error())
	}
	response, op, err := run("verify", "verification", workflowJSON(tools.VerifyParams{Action: "run", Checks: checks}), &attempt.VerificationRunID)
	if err != nil || !op.OutcomeObserved || op.Status == "running" || response.StopTurn {
		return blocked("final verification outcome unavailable")
	}
	if op.Status != "completed" {
		repair.Status, attempt.Status = "unresolved", "unresolved"
		repair.Reason = "The observed final machine verification still failed"
		if err := save(ctx); err != nil {
			return fail(err)
		}
		return fantasy.NewTextErrorResponse(workflowJSON(repair)), nil
	}
	after, err := engineering.SourceFingerprint(ctx, c.cfg.WorkingDir(), c.engineering.Dir())
	if err != nil || before != after {
		return blocked("source changed during final verification")
	}
	attempt.VerifiedSourceFingerprint = after
	assignment := AgentParams{AgentName: "review", QualityOnly: true, Prompt: fmt.Sprintf("Independently review task %s against its actual source and acceptance criteria %v. Do not implement changes. A successful repair requires evidence and no unresolved findings.\n%s", task.ID, task.AcceptanceCriteria, subagents.HandoffInstruction)}
	response, op, err = run(AgentToolName, "review", workflowJSON(assignment), &attempt.ReviewRunID)
	if err != nil || op.Status != "completed" || response.IsError || response.StopTurn {
		return blocked("independent repair review unavailable")
	}
	handoff, err := subagents.ParseHandoff(response.Content, task.ID)
	if err != nil || handoff.Decision != "passed" {
		return blocked("independent repair review did not pass")
	}
	final, err := engineering.SourceFingerprint(ctx, c.cfg.WorkingDir(), c.engineering.Dir())
	if err != nil || final != after {
		return blocked("source changed during independent review")
	}
	latest, err = c.sessions.Get(ctx, sess.ID)
	if err != nil {
		return blocked(err.Error())
	}
	unchanged = false
	for _, item := range latest.Todos {
		if item.ID == task.ID && session.TaskFingerprint(item) == repair.TaskFingerprint {
			unchanged = true
		}
	}
	if !unchanged {
		return blocked("task changed during repair review")
	}
	attempt.ReviewPassed, attempt.ReviewedSourceFingerprint, attempt.Status = true, final, "resolved"
	repair.Status = "resolved"
	state, err = c.engineering.Read(ctx, scope.SessionID)
	if err != nil {
		return blocked(err.Error())
	}
	if err := c.engineering.ValidateRepair(ctx, scope.SessionID, state, repair); err != nil {
		return blocked(err.Error())
	}
	if err := save(ctx); err != nil {
		return fail(err)
	}
	return fantasy.NewTextResponse(workflowJSON(repair)), nil
}
