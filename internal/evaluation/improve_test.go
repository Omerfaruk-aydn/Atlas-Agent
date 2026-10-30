package evaluation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/errmemory"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/harnessmetrics"
	"github.com/stretchr/testify/require"
)

func TestSignTestPIsExact(t *testing.T) {
	t.Parallel()
	require.InDelta(t, 1.0, signTestP(0, 0), 1e-9)
	require.InDelta(t, 1.0, signTestP(3, 3), 1e-9)
	require.InDelta(t, 0.0625, signTestP(5, 0), 1e-9)
	require.InDelta(t, 0.03125, signTestP(6, 0), 1e-9)
	require.InDelta(t, 0.375, signTestP(4, 1), 1e-9)
	require.Equal(t, 6, minTasksForSignificance())
}

func TestWilsonIntervalStaysInsideZeroAndOne(t *testing.T) {
	t.Parallel()
	low, high := wilsonInterval(0, 5)
	require.Zero(t, low)
	require.Greater(t, high, 0.3, "five failures still leave wide room for a real pass rate")
	low, high = wilsonInterval(5, 5)
	require.Less(t, low, 0.6)
	require.InDelta(t, 1.0, high, 1e-9)
	low, high = wilsonInterval(50, 100)
	require.InDelta(t, 0.40, low, 0.01)
	require.InDelta(t, 0.60, high, 0.01)
}

func TestBootstrapIsDeterministic(t *testing.T) {
	t.Parallel()
	values := []float64{100, 100, 0, 100, 100, 100}
	lowA, highA := bootstrapMeanCI(values)
	lowB, highB := bootstrapMeanCI(values)
	require.Equal(t, lowA, lowB)
	require.Equal(t, highA, highB)
	require.Less(t, lowA, highA)
	require.Positive(t, lowA)
}

// benchRecords builds records for tasks task-0..task-n-1. basePass and
// candPass say which attempts of which task pass, per variant.
func benchRecords(tasks, repeats int, basePass, candPass func(task, attempt int) bool) []BenchRecord {
	var records []BenchRecord
	for attempt := range repeats {
		for task := range tasks {
			for _, v := range []struct {
				name string
				pass func(int, int) bool
				cost float64
			}{{ImproveBaseline, basePass, 0.10}, {ImproveCandidate, candPass, 0.12}} {
				passed := v.pass(task, attempt)
				score := 0.0
				if passed {
					score = 100
				}
				records = append(records, BenchRecord{
					Task: fmt.Sprintf("task-%d", task), Variant: v.name, Attempt: attempt,
					Passed: passed, Score: score, Cost: v.cost, Tokens: 1000,
				})
			}
		}
	}
	return records
}

func TestCompareVariantsDeclaresRealImprovement(t *testing.T) {
	t.Parallel()
	records := benchRecords(8, 3,
		func(int, int) bool { return false },
		func(task, _ int) bool { return task < 7 })
	c := CompareVariants(records, ImproveBaseline, ImproveCandidate)
	require.Equal(t, VerdictImproved, c.Verdict)
	require.Equal(t, 8, c.PairedTasks)
	require.Equal(t, 7, c.Wins)
	require.Zero(t, c.Losses)
	require.Less(t, c.SignTestP, 0.05)
	require.Positive(t, c.DeltaLow)
	require.InDelta(t, 20, c.CostDelta, 0.001)
	require.Zero(t, c.Candidate.FlakyTasks)
}

func TestCompareVariantsRefusesVerdictOnTooFewTasks(t *testing.T) {
	t.Parallel()
	// Perfect separation on four tasks is still not enough for a sign test.
	records := benchRecords(4, 3,
		func(int, int) bool { return false },
		func(int, int) bool { return true })
	c := CompareVariants(records, ImproveBaseline, ImproveCandidate)
	require.Equal(t, VerdictInconclusive, c.Verdict)
	require.Contains(t, strings.Join(c.Warnings, "\n"), "Only 4 paired tasks")
}

func TestCompareVariantsDeclaresRegression(t *testing.T) {
	t.Parallel()
	records := benchRecords(8, 3,
		func(int, int) bool { return true },
		func(task, _ int) bool { return task >= 7 })
	c := CompareVariants(records, ImproveBaseline, ImproveCandidate)
	require.Equal(t, VerdictRegressed, c.Verdict)
	require.Contains(t, strings.Join(c.Warnings, "\n"), "Ceiling")
}

func TestCompareVariantsWarnsAboutNoiseFloorAndFewRepeats(t *testing.T) {
	t.Parallel()
	// task-0 flips with the attempt: partly luck.
	flaky := func(task, attempt int) bool { return task == 0 && attempt%2 == 0 }
	c := CompareVariants(benchRecords(6, 2, flaky, flaky), ImproveBaseline, ImproveCandidate)
	joined := strings.Join(c.Warnings, "\n")
	require.Equal(t, VerdictInconclusive, c.Verdict)
	require.Equal(t, 1, c.Baseline.FlakyTasks)
	require.Contains(t, joined, "Flaky tasks")
	require.Contains(t, joined, "at most 2 time(s)")
	require.Contains(t, joined, "Every paired task tied")
	require.Greater(t, c.Baseline.RepeatStdDev, 0.0)

	none := func(int, int) bool { return false }
	c = CompareVariants(benchRecords(6, 3, none, none), ImproveBaseline, ImproveCandidate)
	require.Contains(t, strings.Join(c.Warnings, "\n"), "Floor")
}

