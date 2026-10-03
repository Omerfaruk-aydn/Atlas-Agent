package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/codegraph"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/environment"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/subagents"
)

type WorkflowParams struct {
	RecipeID          string                        `json:"recipe_id,omitempty" description:"Exact versioned recipe ID; never overwrites existing work."`
	RecipeAction      string                        `json:"recipe_action,omitempty" description:"list, validate, plan, run or checks. Plan compiles without mutations; run admits tasks without automatically dispatching them."`
	RecipeParams      map[string]json.RawMessage    `json:"recipe_params,omitempty" description:"Typed literal JSON parameter values defined by the recipe."`
	FindingIDs        []string                      `json:"finding_ids,omitempty" description:"At most 16 persisted finding identities for remediation or verification."`
	FindingAction     string                        `json:"finding_action,omitempty" description:"status, fixed, verify or reinspect. Waivers require direct user controls and cannot be submitted by an agent."`
	Contract          *engineering.ContractRevision `json:"contract,omitempty" description:"Versioned expert contract. Runtime captures root and source hashes; revisions advance with strict compare-and-swap."`
	ContractAction    string                        `json:"contract_action,omitempty" description:"register, query, revise or check. Check executes the exact registered checks through existing permitted tools."`
	Repair            *engineering.RepairCase       `json:"repair,omitempty" description:"Reported repair state is reference data; runtime owns all attempt counts and observed identities."`
	RepairAction      string                        `json:"repair_action,omitempty" description:"run (default) or status. Run requires an observed failed operation and never resets attempt history."`
	RepairHypothesis  string                        `json:"repair_hypothesis,omitempty" description:"Tentative bounded diagnosis for a repair attempt; actual diagnostics and verification are required."`
	CheckpointID      string                        `json:"checkpoint_id,omitempty" description:"Persisted checkpoint identity for resume-plan."`
	ResumePlan        *engineering.ResumePlan       `json:"resume_plan,omitempty" description:"Exact observed resume plan; apply rejects changed tasks, source, state and unresolved operations. Never replays commands."`
	EnvironmentAction string                        `json:"environment_action,omitempty" description:"inspect, plan, apply or verify; inspect never executes commands."`
	EnvironmentPlan   *environment.EnvironmentPlan  `json:"environment_plan,omitempty" description:"Exact source-verified plan returned by environment inspect; runtime rejects modified commands or stale manifests."`
	Action            string                        `json:"action" description:"status, recipe, environment, checkpoint, resume-plan, resume, repair, contract, finding, remediate, prepare, profile, plan, trace, advance, design, critique, artifact, knowledge, decision, lesson, ready, dispatch, review, budget, recover. Plan registers requirements and ordered stages; advance runs real stage checks. Dispatch does not complete tasks."`
	Profile           string                        `json:"profile,omitempty" description:"small_fix, feature, migration, ui or research. This changes workflow guidance, never model or permissions."`
	Plan              *engineering.DeliveryPlan     `json:"plan,omitempty" description:"Requirements mapped to existing task IDs and ordered stages. Runtime overwrites root, fingerprints, stage results and current index."`
	Design            *engineering.DesignBrief      `json:"design,omitempty" description:"Design audience, primary flow, visual direction, states, criteria, task IDs and narrow/wide widths. Critiques are server-managed."`
	Critique          *engineering.UICritique       `json:"critique,omitempty" description:"Reported visual/interaction findings for an actual persisted artifact. All design criteria require evidence."`
	Knowledge         *engineering.KnowledgeRecord  `json:"knowledge,omitempty" description:"Source-grounded decision or verified failure lesson. Sources use path only; runtime captures fingerprints. Never include secrets."`
	ArtifactPath      string                        `json:"artifact_path,omitempty" description:"For artifact: a project-relative TUI transcript file, copied to durable evidence. Web evidence comes from ui_verify."`
	Width             int                           `json:"width,omitempty" description:"Observed TUI transcript width in columns, 20-1000. Reported metadata, not an automated rendering assertion."`
	Height            int                           `json:"height,omitempty" description:"Observed TUI transcript height in rows, 5-300."`
	Checks            []tools.VerificationStep      `json:"checks,omitempty" description:"Explicit verification checks for review; omit to discover checks from the project stack."`
	Limit             int                           `json:"limit,omitempty" description:"Ready wave limit, 1-16; bounded by configured concurrency."`
	Isolate           bool                          `json:"isolate,omitempty" description:"Create a managed Git worktree per dispatched task. Parent uncommitted changes are not copied."`
	TaskID            string                        `json:"task_id,omitempty" description:"Task account for a budget update; empty updates the session budget."`
	Limits            *engineering.Limits           `json:"limits,omitempty" description:"User-approved token, tool-call, active-time and USD limits. Zero leaves a dimension unlimited. Budget mutation requires permission."`
	OperationID       string                        `json:"operation_id,omitempty" description:"Interrupted operation to resolve after inspecting its actual effects."`
	Resolution        string                        `json:"resolution,omitempty" description:"For recover: completed, failed or abandoned. Never reruns the operation."`
	Evidence          string                        `json:"evidence,omitempty" description:"Actual inspection evidence required for manual recovery."`
}

