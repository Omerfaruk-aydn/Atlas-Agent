package agent

import (
	"context"
	"encoding/json"
	"path/filepath"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
)

func (c *coordinator) requirementTraceTool() fantasy.AgentTool {
	return fantasy.NewAgentTool("requirement_trace", "Inspect the registered delivery requirements, their tasks, unchanged task specifications and source-bound contract evidence. Reported todo evidence is distinguished from verified contracts. Register or revise requirements through workflow; this tool never marks work complete.", func(ctx context.Context, _ struct{}, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		id := engineering.GetScope(ctx, tools.GetSessionFromContext(ctx)).SessionID
		state, err := c.engineering.Read(ctx, id)
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		if state.Delivery == nil {
			return fantasy.NewTextErrorResponse("register a delivery plan through workflow before tracing requirements"), nil
		}
		plan := state.Delivery
		if filepath.Clean(plan.Root) != filepath.Clean(c.cfg.WorkingDir()) {
			return fantasy.NewTextErrorResponse("delivery belongs to another workspace"), nil
		}
		sess, err := c.sessions.Get(ctx, id)
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		byID := map[string]session.Todo{}
		for _, task := range sess.Todos {
			byID[task.ID] = task
		}
		requirements := make([]map[string]any, 0, len(plan.Requirements))
		for _, requirement := range plan.Requirements {
			items := make([]map[string]any, 0, len(requirement.TaskIDs))
			for _, taskID := range requirement.TaskIDs {
				task, exists := byID[taskID]
				unchanged := exists && session.TaskFingerprint(task) == plan.TaskFingerprints[taskID]
				contracts, err := c.engineering.ContractsForTask(ctx, plan.Root, taskID)
				if err != nil {
					return fantasy.NewTextErrorResponse(err.Error()), nil
				}
				verified := false
				gap := "no registered contract checks"
				if len(contracts) > 0 && unchanged {
					if err := c.engineering.ValidateTaskContracts(ctx, id, plan.Root, taskID, plan.TaskFingerprints[taskID], plan.TaskContractRefs[taskID]); err == nil {
						verified = true
						gap = ""
					} else {
						gap = err.Error()
					}
				}
				items = append(items, map[string]any{"task_id": taskID, "exists": exists, "specification_unchanged": unchanged, "reported_status": task.Status, "reported_verification": task.Verification, "reported_evidence": task.Evidence, "contracts_verified": verified, "verification_gap": gap})
			}
			requirements = append(requirements, map[string]any{"id": requirement.ID, "description": requirement.Description, "tasks": items})
		}
		data, err := json.Marshal(map[string]any{"requirements": requirements, "stage": plan.CurrentStage, "completion_gate": "workflow delivery gates remain authoritative"})
		return fantasy.NewTextResponse(string(data)), err
	})
}
