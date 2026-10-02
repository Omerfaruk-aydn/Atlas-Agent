package tools

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/execution"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/shell"
	"github.com/google/uuid"
)

func isolatedRunner(ctx context.Context) (execution.Binding, execution.Runner, error) {
	binding, ok := execution.GetBinding(ctx)
	if !ok || binding.Factory == nil || binding.Store == nil {
		return binding, nil, fmt.Errorf("required isolation is unavailable for this job")
	}
	runner, err := binding.Factory(ctx)
	if err == nil && runner == nil {
		err = fmt.Errorf("required isolation returned no runner")
	}
	return binding, runner, err
}

func runIsolatedBash(ctx context.Context, params BashParams, call fantasy.ToolCall, cwd string, blocks []shell.BlockFunc, limits BashLimits) (fantasy.ToolResponse, error) {
	if len(params.Argv) == 0 {
		if err := shell.CheckIsolatedScript(params.Command, blocks); err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
	} else {
		for _, block := range blocks {
			if block(params.Argv) {
				return fantasy.NewTextErrorResponse("Command blocked by policy"), nil
			}
		}
	}
	binding, runner, err := isolatedRunner(ctx)
	if err != nil {
		return fantasy.NewTextErrorResponse("Required isolation unavailable: " + err.Error()), nil
	}
	request, err := execution.ShellRequest(binding.Root, cwd, params.Command)
	if len(params.Argv) > 0 {
		request, err = execution.ArgvRequest(binding.Root, cwd, params.Argv)
	}
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	request.Env = os.Environ()
	request.TaskID = engineering.GetScope(ctx, GetSessionFromContext(ctx)).TaskID
	request.SessionID = engineering.GetScope(ctx, GetSessionFromContext(ctx)).SessionID
	request.ToolCallID = call.ID
	handle, err := runner.Start(ctx, request)
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	start := time.Now()
	background := func() (fantasy.ToolResponse, error) {
		result := execution.Result{RunID: handle.RunID, Backend: handle.Backend, ContainerID: handle.ContainerID, HostOS: runtime.GOOS, ExecutionOS: "linux", Status: "running"}
		metadata := BashResponseMetadata{ShellID: handle.RunID, Background: true, StartTime: start.UnixMilli(), WorkingDirectory: cwd, Description: params.Description, Execution: &result}
		return fantasy.WithResponseMetadata(fantasy.NewTextResponse("Isolated job started: "+handle.RunID+". Use job_output or job_kill with this identity."), metadata), nil
	}
	if params.RunInBackground {
		return background()
	}
	timer := time.NewTimer(time.Duration(limits.autoBackgroundAfter(params.AutoBackgroundAfter)) * time.Second)
	defer timer.Stop()
	backgroundAfter := timer.C
	if len(params.Argv) > 0 {
		backgroundAfter = nil
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			err := runner.Cancel(cleanup, handle.RunID)
			cancel()
			if err != nil {
				return fantasy.NewTextErrorResponse("Cancellation requires reconciliation: " + err.Error()), nil
			}
			return fantasy.ToolResponse{}, ctx.Err()
		case <-backgroundAfter:
			return background()
		case <-ticker.C:
			result, err := runner.Observe(ctx, handle.RunID)
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			if !result.Done {
				continue
			}
			output, err := isolatedOutput(ctx, binding, result)
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			response := fantasy.NewTextResponse(TruncateOutput(output))
			if result.Status != "exited" || result.ExitCode == nil {
				response = fantasy.NewTextErrorResponse("Isolated execution ended with " + result.Status + "\n" + output)
			}
			metadata := BashResponseMetadata{ExitCode: result.ExitCode, StartTime: start.UnixMilli(), EndTime: time.Now().UnixMilli(), Output: TruncateOutput(output), WorkingDirectory: cwd, Description: params.Description, Execution: &result}
			return fantasy.WithResponseMetadata(response, metadata), nil
		}
	}
}

func isolatedOutput(ctx context.Context, binding execution.Binding, result execution.Result) (string, error) {
	parts := []string{}
	for _, ref := range []engineering.ArtifactRef{result.OutputRef, result.ErrorRef} {
		if ref.Hash == "" {
			if result.Done {
				return "", fmt.Errorf("observed output artifact unavailable")
			}
			continue
		}
		data, err := binding.Store.ReadArtifact(ctx, ref)
		if err != nil {
			return "", err
		}
		if len(data) > 0 {
			parts = append(parts, string(data))
		}
	}
	if len(parts) == 0 {
		return BashNoOutput, nil
	}
	return strings.Join(parts, "\n"), nil
}

func isIsolatedJob(id string) bool { _, err := uuid.Parse(id); return err == nil }

func isolatedJobOutput(ctx context.Context, params JobOutputParams) (fantasy.ToolResponse, error) {
	binding, runner, err := isolatedRunner(ctx)
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	var timer <-chan time.Time
	var timeout *time.Timer
	if params.Wait {
		timeout = time.NewTimer(time.Duration(jobWaitSeconds(params.WaitTimeout)) * time.Second)
		defer timeout.Stop()
		timer = timeout.C
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	timedOut := false
	for {
		result, err := runner.Observe(ctx, params.ShellID)
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		if result.Done || !params.Wait || timedOut {
			output, err := isolatedOutput(ctx, binding, result)
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			metadata := JobOutputResponseMetadata{ShellID: params.ShellID, Done: result.Done, ExitCode: result.ExitCode, WaitTimedOut: timedOut, Execution: &result}
			response := fantasy.NewTextResponse("Status: " + result.Status + "\n" + TruncateOutput(output))
			if result.Done && result.Status != "exited" {
				response = fantasy.NewTextErrorResponse(response.Content)
			}
			return fantasy.WithResponseMetadata(response, metadata), nil
		}
		select {
		case <-ctx.Done():
			return fantasy.ToolResponse{}, ctx.Err()
		case <-timer:
			timedOut = true
		case <-ticker.C:
		}
	}
}
