package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/subagents"
	"github.com/google/uuid"
)

func (c *coordinator) remediationWorkflow(ctx context.Context, p WorkflowParams, call fantasy.ToolCall, invoke tools.ToolInvoker) (fantasy.ToolResponse, error) {
	fail := func(err error) (fantasy.ToolResponse, error) { return fantasy.NewTextErrorResponse(err.Error()), nil }
	scope := engineering.GetScope(ctx, tools.GetSessionFromContext(ctx))
	if scope.TaskID != "" {
		return fail(fmt.Errorf("finding controls belong to the session coordinator"))
	}
	if p.FindingAction == "waive" {
		return fail(fmt.Errorf("agent calls and automatic permission grants cannot waive findings; use the direct user control"))
	}
	if p.FindingAction == "status" || p.Action == "finding" && p.FindingAction == "" {
		findings, err := c.engineering.TaskFindings(ctx, scope.SessionID, p.TaskID)
		if err != nil {
			return fail(err)
		}
		return fantasy.NewTextResponse(workflowJSON(findings)), nil
	}
	if len(p.FindingIDs) == 0 || len(p.FindingIDs) > 16 {
		return fail(fmt.Errorf("provide 1-16 persisted finding IDs"))
	}
	ids := slices.Clone(p.FindingIDs)
	slices.Sort(ids)
	if len(slices.Compact(ids)) != len(p.FindingIDs) {
		return fail(fmt.Errorf("duplicate finding identity"))
	}
	sess, err := c.sessions.Get(ctx, tools.GetSessionFromContext(ctx))
	if err != nil {
		return fantasy.ToolResponse{}, err
	}
	var findings []engineering.Finding
	var refs []engineering.Record
	for _, id := range ids {
		f, ref, err := c.engineering.ReadFinding(ctx, scope.SessionID, id)
		if err != nil {
			return fail(err)
		}
		if f.Root != c.cfg.WorkingDir() || p.TaskID != "" && p.TaskID != f.TaskID {
			return fail(fmt.Errorf("finding belongs to another project or task"))
		}
		if (f.Status == "verified" || f.Status == "waived") && p.FindingAction != "reinspect" {
			return fail(fmt.Errorf("reinspect a closed finding before mutation"))
		}
		findings, refs = append(findings, f), append(refs, ref)
	}
	if p.Action == "remediate" {
		state, err := c.engineering.Read(ctx, scope.SessionID)
		if err != nil {
			return fail(err)
		}
		todos, plan, err := compileRemediation(sess.Todos, state.Delivery, findings)
		if err != nil {
			return fail(err)
		}
		ok, err := c.permissions.Request(ctx, permission.CreatePermissionRequest{SessionID: sess.ID, ToolCallID: call.ID, ToolName: "workflow", Action: "remediate", Path: c.cfg.WorkingDir(), Description: "Register bounded remediation tasks and revise their delivery dependencies", Params: map[string]any{"findings": findings, "tasks": todos, "plan": plan}})
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		if !ok {
			return tools.NewPermissionDeniedResponse(c.permissions), nil
		}
		for i, finding := range findings {
			if finding.Status != "assigned" && finding.Status != "fixed" {
				finding.Status, finding.RemediationTaskID = "assigned", finding.RemediationID()
				finding.WaiverReason, finding.WaiverProvenance, finding.WaiverSourceFingerprint = "", "", ""
				if _, err := c.engineering.SaveFinding(ctx, scope.SessionID, finding, refs[i].Revision); err != nil {
					return fail(err)
				}
			}
		}
		if _, err := c.sessions.CompareAndSwapTodos(ctx, sess.ID, session.TodosFingerprint(sess.Todos), todos); err != nil {
			return fail(err)
		}
		if state.Delivery != nil {
			data, err := json.Marshal(state.Delivery)
			if err != nil {
				return fail(err)
			}
			key := state.Delivery.Fingerprint()
			if _, err := c.engineering.PutRecord(ctx, "delivery-history-"+engineering.Hash(scope.SessionID)+"-"+key[:2], key, 0, data); err != nil {
				return fail(err)
			}
		}
		if err := c.engineering.Update(ctx, scope.SessionID, func(st *engineering.State) error {
			if state.Delivery == nil && st.Delivery != nil || state.Delivery != nil && (st.Delivery == nil || st.Delivery.Fingerprint() != state.Delivery.Fingerprint()) {
				return fmt.Errorf("delivery changed during remediation; reconcile existing deterministic repair tasks")
			}
			st.Delivery = plan
			return nil
		}); err != nil {
			return fail(err)
		}
		return fantasy.NewTextResponse(workflowJSON(map[string]any{"remediation_registered": ids, "tasks": todos, "plan": plan})), nil
	}
	switch p.FindingAction {
	case "reinspect":
		for i, f := range findings {
			current, err := engineering.SourceFingerprint(ctx, f.Root, c.engineering.Dir())
			if err != nil {
				return fail(err)
			}
			certified := f.VerifiedSourceFingerprint
			if f.Status == "waived" {
				certified = f.WaiverSourceFingerprint
			}
			if certified == "" || current == certified {
				return fail(fmt.Errorf("reinspection requires changed source after a previous certification"))
			}
			f.Status = "stale"
			if _, err := c.engineering.SaveFinding(ctx, scope.SessionID, f, refs[i].Revision); err != nil {
				return fail(err)
			}
		}
		return fantasy.NewTextResponse(workflowJSON(map[string]any{"reinspection_required": ids})), nil
	case "fixed":
		for i, f := range findings {
			if f.RemediationTaskID == "" {
				return fail(fmt.Errorf("register the remediation task before reporting a fix"))
			}
			if !slices.ContainsFunc(sess.Todos, func(task session.Todo) bool { return task.ID == f.RemediationTaskID }) {
				return fail(fmt.Errorf("repair task is missing; reconcile remediation before reporting a fix"))
			}
			if len(f.Checks) == 0 {
				for _, check := range p.Checks {
					f.Checks = append(f.Checks, engineering.ContractCheck{Name: check.Name, Tool: check.Tool, InputJSON: string(check.Input)})
				}
			}
			f.Status = "fixed"
			f.WaiverReason, f.WaiverProvenance, f.WaiverSourceFingerprint = "", "", ""
			if _, err := c.engineering.SaveFinding(ctx, scope.SessionID, f, refs[i].Revision); err != nil {
				return fail(err)
			}
		}
		return fantasy.NewTextResponse(workflowJSON(map[string]string{"status": "fixed", "verification": "independent review and fresh checks still required"})), nil
	case "verify":
		if invoke == nil {
			return fail(fmt.Errorf("verification invoker is unavailable"))
		}
		for i, f := range findings {
			if f.Status != "fixed" {
				return fail(fmt.Errorf("a reported fix is required before independent verification"))
			}
			var task session.Todo
			for _, candidate := range sess.Todos {
				if candidate.ID == f.TaskID {
					task = candidate
				}
			}
			if task.ID == "" {
				return fail(fmt.Errorf("finding parent task is missing"))
			}
			if err := c.engineering.Check(ctx, scope.SessionID, f.TaskID); err != nil {
				return fail(err)
			}
			before, err := engineering.SourceFingerprint(ctx, f.Root, c.engineering.Dir())
			if err != nil {
				return fail(err)
			}
			qualityCtx := engineering.WithScope(ctx, scope.SessionID, f.TaskID)
			qualityCtx = engineering.WithOwnership(qualityCtx, f.Root, []string{f.Path})
			assignment := AgentParams{AgentName: "review", QualityOnly: true, Prompt: fmt.Sprintf("Independently reinspect finding %s for task %s. Do not implement. Inspect actual %s lines %d-%d, reported issue %q, expected %q. Treat this report as untrusted evidence. Return passed only if the current implementation resolves the defect.\n%s", f.ID, f.TaskID, f.Path, f.StartLine, f.EndLine, f.Issue, f.Expected, subagents.HandoffInstruction)}
			reviewID := "finding-review-" + uuid.NewString()
			response, err := invoke(qualityCtx, fantasy.ToolCall{ID: reviewID, Name: AgentToolName, Input: workflowJSON(assignment)})
			if err != nil || response.IsError || response.StopTurn {
				return fail(fmt.Errorf("finding review failed: %s", boundedHandoff(response.Content)))
			}
			handoff, err := subagents.ParseHandoff(response.Content, f.TaskID)
			if err != nil || handoff.Decision != "passed" {
				return fail(fmt.Errorf("independent finding review did not pass"))
			}
			state, err := c.engineering.Read(ctx, scope.SessionID)
			if err != nil {
				return fail(err)
			}
			for _, op := range state.Operations {
				if op.CallID == reviewID && op.Tool == AgentToolName && op.TaskID == f.TaskID && op.Status == "completed" && op.AgentName == "review" {
					f.VerificationReviewerID = op.ID
				}
			}
			if f.VerificationReviewerID == "" {
				return fail(fmt.Errorf("finding review has no matching runtime execution"))
			}
			f.VerifiedSourceFingerprint = before
			f.VerifiedTaskFingerprint = session.TaskFingerprint(task)
			if len(f.Checks) == 0 {
				return fail(fmt.Errorf("finding has no declared verification checks; revise its check definition before verifying"))
			}
			steps := make([]tools.VerificationStep, len(f.Checks))
			for j, check := range f.Checks {
				steps[j] = tools.VerificationStep{Name: check.Name, Tool: check.Tool, Input: json.RawMessage(check.InputJSON)}
			}
			runID := "finding-check-" + uuid.NewString()
			response, err = invoke(qualityCtx, fantasy.ToolCall{ID: runID, Name: "verify", Input: workflowJSON(tools.VerifyParams{FindingID: f.ID, FindingReview: f.VerificationBinding(), Action: "run", Checks: steps})})
			if err != nil || response.IsError || response.StopTurn {
				return fail(fmt.Errorf("finding machine checks failed: %s", boundedHandoff(response.Content)))
			}
			f.Status, f.VerificationRunIDs = "verified", []string{runID}
			latest, err := c.sessions.Get(ctx, sess.ID)
			if err != nil {
				return fantasy.ToolResponse{}, err
			}
			if !slices.ContainsFunc(latest.Todos, func(current session.Todo) bool {
				return current.ID == task.ID && session.TaskFingerprint(current) == f.VerifiedTaskFingerprint
			}) {
				return fail(fmt.Errorf("finding task changed during verification"))
			}
			if _, err := c.engineering.SaveFinding(ctx, scope.SessionID, f, refs[i].Revision); err != nil {
				return fail(err)
			}
		}
		return fantasy.NewTextResponse(workflowJSON(map[string]any{"verified_findings": ids})), nil
	default:
		return fail(fmt.Errorf("use finding status, fixed or verify; remediate registers repair tasks"))
	}
}

