package evaluation

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// writeBenchTask creates a one-file task. The agent must turn a.txt from
// "broken" into "fixed"; the hidden expected.txt holds "fixed".
func writeBenchTask(t *testing.T, suite, id string) {
	t.Helper()
	dir := filepath.Join(suite, id)
	for path, content := range map[string]string{
		"task.json":           `{"id":"` + id + `","prompt":"Fix a.txt.","check":["verify"]}`,
		"repo/a.txt":          "broken",
		"hidden/expected.txt": "fixed",
		"solution/a.txt":      "fixed",
	} {
		full := filepath.Join(dir, filepath.FromSlash(path))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
	}
}

// fakeBenchRunner stands in for both the agent ("atlas") and the check
// ("verify"). What the fake agent does depends on the variant config that
// RunBench wrote into the workspace.
func fakeBenchRunner(_ context.Context, dir, program string, args []string) ([]byte, error) {
	switch program {
	case "verify":
		got, _ := os.ReadFile(filepath.Join(dir, "a.txt"))
		want, _ := os.ReadFile(filepath.Join(dir, "expected.txt"))
		if string(got) != string(want) {
			return []byte("a.txt is " + string(got)), errors.New("exit status 1")
		}
		return []byte("ok"), nil
	case "atlas":
		config, _ := os.ReadFile(filepath.Join(dir, ".atlas.json"))
		switch {
		case strings.Contains(string(config), "fixes"):
			_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte("fixed"), 0o644)
		case strings.Contains(string(config), "cheats"):
			// Rewrites the check to match its own wrong answer.
			_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte("wrong"), 0o644)
			_ = os.WriteFile(filepath.Join(dir, "expected.txt"), []byte("wrong"), 0o644)
		}
		return []byte(`{"session_id":"s","prompt_tokens":10,"completion_tokens":5,"cost":0.02,"tool_calls":4,"provider_steps":3}`), nil
	}
	return nil, errors.New("unexpected program " + program)
}

func TestRunBenchComparesVariantsAndIgnoresTamperedChecks(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}

	suite := t.TempDir()
	writeBenchTask(t, suite, "one")
	writeBenchTask(t, suite, "two")
	manifest := BenchManifest{
		Suite: suite,
		Model: "test-model",
		Variants: []BenchVariant{
			{Name: "baseline"},
			{Name: "gate", Config: []byte(`{"options":{"note":"fixes"}}`)},
			{Name: "cheater", Config: []byte(`{"options":{"note":"cheats"}}`)},
		},
		Repeats: 2,
	}
	output := filepath.Join(t.TempDir(), "results.json")

	records, err := RunBench(t.Context(), manifest, "atlas", output, false, fakeBenchRunner)
	require.NoError(t, err)
	require.Len(t, records, 2*2*3)

	// Variants alternate inside each task of an attempt.
	require.Equal(t, []string{"baseline", "gate", "cheater"}, []string{records[0].Variant, records[1].Variant, records[2].Variant})
	require.Equal(t, "one", records[0].Task)
	require.Equal(t, "two", records[3].Task)
	require.Equal(t, 0, records[0].Attempt)
	require.Equal(t, 1, records[6].Attempt)

	byVariant := map[string]BenchSummary{}
	for _, s := range SummarizeBench(records) {
		byVariant[s.Variant] = s
	}
	for _, r := range records {
		require.Positive(t, r.DurationMS, "duration is recorded for %s/%s", r.Variant, r.Task)
	}
	require.Equal(t, 0, byVariant["baseline"].Passed)
	require.Equal(t, 4, byVariant["gate"].Passed)
	require.Equal(t, 1.0, byVariant["gate"].PassRate)
	require.Equal(t, 0, byVariant["cheater"].Passed, "overwriting the check must not make it pass")
	require.InDelta(t, 0.02, byVariant["gate"].MeanCost, 1e-9)
	require.InDelta(t, 15, byVariant["gate"].MeanTokens, 1e-9)

	var saved []BenchRecord
	require.NoError(t, ReadJSONFile(output, &saved))
	require.Len(t, saved, len(records))
	for _, r := range records {
		require.DirExists(t, r.Workspace, "workspaces are preserved for inspection")
	}
}

