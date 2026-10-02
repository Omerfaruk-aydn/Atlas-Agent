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
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/google/uuid"
)

func (c *coordinator) contractWorkflow(ctx context.Context, p WorkflowParams, call fantasy.ToolCall, invoke tools.ToolInvoker) (fantasy.ToolResponse, error) {
	fail := func(err error) (fantasy.ToolResponse, error) { return fantasy.NewTextErrorResponse(err.Error()), nil }
	scope := engineering.GetScope(ctx, tools.GetSessionFromContext(ctx))
	root := c.cfg.WorkingDir()
	if p.ContractAction == "query" {
		if scope.TaskID != "" && scope.TaskID != p.TaskID {
			return fail(fmt.Errorf("query only the assigned task contracts"))
		}
		contracts, err := c.engineering.ContractsForTask(ctx, root, p.TaskID)
		if err != nil {
			return fail(err)
		}
		return fantasy.NewTextResponse(workflowJSON(contracts)), nil
	}
	if scope.TaskID != "" {
		return fail(fmt.Errorf("only the session coordinator may revise or certify contracts"))
	}
	sess, err := c.sessions.Get(ctx, tools.GetSessionFromContext(ctx))
	if err != nil {
		return fantasy.ToolResponse{}, err
	}
	tasks := map[string]session.Todo{}
	for _, task := range sess.Todos {
		tasks[task.ID] = task
	}
	state, err := c.engineering.Read(ctx, scope.SessionID)
	if err != nil {
		return fail(err)
	}
	if state.ContractRoot != "" && filepath.Clean(state.ContractRoot) != filepath.Clean(root) {
		return fail(fmt.Errorf("contracts belong to another project root"))
	}
	switch p.ContractAction {
	case "register", "revise":
		if p.Contract == nil {
			return fail(fmt.Errorf("contract is required"))
		}
		contract := *p.Contract
		contract.Root = root
		if p.ContractAction == "register" {
			contract.Revision = 1
		}
		if contract.Revision == 0 || p.ContractAction == "revise" && contract.Revision < 2 {
			return fail(fmt.Errorf("revise requires the next observed revision number"))
		}
		for _, id := range append([]string{contract.OwnerTaskID}, contract.ConsumerTaskIDs...) {
			if _, exists := tasks[id]; !exists || id == "" {
				return fail(fmt.Errorf("contract references unknown task %s", id))
			}
		}
		paths := make([]string, len(contract.Sources))
		for i, source := range contract.Sources {
			paths[i] = source.Path
		}
		contract.Sources, err = engineering.CaptureSources(ctx, root, paths)
		if err != nil {
			return fail(err)
		}
		ok, err := c.permissions.Request(ctx, permission.CreatePermissionRequest{SessionID: sess.ID, ToolCallID: call.ID, ToolName: "workflow", Action: "contract_" + p.ContractAction, Path: root, Description: "Publish expert contract revision", Params: contract})
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		if !ok {
			return tools.NewPermissionDeniedResponse(c.permissions), nil
		}
		latest, err := c.sessions.Get(ctx, sess.ID)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		for _, id := range append([]string{contract.OwnerTaskID}, contract.ConsumerTaskIDs...) {
			found := false
			for _, task := range latest.Todos {
				if task.ID == id && session.TaskFingerprint(task) == session.TaskFingerprint(tasks[id]) {
					found = true
				}
			}
			if !found {
				return fail(fmt.Errorf("contract task changed while awaiting permission"))
			}
		}
		// Publication does not replace accepted references or previous evidence.
		// Consumers must explicitly check the new revision before certification.
		if err := c.engineering.Update(ctx, scope.SessionID, func(st *engineering.State) error {
			if st.ContractRoot != "" && filepath.Clean(st.ContractRoot) != filepath.Clean(root) {
				return fmt.Errorf("contract project changed")
			}
			st.ContractRoot = root
			return nil
		}); err != nil {
			return fail(err)
		}
		ref, err := c.engineering.SaveContract(ctx, contract, contract.Revision-1)
		if err != nil {
			return fail(err)
		}
		return fantasy.NewTextResponse(workflowJSON(map[string]any{"contract": contract, "reference": ref, "consumer_checks_required": true})), nil
	case "check":
		task, exists := tasks[p.TaskID]
		if !exists || p.TaskID == "" || invoke == nil {
			return fail(fmt.Errorf("registered task and verification invoker are required"))
		}
		if role := state.RoleExecutions[task.ID]; role.WorkspaceID != "" {
			workspace, patch, err := c.engineering.WorkspacePatch(ctx, scope.SessionID, role.WorkspaceID)
			if err != nil || patch == "" || workspace.AppliedPatchHash != engineering.Hash(patch) {
				return fail(fmt.Errorf("integrate the current workspace patch before checking parent-tree contracts"))
			}
		}
		contracts, err := c.engineering.ContractsForTask(ctx, root, task.ID)
		if err != nil {
			return fail(err)
		}
		refs, err := c.engineering.ContractRefsForTask(ctx, root, task.ID)
		if err != nil {
			return fail(err)
		}
		if len(contracts) == 0 {
			return fail(fmt.Errorf("task has no registered contracts"))
		}
		checkCtx := engineering.WithScope(ctx, scope.SessionID, task.ID)
		for i, contract := range contracts {
			if err := c.engineering.Check(checkCtx, scope.SessionID, task.ID); err != nil {
				return fail(err)
			}
			before, err := engineering.SourceFingerprint(checkCtx, root, c.engineering.Dir())
			if err != nil {
				return fail(err)
			}
			checks := make([]tools.VerificationStep, len(contract.Checks))
			for j, check := range contract.Checks {
				checks[j] = tools.VerificationStep{Name: check.Name, Tool: check.Tool, Input: json.RawMessage(check.InputJSON)}
			}
			runID := "contract-" + uuid.NewString()
			resp, err := invoke(checkCtx, fantasy.ToolCall{ID: runID, Name: "verify", Input: workflowJSON(tools.VerifyParams{ContractHash: refs[i].Ref.Hash, Action: "run", Checks: checks})})
			if err != nil || resp.IsError || resp.StopTurn {
				return fail(fmt.Errorf("contract %s verification failed: %s", contract.ID, boundedHandoff(resp.Content)))
			}
			latest, err := c.sessions.Get(ctx, sess.ID)
			if err != nil {
				return fantasy.ToolResponse{}, err
			}
			if !slices.ContainsFunc(latest.Todos, func(next session.Todo) bool {
				return next.ID == task.ID && session.TaskFingerprint(next) == session.TaskFingerprint(task)
			}) {
				return fail(fmt.Errorf("contract task changed during verification"))
			}
			proof := engineering.ContractEvidence{Root: root, TaskID: task.ID, TaskFingerprint: session.TaskFingerprint(task), Contract: refs[i], RunID: runID, SourceFingerprint: before}
			if err := c.engineering.SaveContractEvidence(checkCtx, scope.SessionID, contract.ID, proof); err != nil {
				return fail(err)
			}
		}
		if err := c.engineering.ValidateTaskContracts(ctx, scope.SessionID, root, task.ID, session.TaskFingerprint(task), refs); err != nil {
			return fail(err)
		}
		err = c.engineering.Update(ctx, scope.SessionID, func(st *engineering.State) error {
			if st.TaskContractRefs == nil {
				st.TaskContractRefs = map[string][]engineering.Record{}
			}
			st.TaskContractRefs[task.ID] = refs
			st.ContractRoot = root
			if st.Delivery != nil {
				if st.Delivery.TaskFingerprints[task.ID] != session.TaskFingerprint(task) || filepath.Clean(st.Delivery.Root) != filepath.Clean(root) {
					return fmt.Errorf("delivery changed during contract checks")
				}
				if st.Delivery.TaskContractRefs == nil {
					st.Delivery.TaskContractRefs = map[string][]engineering.Record{}
				}
				st.Delivery.TaskContractRefs[task.ID] = refs
			}
			return nil
		})
		if err != nil {
			return fail(err)
		}
		return fantasy.NewTextResponse(workflowJSON(map[string]any{"checked_task": task.ID, "contract_refs": refs, "passed": true})), nil
	default:
		return fail(fmt.Errorf("use contract register, query, revise or check"))
	}
}
