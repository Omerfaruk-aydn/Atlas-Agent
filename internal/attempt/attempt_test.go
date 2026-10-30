package attempt

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-c", "user.name=t", "-c", "user.email=t@example.invalid", "-c", "commit.gpgsign=false", "-C", dir}, args...)
	cmd := exec.CommandContext(t.Context(), "git", full...)
	cmd.Env = gitEnv()
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	return string(out)
}

func newRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "calc.txt"), []byte("status: todo\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "other.txt"), []byte("keep\n"), 0o644))
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", "init")
	return dir
}

// fakeAgent edits calc.txt according to the model name and answers judge
// requests with a fixed verdict.
type fakeAgent struct {
	mu       sync.Mutex
	jobs     []AgentJob
	judgeFor map[string]float64
	judgeRaw string
	seen     map[string]string
}

func (f *fakeAgent) run(ctx context.Context, job AgentJob) (AgentResult, error) {
	f.mu.Lock()
	f.jobs = append(f.jobs, job)
	f.mu.Unlock()
	if strings.Contains(job.Prompt, "impartial senior reviewer") {
		if f.judgeRaw != "" {
			return AgentResult{Response: f.judgeRaw, Cost: 0.01}, nil
		}
		return AgentResult{Response: `Here you go:
{"scores":[{"solution":"A","score":9,"rationale":"clean","concerns":[]},{"solution":"B","score":3,"rationale":"hacky","concerns":["hardcoded"]}],"winner":"A","summary":"A is cleaner"}`, Cost: 0.01}, nil
	}
	write := func(name, body string) { _ = os.WriteFile(filepath.Join(job.Dir, name), []byte(body), 0o644) }
	if f.seen != nil {
		data, _ := os.ReadFile(filepath.Join(job.Dir, "local.txt"))
		f.mu.Lock()
		f.seen[job.Model] = string(data)
		f.mu.Unlock()
	}
	switch job.Model {
	case "good":
		write("calc.txt", "status: fixed\n")
	case "good-big":
		write("calc.txt", "status: fixed\n")
		write("extra.txt", strings.Repeat("padding\n", 30))
	case "bad":
		write("calc.txt", "status: broken\n")
	case "err":
		return AgentResult{}, errors.New("provider unavailable")
	case "slow":
		<-ctx.Done()
		return AgentResult{}, ctx.Err()
	case "finish-error":
		write("calc.txt", "status: fixed\n")
		return AgentResult{FinishReason: "error", FinishError: "context window exceeded"}, nil
	}
	return AgentResult{Response: "done", Cost: 0.5, ToolCalls: 3}, nil
}

// fakeCheck passes when calc.txt contains "fixed".
func fakeCheck(_ context.Context, dir, command string, _ time.Duration) CheckResult {
	data, _ := os.ReadFile(filepath.Join(dir, "calc.txt"))
	ok := strings.Contains(string(data), "fixed")
	res := CheckResult{Command: command, Passed: ok}
	if !ok {
		res.ExitCode, res.Output = 1, "calc.txt is not fixed"
	}
	return res
}

func baseOptions(t *testing.T, root string, agent *fakeAgent, models ...string) Options {
	t.Helper()
	return Options{
		Root:     root,
		StateDir: filepath.Join(t.TempDir(), "state"),
		WorkDir:  filepath.Join(t.TempDir(), "work"),
		Task:     "mark the calculator fixed",
		N:        len(models),
		Models:   models,
		Verify:   []string{"check-fixed"},
		Agent:    agent.run,
		Check:    fakeCheck,
	}
}

func worktreeCount(t *testing.T, root string) int {
	t.Helper()
	return strings.Count(runGit(t, root, "worktree", "list", "--porcelain"), "worktree ")
}