// A check program missing from PATH must stop the run before any agent
// starts, not turn every run into a silent failure.
func TestRunBenchRefusesUnavailableCheckProgram(t *testing.T) {
	t.Parallel()

	suite := t.TempDir()
	writeBenchTask(t, suite, "one")
	manifest := BenchManifest{Suite: suite, Model: "m", Variants: []BenchVariant{{Name: "baseline"}}}
	output := filepath.Join(t.TempDir(), "results.json")

	_, err := RunBench(t.Context(), manifest, "atlas", output, false, nil)
	require.ErrorContains(t, err, "not available on PATH")
	require.NoFileExists(t, output)
}

// After a hidden test is fixed, preserved workspaces can be judged again
// without running any agent.
func TestRescoreBenchJudgesPreservedWorkspacesAgain(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}

	suite := t.TempDir()
	writeBenchTask(t, suite, "one")
	manifest := BenchManifest{Suite: suite, Model: "m", Variants: []BenchVariant{{Name: "gate", Config: []byte(`{"note":"fixes"}`)}}}
	records, err := RunBench(t.Context(), manifest, "atlas", filepath.Join(t.TempDir(), "results.json"), false, fakeBenchRunner)
	require.NoError(t, err)
	require.True(t, records[0].Passed)

	// The suite author tightens the hidden expectation; the old record is stale.
	require.NoError(t, os.WriteFile(filepath.Join(suite, "one", "hidden", "expected.txt"), []byte("fixed twice"), 0o644))
	rescored, err := RescoreBench(t.Context(), suite, records, fakeBenchRunner)
	require.NoError(t, err)
	require.False(t, rescored[0].Passed)
	require.True(t, records[0].Passed, "the input records are not modified")

	// A record whose workspace is gone cannot be judged again.
	gone := []BenchRecord{{Task: "one", Workspace: filepath.Join(t.TempDir(), "missing")}}
	_, err = RescoreBench(t.Context(), suite, gone, fakeBenchRunner)
	require.ErrorContains(t, err, "not available")
}

func TestSummarizeBenchExcludesProviderFailures(t *testing.T) {
	t.Parallel()

	summaries := SummarizeBench([]BenchRecord{
		{Task: "a", Variant: "base", Passed: true},
		{Task: "a", Variant: "base", ErrorKind: ErrorProvider},
		{Task: "a", Variant: "base", ErrorKind: ErrorAgent},
		{Task: "a", Variant: "next", Passed: true},
		{Task: "b", Variant: "next", Passed: true},
	})
	require.Len(t, summaries, 2)
	base, next := summaries[0], summaries[1]
	require.Equal(t, 3, base.Runs)
	require.Equal(t, 1, base.ProviderFails)
	require.Equal(t, 2, base.Valid)
	require.Equal(t, 0.5, base.PassRate)
	require.Equal(t, 1.0, next.PassRate)

	report := FormatBenchReport(summaries)
	require.Contains(t, report, "1 provider failures excluded")
	require.Contains(t, report, "next: pass rate +50 points")
	require.Contains(t, report, "Small samples are noisy")
}

// A model that never answers (404, expired login) must show up as a setup
// problem, not as an agent that failed every task.
func TestRunBenchSeparatesRunsThatNeverReachedTheModel(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}

	suite := t.TempDir()
	writeBenchTask(t, suite, "one")
	manifest := BenchManifest{Suite: suite, Model: "missing-model", Variants: []BenchVariant{{Name: "baseline"}}}
	deadModel := func(ctx context.Context, dir, program string, args []string) ([]byte, error) {
		if program == "atlas" {
			return nil, errors.New("exit status 1: 404 NOT_FOUND Requested entity was not found")
		}
		return fakeBenchRunner(ctx, dir, program, args)
	}

	records, err := RunBench(t.Context(), manifest, "atlas", filepath.Join(t.TempDir(), "results.json"), false, deadModel)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, benchErrorUnreached, records[0].ErrorKind)

	summaries := SummarizeBench(records)
	require.Equal(t, 1, summaries[0].Unreached)
	require.Zero(t, summaries[0].Valid)
	require.Contains(t, FormatBenchReport(summaries), "never reached the model")
}

