package tools

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/attempt"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
)

// AttemptToolName is the tool that runs parallel attempts with a judge.
const AttemptToolName = "attempt"

//go:embed attempt.md
var attemptDescription string

// maxToolAttempts caps the fan-out of one tool call. The CLI allows more;
// a model deciding on its own should not be able to launch a large bill.
const maxToolAttempts = 5

// AttemptParams are the arguments of the attempt tool.
type AttemptParams struct {
	Action     string   `json:"action,omitempty" enum:"run,list,show,apply" description:"run (default) executes the attempts and ranks them without changing the tree; list and show read stored runs; apply puts one stored attempt's patch into the working tree."`
	Task       string   `json:"task,omitempty" description:"run: the complete task, as you would hand it to a colleague. Every attempt receives exactly this text."`
	N          int      `json:"n,omitempty" description:"run: number of attempts, 2-5 (default 3)."`
	Models     []string `json:"models,omitempty" description:"run: model IDs or model role names for the attempts, cycled over them; omit to use the default model for all."`
	Judge      string   `json:"judge,omitempty" description:"run: judge model or role; prefer a different and stronger model than the attempts. Defaults to the judge or evaluator role."`
	NoJudge    bool     `json:"no_judge,omitempty" description:"run: rank by deterministic checks only."`
	Verify     []string `json:"verify,omitempty" description:"run: commands that must pass for an attempt to count. Defaults to the verify list of .atlas/work.json, else the detected test command."`
	Setup      string   `json:"setup,omitempty" description:"run: command run once in each fresh worktree before the agent starts, e.g. npm ci, for dependencies git does not track."`
	Autonomous bool     `json:"autonomous,omitempty" description:"run: let the attempts use tools without asking. Required for attempts to edit files headlessly; the user is shown this when approving."`
	RunID      string   `json:"run_id,omitempty" description:"show and apply: the run ID."`
	Attempt    string   `json:"attempt,omitempty" description:"apply: attempt label (A, B, ...) to apply instead of the recommended winner."`
}

// AttemptConfig supplies the attempt tool with the live configuration so
// role names resolve against the current models.
type AttemptConfig func() *config.Config

// NewAttemptTool builds the attempt tool. dataDir is the project data
// directory; reports and patches live in its attempts subdirectory.
func NewAttemptTool(workingDir, dataDir string, permissions permission.Service, cfg AttemptConfig) fantasy.AgentTool {
	stateDir := filepath.Join(dataDir, "attempts")
	if !filepath.IsAbs(stateDir) {
		stateDir = filepath.Join(workingDir, stateDir)
	}
	return fantasy.NewAgentTool(
		AttemptToolName,
		attemptDescription,
		func(ctx context.Context, p AttemptParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			current := func() *config.Config {
				if cfg == nil {
					return nil
				}
				return cfg()
			}
			switch p.Action {
			case "", "run":
				return runAttemptTool(ctx, call, p, workingDir, stateDir, permissions, current())
			case "list":
				return listAttempts(stateDir)
			case "show":
				report, err := attempt.Load(stateDir, p.RunID)
				if err != nil {
					return fantasy.NewTextErrorResponse(err.Error()), nil
				}
				return fantasy.NewTextResponse(attempt.Format(report)), nil
			case "apply":
				return applyAttemptTool(ctx, call, p, workingDir, stateDir, permissions)
			}
			return fantasy.NewTextErrorResponse(fmt.Sprintf("unknown action %q: use run, list, show or apply", p.Action)), nil
		},
	)
}

func runAttemptTool(ctx context.Context, call fantasy.ToolCall, p AttemptParams, workingDir, stateDir string, permissions permission.Service, cfg *config.Config) (fantasy.ToolResponse, error) {
	if strings.TrimSpace(p.Task) == "" {
		return fantasy.NewTextErrorResponse("task is required for run"), nil
	}
	if os.Getenv(attempt.ChildEnv) != "" {
		return fantasy.NewTextErrorResponse("attempts cannot be started from inside an attempt"), nil
	}
	n := p.N
	if n == 0 {
		n = 3
	}
	if n < 1 || n > maxToolAttempts {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("n must be between 1 and %d", maxToolAttempts)), nil
	}
	models := attempt.ResolveModels(cfg, p.Models)
	judge := attempt.DefaultJudge(cfg)
	if strings.TrimSpace(p.Judge) != "" {
		judge = attempt.ResolveModel(cfg, p.Judge)
	}

	desc := fmt.Sprintf("Run %d independent attempts in isolated worktrees (models: %s; judge: %s)", n, listOrDefault(models), orDefault(judge, "default"))
	if p.Autonomous {
		desc += "; attempts may use tools without asking"
	}
	if err := requestAttemptPermission(ctx, call, permissions, workingDir, "run", desc, p); err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}

	report, err := attempt.Run(ctx, attempt.Options{
		Root:     workingDir,
		StateDir: stateDir,
		Task:     p.Task,
		N:        n,
		Models:   models,
		Judge:    judge,
		NoJudge:  p.NoJudge,
		Verify:   p.Verify,
		Setup:    p.Setup,
		Yolo:     p.Autonomous,
	})
	if err != nil {
		return fantasy.NewTextErrorResponse("attempt run failed: " + err.Error()), nil
	}
	return fantasy.WithResponseMetadata(
		fantasy.NewTextResponse(attempt.Format(report)+"\nTo apply, call attempt with action \"apply\" and run_id \""+report.ID+"\" after the user agrees."),
		AttemptResponseMetadata{RunID: report.ID, Winner: report.Winner, Confidence: report.Confidence, Cost: report.TotalCost},
	), nil
}