func TestRunRanksPassingAttemptFirstAndLeavesTreeUntouched(t *testing.T) {
	t.Parallel()
	root := newRepo(t)
	statusBefore := runGit(t, root, "status", "--porcelain")
	agent := &fakeAgent{}
	opts := baseOptions(t, root, agent, "good", "bad")
	opts.Judge = "judge-model"

	report, err := Run(t.Context(), opts)
	require.NoError(t, err)

	require.Equal(t, 1, report.Winner)
	require.Equal(t, 1, report.Attempts[0].Rank)
	require.Equal(t, 2, report.Attempts[1].Rank)
	require.True(t, report.Attempts[0].VerifyPassed)
	require.False(t, report.Attempts[1].VerifyPassed)
	require.NotNil(t, report.Attempts[0].Judge)
	require.True(t, report.Judge.Ran)
	require.InDelta(t, 1.01, report.TotalCost, 0.001)
	require.Equal(t, ConfidenceHigh, report.Confidence, report.ConfidenceReasons)
	require.Contains(t, report.Attempts[0].Files[0].Path, "calc.txt")

	// The tree is untouched and every worktree is gone.
	require.Equal(t, statusBefore, runGit(t, root, "status", "--porcelain"))
	require.Equal(t, 1, worktreeCount(t, root))
	_, err = os.Stat(opts.WorkDir + string(filepath.Separator) + report.ID)
	require.True(t, os.IsNotExist(err))

	// The report round-trips from disk.
	loaded, err := Load(opts.StateDir, report.ID)
	require.NoError(t, err)
	require.Equal(t, report.Winner, loaded.Winner)
}

func TestVerifyFailureOutranksJudgePreference(t *testing.T) {
	t.Parallel()
	root := newRepo(t)
	agent := &fakeAgent{judgeRaw: `{"scores":[{"solution":"A","score":10,"rationale":"x"},{"solution":"B","score":1,"rationale":"y"}],"winner":"A","summary":"A"}`}
	// Attempt A fails the check, attempt B passes it.
	opts := baseOptions(t, root, agent, "bad", "good")

	report, err := Run(t.Context(), opts)
	require.NoError(t, err)

	require.Equal(t, 2, report.Winner, "a failing test must not be overridden by the judge")
	require.Equal(t, ConfidenceLow, report.Confidence)
	require.Contains(t, strings.Join(report.ConfidenceReasons, " "), "judge preferred")
}

func TestSeedsUncommittedAndUntrackedChangesAndAppliesOnTopOfThem(t *testing.T) {
	t.Parallel()
	root := newRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "other.txt"), []byte("edited locally\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "local.txt"), []byte("untracked\n"), 0o644))
	agent := &fakeAgent{seen: map[string]string{}}
	opts := baseOptions(t, root, agent, "good")

	report, err := Run(t.Context(), opts)
	require.NoError(t, err)
	require.Equal(t, "untracked\n", agent.seen["good"])
	// Only the attempt's own change is in the patch, not the seeded edits.
	require.Len(t, report.Attempts[0].Files, 1)
	require.Equal(t, "calc.txt", report.Attempts[0].Files[0].Path)

	_, applied, err := Apply(t.Context(), opts.StateDir, report.ID, 0)
	require.NoError(t, err)
	require.Equal(t, "A", applied.Label)
	data, err := os.ReadFile(filepath.Join(root, "calc.txt"))
	require.NoError(t, err)
	require.Equal(t, "status: fixed\n", strings.ReplaceAll(string(data), "\r\n", "\n"))
	other, err := os.ReadFile(filepath.Join(root, "other.txt"))
	require.NoError(t, err)
	require.Equal(t, "edited locally\n", strings.ReplaceAll(string(other), "\r\n", "\n"))

	_, _, err = Apply(t.Context(), opts.StateDir, report.ID, 0)
	require.ErrorContains(t, err, "already applied")
}

func TestFailedAttemptsAreClassifiedAndNeverWin(t *testing.T) {
	t.Parallel()
	root := newRepo(t)
	agent := &fakeAgent{}
	opts := baseOptions(t, root, agent, "err", "noop", "finish-error", "slow")
	opts.Timeout = 300 * time.Millisecond

	report, err := Run(t.Context(), opts)
	require.NoError(t, err)

	require.Equal(t, StatusAgentError, report.Attempts[0].Status)
	require.Equal(t, StatusNoChanges, report.Attempts[1].Status)
	require.Equal(t, StatusAgentError, report.Attempts[2].Status)
	require.Equal(t, StatusTimeout, report.Attempts[3].Status)
	require.Zero(t, report.Winner)
	require.Equal(t, ConfidenceNone, report.Confidence)
	// Partial work of a failed attempt is kept for inspection.
	require.NotEmpty(t, report.Attempts[2].PatchFile)
	require.Equal(t, 1, worktreeCount(t, root))

	_, _, err = Apply(t.Context(), opts.StateDir, report.ID, 0)
	require.ErrorContains(t, err, "no winner")
}