func TestFormatBenchReportNamesBetterAndWorseTasks(t *testing.T) {
	t.Parallel()

	report := FormatBenchReport(SummarizeBench([]BenchRecord{
		{Task: "x", Variant: "base", Passed: false},
		{Task: "y", Variant: "base", Passed: true},
		{Task: "x", Variant: "next", Passed: true},
		{Task: "y", Variant: "next", Passed: false},
	}))
	require.Contains(t, report, "better on x")
	require.Contains(t, report, "worse on y")
}

// A run that crashed after editing files reached the model, even though its
// JSON summary is missing; only an untouched workspace counts as unreached.
func TestRunBenchKeepsCrashedRunsThatChangedFiles(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}

	suite := t.TempDir()
	writeBenchTask(t, suite, "one")
	manifest := BenchManifest{Suite: suite, Model: "m", Variants: []BenchVariant{{Name: "baseline"}}}
	crashAfterEditing := func(ctx context.Context, dir, program string, args []string) ([]byte, error) {
		if program == "atlas" {
			_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte("half done"), 0o644)
			return nil, errors.New("exit status 1: 400 image too large")
		}
		return fakeBenchRunner(ctx, dir, program, args)
	}

	records, err := RunBench(t.Context(), manifest, "atlas", filepath.Join(t.TempDir(), "results.json"), false, crashAfterEditing)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.NotEqual(t, benchErrorUnreached, records[0].ErrorKind)
	require.NotEmpty(t, records[0].AgentError)
}

func TestBenchScoreReadsPartialCredit(t *testing.T) {
	t.Parallel()

	require.Equal(t, 100.0, benchScore("ok", true))
	require.Equal(t, 0.0, benchScore("FAIL", false))
	require.Equal(t, 72.5, benchScore("checks...\nBENCH_SCORE: 40\nBENCH_SCORE: 72.5\n", false))
	require.Equal(t, 100.0, benchScore("BENCH_SCORE: 250", true))
	// A score inside other text is not a score line.
	require.Equal(t, 0.0, benchScore("said BENCH_SCORE: 90 somewhere", false))

	summaries := SummarizeBench([]BenchRecord{
		{Task: "site", Variant: "base", Passed: false, Score: 60},
		{Task: "site", Variant: "next", Passed: false, Score: 80},
	})
	require.Equal(t, 60.0, summaries[0].MeanScore)
	report := FormatBenchReport(summaries)
	require.Contains(t, report, "score +20.0")
	require.Contains(t, report, "better on site")
}

func TestBenchCheckArgsExpandPlaceholders(t *testing.T) {
	t.Parallel()

	task := BenchTask{Dir: filepath.Join("suite", "site"), Check: []string{"node", "{suite}/../judges/site/judge.mjs", "{task}"}}
	args := benchCheckArgs(task)
	require.Equal(t, []string{"node", "suite/../judges/site/judge.mjs", filepath.Join("suite", "site")}, []string{args[0], filepath.ToSlash(args[1]), args[2]})
}

func TestLoadBenchSuiteRejectsMalformedTasks(t *testing.T) {
	t.Parallel()

	cases := map[string]func(dir string){
		"id differs from directory": func(dir string) {
			require.NoError(t, os.WriteFile(filepath.Join(dir, "task.json"), []byte(`{"id":"other","prompt":"p","check":["verify"]}`), 0o644))
		},
		"missing hidden tests": func(dir string) { require.NoError(t, os.RemoveAll(filepath.Join(dir, "hidden"))) },
		"missing repo":         func(dir string) { require.NoError(t, os.RemoveAll(filepath.Join(dir, "repo"))) },
		"option as program": func(dir string) {
			require.NoError(t, os.WriteFile(filepath.Join(dir, "task.json"), []byte(`{"id":"t","prompt":"p","check":["--version"]}`), 0o644))
		},
		"empty prompt": func(dir string) {
			require.NoError(t, os.WriteFile(filepath.Join(dir, "task.json"), []byte(`{"id":"t","prompt":" ","check":["verify"]}`), 0o644))
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			suite := t.TempDir()
			writeBenchTask(t, suite, "t")
			mutate(filepath.Join(suite, "t"))
			_, err := LoadBenchSuite(suite, nil)
			require.Error(t, err)
		})
	}

	suite := t.TempDir()
	writeBenchTask(t, suite, "t")
	_, err := LoadBenchSuite(suite, []string{"missing"})
	require.ErrorContains(t, err, "not in the suite")
}

