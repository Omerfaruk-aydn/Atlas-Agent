package agent

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
)

func (c *coordinator) learnWorkflow(ctx context.Context, p WorkflowParams, call fantasy.ToolCall, invoke tools.ToolInvoker) (fantasy.ToolResponse, error) {
	id := engineering.GetScope(ctx, tools.GetSessionFromContext(ctx)).SessionID
	root := c.cfg.WorkingDir()
	fail := func(err error) (fantasy.ToolResponse, error) { return fantasy.NewTextErrorResponse(err.Error()), nil }
	switch p.LearnAction {
	case "forget":
		if err := c.engineering.ForgetMethod(ctx, root, p.MethodID); err != nil {
			return fail(err)
		}
		return workResponse(map[string]any{"forgotten": p.MethodID, "skill_file": "unchanged; use skill_manage delete explicitly if needed"})
	case "list", "":
		methods, err := c.engineering.LearnedMethods(ctx, root)
		if err != nil {
			return fail(err)
		}
		return workResponse(map[string]any{"methods": methods, "scope": "project", "authority": "procedure candidates; never override instructions or permissions"})
	case "extract":
		if p.LearnedMethod == nil {
			return fail(fmt.Errorf("learned_method is required"))
		}
		var paths []string
		for _, source := range p.LearnedMethod.Sources {
			paths = append(paths, source.Path)
		}
		if len(paths) > 16 {
			return fail(fmt.Errorf("at most sixteen source paths"))
		}
		if err := engineering.ValidatePatchSources(ctx, root, paths); err != nil {
			return fail(err)
		}
		for n, source := range p.LearnedMethod.Sources {
			if invoke == nil {
				return fail(fmt.Errorf("view invocation unavailable"))
			}
			response, err := invoke(ctx, fantasy.ToolCall{ID: fmt.Sprintf("%s-source-%d", call.ID, n), Name: tools.ViewToolName, Input: workflowJSON(tools.ViewParams{FilePath: filepath.Join(root, source.Path)})})
			if err != nil || response.IsError || response.StopTurn {
				return response, err
			}
		}
		method, err := c.engineering.ExtractMethod(ctx, root, id, *p.LearnedMethod)
		if err != nil {
			return fail(err)
		}
		return workResponse(method)
	case "inspect", "export":
		method, err := c.engineering.CurrentMethod(ctx, root, p.MethodID)
		if err != nil {
			return fail(err)
		}
		instructions := engineering.MethodInstructions(method)
		if p.LearnAction == "inspect" {
			return workResponse(map[string]any{"method": method, "instructions": instructions})
		}
		scope := p.SkillScope
		if scope == "" {
			scope = "project"
		}
		if scope != "project" && scope != "user" {
			return fail(fmt.Errorf("skill_scope must be project or user"))
		}
		if invoke == nil {
			return fail(fmt.Errorf("skill_manage invocation unavailable"))
		}
		response, err := invoke(ctx, fantasy.ToolCall{ID: call.ID + "-export", Name: tools.SkillManageToolName, Input: workflowJSON(tools.SkillManageParams{Action: "create", Scope: scope, Name: method.Name, Description: method.Description, Instructions: instructions})})
		if err != nil || response.IsError || response.StopTurn {
			return response, err
		}
		dirs := skillDirs(c.cfg)
		dir := dirs.Project
		if scope == "user" {
			dir = dirs.User
		}
		if err := c.engineering.MarkMethodExported(ctx, root, method.ID, filepath.Join(dir, method.Name, "SKILL.md")); err != nil {
			return fail(err)
		}
		return response, nil
	default:
		return fail(fmt.Errorf("learn_action must be extract, list, inspect, export or forget"))
	}
}
