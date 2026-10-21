package tools

import (
	"context"
	_ "embed"
	"errors"
	"fmt"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/shell"
)

const (
	JobKillToolName = "job_kill"
)

//go:embed job_kill.md
var jobKillDescription string

type JobKillParams struct {
	ShellID string `json:"shell_id" description:"The ID of the background shell to terminate"`
}

type JobKillResponseMetadata struct {
	ShellID     string `json:"shell_id"`
	Command     string `json:"command"`
	Description string `json:"description"`
}

func NewJobKillTool() fantasy.AgentTool {
	return fantasy.NewAgentTool(
		JobKillToolName,
		jobKillDescription,
		func(ctx context.Context, params JobKillParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.ShellID == "" {
				return fantasy.NewTextErrorResponse("missing shell_id"), nil
			}
			if isIsolatedJob(params.ShellID) {
				_, runner, err := isolatedRunner(ctx)
				if err != nil {
					return fantasy.NewTextErrorResponse(err.Error()), nil
				}
				if err := runner.Cancel(ctx, params.ShellID); err != nil {
					return fantasy.NewTextErrorResponse(err.Error()), nil
				}
				return fantasy.NewTextResponse("Cancellation requested for isolated job " + params.ShellID + ". Observe job_output for its final state."), nil
			}

			bgManager := shell.GetBackgroundShellManager()

			bgShell, ok := bgManager.Get(params.ShellID)
			if !ok {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("background shell not found: %s", params.ShellID)), nil
			}

			metadata := JobKillResponseMetadata{
				ShellID:     params.ShellID,
				Command:     bgShell.Command,
				Description: bgShell.Description,
			}

			err := bgManager.KillContext(ctx, params.ShellID)
			if errors.Is(err, shell.ErrStillStopping) {
				result := fmt.Sprintf("Background shell %s was told to stop but had not exited after waiting; it is no longer tracked", params.ShellID)
				return fantasy.WithResponseMetadata(fantasy.NewTextResponse(result), metadata), nil
			}
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}

			result := fmt.Sprintf("Background shell %s terminated successfully", params.ShellID)
			return fantasy.WithResponseMetadata(fantasy.NewTextResponse(result), metadata), nil
		},
	)
}
