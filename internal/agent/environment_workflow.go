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
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/environment"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/google/uuid"
)

type environmentAttempt struct {
	AttemptID          string                        `json:"attempt_id"`
	InspectionRevision uint64                        `json:"inspection_revision"`
	Status             string                        `json:"status"`
	CommandsPassed     int                           `json:"commands_passed"`
	Report             environment.EnvironmentReport `json:"report"`
}

func (c *coordinator) environmentWorkflow(ctx context.Context, p WorkflowParams, call fantasy.ToolCall, invoke tools.ToolInvoker) (fantasy.ToolResponse, error) {
	root := c.cfg.WorkingDir()
	if policy := tools.ExecutionPolicy(c.cfg.Config()); policy.Mode == "container-required" {
		ctx = environment.WithExecutionOS(ctx, "linux")
	}
	namespace, err := codegraph.Namespace(root)
	if err != nil {
		return fantasy.ToolResponse{}, err
	}
	namespace = "environment-" + namespace
	respond := func(value any) (fantasy.ToolResponse, error) {
		data, err := json.Marshal(value)
		return fantasy.NewTextResponse(string(data)), err
	}
	fail := func(err error) (fantasy.ToolResponse, error) { return fantasy.NewTextErrorResponse(err.Error()), nil }
	action := p.EnvironmentAction
	if action == "" {
		action = "inspect"
	}
	if action == "inspect" || action == "plan" {
		plan, err := environment.Inspect(ctx, root)
		if err != nil {
			return fail(err)
		}
		hash, err := environment.PlanHash(plan)
		if err != nil {
			return fail(err)
		}
		previous, _, readErr := c.engineering.ReadRecord(ctx, namespace, "plan-"+hash)
		if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
			return fail(readErr)
		}
		data, err := json.Marshal(plan)
		if err != nil {
			return fail(err)
		}
		record, err := c.engineering.PutRecord(ctx, namespace, "plan-"+hash, previous.Revision, data)
		if err != nil {
			return fail(err)
		}
		return respond(map[string]any{"plan": plan, "record": record, "plan_hash": hash})
	}
	if action != "apply" && action != "verify" {
		return fail(fmt.Errorf("unsupported environment action"))
	}
	if p.EnvironmentPlan == nil || invoke == nil {
		return fail(fmt.Errorf("inspect a plan before apply or verify"))
	}
	plan := *p.EnvironmentPlan
	project, _ := codegraph.Namespace(plan.Root)
	current, _ := codegraph.Namespace(root)
	if project != current {
		return fail(fmt.Errorf("environment plan belongs to another project"))
	}
	if err := environment.ValidatePlan(ctx, plan); err != nil {
		return fail(err)
	}
	if len(plan.Conflicts) > 0 {
		return fail(fmt.Errorf("environment plan has unresolved conflicts"))
	}
	hash, err := environment.PlanHash(plan)
	if err != nil {
		return fail(err)
	}
	inspection, data, err := c.engineering.ReadRecord(ctx, namespace, "plan-"+hash)
	if err != nil {
		return fail(fmt.Errorf("inspect this environment plan through workflow first"))
	}
	var inspected environment.EnvironmentPlan
	if err := json.Unmarshal(data, &inspected); err != nil {
		return fail(err)
	}
	inspectedHash, err := environment.PlanHash(inspected)
	if err != nil || inspectedHash != hash {
		return fail(fmt.Errorf("stored environment plan identity mismatch"))
	}
	ref, data, readErr := c.engineering.ReadRecord(ctx, namespace, "attempt-"+hash)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return fail(readErr)
	}
	attempt := environmentAttempt{InspectionRevision: inspection.Revision, Report: environment.EnvironmentReport{PlanHash: hash, ObservedVersions: map[string]string{}, Gaps: []string{}}}
	if readErr == nil {
		if err := json.Unmarshal(data, &attempt); err != nil {
			return fail(err)
		}
	}
	if action == "apply" && (attempt.Status == "cancelled" || attempt.Status == "applying" || attempt.Status == "ambiguous") && attempt.InspectionRevision >= inspection.Revision {
		return fail(fmt.Errorf("interrupted preparation requires fresh environment inspection"))
	}
	scope := engineering.GetScope(ctx, tools.GetSessionFromContext(ctx))
	save := func(saveCtx context.Context) error {
		data, err := json.Marshal(attempt)
		if err != nil {
			return err
		}
		updated, err := c.engineering.PutRecord(saveCtx, namespace, "attempt-"+hash, ref.Revision, data)
		if err == nil {
			ref = updated
		}
		return err
	}
	if action == "apply" {
		allowed, err := c.permissions.Request(ctx, permission.CreatePermissionRequest{SessionID: tools.GetSessionFromContext(ctx), ToolCallID: call.ID, ToolName: "workflow", Action: "environment_apply", Path: root, Description: "Prepare project-local dependencies from the verified environment plan", Params: plan})
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		if !allowed {
			return tools.NewPermissionDeniedResponse(c.permissions), nil
		}
		if err := environment.ValidatePlan(ctx, plan); err != nil {
			return fail(err)
		}
		attempt = environmentAttempt{AttemptID: uuid.NewString(), InspectionRevision: inspection.Revision, Status: "applying", Report: environment.EnvironmentReport{PlanHash: hash, ObservedVersions: map[string]string{}, Gaps: []string{}}}
		if err := save(ctx); err != nil {
			return fail(err)
		}
		for index, command := range plan.Commands {
			if err := c.engineering.Check(ctx, scope.SessionID, scope.TaskID); err != nil {
				return fail(err)
			}
			if err := environment.ValidatePlan(ctx, plan); err != nil {
				return fail(err)
			}
			response, err := invoke(ctx, fantasy.ToolCall{ID: fmt.Sprintf("%s-environment-%d", call.ID, index), Name: "bash", Input: workflowJSON(tools.BashParams{Command: environmentCommand(command), WorkingDir: command.Directory, Description: "Prepare project dependencies", AutoBackgroundAfter: 600})})
			if ctx.Err() != nil {
				attempt.Status = "cancelled"
				_ = save(context.WithoutCancel(ctx))
				return fantasy.ToolResponse{}, ctx.Err()
			}
			if !tools.ToolSucceeded("bash", response, err) {
				attempt.Status = "failed"
				var metadata tools.BashResponseMetadata
				if json.Unmarshal([]byte(response.Metadata), &metadata) != nil || metadata.Background || metadata.ExitCode == nil {
					attempt.Status = "ambiguous"
				}
				attempt.Report.Gaps = append(attempt.Report.Gaps, "Preparation command did not complete successfully")
				if err := save(ctx); err != nil {
					return fail(err)
				}
				return respond(attempt.Report)
			}
			attempt.CommandsPassed++
			if err := save(ctx); err != nil {
				return fail(err)
			}
		}
		attempt.Status = "installed"
		if err := save(ctx); err != nil {
			return fail(err)
		}
	}
	attempt.Report.Passed, attempt.Report.Gaps = false, []string{}
	attempt.Report.ObservedVersions = map[string]string{}
	if attempt.Status == "verified" {
		attempt.Status = "installed"
	}
	if attempt.Status != "installed" && attempt.Status != "verified" || attempt.CommandsPassed != len(plan.Commands) {
		attempt.Report.Gaps = append(attempt.Report.Gaps, "No complete matching dependency preparation was observed")
	}
	for index, requirement := range plan.Requirements {
		if err := c.engineering.Check(ctx, scope.SessionID, scope.TaskID); err != nil {
			return fail(err)
		}
		argv, err := environment.Probe(requirement)
		if err != nil {
			attempt.Report.Gaps = append(attempt.Report.Gaps, err.Error())
			continue
		}
		response, err := invoke(ctx, fantasy.ToolCall{ID: fmt.Sprintf("%s-version-%d", call.ID, index), Name: "bash", Input: workflowJSON(tools.BashParams{Command: environmentCommand(environment.Command{Argv: argv}), WorkingDir: root, Description: "Observe tool version", AutoBackgroundAfter: 60})})
		if ctx.Err() != nil {
			attempt.Status = "cancelled"
			_ = save(context.WithoutCancel(ctx))
			return fantasy.ToolResponse{}, ctx.Err()
		}
		if !tools.ToolSucceeded("bash", response, err) {
			attempt.Report.Gaps = append(attempt.Report.Gaps, requirement.Name+" version unavailable")
			continue
		}
		var metadata tools.BashResponseMetadata
		if err := json.Unmarshal([]byte(response.Metadata), &metadata); err != nil {
			attempt.Report.Gaps = append(attempt.Report.Gaps, "Version metadata unavailable")
			continue
		}
		version, err := environment.ObservedVersion(requirement, metadata.Output)
		if version != "" {
			attempt.Report.ObservedVersions[requirement.Name] = version
		}
		if err != nil {
			attempt.Report.Gaps = append(attempt.Report.Gaps, requirement.Name+": "+err.Error())
		}
	}
	if err := environment.ValidatePlan(ctx, plan); err != nil {
		attempt.Report.Gaps = append(attempt.Report.Gaps, err.Error())
	}
	attempt.Report.Passed = len(attempt.Report.Gaps) == 0 && len(plan.Requirements) > 0
	if attempt.Report.Passed {
		attempt.Status = "verified"
	}
	if err := save(ctx); err != nil {
		return fail(err)
	}
	return respond(attempt.Report)
}

func environmentCommand(command environment.Command) string {
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
	parts := []string{}
	for _, value := range command.Env {
		key, value, ok := strings.Cut(value, "=")
		if ok {
			parts = append(parts, key+"="+quote(value))
		}
	}
	for _, arg := range command.Argv {
		parts = append(parts, quote(arg))
	}
	return strings.Join(parts, " ")
}
