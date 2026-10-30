package tools

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/rehearsal"
)

// RehearseToolName is the model-facing name of the rehearsal tool.
const RehearseToolName = "rehearse"

//go:embed rehearse.md
var rehearseDescription string

// RehearseParams are the arguments of the rehearse tool.
type RehearseParams struct {
	Action       string           `json:"action" description:"run (default), show, list, apply or discard."`
	Task         string           `json:"task,omitempty" description:"What the change is for (run)."`
	Steps        []string         `json:"steps,omitempty" description:"Shell commands that make the change, run in order inside the isolated copy (run). Use relative paths only."`
	Files        []rehearsal.File `json:"files,omitempty" description:"Files to create or replace in the copy before the steps run (run)."`
	Checks       []string         `json:"checks,omitempty" description:"Commands such as tests, builds or linters that must still pass; they also run on the untouched copy so pre-existing failures are not blamed on the change (run)."`
	SkipBaseline bool             `json:"skip_baseline,omitempty" description:"Do not run the checks on the untouched copy first (run)."`
	ID           string           `json:"id,omitempty" description:"Rehearsal id (show, apply, discard)."`
	Force        bool             `json:"force,omitempty" description:"Apply even though the verdict is failed or breaks_checks (apply). Only after the user accepted that."`
	TimeoutSecs  int              `json:"timeout_seconds,omitempty" description:"Per-command timeout in seconds (run). Default 600."`
}

func secondsOrZero(n int) time.Duration {
	if n <= 0 {
		return 0
	}
	return time.Duration(n) * time.Second
}

// NewRehearseTool builds the rehearse tool. root is the project, dataDir
// the Atlas data directory that holds stored rehearsals.
func NewRehearseTool(root, dataDir string, perms permission.Service) fantasy.AgentTool {
	store := rehearsal.NewStore(dataDir)
	return fantasy.NewAgentTool(RehearseToolName, rehearseDescription, func(ctx context.Context, p RehearseParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		action := strings.ToLower(strings.TrimSpace(p.Action))
		if action == "" {
			action = "run"
		}
		ask := func(permAction, description string) (fantasy.ToolResponse, bool, error) {
			sessionID := GetSessionFromContext(ctx)
			if perms == nil || sessionID == "" {
				return fantasy.NewTextErrorResponse("rehearse requires permissions and a session"), false, nil
			}
			ok, err := perms.Request(ctx, permission.CreatePermissionRequest{SessionID: sessionID, Path: root, ToolCallID: call.ID, ToolName: RehearseToolName, Action: permAction, Description: description, Params: p})
			if err != nil {
				return fantasy.ToolResponse{}, false, err
			}
			if !ok {
				return NewPermissionDeniedResponse(perms), false, nil
			}
			return fantasy.ToolResponse{}, true, nil
		}

		switch action {
		case "run":
			if len(p.Steps) == 0 && len(p.Files) == 0 {
				return fantasy.NewTextErrorResponse("run needs steps and/or files describing the change"), nil
			}
			// The copy isolates the project tree only; commands still run
			// with the user's privileges, so the user approves them.
			desc := "Rehearse in an isolated copy: " + strings.Join(append(append([]string{}, p.Steps...), p.Checks...), "; ")
			if resp, ok, err := ask("execute", desc); !ok {
				return resp, err
			}
			report, err := rehearsal.Run(ctx, rehearsal.Options{
				Root: root, Store: store, Task: p.Task, Steps: p.Steps, Files: p.Files, Checks: p.Checks,
				SkipBaseline: p.SkipBaseline, CommandTimeout: secondsOrZero(p.TimeoutSecs),
			})
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			return fantasy.NewTextResponse(rehearsal.Render(report)), nil
		case "show":
			report, err := store.Load(p.ID)
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			return fantasy.NewTextResponse(rehearsal.Render(report)), nil
		case "list":
			reports, err := store.List()
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			if len(reports) == 0 {
				return fantasy.NewTextResponse("No rehearsals."), nil
			}
			var b strings.Builder
			for _, r := range reports {
				fmt.Fprintf(&b, "%s  %-9s %-13s %d file(s)  %s\n", r.ID, r.Status, r.Verdict, len(r.Files), r.Task)
			}
			return fantasy.NewTextResponse(b.String()), nil
		case "apply":
			report, err := store.Load(p.ID)
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			desc := fmt.Sprintf("Apply rehearsal %s (%s, %d file(s)) to the project", report.ID, report.Verdict, len(report.Files))
			if resp, ok, err := ask("write", desc); !ok {
				return resp, err
			}
			applied, err := store.Apply(ctx, p.ID, rehearsal.ApplyOptions{Root: root, Force: p.Force})
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			return fantasy.NewTextResponse(fmt.Sprintf("Applied rehearsal %s: %d file(s) written. Re-run any commands with effects outside files (migrations, installs) for real.", applied.ID, len(applied.Files))), nil
		case "discard":
			if _, err := store.Discard(p.ID); err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			return fantasy.NewTextResponse("Rehearsal discarded."), nil
		}
		return fantasy.NewTextErrorResponse("unknown action " + action), nil
	})
}
