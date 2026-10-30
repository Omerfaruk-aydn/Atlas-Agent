package attempt

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const (
	// maxCheckTail is how much of a check's output is kept.
	maxCheckTail = 4000
	// maxAgentOutput bounds the JSON an agent subprocess may print.
	maxAgentOutput = 8 << 20
)

// tailBuffer keeps the last maxCheckTail bytes written to it.
type tailBuffer struct {
	data []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	n := len(p)
	t.data = append(t.data, p...)
	if len(t.data) > 2*maxCheckTail {
		t.data = append([]byte(nil), t.data[len(t.data)-maxCheckTail:]...)
	}
	return n, nil
}

func (t *tailBuffer) String() string {
	data := t.data
	if len(data) > maxCheckTail {
		data = data[len(data)-maxCheckTail:]
	}
	return strings.TrimSpace(strings.ToValidUTF8(string(data), ""))
}

// shellCommand builds a command that runs line through the platform shell.
func shellCommand(ctx context.Context, line string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.CommandContext(ctx, "cmd", "/C", line)
	}
	return exec.CommandContext(ctx, "sh", "-c", line)
}

// ExecCheck is the default CheckFunc: it runs command through the platform
// shell in dir and records exit status and the tail of the output.
func ExecCheck(ctx context.Context, dir, command string, timeout time.Duration) CheckResult {
	res := CheckResult{Command: command}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	started := time.Now()
	cmd := shellCommand(runCtx, command)
	cmd.Dir = dir
	cmd.WaitDelay = 5 * time.Second
	var out tailBuffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	res.DurationMS = time.Since(started).Milliseconds()
	res.Output = out.String()
	switch {
	case err == nil:
		res.Passed = true
	case errors.Is(runCtx.Err(), context.DeadlineExceeded):
		res.TimedOut, res.ExitCode = true, -1
	default:
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			res.ExitCode = exit.ExitCode()
		} else {
			res.ExitCode = -1
			res.Output = strings.TrimSpace(res.Output + "\n" + err.Error())
		}
	}
	return res
}

// ExecAgent returns an AgentFunc that runs the Atlas binary at program as
// a headless "run" with JSON output, in the job's directory.
func ExecAgent(program string) AgentFunc {
	return func(ctx context.Context, job AgentJob) (AgentResult, error) {
		args := []string{"--cwd", job.Dir, "--data-dir", job.StateDir, "run", "--json", "--quiet"}
		if job.Model != "" {
			args = append(args, "--model", job.Model)
		}
		if job.Yolo {
			args = append(args, "--yolo")
		}
		args = append(args, "--", job.Prompt)
		cmd := exec.CommandContext(ctx, program, args...)
		cmd.Dir = job.Dir
		cmd.WaitDelay = 10 * time.Second
		for _, v := range os.Environ() {
			switch {
			case strings.HasPrefix(v, "ATLAS_AGENT_CLIENT_SERVER="),
				strings.HasPrefix(v, "GIT_DIR="),
				strings.HasPrefix(v, "GIT_WORK_TREE="),
				strings.HasPrefix(v, "GIT_INDEX_FILE="),
				strings.HasPrefix(v, "GIT_COMMON_DIR="):
				continue
			}
			cmd.Env = append(cmd.Env, v)
		}
		// JSON output needs the in-process execution path.
		cmd.Env = append(cmd.Env, "ATLAS_AGENT_CLIENT_SERVER=false", ChildEnv+"=1")
		var stdout limitedBuffer
		var stderr tailBuffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		runErr := cmd.Run()
		var res AgentResult
		if stdout.exceeded || stdout.Len() > maxAgentOutput {
			return res, errors.New("agent output exceeded the size limit")
		}
		decoded, decodeErr := decodeAgentJSON(stdout.Bytes())
		if decodeErr == nil {
			res = decoded
		}
		if runErr != nil {
			if ctx.Err() != nil {
				return res, ctx.Err()
			}
			return res, fmt.Errorf("agent process failed: %w: %s", runErr, clip(stderr.String(), 600))
		}
		if decodeErr != nil {
			return res, fmt.Errorf("agent returned invalid JSON: %w", decodeErr)
		}
		return res, nil
	}
}

// decodeAgentJSON reads the "run --json" result object.
func decodeAgentJSON(data []byte) (AgentResult, error) {
	var raw struct {
		Response         string  `json:"response"`
		SessionID        string  `json:"session_id"`
		PromptTokens     int64   `json:"prompt_tokens"`
		CompletionTokens int64   `json:"completion_tokens"`
		Cost             float64 `json:"cost"`
		ToolCalls        int     `json:"tool_calls"`
		ToolErrors       int     `json:"tool_errors"`
		FinishReason     string  `json:"finish_reason"`
		FinishError      string  `json:"finish_error"`
		Provider         string  `json:"provider"`
		Model            string  `json:"model"`
	}
	trimmed := bytes.TrimSpace(data)
	// Tolerate log lines before the object.
	if i := bytes.IndexByte(trimmed, '{'); i > 0 {
		trimmed = trimmed[i:]
	}
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return AgentResult{}, err
	}
	return AgentResult(raw), nil
}