func (c *coordinator) scopedCoordinator(root string) *coordinator {
	return &coordinator{
		cfg: c.cfg.Scoped(root), sessions: c.sessions, messages: c.messages,
		permissions: c.permissions, questions: c.questions, history: c.history, filetracker: c.filetracker,
		notify: c.notify, runComplete: c.runComplete, interactive: c.interactive, engineering: c.engineering,
		allSkills: c.allSkills, activeSkills: c.activeSkills, skillTracker: c.skillTracker, memory: c.memory,
		facts: c.facts, credentials: c.credentials, teams: c.teams,
	}
}

func (c *coordinator) workflowTool(invoke tools.ToolInvoker) fantasy.AgentTool {
	return fantasy.NewAgentTool("workflow", "Manage dependency-aware execution using the persisted todos graph. Give tasks stable IDs, dependencies, named agents, explicit ownership and criteria. Dispatch a ready wave and inspect structured handoffs. Integrate isolated patches, then review with task_id to run independent test/review specialists and machine verification. Update todos only after the quality gate passes; then dispatch the next wave. Status includes role executions, budgets and recovery records. Normal permissions and budgets apply.", func(ctx context.Context, p WorkflowParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		id := tools.GetSessionFromContext(ctx)
		scope := engineering.GetScope(ctx, id)
		sess, err := c.sessions.Get(ctx, id)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		respond := func(v any) (fantasy.ToolResponse, error) {
			data, err := json.Marshal(v)
			return fantasy.NewTextResponse(string(data)), err
		}
		switch p.Action {
		case "recipe":
			return c.recipeWorkflow(ctx, p, call, invoke)
		case "remediate", "finding":
			return c.remediationWorkflow(ctx, p, call, invoke)
		case "contract":
			return c.contractWorkflow(ctx, p, call, invoke)
		case "repair":
			return c.repairWorkflow(ctx, p, call, invoke)
		case "checkpoint", "resume-plan", "resume":
			return c.resumeWorkflow(ctx, p, call)
		case "environment":
			return c.environmentWorkflow(ctx, p, call, invoke)
		case "prepare", "profile", "plan", "trace", "advance", "design", "critique", "artifact", "knowledge", "decision", "lesson":
			return c.deliveryWorkflow(ctx, p, call, sess, invoke)
		case "status":
			st, err := c.engineering.Read(ctx, scope.SessionID)
			if err != nil {
				return fantasy.ToolResponse{}, err
			}
			_, controls, err := c.engineering.ReadControlBoard(ctx, scope.SessionID)
			if err != nil {
				return fantasy.ToolResponse{}, err
			}
			return respond(struct {
				engineering.State
				Controls engineering.ControlBoard `json:"user_controls"`
				Runners  []engineering.LiveRunner `json:"live_runners"`
			}{State: st, Controls: controls, Runners: c.engineering.LiveRunners(scope.SessionID)})
		case "ready":
			wave, err := c.deliveryReady(ctx, scope.SessionID, sess.Todos, p.Limit)
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			return respond(wave)
		case "budget", "recover":
			ok, err := c.permissions.Request(ctx, permission.CreatePermissionRequest{SessionID: id, ToolCallID: call.ID, ToolName: "workflow", Action: p.Action, Path: c.cfg.WorkingDir(), Description: "Update engineering " + p.Action, Params: p})
			if err != nil {
				return fantasy.ToolResponse{}, err
			}
			if !ok {
				return tools.NewPermissionDeniedResponse(c.permissions), nil
			}
			if p.Action == "budget" {
				if p.Limits == nil {
					return fantasy.NewTextErrorResponse("limits are required"), nil
				}
				err = c.engineering.SetBudget(ctx, scope.SessionID, p.TaskID, *p.Limits)
			} else {
				err = c.engineering.Resolve(ctx, scope.SessionID, p.OperationID, p.Resolution, p.Evidence)
			}
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			return respond(map[string]bool{"updated": true})
		case "dispatch":
			return c.dispatchWave(ctx, p, call, sess, invoke)
		case "review":
			return c.reviewTask(ctx, p, call, sess, invoke)
		default:
			return fantasy.NewTextErrorResponse("unknown workflow action"), nil
		}
	})
}

