package tools

import (
	"context"
	_ "embed"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
)

//go:embed enter_plan_mode.md
var enterPlanModeDescription string

// EnterPlanModeToolName is the tool that lets the agent ask for its own
// planning phase.
const EnterPlanModeToolName = permission.EnterPlanModeToolName

// EnterPlanModeParams explains why the agent wants to plan first.
type EnterPlanModeParams struct {
	Reason string `json:"reason" description:"One sentence on why this work should be planned and approved before any change."`
}

// NewEnterPlanModeTool switches the permission service to plan mode. Entering
// only takes capability away, so it needs no approval; leaving does, through
// exit_plan_mode. It does nothing for a user who chose to work without
// approval prompts, since plan mode would then stall them.
func NewEnterPlanModeTool(permissions permission.Service) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		EnterPlanModeToolName,
		enterPlanModeDescription,
		func(_ context.Context, params EnterPlanModeParams, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			switch permissions.Mode() {
			case permission.ModePlan:
				return fantasy.NewTextResponse("Already in plan mode."), nil
			case permission.ModeBypass, permission.ModeAutoApprove:
				return fantasy.NewTextResponse("Plan mode is not available while permissions are set to run without approval prompts. Write the plan down yourself (for example in .atlas/work.json) and carry on."), nil
			}
			permissions.SetMode(permission.ModePlan)
			return fantasy.NewTextResponse("Plan mode is on: reading and searching still work; writing, editing and running do not. Explore, write the plan out as text, then call exit_plan_mode with it so the user can approve."), nil
		},
	)
}
