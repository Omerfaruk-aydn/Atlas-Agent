package tools

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/planspec"
)

//go:embed exit_plan_mode.md
var exitPlanModeDescription string

// ExitPlanModeToolName must match permission.ExitPlanModeToolName: the
// permission service special-cases this exact tool name so it can still
// reach the confirmation dialog while ModePlan denies everything else
// outright.
const ExitPlanModeToolName = permission.ExitPlanModeToolName

type ExitPlanModeParams struct {
	Plan string `json:"plan" description:"The implementation plan you just presented to the user, in markdown. Shown again in the approval prompt."`
}

// NewExitPlanModeTool builds the tool. approval, when given, reports the
// configured approval mode (planspec.ApprovalAlways, ApprovalAuto or
// ApprovalTurbo) each time the tool runs.
func NewExitPlanModeTool(permissions permission.Service, workingDir string, approval ...func() string) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		ExitPlanModeToolName,
		exitPlanModeDescription,
		func(ctx context.Context, params ExitPlanModeParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if permissions.Mode() != permission.ModePlan {
				return fantasy.NewTextResponse("Not in plan mode; nothing to exit."), nil
			}

			sessionID := GetSessionFromContext(ctx)
			if sessionID == "" {
				return fantasy.ToolResponse{}, fmt.Errorf("session_id is required")
			}

			description := params.Plan
			if description == "" {
				description = "Exit plan mode and start implementing."
			}

			// A plan that tags files with [NEW]/[MODIFY]/[DELETE] is checked
			// against the tree before the user is asked, so they never
			// approve a plan that names files that cannot exist.
			spec := planspec.Parse(params.Plan)
			if problems := spec.Lint(workingDir); len(problems) > 0 {
				return fantasy.NewTextErrorResponse("The plan has file entries that do not match the project. Fix them and call exit_plan_mode again:\n- " + strings.Join(problems, "\n- ")), nil
			}
			planNote := ""
			if len(spec.Entries) > 0 || len(spec.ReplanIf) > 0 {
				planNote = spec.Summary()
				description += "\n\n---\n" + planNote
			}

			mode := planspec.ApprovalAlways
			if len(approval) > 0 && approval[0] != nil {
				mode = approval[0]()
			}
			autoNote := ""
			switch mode {
			case planspec.ApprovalTurbo:
				autoNote = "Approved without asking (plan_approval: turbo)."
			case planspec.ApprovalAuto:
				if ok, _ := spec.AutoProceed(); ok {
					autoNote = "Approved without asking: a small plan that touches no build, CI, dependency or credential file and deletes nothing (plan_approval: auto)."
				}
			}
			if autoNote != "" {
				permissions.SetMode(permission.ModeManual)
				saveApprovedPlan(workingDir, spec)
				return fantasy.NewTextResponse("Plan mode is now off (switched to manual mode); proceed with the implementation. " + autoNote), nil
			}

			granted, err := permissions.Request(ctx, permission.CreatePermissionRequest{
				SessionID:   sessionID,
				Path:        workingDir,
				ToolCallID:  call.ID,
				ToolName:    ExitPlanModeToolName,
				Action:      "exit_plan_mode",
				Description: description,
			})
			if err != nil {
				return fantasy.ToolResponse{}, err
			}
			if !granted {
				if feedback := permission.TakeDenyFeedback(call.ID); feedback != "" {
					return fantasy.NewTextResponse("The user requested changes to the plan. Stay in plan mode, revise the plan to address this, present it again and call exit_plan_mode once more. Their comment:\n" + feedback), nil
				}
				return fantasy.NewTextResponse("The user did not approve the plan. Stay in plan mode: keep discussing or refining the plan as text, and do not retry write/edit/bash tools."), nil
			}

			// The dialog approval IS the user's mode switch here — flipping
			// back to ModeManual (not ModeBypass) so the tools this plan
			// needs still go through their normal confirmation, same as if
			// the user had pressed Shift+Tab themselves.
			permissions.SetMode(permission.ModeManual)
			saveApprovedPlan(workingDir, spec)
			msg := "Plan approved. Plan mode is now off (switched to manual mode); proceed with the implementation."
			if planNote != "" {
				msg += " Approved " + planNote + "."
				if len(spec.ReplanIf) > 0 {
					msg += " Stop and re-plan with the user if: " + strings.Join(spec.ReplanIf, "; ") + "."
				}
			}
			return fantasy.NewTextResponse(msg), nil
		},
	)
}