func TestSetupFailureStopsAttempt(t *testing.T) {
	t.Parallel()
	root := newRepo(t)
	agent := &fakeAgent{}
	opts := baseOptions(t, root, agent, "good")
	opts.Setup = "install-deps"
	opts.Check = func(ctx context.Context, dir, command string, d time.Duration) CheckResult {
		if command == "install-deps" {
			return CheckResult{Command: command, ExitCode: 7, Output: "no network"}
		}
		return fakeCheck(ctx, dir, command, d)
	}

	report, err := Run(t.Context(), opts)
	require.NoError(t, err)
	require.Equal(t, StatusSetupFailed, report.Attempts[0].Status)
	require.Contains(t, report.Attempts[0].Error, "no network")
	require.Empty(t, agent.jobs)
}

func TestIdenticalPatchesShareOneJudgeCandidate(t *testing.T) {
	t.Parallel()
	root := newRepo(t)
	agent := &fakeAgent{judgeRaw: `{"scores":[{"solution":"A","score":8,"rationale":"ok"},{"solution":"C","score":5,"rationale":"meh"}],"winner":"A","summary":"A"}`}
	opts := baseOptions(t, root, agent, "good", "good", "good-big")
	opts.Judge = "j"

	report, err := Run(t.Context(), opts)
	require.NoError(t, err)

	require.Equal(t, 1, report.Attempts[1].DuplicateOf)
	require.NotNil(t, report.Attempts[1].Judge)
	require.InDelta(t, report.Attempts[0].Final, report.Attempts[1].Final, 0.001)
	require.Equal(t, 1, report.Winner)
	require.Greater(t, report.Attempts[0].Final, report.Attempts[2].Final)
}

func TestBrokenJudgeFallsBackToDeterministicRanking(t *testing.T) {
	t.Parallel()
	root := newRepo(t)
	agent := &fakeAgent{judgeRaw: `{"scores":[{"solution":"A","score":8}],"winner":"A"}`}
	opts := baseOptions(t, root, agent, "good", "bad")

	report, err := Run(t.Context(), opts)
	require.NoError(t, err)

	require.False(t, report.Judge.Ran)
	require.Contains(t, report.Judge.Error, "did not score solution B")
	require.Equal(t, 1, report.Winner)
	require.Nil(t, report.Attempts[0].Judge)
	require.Contains(t, strings.Join(report.Warnings, " "), "judge failed")
}

func TestNoJudgeAndSingleAttempt(t *testing.T) {
	t.Parallel()
	root := newRepo(t)
	agent := &fakeAgent{}
	opts := baseOptions(t, root, agent, "good")

	report, err := Run(t.Context(), opts)
	require.NoError(t, err)
	require.Equal(t, 1, report.Winner)
	require.Contains(t, report.Judge.Skipped, "fewer than two")
	require.Equal(t, ConfidenceMedium, report.Confidence)
	require.Empty(t, report.Attempts[0].Strategy, "a single attempt gets no strategy hint")
}

func TestKeepLeavesWorktrees(t *testing.T) {
	t.Parallel()
	root := newRepo(t)
	agent := &fakeAgent{}
	opts := baseOptions(t, root, agent, "good")
	opts.Keep = true

	_, err := Run(t.Context(), opts)
	require.NoError(t, err)
	require.Equal(t, 2, worktreeCount(t, root))
}