func TestCompareVariantsExcludesProviderFailuresAndHandlesNoData(t *testing.T) {
	t.Parallel()
	records := benchRecords(6, 3, func(int, int) bool { return false }, func(int, int) bool { return true })
	for i := range records {
		if records[i].Variant == ImproveCandidate && records[i].Task == "task-0" {
			records[i].Passed, records[i].ErrorKind = false, ErrorProvider
		}
	}
	c := CompareVariants(records, ImproveBaseline, ImproveCandidate)
	require.Equal(t, 3, c.Candidate.Excluded)
	require.Equal(t, 5, c.PairedTasks, "a task with no valid candidate run cannot be paired")
	require.Contains(t, strings.Join(c.Warnings, "\n"), "excluded")

	empty := CompareVariants(nil, ImproveBaseline, ImproveCandidate)
	require.Equal(t, VerdictNoData, empty.Verdict)
}

func TestHardnessPrefersMultiFileVagueCases(t *testing.T) {
	t.Parallel()
	easy := harnessmetrics.Case{
		Request: "In internal/app/app.go rename `oldName` to newName_value",
		Files:   []harnessmetrics.CaseFile{{Path: "internal/app/app.go", Existed: true}},
	}
	hard := harnessmetrics.Case{
		Request:  "make the search feel better somehow",
		Reason:   "checks unmet after the round limit",
		Checks:   []string{"go test ./..."},
		Findings: "FAIL: TestSearch expected 3 got 2\nerror: cannot find package",
		Files: []harnessmetrics.CaseFile{
			{Path: "internal/search/a.go", Existed: true},
			{Path: "internal/search/a_test.go", Existed: true},
			{Path: "cmd/main.go", Existed: true},
		},
	}
	easyScore, _ := HardnessOf(easy, nil)
	hardScore, reasons := HardnessOf(hard, nil)
	require.Less(t, easyScore, DefaultMinHardness)
	require.GreaterOrEqual(t, hardScore, DefaultMinHardness)
	require.Contains(t, strings.Join(reasons, ";"), "multi-file")
	require.Contains(t, strings.Join(reasons, ";"), "vague")

	sig, _, ok := errmemory.Signature(hard.Findings)
	require.True(t, ok)
	boosted, _ := HardnessOf(hard, map[string]bool{sig: true})
	require.Equal(t, hardScore+2, boosted)
}

