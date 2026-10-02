package tools

import (
	"context"
	"encoding/json"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
)

type WorktreeParams struct {
	Action string `json:"action" description:"create, list, inspect, patch, apply or remove. Creation starts at HEAD; uncommitted parent changes are not copied. Apply checks conflicts before changing the parent. Remove only accepts clean workspaces still at their base."`
	ID     string `json:"id,omitempty" description:"Registered workspace ID; paths cannot be supplied."`
	TaskID string `json:"task_id,omitempty" description:"Task that owns the new workspace."`
}

func NewWorktreeTool(root string, store *engineering.Store, permissions permission.Service) fantasy.AgentTool {
	return fantasy.NewAgentTool("worktree", "Create and inspect isolated registered Git workspaces. Review a complete patch before applying it; conflicts and dirty removal are rejected. No force deletion, automatic commit, merge or push.", func(ctx context.Context, p WorktreeParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		scope := engineering.GetScope(ctx, GetSessionFromContext(ctx))
		mutating := p.Action == "create" || p.Action == "apply" || p.Action == "remove"
		if mutating {
			ok, err := permissions.Request(ctx, permission.CreatePermissionRequest{SessionID: GetSessionFromContext(ctx), ToolCallID: call.ID, ToolName: "worktree", Action: p.Action, Path: root, Description: "Managed workspace " + p.Action, Params: p})
			if err != nil {
				return fantasy.ToolResponse{}, err
			}
			if !ok {
				return NewPermissionDeniedResponse(permissions), nil
			}
		}
		var result any
		var err error
		switch p.Action {
		case "create":
			result, err = store.CreateWorkspace(ctx, root, scope.SessionID, p.TaskID)
		case "list":
			var st engineering.State
			st, err = store.Read(ctx, scope.SessionID)
			result = st.Workspaces
		case "inspect":
			result, err = store.Workspace(ctx, scope.SessionID, p.ID)
		case "patch":
			var w engineering.Workspace
			var patch string
			w, patch, err = store.WorkspacePatch(ctx, scope.SessionID, p.ID)
			result = map[string]any{"workspace": w, "patch": patch}
		case "apply":
			err = store.ApplyWorkspace(ctx, root, scope.SessionID, p.ID)
			result = "Workspace changes applied; run integration verification in the parent checkout."
		case "remove":
			err = store.RemoveWorkspace(ctx, root, scope.SessionID, p.ID)
			result = "Clean workspace removed."
		default:
			return fantasy.NewTextErrorResponse("unknown workspace action"), nil
		}
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		data, _ := json.Marshal(result)
		return fantasy.NewTextResponse(string(data)), nil
	})
}
