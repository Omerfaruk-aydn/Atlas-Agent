package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/nodes"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/taskstate"
	"github.com/stretchr/testify/require"
)

func TestLocalPlanCommandHelper(t *testing.T) {
	if os.Getenv("ATLAS_LOCAL_PLAN_HELPER") != "1" {
		return
	}
	if os.Getenv("ATLAS_LOCAL_PLAN_FAIL") == "1" {
		os.Exit(7)
	}
	if os.Getenv("ATLAS_LOCAL_PLAN_WAIT") == "1" {
		time.Sleep(5 * time.Second)
	}
	f, err := os.OpenFile(os.Getenv("ATLAS_LOCAL_PLAN_FILE"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		os.Exit(2)
	}
	_, _ = f.WriteString("x")
	_ = f.Close()
	os.Exit(0)
}

func localPlanFixture(t *testing.T) (string, string, string, []nodes.PlanStep) {
	t.Helper()
	root, state := t.TempDir(), t.TempDir()
	path := filepath.Join(root, "effects.txt")
	t.Setenv("ATLAS_LOCAL_PLAN_HELPER", "1")
	t.Setenv("ATLAS_LOCAL_PLAN_FILE", path)
	exe, err := os.Executable()
	require.NoError(t, err)
	steps := []nodes.PlanStep{{ID: "write", Node: "local", Action: "exec", Argv: []string{exe, "-test.run=^TestLocalPlanCommandHelper$"}}}
	return root, state, path, steps
}

func TestLocalPlanReplayIsPersistentAndDeduplicated(t *testing.T) {
	root, state, path, steps := localPlanFixture(t)
	var wg sync.WaitGroup
	results := make(chan nodes.PlanResult, 2)
	errs := make(chan error, 2)
	for range 2 {
		wg.Go(func() {
			res, err := RunNodePlan(t.Context(), root, state, "same-run", steps)
			results <- res
			errs <- err
		})
	}
	wg.Wait()
	for range 2 {
		require.NoError(t, <-errs)
		require.Equal(t, taskstate.Completed, (<-results).State)
	}
	res, err := RunNodePlan(t.Context(), root, state, "same-run", steps)
	require.NoError(t, err)
	require.True(t, res.Steps[0].Replayed)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "x", string(data))
	steps[0].Argv = append(steps[0].Argv, "different")
	res, err = RunNodePlan(t.Context(), root, state, "same-run", steps)
	require.NoError(t, err)
	require.Equal(t, "operation_conflict", res.Steps[0].ErrorCode)
}

func TestLocalPlanDoesNotReplayInterruptedEffects(t *testing.T) {
	root, state, path, steps := localPlanFixture(t)
	opID := nodes.OperationID("cli", "interrupted-run", steps[0].ID)
	hash := nodes.RequestHash(struct {
		Root    string
		Session string
		Step    nodes.PlanStep
	}{filepath.Clean(root), "cli", steps[0]})
	record := localStepRecord{RequestHash: hash, Result: nodes.StepResult{OperationID: opID, State: taskstate.Running, Effect: "unknown"}}
	journal := filepath.Join(state, "runs", "local-operations", nodes.RequestHash(opID)+".json")
	require.NoError(t, taskstate.WriteJSONAtomic(journal, record))
	res, err := RunNodePlan(t.Context(), root, state, "interrupted-run", steps)
	require.NoError(t, err)
	require.Equal(t, taskstate.Failed, res.State)
	require.Equal(t, "interrupted_operation", res.Steps[0].ErrorCode)
	require.NoFileExists(t, path)
	data, err := os.ReadFile(journal)
	require.NoError(t, err)
	require.True(t, json.Valid(data))
}

func TestLocalPlanReportsCommandExitWithoutFalseTimeout(t *testing.T) {
	root, state, _, steps := localPlanFixture(t)
	t.Setenv("ATLAS_LOCAL_PLAN_FAIL", "1")
	res, err := RunNodePlan(context.Background(), root, state, "failure-run", steps)
	require.NoError(t, err)
	require.Equal(t, taskstate.Failed, res.State)
	require.Equal(t, 7, *res.Steps[0].ExitCode)
	require.Equal(t, "exit status 7", res.Steps[0].Error)
}

func TestLocalPlanDistinguishesTimeoutAndCancellation(t *testing.T) {
	root, state, path, steps := localPlanFixture(t)
	t.Setenv("ATLAS_LOCAL_PLAN_WAIT", "1")
	steps[0].TimeoutMS = 50
	res, err := RunNodePlan(t.Context(), root, state, "timeout-run", steps)
	require.NoError(t, err)
	require.Equal(t, taskstate.Failed, res.State)
	require.Contains(t, res.Steps[0].Error, "timed out")
	require.NoFileExists(t, path)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	res, err = RunNodePlan(ctx, root, state, "canceled-run", steps)
	require.NoError(t, err)
	require.Equal(t, taskstate.Canceled, res.State)
	require.NoFileExists(t, path)
}