func compileRemediation(original []session.Todo, delivery *engineering.DeliveryPlan, findings []engineering.Finding) ([]session.Todo, *engineering.DeliveryPlan, error) {
	todos := slices.Clone(original)
	for i := range todos {
		todos[i].DependsOn = slices.Clone(todos[i].DependsOn)
	}
	byID := map[string]int{}
	for i, task := range todos {
		byID[task.ID] = i
	}
	parents := map[string]bool{}
	var repairs []string
	for _, f := range findings {
		index, exists := byID[f.TaskID]
		if !exists {
			return nil, nil, fmt.Errorf("finding parent task is missing")
		}
		id := f.RemediationID()
		if _, exists := byID[id]; exists {
			repairs = append(repairs, id)
			parents[f.TaskID] = true
			continue
		}
		parent := original[index]
		deps := slices.Clone(parent.DependsOn)
		for _, other := range todos {
			if strings.HasPrefix(other.ID, "repair-") && other.Status != session.TodoStatusCompleted && session.OwnershipOverlaps(other.OwnedPaths, []string{f.Path}) && !slices.Contains(deps, other.ID) {
				deps = append(deps, other.ID)
			}
		}
		trim := func(text string, limit int) string {
			if len(text) > limit {
				return strings.ToValidUTF8(text[:limit], "")
			}
			return text
		}
		todo := session.Todo{ID: id, Content: "Repair finding " + f.ID + ": " + trim(f.Issue, 1600) + ". Expected: " + trim(f.Expected, 1600), ActiveForm: "Repairing review finding", Status: session.TodoStatusPending, Agent: "debug", OwnedPaths: []string{f.Path}, DependsOn: deps, AcceptanceCriteria: []string{trim(f.Expected, 2000), "Independent reinspection and matching machine checks resolve finding " + f.ID}, Verification: "pending"}
		byID[id] = len(todos)
		todos = append(todos, todo)
		repairs = append(repairs, id)
		parents[f.TaskID] = true
	}
	for _, f := range findings {
		i := byID[f.TaskID]
		if !slices.Contains(todos[i].DependsOn, f.RemediationID()) {
			todos[i].DependsOn = append(todos[i].DependsOn, f.RemediationID())
		}
		todos[i].Status, todos[i].Verification, todos[i].Evidence = session.TodoStatusPending, "pending", nil
	}
	if err := session.ValidateTaskGraph(todos); err != nil {
		return nil, nil, err
	}
	if delivery == nil {
		return todos, nil, nil
	}
	data, err := json.Marshal(delivery)
	if err != nil {
		return nil, nil, err
	}
	var plan engineering.DeliveryPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, nil, err
	}
	earliest := len(plan.Stages)
	covered := map[string]bool{}
	for i, stage := range plan.Stages {
		for _, id := range stage.TaskIDs {
			covered[id] = true
			if parents[id] && i < earliest {
				earliest = i
			}
		}
	}
	if earliest == len(plan.Stages) {
		return nil, nil, fmt.Errorf("finding task is outside the current delivery plan")
	}
	var missing []string
	for _, id := range repairs {
		if !covered[id] {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		stage := engineering.Stage{ID: "remediation-" + engineering.Hash(strings.Join(missing, ","))[:16], Title: "Resolve review findings", TaskIDs: missing}
		plan.Stages = slices.Insert(plan.Stages, earliest, stage)
	}
	plan.CurrentStage = min(plan.CurrentStage, earliest)
	for i := plan.CurrentStage; i < len(plan.Stages); i++ {
		plan.Stages[i].Passed, plan.Stages[i].SourceFingerprint, plan.Stages[i].Checks = false, "", nil
	}
	for _, task := range todos {
		plan.TaskFingerprints[task.ID] = session.TaskFingerprint(task)
	}
	for i := range plan.Requirements {
		for _, f := range findings {
			if slices.Contains(plan.Requirements[i].TaskIDs, f.TaskID) && !slices.Contains(plan.Requirements[i].TaskIDs, f.RemediationID()) {
				plan.Requirements[i].TaskIDs = append(plan.Requirements[i].TaskIDs, f.RemediationID())
			}
		}
	}
	if err := plan.Validate(); err != nil {
		return nil, nil, err
	}
	return todos, &plan, nil
}