func (c *coordinator) dispatchWave(ctx context.Context, p WorkflowParams, call fantasy.ToolCall, sess session.Session, invoke tools.ToolInvoker) (fantasy.ToolResponse, error) {
	state, pauseErr := c.engineering.Read(ctx, engineering.GetScope(ctx, sess.ID).SessionID)
	if pauseErr != nil {
		return fantasy.NewTextErrorResponse(pauseErr.Error()), nil
	}
	if state.Paused {
		return fantasy.NewTextErrorResponse("Workflow dispatch is paused; resume through user controls"), nil
	}
	limit := p.Limit
	if limit <= 0 {
		limit = 4
	}
	if limit > 16 {
		return fantasy.NewTextErrorResponse("wave limit cannot exceed 16"), nil
	}
	if configured := c.cfg.Config().Options.MaxConcurrentSubAgents; configured > 0 {
		limit = min(limit, configured)
	}
	wave, err := c.deliveryReady(ctx, engineering.GetScope(ctx, sess.ID).SessionID, sess.Todos, limit)
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	if len(wave) == 0 {
		return fantasy.NewTextResponse("No ready tasks. Inspect dependencies and verification; in-progress tasks are not automatically replayed."), nil
	}
	discovered := subagents.Discover(c.cfg.Config().Options.SubagentsPaths)
	for _, task := range wave {
		if _, ok := subagents.Find(discovered, task.Agent); !ok || len(task.AcceptanceCriteria) == 0 {
			return fantasy.NewTextErrorResponse("each dispatched task requires a configured named agent and acceptance criteria"), nil
		}
	}
	// Persist required quality gates before any specialist can claim completion.
	if err := c.engineering.Update(ctx, engineering.GetScope(ctx, sess.ID).SessionID, func(st *engineering.State) error {
		for _, task := range wave {
			sub, _ := subagents.Find(discovered, task.Agent)
			st.RoleExecutions[task.ID] = engineering.RoleExecution{TaskID: task.ID, Agent: task.Agent, Root: c.cfg.WorkingDir(), TaskFingerprint: session.TaskFingerprint(task), RequireReview: sub.Contract != nil && sub.Contract.IndependentReview}
		}
		return nil
	}); err != nil {
		return fantasy.ToolResponse{}, err
	}
	results := make([]map[string]any, len(wave))
	pausedBeforeAdmission := make([]bool, len(wave))
	workspaces := make([]string, len(wave))
	contextRoots := make([]string, len(wave))
	for i := range contextRoots {
		contextRoots[i] = c.cfg.WorkingDir()
	}
	if p.Isolate {
		for i, t := range wave {
			resp, err := invoke(ctx, fantasy.ToolCall{ID: fmt.Sprintf("%s-workspace-%d", call.ID, i), Name: "worktree", Input: workflowJSON(tools.WorktreeParams{Action: "create", TaskID: t.ID})})
			if err != nil || resp.IsError || resp.StopTurn {
				return resp, err
			}
			var w engineering.Workspace
			if err := json.Unmarshal([]byte(resp.Content), &w); err != nil {
				return fantasy.ToolResponse{}, err
			}
			workspaces[i] = w.ID
			contextRoots[i] = w.Path
		}
	}
	contextTexts := make([]string, len(wave))
	for i, task := range wave {
		packet, err := prepareTaskContext(ctx, c.cfg.Scoped(contextRoots[i]), c.engineering, task, codegraph.CodeGraph{})
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		contextTexts[i], err = renderTaskContext(ctx, packet)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
	}
	if err := c.engineering.Update(ctx, engineering.GetScope(ctx, sess.ID).SessionID, func(st *engineering.State) error {
		if st.TaskContractRefs == nil {
			st.TaskContractRefs = map[string][]engineering.Record{}
		}
		for _, task := range wave {
			refs, err := c.engineering.ContractRefsForTask(ctx, c.cfg.WorkingDir(), task.ID)
			if err != nil {
				return err
			}
			st.TaskContractRefs[task.ID] = refs
		}
		return nil
	}); err != nil {
		return fantasy.ToolResponse{}, err
	}
	for i := range sess.Todos {
		for _, task := range wave {
			if sess.Todos[i].ID == task.ID {
				sess.Todos[i].Status = session.TodoStatusInProgress
			}
		}
	}
	if _, err := c.sessions.Save(ctx, sess); err != nil {
		return fantasy.ToolResponse{}, err
	}
	var wg sync.WaitGroup
	for i, t := range wave {
		wg.Add(1)
		go func(i int, t session.Todo) {
			defer wg.Done()
			input := AgentParams{AgentName: t.Agent, WorkspaceID: workspaces[i], Prompt: fmt.Sprintf("Task %s: %s\nOwned paths: %v\nAcceptance criteria: %v\nDependencies: %v\nDo only this assignment. Do not mark the parent task completed; its coordinator must integrate and verify.\n%s", t.ID, t.Content, t.OwnedPaths, t.AcceptanceCriteria, t.DependsOn, subagents.HandoffInstruction)}
			input.Prompt += contextTexts[i]
			childCtx := engineering.WithScope(ctx, engineering.GetScope(ctx, sess.ID).SessionID, t.ID)
			childCtx = engineering.WithOwnership(childCtx, c.cfg.WorkingDir(), t.OwnedPaths)
			resp, err := invoke(childCtx, fantasy.ToolCall{ID: fmt.Sprintf("%s-task-%d", call.ID, i), Name: AgentToolName, Input: workflowJSON(input)})
			pausedBeforeAdmission[i] = resp.IsError && resp.StopTurn && resp.Content == "workflow dispatch is paused"
			output := resp.Content
			if err != nil {
				output = err.Error()
			}
			handoff, handoffErr := subagents.ParseHandoff(output, t.ID)
			if handoffErr == nil {
				for _, file := range handoff.ChangedFiles {
					if err := checkToolOwnership(engineering.GetScope(childCtx, sess.ID), fantasy.ToolCall{Name: "write", Input: workflowJSON(tools.WriteParams{FilePath: file})}); err != nil {
						handoffErr = err
						break
					}
				}
			}
			var failure string
			if err != nil || resp.IsError {
				failure = boundedHandoff(output)
			} else if handoffErr != nil {
				failure = handoffErr.Error()
			}
			persistErr := c.engineering.Update(ctx, engineering.GetScope(ctx, sess.ID).SessionID, func(st *engineering.State) error {
				run := st.RoleExecutions[t.ID]
				for _, op := range st.Operations {
					if op.CallID == fmt.Sprintf("%s-task-%d", call.ID, i) && op.TaskID == t.ID && op.Tool == AgentToolName {
						run.ExecutionID = op.ID
					}
				}
				run.WorkspaceID, run.Error = workspaces[i], failure
				if handoffErr == nil {
					run.Handoff = &handoff
				}
				st.RoleExecutions[t.ID] = run
				return nil
			})
			if persistErr != nil {
				failure = persistErr.Error()
			}
			results[i] = map[string]any{"task_id": t.ID, "workspace_id": workspaces[i], "handoff": handoff, "handoff_error": failure, "raw_preview": boundedHandoff(output), "execution_error": failure != "", "verification": "pending"}
		}(i, t)
	}
	wg.Wait()
	// A pause can reject admission after the wave has been prepared. Restore
	// only this exact still-in-progress assignment with no runtime operation.
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	for retry := range 3 {
		latest, err := c.sessions.Get(cleanup, sess.ID)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		before := session.TodosFingerprint(latest.Todos)
		st, err := c.engineering.Read(cleanup, engineering.GetScope(ctx, sess.ID).SessionID)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		changed := false
		for i, task := range wave {
			if !pausedBeforeAdmission[i] {
				continue
			}
			admitted := false
			for _, op := range st.Operations {
				admitted = admitted || op.CallID == fmt.Sprintf("%s-task-%d", call.ID, i)
			}
			if admitted {
				continue
			}
			for j := range latest.Todos {
				current := &latest.Todos[j]
				if current.ID == task.ID && current.Status == session.TodoStatusInProgress && session.TaskFingerprint(*current) == session.TaskFingerprint(task) {
					current.Status = session.TodoStatusPending
					changed = true
				}
			}
		}
		if !changed {
			break
		}
		if _, err := c.sessions.CompareAndSwapTodos(cleanup, sess.ID, before, latest.Todos); err == nil {
			break
		} else if cleanup.Err() != nil {
			return fantasy.ToolResponse{}, err
		}
		if retry == 2 {
			return fantasy.NewTextErrorResponse("Paused dispatch requires reconciliation: task graph changed during all retries"), nil
		}
	}
	data, _ := json.Marshal(map[string]any{"tasks": results, "next": "Inspect handoffs, integrate isolated patches, run verify, then update todos with acceptance evidence. No task was marked complete."})
	return fantasy.NewTextResponse(string(data)), nil
}

func workflowJSON(v any) string { data, _ := json.Marshal(v); return string(data) }
func boundedHandoff(s string) string {
	if len(s) > 4096 {
		return strings.ToValidUTF8(s[:4096], "") + "\n[truncated; inspect the child session]"
	}
	return s
}