// minePerm creates a git project holding the end state of a hard case.
func mineProject(t *testing.T) harnessmetrics.Case {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	project := t.TempDir()
	git := func(args ...string) string {
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = project
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	for path, content := range map[string]string{
		"pkg/a.go":      "package pkg // end",
		"pkg/a_test.go": "package pkg // end state test",
		"cmd/main.go":   "package main // end",
	} {
		require.NoError(t, os.MkdirAll(filepath.Join(project, filepath.Dir(path)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(project, path), []byte(content), 0o644))
	}
	git("add", "-A")
	git("commit", "-q", "-m", "end")
	return harnessmetrics.Case{
		ID: "20261016-141500-abcd1234", Request: "make the search better somehow", WorkingDir: project,
		Commit: git("rev-parse", "HEAD"), Reason: "checks unmet", Checks: []string{"make test"},
		Findings: "error: tests fail",
		Files: []harnessmetrics.CaseFile{
			{Path: "pkg/a.go", Before: "package pkg // start", Existed: true},
			{Path: "pkg/a_test.go", Before: "package pkg // start test", Existed: true},
			{Path: "cmd/main.go", Before: "package main // start", Existed: true},
		},
	}
}

func TestMineCorpusKeepsOnlyRedAtStartTasks(t *testing.T) {
	t.Parallel()
	c := mineProject(t)

	failing := func(context.Context, string, string, []string) ([]byte, error) {
		return []byte("FAIL: expected 3 got 2"), errors.New("exit status 1")
	}
	suite := filepath.Join(t.TempDir(), "mined")
	mined, err := MineCorpus(t.Context(), []harnessmetrics.Case{c}, suite, MineOptions{Runner: failing})
	require.NoError(t, err)
	require.Len(t, mined, 1)
	require.True(t, mined[0].Valid, mined[0].Rejected)
	require.Equal(t, []string{"pkg/a_test.go"}, mined[0].HiddenTests)

	// The suite loads as a normal bench suite, with the end-state test hidden.
	tasks, err := LoadBenchSuite(suite, nil)
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	hidden, err := os.ReadFile(filepath.Join(suite, c.ID, "hidden", "pkg", "a_test.go"))
	require.NoError(t, err)
	require.Contains(t, string(hidden), "end state test")
	visible, err := os.ReadFile(filepath.Join(suite, c.ID, "repo", "pkg", "a_test.go"))
	require.NoError(t, err)
	require.Contains(t, string(visible), "start test")

	// Mining again re-validates the existing task instead of failing.
	again, err := MineCorpus(t.Context(), []harnessmetrics.Case{c}, suite, MineOptions{Runner: failing})
	require.NoError(t, err)
	require.True(t, again[0].Valid, again[0].Rejected)
}

func TestMineCorpusRejectsChecksThatAreNotRed(t *testing.T) {
	t.Parallel()
	c := mineProject(t)
	cases := map[string]CommandRunner{
		"already passes": func(context.Context, string, string, []string) ([]byte, error) { return nil, nil },
		"program missing": func(context.Context, string, string, []string) ([]byte, error) {
			return []byte("bash: make: command not found"), errors.New("exit status 127")
		},
		"flaky": func() CommandRunner {
			calls := 0
			return func(context.Context, string, string, []string) ([]byte, error) {
				calls++
				if calls == 1 {
					return []byte("FAIL"), errors.New("exit status 1")
				}
				return []byte("ok"), nil
			}
		}(),
	}
	for name, runner := range cases {
		suite := filepath.Join(t.TempDir(), "mined")
		mined, err := MineCorpus(t.Context(), []harnessmetrics.Case{c}, suite, MineOptions{Runner: runner})
		require.NoError(t, err, name)
		require.False(t, mined[0].Valid, name)
		require.NotEmpty(t, mined[0].Rejected, name)
		require.NoDirExists(t, filepath.Join(suite, c.ID), "%s: a rejected task must not stay in the suite", name)
	}
}

func TestMineCorpusSkipsEasyCasesAndRespectsLimit(t *testing.T) {
	t.Parallel()
	c := mineProject(t)
	easy := harnessmetrics.Case{ID: "easy-one", Request: "In internal/app/app.go rename `oldName` to newName_value", Commit: c.Commit, WorkingDir: c.WorkingDir, Checks: []string{"x"}}
	failing := func(context.Context, string, string, []string) ([]byte, error) {
		return []byte("FAIL"), errors.New("exit status 1")
	}
	suite := filepath.Join(t.TempDir(), "mined")
	mined, err := MineCorpus(t.Context(), []harnessmetrics.Case{easy, c}, suite, MineOptions{Runner: failing})
	require.NoError(t, err)
	require.Equal(t, c.ID, mined[0].ID, "hardest first")
	require.True(t, mined[0].Valid)
	require.Contains(t, mined[1].Rejected, "below the minimum")
	require.NoDirExists(t, filepath.Join(suite, "easy-one"))

	limited, err := MineCorpus(t.Context(), []harnessmetrics.Case{c}, filepath.Join(t.TempDir(), "s"), MineOptions{Runner: failing, MaxTasks: 1, MinHardness: 1})
	require.NoError(t, err)
	require.True(t, limited[0].Valid)
}

func TestRunImproveComparesWithMockAgentsAndWritesProposal(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	suite := t.TempDir()
	for i := range 7 {
		writeBenchTask(t, suite, fmt.Sprintf("task-%d", i))
	}
	dir := t.TempDir()
	options := ImproveOptions{
		Suite: suite, Model: "mock-model", Program: "atlas", Output: filepath.Join(dir, "results.json"),
		CandidateConfig: []byte(`{"options":{"note":"fixes"}}`), Repeats: 3, Runner: fakeBenchRunner,
	}
	result, err := RunImprove(t.Context(), options)
	require.NoError(t, err)
	require.Len(t, result.Records, 7*2*3)
	require.Equal(t, VerdictImproved, result.Comparison.Verdict)
	require.Equal(t, 7, result.Comparison.Wins)

	proposal := BuildProposal(options, nil, result, time.Unix(1_700_000_000, 0))
	require.False(t, proposal.Applied)
	require.Contains(t, proposal.Recommendation, "adopt it manually")
	path := filepath.Join(dir, "proposal.json")
	require.NoError(t, WriteProposal(path, proposal))
	var decoded Proposal
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.False(t, decoded.Applied)
	require.Equal(t, VerdictImproved, decoded.Comparison.Verdict)
	require.Equal(t, 3, decoded.Repeats)
	require.NotEmpty(t, decoded.Limits)

	text := FormatProposal(proposal)
	require.Contains(t, text, "IMPROVED")
	require.Contains(t, text, "Nothing was applied.")
}

func TestRunImproveRejectsMeaninglessComparisons(t *testing.T) {
	t.Parallel()
	suite := t.TempDir()
	writeBenchTask(t, suite, "one")
	base := ImproveOptions{Suite: suite, Model: "m", Output: filepath.Join(t.TempDir(), "r.json"), Runner: fakeBenchRunner}

	_, err := RunImprove(t.Context(), base)
	require.ErrorContains(t, err, "candidate config is required")

	same := base
	same.BaselineConfig = []byte(`{"options": {"a": 1}}`)
	same.CandidateConfig = []byte(`{"options":{"a":1}}`)
	_, err = RunImprove(t.Context(), same)
	require.ErrorContains(t, err, "identical")
}
