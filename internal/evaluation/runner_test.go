package evaluation

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/stretchr/testify/require"
)

func TestLiveRunsUseSeparateWorktreesAndExternalCriterionResults(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	root := t.TempDir()
	for _, args := range [][]string{{"init"}, {"config", "user.name", "Test"}, {"config", "user.email", "test@example.invalid"}, {"-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "test: fixture"}} {
		_, err := engineering.Git(t.Context(), root, args...)
		require.NoError(t, err)
	}
	scenarios, err := Scenarios()
	require.NoError(t, err)
	scenario := scenarios[0]
	c := LiveCase{Scenario: scenario.ID, Prompt: "Concrete fixture task"}
	for _, id := range scenario.Criteria {
		c.Checks = append(c.Checks, LiveCheck{Criterion: id, Command: []string{"checker", id}})
	}
	m := LiveManifest{Repository: root, Models: []string{"provider/a", "provider/b"}, Cases: []LiveCase{c}, PromptVersion: "test-v1", Role: "backend", Repeats: 1}
	var dirs []string
	checkerCalls := 0
	runner := func(ctx context.Context, dir, program string, args []string) ([]byte, error) {
		if program == "atlas-fixture" {
			dirs = append(dirs, dir)
			require.Contains(t, args, "--json")
			require.Contains(t, args, "--role")
			require.Contains(t, args, "backend")
			require.NotContains(t, args, "--yolo")
			if len(dirs) == 1 {
				store := engineering.NewStore(filepath.Join(dir, ".atlas-eval-state"))
				require.NoError(t, store.Charge(ctx, "fixture", "", 80, 0.3, 0))
			}
			return []byte(`{"session_id":"fixture","prompt_tokens":12,"completion_tokens":8,"cost":0.1}`), nil
		}
		checkerCalls++
		if args[0] == scenario.Criteria[0] {
			return []byte("fail"), errors.New("criterion failed")
		}
		return []byte("pass"), nil
	}
	output := filepath.Join(t.TempDir(), "results.json")
	records, err := RunLive(t.Context(), m, "atlas-fixture", output, false, runner)
	require.NoError(t, err)
	require.Len(t, records, 2)
	require.Equal(t, "backend", records[0].Role)
	require.Positive(t, records[0].RecordedAt)
	require.NotEqual(t, dirs[0], dirs[1])
	require.Equal(t, len(scenario.Criteria)*2, checkerCalls)
	require.False(t, records[0].Checks[0].Passed)
	require.EqualValues(t, 80, records[0].Tokens)
	require.InDelta(t, 0.3, records[0].Cost, 0.00001)
	require.Equal(t, "cumulative_runtime_ledger", records[0].MetricsSource)
	require.EqualValues(t, 20, records[1].Tokens)
	require.Equal(t, "session_context_fallback", records[1].MetricsSource)
	data, err := os.ReadFile(output)
	require.NoError(t, err)
	var runs []Run
	require.NoError(t, json.Unmarshal(data, &runs))
	comparison, err := Compare(scenarios, runs)
	require.NoError(t, err)
	require.Len(t, comparison, 2)
	require.Equal(t, 1, comparison[0].Report.UnobservedClaims)
}

func TestLiveManifestRejectsIncompleteChecksBeforeLaunching(t *testing.T) {
	require.Error(t, ValidateLive(LiveManifest{Repository: t.TempDir(), Models: []string{"model"}, PromptVersion: "v1", Cases: []LiveCase{{Scenario: "ui-states", Prompt: "task"}}}))
}

func TestLiveCommandRunnerObservesActualExitCodeAndIsolatedEnvironment(t *testing.T) {
	program, err := os.Executable()
	require.NoError(t, err)
	dir := t.TempDir()
	t.Setenv("GIT_DIR", "inherited-repository")
	t.Setenv("GIT_WORK_TREE", "inherited-worktree")
	t.Setenv("ATLAS_LIVE_COMMAND_FIXTURE", "pass")
	output, err := RunCommand(t.Context(), dir, program, []string{"-test.run=^TestLiveCommandFixture$"})
	require.NoError(t, err)
	require.Contains(t, string(output), "observed execution")
	t.Setenv("ATLAS_LIVE_COMMAND_FIXTURE", "fail")
	output, err = RunCommand(t.Context(), dir, program, []string{"-test.run=^TestLiveCommandFixture$"})
	require.Error(t, err)
	require.Contains(t, string(output), "observed execution")
}

func TestLiveCommandFixture(t *testing.T) {
	mode := os.Getenv("ATLAS_LIVE_COMMAND_FIXTURE")
	if mode == "" {
		t.Skip("External execution fixture")
	}
	if os.Getenv("GIT_DIR") != "" || os.Getenv("GIT_WORK_TREE") != "" || os.Getenv("ATLAS_AGENT_CLIENT_SERVER") != "false" {
		os.Exit(5)
	}
	_, _ = os.Stdout.WriteString("observed execution\n")
	if mode == "fail" {
		os.Exit(3)
	}
	os.Exit(0)
}

func TestProviderFailureCannotBecomeBenchmarkSuccessFromExistingFixture(t *testing.T) {
	t.Parallel()
	scenarios, err := Scenarios()
	require.NoError(t, err)
	run := Run{Scenario: scenarios[0].ID, ExecutionError: "Provider request failed"}
	for _, criterion := range scenarios[0].Criteria {
		run.Checks = append(run.Checks, Check{Criterion: criterion, Passed: true, Evidence: "Existing fixture passes"})
	}
	report, err := Score(scenarios, []Run{run})
	require.NoError(t, err)
	require.Zero(t, report.Successful)
	require.Zero(t, report.AcceptanceRate)
	require.Equal(t, 1, report.ExecutionFailures)
}
