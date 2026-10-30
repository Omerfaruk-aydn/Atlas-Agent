package agent

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/harnessmetrics"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
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
		// Each method is shown with how far it has been measured.
		type listed struct {
			engineering.LearnedMethod
			Standing string `json:"standing"`
			Basis    string `json:"basis"`
		}
		annotated := make([]listed, 0, len(methods))
		for _, m := range methods {
			standing, basis := engineering.MethodStanding(m)
			annotated = append(annotated, listed{LearnedMethod: m, Standing: standing, Basis: basis})
		}
		return workResponse(map[string]any{"methods": annotated, "scope": "project", "authority": "procedure candidates; never override instructions or permissions"})
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
		if standing, _ := engineering.MethodStanding(method); standing == engineering.StandingUnreliable {
			return fail(fmt.Errorf("an unreliable method cannot be exported; nothing was written"))
		}
		scope := p.SkillScope
		if scope == "" {
			scope = "project"
		}
		if scope != "project" && scope != "user" {
			return fail(fmt.Errorf("skill_scope must be project or user"))
		}
		// Writing a skill is how a method becomes a standing practice. A method
		// that has not succeeded on other tasks is exported only as a
		// candidate, and only with a grant from the permission service. The
		// accept_unmeasured argument is the model saying what it wants; it is
		// never the user's consent.
		if standing, basis := engineering.MethodStanding(method); standing != engineering.StandingEffective {
			if !p.AcceptUnmeasured {
				return fail(fmt.Errorf("this method is %s (%s), so it is not exported as an established practice. Record outcomes of other tasks that used it (learn_action outcome), or ask to export it as an unmeasured candidate with accept_unmeasured: true, which needs the user's permission", standing, basis))
			}
			if denied, ok := c.requestCandidateExport(ctx, call, root, method, standing, basis); !ok {
				return denied, nil
			}
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
	case "outcome":
		if p.MethodOutcome != "success" && p.MethodOutcome != "failure" {
			return fail(fmt.Errorf("method_outcome must be success or failure"))
		}
		success := p.MethodOutcome == "success"
		var fingerprint []string
		if success {
			completion, err := c.engineering.CompletionStatus(ctx, id)
			if err != nil {
				return fail(err)
			}
			if completion != nil {
				fingerprint = []string{completion.TasksFingerprint}
			}
		}
		method, err := c.engineering.RecordMethodOutcome(ctx, root, p.MethodID, id, success, fingerprint...)
		if err != nil {
			return fail(err)
		}
		kind := harnessmetrics.Kind("rule_reuse_failed")
		if success {
			kind = harnessmetrics.Kind("rule_reuse_succeeded")
		}
		harnessmetrics.New(harnessDataDir(c.cfg, root)).Record(kind, id, map[string]string{"source": "learned_method", "method_id": method.ID})
		return workResponse(map[string]any{"method": method, "outcome": p.MethodOutcome})
	default:
		return fail(fmt.Errorf("learn_action must be extract, list, inspect, export, outcome or forget"))
	}
}

// LearnExportUnmeasuredToolName is the permission name for writing down a
// method that has not succeeded on other tasks. It is its own name so that
// an allowlist entry for skill_manage does not cover it.
const LearnExportUnmeasuredToolName = "learn_export_unmeasured"

// requestCandidateExport asks the permission service, which is the only
// source of the user's consent here. Without a service it fails closed. The
// second result is false when the export must not go ahead, and the response
// then says why. Bypass mode is the user's own choice to skip every prompt, so
// it grants this as it grants any tool; plan mode denies it.
func (c *coordinator) requestCandidateExport(ctx context.Context, call fantasy.ToolCall, root string, method engineering.LearnedMethod, standing, basis string) (fantasy.ToolResponse, bool) {
	if c.permissions == nil {
		return fantasy.NewTextErrorResponse("exporting an unmeasured method needs the user's permission and no permission service is available; nothing was written"), false
	}
	granted, err := c.permissions.Request(ctx, permission.CreatePermissionRequest{
		SessionID:   tools.GetSessionFromContext(ctx),
		Path:        root,
		ToolCallID:  call.ID,
		ToolName:    LearnExportUnmeasuredToolName,
		Action:      "export",
		Description: fmt.Sprintf("Write down method %q as a candidate skill. It is %s (%s) and has not been shown to work beyond the task it came from.", method.Name, standing, basis),
		Params:      map[string]string{"method_id": method.ID, "name": method.Name, "standing": standing, "basis": basis},
	})
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), false
	}
	if !granted {
		return tools.NewPermissionDeniedResponse(c.permissions), false
	}
	return fantasy.ToolResponse{}, true
}
