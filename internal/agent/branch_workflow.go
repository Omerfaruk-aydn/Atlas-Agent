package agent

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
)

func (c *coordinator) branchWorkflow(ctx context.Context, p WorkflowParams, call fantasy.ToolCall, invoke tools.ToolInvoker) (fantasy.ToolResponse, error) {
	id := engineering.GetScope(ctx, tools.GetSessionFromContext(ctx)).SessionID
	root := c.cfg.WorkingDir()
	fail := func(err error) (fantasy.ToolResponse, error) { return fantasy.NewTextErrorResponse(err.Error()), nil }
	switch p.BranchAction {
	case "fork":
		if len(p.BranchPaths) == 0 || len(p.BranchPaths) > 8 {
			return fail(fmt.Errorf("branch needs 1-8 paths"))
		}
		if err := engineering.ValidatePatchSources(ctx, root, p.BranchPaths); err != nil {
			return fail(err)
		}
		if invoke == nil {
			return fail(fmt.Errorf("view invocation unavailable"))
		}
		for n, path := range p.BranchPaths {
			if err := ctx.Err(); err != nil {
				return fantasy.ToolResponse{}, err
			}
			response, err := invoke(ctx, fantasy.ToolCall{ID: fmt.Sprintf("%s-snapshot-%d", call.ID, n), Name: tools.ViewToolName, Input: workflowJSON(tools.ViewParams{FilePath: filepath.Join(root, path)})})
			if err != nil || response.IsError || response.StopTurn {
				return response, err
			}
		}
		b, err := c.engineering.ForkPatchBranch(ctx, id, root, p.BranchName, p.TaskID, p.BranchPaths)
		if err != nil {
			return fail(err)
		}
		return workResponse(b)
	case "propose":
		b, err := c.engineering.ProposeBranch(ctx, id, p.BranchID, p.BranchFiles, p.BranchNotes)
		if err != nil {
			return fail(err)
		}
		return workResponse(b)
	case "status", "":
		st, err := c.engineering.Read(ctx, id)
		if err != nil {
			return fail(err)
		}
		return workResponse(map[string]any{"branches": st.Branches, "mode": "patch snapshots; no checkout, no automatic execution", "next": "merge applies through edit; verify the integrated result afterwards"})
	case "merge":
		if invoke == nil {
			return fail(fmt.Errorf("edit invocation unavailable"))
		}
		b, err := c.engineering.BeginBranchMerge(ctx, id, root, p.BranchID)
		if err != nil {
			return fail(err)
		}
		for n, f := range b.Files {
			if f.Proposed == nil || *f.Proposed == f.Base {
				continue
			}
			if err := ctx.Err(); err != nil {
				return fantasy.ToolResponse{}, err
			}
			observed, err := invoke(ctx, fantasy.ToolCall{ID: fmt.Sprintf("%s-read-%d", call.ID, n), Name: tools.ViewToolName, Input: workflowJSON(tools.ViewParams{FilePath: filepath.Join(root, f.Path)})})
			if err != nil || observed.IsError || observed.StopTurn {
				_ = c.engineering.FinishBranchMerge(context.WithoutCancel(ctx), id, b.ID, "interrupted")
				return observed, err
			}
			if err := c.engineering.RecordBranchDispatch(ctx, id, b.ID, f.Path, "unknown"); err != nil {
				return fail(err)
			}
			response, err := invoke(ctx, fantasy.ToolCall{ID: fmt.Sprintf("%s-merge-%d", call.ID, n), Name: tools.EditToolName, Input: workflowJSON(tools.EditParams{FilePath: filepath.Join(root, f.Path), OldString: f.Base, NewString: *f.Proposed, ExpectedFileHash: f.BaseHash})})
			if err != nil || response.IsError || response.StopTurn {
				_ = c.engineering.FinishBranchMerge(context.WithoutCancel(ctx), id, b.ID, "interrupted")
				return response, err
			}
			if err := c.engineering.RecordBranchDispatch(ctx, id, b.ID, f.Path, "applied"); err != nil {
				return fail(err)
			}
		}
		if err := c.engineering.FinishBranchMerge(ctx, id, b.ID, "applied"); err != nil {
			return fail(err)
		}
		return workResponse(map[string]any{"branch_id": b.ID, "status": "applied", "verification": "required; merge is not a completed acceptance check"})
	default:
		return fail(fmt.Errorf("branch_action must be fork, propose, status or merge"))
	}
}
