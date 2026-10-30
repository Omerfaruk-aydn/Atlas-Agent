package agent

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
)

func (c *coordinator) requestPermissionTool() fantasy.AgentTool {
	type params struct {
		Tool   string `json:"tool" jsonschema:"required,description=Tool requiring permission, for example bash"`
		Action string `json:"action" jsonschema:"required,description=Exact action, for example execute"`
		Path   string `json:"path" jsonschema:"description=Directory scope; defaults to the project root"`
		Reason string `json:"reason" jsonschema:"required,description=Concrete user-facing justification"`
		Scope  string `json:"scope" jsonschema:"required,enum=call,enum=turn,enum=session,description=Duration of the requested elevation"`
	}
	return fantasy.NewAgentTool("request_permission", "Request bounded additional permission without cancelling the active turn. Explain the exact tool, action, path, reason and duration. Approval applies to subsequent matching calls.", func(ctx context.Context, p params, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		p.Tool, p.Action, p.Reason = strings.TrimSpace(p.Tool), strings.TrimSpace(p.Action), strings.TrimSpace(p.Reason)
		if p.Tool == "" || p.Tool == "request_permission" || p.Action == "" || len(p.Reason) < 10 || len(p.Reason) > 1000 {
			return fantasy.NewTextErrorResponse("provide a non-recursive tool, exact action and a concrete reason between 10 and 1000 characters"), nil
		}
		if p.Path == "" {
			p.Path = c.cfg.WorkingDir()
		}
		absolute, err := filepath.Abs(p.Path)
		if err != nil {
			return fantasy.NewTextErrorResponse("invalid permission path"), nil
		}
		p.Path = absolute
		scoped, ok := c.permissions.(permission.ScopedService)
		if !ok {
			return fantasy.NewTextErrorResponse("runtime permission elevation is unavailable on this connection"), nil
		}
		granted, err := c.permissions.Request(ctx, permission.CreatePermissionRequest{
			SessionID: tools.GetSessionFromContext(ctx), ToolCallID: call.ID,
			ToolName: "request_permission", Action: p.Tool + ":" + p.Action,
			Path: p.Path, Description: fmt.Sprintf("Allow %s %s for %s scope: %s", p.Tool, p.Action, p.Scope, p.Reason), Params: p,
		})
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		if !granted {
			return fantasy.NewTextResponse("Permission denied. Do not retry the same request; choose an alternative within existing permissions."), nil
		}
		if err := scoped.GrantScoped(tools.GetSessionFromContext(ctx), p.Tool, p.Action, p.Path, permission.GrantScope(p.Scope)); err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		return fantasy.NewTextResponse("Permission granted for the requested scope. Continue the active turn."), nil
	})
}