// AttemptResponseMetadata is attached to run results.
type AttemptResponseMetadata struct {
	RunID      string  `json:"run_id"`
	Winner     int     `json:"winner"`
	Confidence string  `json:"confidence"`
	Cost       float64 `json:"cost"`
}

func listAttempts(stateDir string) (fantasy.ToolResponse, error) {
	reports, err := attempt.List(stateDir)
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	if len(reports) == 0 {
		return fantasy.NewTextResponse("No attempt runs."), nil
	}
	var b strings.Builder
	for _, r := range reports {
		state := "not applied"
		if r.Applied != 0 {
			state = fmt.Sprintf("applied attempt %d", r.Applied)
		}
		winner := "no winner"
		if r.Winner != 0 {
			winner = fmt.Sprintf("winner attempt %d (%s confidence)", r.Winner, r.Confidence)
		}
		fmt.Fprintf(&b, "%s: %d attempts, %s, %s: %s\n", r.ID, len(r.Attempts), winner, state, truncateAttemptTask(r.Task))
	}
	return fantasy.NewTextResponse(b.String()), nil
}

func applyAttemptTool(ctx context.Context, call fantasy.ToolCall, p AttemptParams, workingDir, stateDir string, permissions permission.Service) (fantasy.ToolResponse, error) {
	report, err := attempt.Load(stateDir, p.RunID)
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	pick := report.Winner
	if label := strings.ToUpper(strings.TrimSpace(p.Attempt)); label != "" {
		if len(label) != 1 || label[0] < 'A' || int(label[0]-'A') >= len(report.Attempts) {
			return fantasy.NewTextErrorResponse(fmt.Sprintf("attempt must be a label between A and %c", 'A'+len(report.Attempts)-1)), nil
		}
		pick = int(label[0]-'A') + 1
	}
	if pick == 0 {
		return fantasy.NewTextErrorResponse("this run has no winner; name an attempt label explicitly"), nil
	}
	chosen := report.Attempts[pick-1]
	desc := fmt.Sprintf("Apply attempt %s of run %s to the working tree (%d file(s), +%d/-%d, status %s)", chosen.Label, report.ID, len(chosen.Files), chosen.Additions, chosen.Deletions, chosen.Status)
	if err := requestAttemptPermission(ctx, call, permissions, workingDir, "apply", desc, p); err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	_, applied, err := attempt.Apply(ctx, stateDir, p.RunID, pick)
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	return fantasy.NewTextResponse(fmt.Sprintf("Applied attempt %s (%d file(s), +%d/-%d). The change is in the working tree, uncommitted; review it and run the project's checks.", applied.Label, len(applied.Files), applied.Additions, applied.Deletions)), nil
}

func requestAttemptPermission(ctx context.Context, call fantasy.ToolCall, permissions permission.Service, root, action, desc string, params any) error {
	if permissions == nil {
		return nil
	}
	ok, err := permissions.Request(ctx, permission.CreatePermissionRequest{
		SessionID:   GetSessionFromContext(ctx),
		ToolCallID:  call.ID,
		ToolName:    AttemptToolName,
		Action:      action,
		Description: desc,
		Params:      params,
		Path:        root,
	})
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("permission denied: the user did not approve attempt %s", action)
	}
	return nil
}

func listOrDefault(models []string) string {
	if len(models) == 0 {
		return "default"
	}
	return strings.Join(models, ", ")
}

func orDefault(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func truncateAttemptTask(task string) string {
	task = strings.Join(strings.Fields(task), " ")
	if len(task) > 80 {
		return task[:80] + "..."
	}
	return task
}