func TestValidateBenchRejectsBadManifests(t *testing.T) {
	t.Parallel()

	tasks := []BenchTask{{ID: "t"}}
	good := BenchManifest{Suite: "s", Model: "m", Variants: []BenchVariant{{Name: "a"}, {Name: "b"}}}
	require.NoError(t, ValidateBench(good, tasks))

	bad := map[string]func(m *BenchManifest){
		"no model":        func(m *BenchManifest) { m.Model = "" },
		"option as model": func(m *BenchManifest) { m.Model = "--yolo" },
		"no variants":     func(m *BenchManifest) { m.Variants = nil },
		"duplicate names": func(m *BenchManifest) { m.Variants[1].Name = "a" },
		"bad name":        func(m *BenchManifest) { m.Variants[0].Name = "Has Space" },
		"config not an object": func(m *BenchManifest) {
			m.Variants[0].Config = []byte(`[1]`)
		},
		"too many runs": func(m *BenchManifest) { m.Repeats = 20; m.Variants = append(m.Variants, make([]BenchVariant, 0)...) },
	}
	for name, mutate := range bad {
		m := good
		m.Variants = append([]BenchVariant(nil), good.Variants...)
		mutate(&m)
		manyTasks := tasks
		if name == "too many runs" {
			manyTasks = make([]BenchTask, 20)
		}
		require.Error(t, ValidateBench(m, manyTasks), name)
	}
}

func TestValidateBenchTaskCatchesUnfairChecks(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}

	suite := t.TempDir()
	writeBenchTask(t, suite, "fair")
	tasks, err := LoadBenchSuite(suite, nil)
	require.NoError(t, err)
	require.NoError(t, ValidateBenchTask(t.Context(), tasks[0], t.TempDir(), fakeBenchRunner))

	// Starting files that already satisfy the check measure nothing.
	passing := t.TempDir()
	writeBenchTask(t, passing, "trivial")
	require.NoError(t, os.WriteFile(filepath.Join(passing, "trivial", "repo", "a.txt"), []byte("fixed"), 0o644))
	tasks, err = LoadBenchSuite(passing, nil)
	require.NoError(t, err)
	require.ErrorContains(t, ValidateBenchTask(t.Context(), tasks[0], t.TempDir(), fakeBenchRunner), "already passes")

	// A reference solution that does not satisfy the check is a broken task.
	broken := t.TempDir()
	writeBenchTask(t, broken, "unsolvable")
	require.NoError(t, os.WriteFile(filepath.Join(broken, "unsolvable", "solution", "a.txt"), []byte("still wrong"), 0o644))
	tasks, err = LoadBenchSuite(broken, nil)
	require.NoError(t, err)
	require.ErrorContains(t, ValidateBenchTask(t.Context(), tasks[0], t.TempDir(), fakeBenchRunner), "still fails")
}

// The shipped starter tasks must each fail on their starting files and pass
// with their reference solution; otherwise a benchmark score means nothing.
func TestStarterBenchTasksAreFair(t *testing.T) {
	t.Parallel()
	for _, tool := range []string{"git", "go"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not installed", tool)
		}
	}

	suite := filepath.Join("..", "..", "bench", "tasks")
	tasks, err := LoadBenchSuite(suite, nil)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(tasks), 6)
	scratch := t.TempDir()
	for _, task := range tasks {
		t.Run(task.ID, func(t *testing.T) {
			t.Parallel()
			require.NoError(t, ValidateBenchTask(t.Context(), task, scratch, nil))
		})
	}
}

// The site tasks need Node, the judge's packages (npm install in
// bench/judges/site) and an installed Chrome or Edge, so they are skipped
// where those are missing.
func TestSiteBenchTasksAreFair(t *testing.T) {
	t.Parallel()
	for _, tool := range []string{"git", "node"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not installed", tool)
		}
	}
	if _, err := os.Stat(filepath.Join("..", "..", "bench", "judges", "site", "node_modules", "playwright-core")); err != nil {
		t.Skip("the site judge's packages are not installed")
	}

	tasks, err := LoadBenchSuite(filepath.Join("..", "..", "bench", "sites"), nil)
	require.NoError(t, err)
	scratch := t.TempDir()
	for _, task := range tasks {
		require.NoError(t, ValidateBenchTask(t.Context(), task, scratch, nil), task.ID)
	}
}
