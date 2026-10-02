package agent

import (
	"context"
	"fmt"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/subagents"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workflows"
)

func (c *coordinator) recipeRoles() []string {
	var roles []string
	for _, role := range subagents.Discover(c.cfg.Config().Options.SubagentsPaths) {
		roles = append(roles, role.Name)
	}
	return roles
}

func (c *coordinator) currentRecipe(ctx context.Context, sessionID string) error {
	if c.engineering == nil {
		return nil
	}
	state, err := c.engineering.Read(ctx, sessionID)
	if err != nil || state.Recipe == nil {
		return err
	}
	return workflows.Current(ctx, c.engineering, sessionID, workflows.Paths(c.cfg.WorkingDir(), c.cfg.Config().Options.WorkflowPaths), c.recipeRoles())
}

func recipeSteps(checks []workflows.Check) []tools.VerificationStep {
	var steps []tools.VerificationStep
	for _, check := range checks {
		steps = append(steps, tools.VerificationStep{Name: check.Name, Tool: tools.BashToolName, Input: []byte(workflowJSON(tools.BashParams{Description: check.Name, Argv: check.Argv, WorkingDir: check.Directory}))})
	}
	return steps
}

func (c *coordinator) recipeChecks(ctx context.Context, sessionID string, taskIDs []string) ([]tools.VerificationStep, error) {
	st, err := c.engineering.Read(ctx, sessionID)
	if err != nil || st.Recipe == nil {
		return nil, err
	}
	if err := c.currentRecipe(ctx, sessionID); err != nil {
		return nil, err
	}
	compiled, err := workflows.ReadCompiled(ctx, c.engineering, *st.Recipe)
	if err != nil {
		return nil, err
	}
	var checks []tools.VerificationStep
	for _, taskID := range taskIDs {
		for _, step := range recipeSteps(compiled.Checks[taskID]) {
			step.Name = taskID + "/" + step.Name
			checks = append(checks, step)
		}
	}
	if len(checks) > 12 {
		return nil, fmt.Errorf("recipe stage exceeds 12 checks")
	}
	return checks, nil
}

func (c *coordinator) recipeWorkflow(ctx context.Context, p WorkflowParams, call fantasy.ToolCall, invoke tools.ToolInvoker) (fantasy.ToolResponse, error) {
	id := tools.GetSessionFromContext(ctx)
	scope := engineering.GetScope(ctx, id)
	if scope.TaskID != "" {
		return fantasy.NewTextErrorResponse("recipe admission belongs to the session coordinator"), nil
	}
	fail := func(err error) (fantasy.ToolResponse, error) { return fantasy.NewTextErrorResponse(err.Error()), nil }
	if p.RecipeAction == "checks" {
		st, err := c.engineering.Read(ctx, id)
		if err != nil || st.Recipe == nil {
			return fail(fmt.Errorf("no admitted recipe"))
		}
		if err := c.currentRecipe(ctx, id); err != nil {
			return fail(err)
		}
		compiled, err := workflows.ReadCompiled(ctx, c.engineering, *st.Recipe)
		if err != nil {
			return fail(err)
		}
		return fantasy.NewTextResponse(workflowJSON(recipeSteps(compiled.Checks[p.TaskID]))), nil
	}
	recipes, err := workflows.Load(ctx, workflows.Paths(c.cfg.WorkingDir(), c.cfg.Config().Options.WorkflowPaths))
	if err != nil {
		return fail(err)
	}
	if p.RecipeAction == "list" {
		return fantasy.NewTextResponse(workflowJSON(recipes)), nil
	}
	for _, recipe := range recipes {
		if recipe.ID != p.RecipeID {
			continue
		}
		if err := workflows.Validate(ctx, recipe, c.recipeRoles()); err != nil {
			return fail(err)
		}
		if p.RecipeAction == "validate" {
			return fantasy.NewTextResponse("Recipe schema, role and dependency validation passed; no commands executed."), nil
		}
		compiled, err := workflows.Compile(ctx, recipe, p.RecipeParams, c.recipeRoles())
		if err != nil {
			return fail(err)
		}
		compiled.Plan.Root = c.cfg.WorkingDir()
		if p.RecipeAction == "plan" {
			return fantasy.NewTextResponse(workflowJSON(compiled)), nil
		}
		if p.RecipeAction != "run" {
			return fail(fmt.Errorf("use list, validate, plan, run or checks"))
		}
		if err := c.engineering.Check(ctx, id, ""); err != nil {
			return fail(err)
		}
		ok, err := c.permissions.Request(ctx, permission.CreatePermissionRequest{SessionID: id, ToolCallID: call.ID, ToolName: "workflow", Action: "recipe", Path: c.cfg.WorkingDir(), Description: "Admit workflow recipe " + recipe.ID, Params: compiled})
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		if !ok {
			return tools.NewPermissionDeniedResponse(c.permissions), nil
		}
		// Recompile after permission, since a project recipe can change while
		// the user is reviewing the proposed graph.
		fresh, err := workflows.Load(ctx, workflows.Paths(c.cfg.WorkingDir(), c.cfg.Config().Options.WorkflowPaths))
		if err != nil {
			return fail(err)
		}
		found := false
		for _, current := range fresh {
			if current.ID == recipe.ID {
				data := workflowJSON(current)
				found = engineering.Hash(data) == compiled.RecipeHash
			}
		}
		if !found {
			return fail(fmt.Errorf("recipe changed during permission review"))
		}
		run, err := workflows.Install(ctx, c.engineering, c.sessions, id, c.cfg.WorkingDir(), compiled)
		if err != nil {
			return fail(err)
		}
		return fantasy.NewTextResponse(workflowJSON(run)), nil
	}
	return fail(fmt.Errorf("unknown recipe ID"))
}