func TestCancellationCleansUp(t *testing.T) {
	t.Parallel()
	root := newRepo(t)
	agent := &fakeAgent{}
	opts := baseOptions(t, root, agent, "slow", "slow")
	ctx, cancel := context.WithCancel(t.Context())
	opts.Agent = func(ctx context.Context, job AgentJob) (AgentResult, error) {
		// Cancel once the agents are really running, however long the
		// worktrees took to create.
		time.AfterFunc(200*time.Millisecond, cancel)
		return agent.run(ctx, job)
	}

	report, err := Run(ctx, opts)
	require.NoError(t, err)
	require.Zero(t, report.Winner)
	for _, a := range report.Attempts {
		require.Equal(t, StatusCanceled, a.Status)
	}
	require.Equal(t, 1, worktreeCount(t, root), runGit(t, root, "worktree", "list"))
}

func TestOptionValidation(t *testing.T) {
	t.Parallel()
	root := newRepo(t)
	agent := &fakeAgent{}
	for name, mutate := range map[string]func(*Options){
		"no task":      func(o *Options) { o.Task = "  " },
		"zero":         func(o *Options) { o.N = 0 },
		"too many":     func(o *Options) { o.N = MaxAttempts + 1 },
		"no state dir": func(o *Options) { o.StateDir = "" },
	} {
		opts := baseOptions(t, root, agent, "good")
		mutate(&opts)
		_, err := Run(t.Context(), opts)
		require.Error(t, err, name)
	}

	notRepo := t.TempDir()
	opts := baseOptions(t, notRepo, agent, "good")
	_, err := Run(t.Context(), opts)
	require.Error(t, err)
}

func TestRepoWithoutCommitsIsRejected(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	_, err := Run(t.Context(), baseOptions(t, dir, &fakeAgent{}, "good"))
	require.ErrorContains(t, err, "at least one commit")
}

func TestApplyRejectsBadIDsAndStalePatches(t *testing.T) {
	t.Parallel()
	state := t.TempDir()
	for _, id := range []string{"..", "../x", "20260101-000000-zzzz", ""} {
		_, err := Load(state, id)
		require.Error(t, err, id)
	}

	root := newRepo(t)
	opts := baseOptions(t, root, &fakeAgent{}, "good")
	report, err := Run(t.Context(), opts)
	require.NoError(t, err)

	// The tree changes after the run: the patch no longer applies.
	require.NoError(t, os.WriteFile(filepath.Join(root, "calc.txt"), []byte("rewritten\n"), 0o644))
	_, _, err = Apply(t.Context(), opts.StateDir, report.ID, 0)
	require.ErrorContains(t, err, "does not apply")
	loaded, err := Load(opts.StateDir, report.ID)
	require.NoError(t, err)
	require.Zero(t, loaded.Applied)

	_, _, err = Apply(t.Context(), opts.StateDir, report.ID, 9)
	require.ErrorContains(t, err, "no attempt 9")
}

func TestListAndClean(t *testing.T) {
	t.Parallel()
	root := newRepo(t)
	opts := baseOptions(t, root, &fakeAgent{}, "good")
	report, err := Run(t.Context(), opts)
	require.NoError(t, err)

	reports, err := List(opts.StateDir)
	require.NoError(t, err)
	require.Len(t, reports, 1)

	removed, err := Clean(opts.StateDir, time.Hour)
	require.NoError(t, err)
	require.Empty(t, removed, "a fresh run is newer than the cutoff")

	removed, err = Clean(opts.StateDir, 0)
	require.NoError(t, err)
	require.Equal(t, []string{report.ID}, removed)
	reports, err = List(opts.StateDir)
	require.NoError(t, err)
	require.Empty(t, reports)

	reports, err = List(filepath.Join(t.TempDir(), "missing"))
	require.NoError(t, err)
	require.Empty(t, reports)
}

func TestFormatMentionsWinnerAndApplyCommand(t *testing.T) {
	t.Parallel()
	root := newRepo(t)
	opts := baseOptions(t, root, &fakeAgent{}, "good", "bad")
	report, err := Run(t.Context(), opts)
	require.NoError(t, err)

	text := Format(report)
	require.Contains(t, text, "[winner]")
	require.Contains(t, text, "Nothing has been applied")
	require.Contains(t, text, "attempts apply "+report.ID)
	require.Contains(t, text, "FAIL(exit 1)")
	require.Less(t, strings.Index(text, "Attempt A"), strings.Index(text, "Attempt B"))
}
