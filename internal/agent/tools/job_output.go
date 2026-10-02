package tools

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/execution"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/shell"
)

const (
	JobOutputToolName = "job_output"

	// DefaultJobWaitSeconds bounds a wait=true call. Plenty of background
	// jobs never exit on their own -- dev servers, file watchers, REPLs --
	// and an unbounded wait on one parks the whole turn until the user
	// cancels it by hand. Waiting is still useful for a build or a test
	// run, so the wait stays, it just always has an end.
	DefaultJobWaitSeconds = 30
	// MaxJobWaitSeconds caps wait_timeout so no single call can park the
	// turn for an unreasonable stretch, however large a number is asked
	// for.
	MaxJobWaitSeconds = 600
)

//go:embed job_output.md
var jobOutputDescription string

type JobOutputParams struct {
	ShellID string `json:"shell_id" description:"The ID of the background shell to retrieve output from"`
	Wait    bool   `json:"wait" description:"If true, block until the background shell completes or wait_timeout elapses, whichever comes first. Leave false for a job that is not expected to exit (a server, a watcher)."`
	// WaitTimeout is in seconds and only consulted when Wait is true.
	WaitTimeout int `json:"wait_timeout,omitempty" description:"Seconds to wait when wait is true (default: 30, max: 600). The call returns whatever output exists so far once this elapses; the job keeps running."`
}

type JobOutputResponseMetadata struct {
	Execution        *execution.Result `json:"execution,omitempty"`
	ExitCode         *int              `json:"exit_code,omitempty"`
	ShellID          string            `json:"shell_id"`
	Command          string            `json:"command"`
	Description      string            `json:"description"`
	Done             bool              `json:"done"`
	WorkingDirectory string            `json:"working_directory"`
	// WaitTimedOut is true when wait was requested and the wait window
	// elapsed with the job still running.
	WaitTimedOut bool `json:"wait_timed_out,omitempty"`
	// WaitedSeconds is how long the wait window was, when one was used.
	WaitedSeconds int `json:"waited_seconds,omitempty"`
}

// jobWaitSeconds resolves the wait window from what the model asked for,
// clamping it into (0, MaxJobWaitSeconds].
func jobWaitSeconds(requested int) int {
	if requested <= 0 {
		return DefaultJobWaitSeconds
	}
	if requested > MaxJobWaitSeconds {
		return MaxJobWaitSeconds
	}
	return requested
}

func NewJobOutputTool() fantasy.AgentTool {
	return fantasy.NewAgentTool(
		JobOutputToolName,
		jobOutputDescription,
		func(ctx context.Context, params JobOutputParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.ShellID == "" {
				return fantasy.NewTextErrorResponse("missing shell_id"), nil
			}
			if isIsolatedJob(params.ShellID) {
				return isolatedJobOutput(ctx, params)
			}

			bgManager := shell.GetBackgroundShellManager()
			bgShell, ok := bgManager.Get(params.ShellID)
			if !ok {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("background shell not found: %s", params.ShellID)), nil
			}

			var (
				waitedSeconds int
				timedOut      bool
			)
			if params.Wait {
				waitedSeconds = jobWaitSeconds(params.WaitTimeout)
				waitCtx, cancel := context.WithTimeout(ctx, time.Duration(waitedSeconds)*time.Second)
				finished := bgShell.WaitContext(waitCtx)
				cancel()
				// WaitContext also returns false when the turn itself is
				// cancelled; only the wait window elapsing counts as a
				// timeout worth reporting back.
				timedOut = !finished && ctx.Err() == nil
			}

			stdout, stderr, done, err := bgShell.GetOutput()

			var outputParts []string
			if stdout != "" {
				outputParts = append(outputParts, stdout)
			}
			if stderr != "" {
				outputParts = append(outputParts, stderr)
			}

			status := "running"
			switch {
			case done:
				status = "completed"
				if err != nil {
					exitCode := shell.ExitCode(err)
					if exitCode != 0 {
						outputParts = append(outputParts, fmt.Sprintf("Exit code %d", exitCode))
					}
				}
			case timedOut:
				status = fmt.Sprintf("running (still running after waiting %ds)", waitedSeconds)
				outputParts = append(outputParts,
					fmt.Sprintf("The job did not finish within %ds and is still running. The output above is everything it has produced so far. Long-lived jobs such as servers and watchers never finish on their own: read their output without wait, and use job_kill when you are done with them.", waitedSeconds))
			}

			output := strings.Join(outputParts, "\n")
			output = TruncateOutput(output)

			metadata := JobOutputResponseMetadata{
				ShellID:          params.ShellID,
				Command:          bgShell.Command,
				Description:      bgShell.Description,
				Done:             done,
				WorkingDirectory: bgShell.WorkingDir,
				WaitTimedOut:     timedOut,
				WaitedSeconds:    waitedSeconds,
			}
			if done {
				code := shell.ExitCode(err)
				metadata.ExitCode = &code
			}

			if output == "" {
				output = BashNoOutput
			}

			result := fmt.Sprintf("Status: %s\n\n%s", status, output)
			return fantasy.WithResponseMetadata(fantasy.NewTextResponse(result), metadata), nil
		},
	)
}
