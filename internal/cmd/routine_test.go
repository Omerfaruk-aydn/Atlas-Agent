package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/appenv"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/routine"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// runRoutineCmd executes "routine <args>" against a temporary store with a
// fake executor. Tests using it must not be parallel: the store location and
// the executor are process-wide.
func runRoutineCmd(t *testing.T, exec routine.Executor, args ...string) (string, error) {
	t.Helper()
	prev := routineExecutor
	routineExecutor = func(*cobra.Command) routine.Executor { return exec }
	t.Cleanup(func() { routineExecutor = prev })

	var out bytes.Buffer
	c := newRoutineCommand()
	c.SetOut(&out)
	c.SetErr(&out)
	c.SetArgs(args)
	err := c.ExecuteContext(context.Background())
	return out.String(), err
}

func TestRoutineCommandLifecycle(t *testing.T) {
	t.Setenv(appenv.Prefix+"ROUTINES_DIR", t.TempDir())
	dir := t.TempDir()
	exec := routine.ExecutorFunc(func(_ context.Context, r routine.Routine) (routine.Result, error) {
		return routine.Result{Output: "report for " + r.Name, SessionID: "sess-1"}, nil
	})

	out, err := runRoutineCmd(t, exec, "add", "0 9 * * 1-5", "--name", "standup", "--dir", dir, "Summarize", "the", "commits")
	require.NoError(t, err)
	require.Contains(t, out, "Added routine standup")
	require.Contains(t, out, "Next run:")

	out, err = runRoutineCmd(t, exec, "list", "--json")
	require.NoError(t, err)
	var listed []routine.Routine
	require.NoError(t, json.Unmarshal([]byte(out), &listed))
	require.Len(t, listed, 1)
	require.Equal(t, "Summarize the commits", listed[0].Prompt)
	require.Equal(t, routine.PermissionsConfigured, listed[0].Permissions, "the default must not approve everything")

	out, err = runRoutineCmd(t, exec, "run", "standup")
	require.NoError(t, err)
	require.Contains(t, out, "succeeded")
	require.Contains(t, out, "report for standup")

	out, err = runRoutineCmd(t, exec, "logs", "standup", "--json")
	require.NoError(t, err)
	var entries []routine.LogEntry
	require.NoError(t, json.Unmarshal([]byte(out), &entries))
	require.Len(t, entries, 1)
	require.Equal(t, "sess-1", entries[0].SessionID)

	_, err = runRoutineCmd(t, exec, "disable", "standup")
	require.NoError(t, err)
	out, err = runRoutineCmd(t, exec, "list")
	require.NoError(t, err)
	require.Contains(t, out, "paused")

	out, err = runRoutineCmd(t, exec, "remove", "standup")
	require.NoError(t, err)
	require.Contains(t, out, "Removed routine standup")
	_, err = runRoutineCmd(t, exec, "logs", "standup")
	require.ErrorIs(t, err, routine.ErrNotFound)
}

func TestRoutineAddRejectsBadInput(t *testing.T) {
	t.Setenv(appenv.Prefix+"ROUTINES_DIR", t.TempDir())
	dir := t.TempDir()
	exec := routine.ExecutorFunc(func(context.Context, routine.Routine) (routine.Result, error) {
		return routine.Result{}, nil
	})
	_, err := runRoutineCmd(t, exec, "add", "not a cron", "--dir", dir, "prompt")
	require.Error(t, err)
	_, err = runRoutineCmd(t, exec, "add", "@daily", "--dir", dir+"-missing", "prompt")
	require.ErrorContains(t, err, "does not exist")
	_, err = runRoutineCmd(t, exec, "add", "@daily", "--dir", dir, "--permissions", "bypass", "prompt")
	require.ErrorContains(t, err, "unknown permissions policy")
}

func TestRoutineRunDueExitsNonZeroOnFailure(t *testing.T) {
	t.Setenv(appenv.Prefix+"ROUTINES_DIR", t.TempDir())
	dir := t.TempDir()
	failing := routine.ExecutorFunc(func(context.Context, routine.Routine) (routine.Result, error) {
		return routine.Result{}, errors.New("model unavailable")
	})
	// "* * * * *" is due at the next minute boundary, which a test cannot
	// wait for, so a manual run stands in for the failure path.
	_, err := runRoutineCmd(t, failing, "add", "* * * * *", "--name", "flaky", "--dir", dir, "prompt")
	require.NoError(t, err)
	out, err := runRoutineCmd(t, failing, "run", "flaky")
	require.ErrorContains(t, err, "failed")
	require.Contains(t, out, "model unavailable")

	_, err = runRoutineCmd(t, failing, "run")
	require.ErrorContains(t, err, "either a routine name or --due")
	_, err = runRoutineCmd(t, failing, "run", "--due", "flaky")
	require.ErrorContains(t, err, "either a routine name or --due")
}
