package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/attempt"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/stretchr/testify/require"
)

func runAttemptToolCall(t *testing.T, tool fantasy.AgentTool, params AttemptParams) fantasy.ToolResponse {
	t.Helper()
	input, err := json.Marshal(params)
	require.NoError(t, err)
	resp, err := tool.Run(t.Context(), fantasy.ToolCall{ID: "call-1", Name: AttemptToolName, Input: string(input)})
	require.NoError(t, err)
	return resp
}

// storedAttemptRun produces a persisted run with one winning attempt that
// changes calc.txt, using fake agents.
func storedAttemptRun(t *testing.T) (root, stateDir string, report *attempt.Report) {
	t.Helper()
	root = toolGitRepo(t)
	writeDeadCodeFile(t, root, "calc.txt", "todo\n")
	toolCommit(t, root, "chore: init")
	stateDir = filepath.Join(t.TempDir(), "attempts")
	report, err := attempt.Run(t.Context(), attempt.Options{
		Root:     root,
		StateDir: stateDir,
		WorkDir:  filepath.Join(t.TempDir(), "work"),
		Task:     "fix calc",
		N:        1,
		Verify:   []string{"true"},
		Agent: func(_ context.Context, job attempt.AgentJob) (attempt.AgentResult, error) {
			return attempt.AgentResult{}, os.WriteFile(filepath.Join(job.Dir, "calc.txt"), []byte("fixed\n"), 0o644)
		},
		Check: func(_ context.Context, _, command string, _ time.Duration) attempt.CheckResult {
			return attempt.CheckResult{Command: command, Passed: true}
		},
	})
	require.NoError(t, err)
	require.Equal(t, 1, report.Winner)
	return root, stateDir, report
}

func TestAttemptToolValidatesRunArguments(t *testing.T) {
	root := t.TempDir()
	tool := NewAttemptTool(root, ".atlas", nil, nil)

	resp := runAttemptToolCall(t, tool, AttemptParams{})
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "task is required")

	resp = runAttemptToolCall(t, tool, AttemptParams{Task: "x", N: 9})
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "between 1 and")

	resp = runAttemptToolCall(t, tool, AttemptParams{Action: "explode"})
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "unknown action")
}

func TestAttemptToolRefusesToNest(t *testing.T) {
	t.Setenv(attempt.ChildEnv, "1")
	tool := NewAttemptTool(t.TempDir(), ".atlas", nil, nil)
	resp := runAttemptToolCall(t, tool, AttemptParams{Task: "x"})
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "inside an attempt")
}

func TestAttemptToolListShowAndApply(t *testing.T) {
	root, stateDir, report := storedAttemptRun(t)
	// The tool derives the state directory from the data directory.
	tool := NewAttemptTool(root, filepath.Dir(stateDir), permission.NewPermissionService(root, true, nil), nil)

	resp := runAttemptToolCall(t, tool, AttemptParams{Action: "list"})
	require.False(t, resp.IsError)
	require.Contains(t, resp.Content, report.ID)

	resp = runAttemptToolCall(t, tool, AttemptParams{Action: "show", RunID: report.ID})
	require.False(t, resp.IsError)
	require.Contains(t, resp.Content, "[winner]")

	resp = runAttemptToolCall(t, tool, AttemptParams{Action: "apply", RunID: report.ID, Attempt: "Z"})
	require.True(t, resp.IsError)

	resp = runAttemptToolCall(t, tool, AttemptParams{Action: "apply", RunID: report.ID})
	require.False(t, resp.IsError, resp.Content)
	data, err := os.ReadFile(filepath.Join(root, "calc.txt"))
	require.NoError(t, err)
	require.Contains(t, string(data), "fixed")

	resp = runAttemptToolCall(t, tool, AttemptParams{Action: "apply", RunID: report.ID})
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "already applied")
}

func TestAttemptToolApplyNeedsApproval(t *testing.T) {
	root, stateDir, report := storedAttemptRun(t)
	permissions := permission.NewPermissionService(root, false, nil)
	events := permissions.Subscribe(t.Context())
	go func() {
		for event := range events {
			permissions.Deny(event.Payload)
		}
	}()
	tool := NewAttemptTool(root, filepath.Dir(stateDir), permissions, nil)

	resp := runAttemptToolCall(t, tool, AttemptParams{Action: "apply", RunID: report.ID})
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "permission denied")
	data, err := os.ReadFile(filepath.Join(root, "calc.txt"))
	require.NoError(t, err)
	require.NotContains(t, string(data), "fixed")
}
