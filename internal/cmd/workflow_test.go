package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/stretchr/testify/require"
)

func TestWorkflowCLIManagesExhaustedBudgetAndRecoveryWithoutProvider(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := engineering.NewStore(dir)
	require.NoError(t, store.Charge(t.Context(), "session", "task", 50, 0.2, 0))
	op, err := store.Begin(t.Context(), "session", "call", "bash", `{}`, "task")
	require.NoError(t, err)
	var output bytes.Buffer
	run := func(args ...string) error {
		output.Reset()
		command := newWorkflowCommand()
		command.PersistentFlags().String("data-dir", dir, "test")
		command.SetOut(&output)
		command.SetErr(&output)
		command.SetArgs(args)
		return command.Execute()
	}
	require.NoError(t, run("budget", "session", "--tokens", "50", "--duration", "30m"))
	require.ErrorContains(t, store.Check(t.Context(), "session", "task"), "exhausted")
	require.NoError(t, run("status", "session"))
	var state engineering.State
	require.NoError(t, json.Unmarshal(output.Bytes(), &state))
	require.EqualValues(t, 50, state.Usage.Tokens)
	require.EqualValues(t, 1800000, state.Limits.MaxDurationMS)
	require.NoError(t, run("recover", "session", op, "abandoned", "--evidence", "Inspected process and repository; no replay"))
	require.NoError(t, run("budget", "session", "--tokens", "100"))
	require.NoError(t, store.Check(t.Context(), "session", "task"))
	require.Error(t, run("budget", "session", "--cost", "-1"))
	require.Error(t, run("recover", "session", op, "completed"))
	state, err = store.Read(t.Context(), "session")
	require.NoError(t, err)
	require.EqualValues(t, 50, state.Usage.Tokens)
	require.Equal(t, "abandoned", state.Operations[0].Status)
}

func TestWorkflowOrientationCLIUsesNoProviderAndDoesNotExecuteScripts(t *testing.T) {
	t.Parallel()
	originalCwd, err := os.Getwd()
	require.NoError(t, err)
	root, dir := t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module fixture\n"), 0o644))
	var output bytes.Buffer
	run := func(args ...string) error {
		output.Reset()
		command := newWorkflowCommand()
		command.PersistentFlags().String("data-dir", dir, "test")
		command.PersistentFlags().String("cwd", root, "test")
		command.SetOut(&output)
		command.SetErr(&output)
		command.SetArgs(args)
		return command.Execute()
	}
	require.NoError(t, run("profiles"))
	var profiles []engineering.WorkProfile
	require.NoError(t, json.Unmarshal(output.Bytes(), &profiles))
	require.Len(t, profiles, 5)
	require.NoError(t, run("prepare"))
	var brief engineering.ProjectBrief
	require.NoError(t, json.Unmarshal(output.Bytes(), &brief))
	require.Contains(t, brief.Stacks, "go")
	require.NoError(t, run("knowledge"))
	var view struct {
		Brief *engineering.ProjectBrief `json:"brief"`
	}
	require.NoError(t, json.Unmarshal(output.Bytes(), &view))
	require.NotNil(t, view.Brief)
	require.Equal(t, brief.MapFingerprint, view.Brief.MapFingerprint)
	currentCwd, err := os.Getwd()
	require.NoError(t, err)
	require.Equal(t, originalCwd, currentCwd, "read-only workflow commands must not change process cwd")
}
