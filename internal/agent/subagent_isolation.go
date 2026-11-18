package agent

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
)

// isolationWorktree is the only isolation the agent tool offers: the
// sub-agent works in its own managed Git worktree, so parallel writers never
// touch the same checkout.
const isolationWorktree = "worktree"

// isolateRun checks an `isolation` request and creates the workspace it
// asks for. It returns the workspace id to run in, or an error response for
// the calling model. An empty id with no response means no isolation was
// requested.
//
// Creating a workspace is a change to the machine like the worktree tool's
// create action, so it asks for the same permission.
func (c *coordinator) isolateRun(ctx context.Context, p agentToolCallParams) (string, *fantasy.ToolResponse) {
	deny := func(message string) (string, *fantasy.ToolResponse) {
		response := fantasy.NewTextErrorResponse(message)
		return "", &response
	}
	if p.isolation == "" {
		return "", nil
	}
	switch {
	case p.isolation != isolationWorktree:
		return deny(`isolation must be "worktree"`)
	case p.workspaceID != "":
		return deny("isolation and workspace_id are exclusive: workspace_id names a workspace you already made")
	case p.resultSchema != nil:
		return deny("isolation cannot be combined with result_schema: the answer must stay pure JSON, so the workspace id would have nowhere to go")
	case p.agentName == "" || p.qualityOnly || p.sessionKey != "":
		return deny("isolation requires a named specialist and cannot be combined with quality_only or session_key")
	case c.engineering == nil:
		return deny("isolation requires durable storage")
	}
	root := c.cfg.WorkingDir()
	if c.permissions != nil {
		ok, err := c.permissions.Request(ctx, permission.CreatePermissionRequest{
			SessionID: p.sessionID, ToolCallID: p.toolCallID, ToolName: "worktree", Action: "create", Path: root,
			Description: "Managed workspace create, for an isolated sub-agent",
		})
		if err != nil {
			return deny(err.Error())
		}
		if !ok {
			return deny("The user did not allow creating an isolated workspace for this sub-agent.")
		}
	}
	scope := engineering.GetScope(ctx, p.sessionID)
	workspace, err := c.engineering.CreateWorkspace(ctx, root, scope.SessionID, scope.TaskID)
	if err != nil {
		return deny("could not create an isolated workspace: " + err.Error())
	}
	return workspace.ID, nil
}

// patchedFiles lists the files a unified diff touches, in order, once each.
var diffHeader = regexp.MustCompile(`(?m)^diff --git a/(\S+) b/(\S+)$`)

func patchedFiles(patch string) []string {
	var files []string
	for _, m := range diffHeader.FindAllStringSubmatch(patch, -1) {
		if !slices.Contains(files, m[2]) {
			files = append(files, m[2])
		}
	}
	return files
}

// isolatedFooter tells the parent where the sub-agent's work is and how to
// take it: the sub-agent changed an isolated copy, nothing reached the
// parent checkout, and the parent applies the patch deliberately.
func (c *coordinator) isolatedFooter(ctx context.Context, sessionID, workspaceID string) string {
	scope := engineering.GetScope(ctx, sessionID)
	_, patch, err := c.engineering.WorkspacePatch(context.WithoutCancel(ctx), scope.SessionID, workspaceID)
	var b strings.Builder
	fmt.Fprintf(&b, "\n\n[isolated workspace %s]", workspaceID)
	switch files := patchedFiles(patch); {
	case err != nil:
		fmt.Fprintf(&b, " could not read its changes: %v.", err)
	case len(files) == 0:
		b.WriteString(" The sub-agent changed no files.")
	default:
		fmt.Fprintf(&b, " Changed files (%d): %s.", len(files), strings.Join(files, ", "))
	}
	b.WriteString(" Nothing was applied to your checkout. Review it with the worktree tool (action patch, id above), then apply it, or remove the workspace if the work is not wanted.")
	return b.String()
}
