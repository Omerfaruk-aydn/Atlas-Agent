package evaluation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
)

// The task bench measures whether a harness change helps real work. A task
// is a small repository with a bug or a missing feature, a prompt, and a
// hidden test that is the only judge. The test is copied in after the agent
// finishes, overwriting anything the agent wrote at those paths, so an agent
// cannot pass by editing or deleting the check. Variants run the same tasks
// with different Atlas configuration (for example the evaluator gate on and
// off), interleaved attempt by attempt so drift in provider speed or load
// lands on every variant alike.
//
// Layout of a suite directory:
//
//	<suite>/<task-id>/task.json   id, prompt, check command
//	<suite>/<task-id>/repo/       starting files, committed before the run
//	<suite>/<task-id>/hidden/     files copied over the workspace before the check
//	<suite>/<task-id>/solution/   reference fix, used only by validation
//
// The check passes or fails by its exit status. It may also print a line
// "BENCH_SCORE: N" with N from 0 to 100 for partial credit, which matters for
// work such as a web page where "works but is clumsy on a phone" is neither a
// pass nor a zero. Without that line the score is 100 for a pass and 0 for a
// fail. In check arguments "{task}" is replaced with the task directory and
// "{suite}" with the suite directory, so a shared judge can live outside the
// workspace where the agent cannot see or change it.

const (
	benchMaxTasks    = 64
	benchMaxRuns     = 512
	benchMaxFileSize = 1 << 20
)

// benchErrorUnreached marks an attempt that failed before the model produced
// anything. It is reported separately and excluded from pass rates.
const benchErrorUnreached = "unreached"

var benchIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// BenchTask is one task of a suite, as described by its task.json.
type BenchTask struct {
	ID             string   `json:"id"`
	Prompt         string   `json:"prompt"`
	Check          []string `json:"check"`
	TimeoutSeconds int      `json:"timeout_seconds,omitempty"`
	// Dir is the task directory; it is set when the suite is loaded.
	Dir string `json:"-"`
}

// BenchVariant is one harness configuration under comparison. Config is
// written to .atlas.json in each workspace, which wins over a repository's
// own atlas.json and merges over the user's global configuration.
type BenchVariant struct {
	Name   string          `json:"name"`
	Config json.RawMessage `json:"config,omitempty"`
}

// BenchManifest selects tasks, one model and the variants to compare.
type BenchManifest struct {
	Suite          string         `json:"suite"`
	Model          string         `json:"model"`
	Tasks          []string       `json:"tasks,omitempty"`
	Variants       []BenchVariant `json:"variants"`
	Repeats        int            `json:"repeats,omitempty"`
	TimeoutSeconds int            `json:"timeout_seconds,omitempty"`
}

// BenchRecord is the outcome of one attempt of one task under one variant.
type BenchRecord struct {
	Task          string  `json:"task"`
	Variant       string  `json:"variant"`
	Model         string  `json:"model"`
	Attempt       int     `json:"attempt"`
	Passed        bool    `json:"passed"`
	Score         float64 `json:"score"`
	CheckOutput   string  `json:"check_output,omitempty"`
	AgentError    string  `json:"agent_error,omitempty"`
	ErrorKind     string  `json:"error_kind,omitempty"`
	FinishReason  string  `json:"finish_reason,omitempty"`
	SessionID     string  `json:"session_id,omitempty"`
	Workspace     string  `json:"workspace"`
	Tokens        int64   `json:"tokens"`
	Cost          float64 `json:"cost_usd"`
	ToolCalls     int64   `json:"tool_calls"`
	ProviderSteps int64   `json:"provider_steps"`
	DurationMS    int64   `json:"duration_ms"`
}

// LoadBenchSuite reads every task of a suite directory, sorted by ID. Only
// the IDs in only are returned when it is not empty.
func LoadBenchSuite(dir string, only []string) ([]BenchTask, error) {
	// Checks run inside each workspace, so the {task} and {suite}
	// placeholders must not depend on the current directory.
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var tasks []BenchTask
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if len(only) > 0 && !slices.Contains(only, entry.Name()) {
			continue
		}
		task, err := loadBenchTask(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("task %s: %w", entry.Name(), err)
		}
		tasks = append(tasks, task)
	}
	for _, id := range only {
		if !slices.ContainsFunc(tasks, func(t BenchTask) bool { return t.ID == id }) {
			return nil, fmt.Errorf("task %q is not in the suite", id)
		}
	}
	if len(tasks) == 0 || len(tasks) > benchMaxTasks {
		return nil, fmt.Errorf("a suite needs 1-%d tasks, found %d", benchMaxTasks, len(tasks))
	}
	return tasks, nil
}

func loadBenchTask(dir string) (BenchTask, error) {
	data, err := os.ReadFile(filepath.Join(dir, "task.json"))
	if err != nil {
		return BenchTask{}, err
	}
	var task BenchTask
	if err := json.Unmarshal(data, &task); err != nil {
		return BenchTask{}, fmt.Errorf("decode task.json: %w", err)
	}
	task.Dir = dir
	if task.ID != filepath.Base(dir) || !benchIDPattern.MatchString(task.ID) {
		return task, errors.New("id must be lowercase letters, digits and dashes, and match the directory name")
	}
	if strings.TrimSpace(task.Prompt) == "" || len(task.Prompt) > 32*1024 {
		return task, errors.New("prompt is required and at most 32 KiB")
	}
	if len(task.Check) == 0 || len(task.Check) > 64 || task.Check[0] == "" || strings.HasPrefix(task.Check[0], "-") {
		return task, errors.New("check must be an executable argument array")
	}
	if task.TimeoutSeconds < 0 || task.TimeoutSeconds > 1800 {
		return task, errors.New("timeout_seconds must be at most 1800")
	}
	if info, err := os.Stat(filepath.Join(dir, "repo")); err != nil || !info.IsDir() {
		return task, errors.New("repo/ directory is required")
	}
	if info, err := os.Stat(filepath.Join(dir, "hidden")); err != nil || !info.IsDir() {
		return task, errors.New("hidden/ directory with the checking tests is required")
	}
	return task, nil
}

// ValidateBench checks a manifest against the tasks it selects.
func ValidateBench(m BenchManifest, tasks []BenchTask) error {
	if m.Suite == "" || m.Model == "" || strings.HasPrefix(m.Model, "-") {
		return errors.New("suite and an explicit model ID are required")
	}
	if len(m.Variants) == 0 || len(m.Variants) > 8 {
		return errors.New("1-8 variants are required")
	}
	names := map[string]bool{}
	for _, v := range m.Variants {
		if !benchIDPattern.MatchString(v.Name) || names[v.Name] {
			return errors.New("variant names must be unique lowercase letters, digits and dashes")
		}
		names[v.Name] = true
		if len(v.Config) > 0 {
			var object map[string]json.RawMessage
			if err := json.Unmarshal(v.Config, &object); err != nil {
				return fmt.Errorf("variant %s: config must be a JSON object", v.Name)
			}
		}
	}
	repeats := benchRepeats(m)
	if repeats < 1 || repeats > 20 || len(tasks)*len(m.Variants)*repeats > benchMaxRuns {
		return fmt.Errorf("repeats must be 1-20 with at most %d total runs", benchMaxRuns)
	}
	if m.TimeoutSeconds < 0 || m.TimeoutSeconds > 1800 {
		return errors.New("timeout_seconds must be at most 1800")
	}
	return nil
}

func benchRepeats(m BenchManifest) int {
	if m.Repeats == 0 {
		return 1
	}
	return m.Repeats
}

// copyBenchTree copies the files under src into dst, overwriting files that
// exist. It refuses symlinks and oversized files so a suite cannot reach
// outside its own directory.
func copyBenchTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() > benchMaxFileSize {
			return fmt.Errorf("%s: only regular files up to 1 MiB are allowed", rel)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

// materializeBenchTask creates a Git repository at dest holding the task's
// starting files in a single commit, so a run is diffable and disposable.
func materializeBenchTask(ctx context.Context, task BenchTask, dest string) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	if err := copyBenchTree(filepath.Join(task.Dir, "repo"), dest); err != nil {
		return err
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"add", "-A"},
		{"-c", "user.name=atlas-bench", "-c", "user.email=bench@atlas.invalid", "-c", "commit.gpgsign=false", "commit", "-q", "-m", "Task fixture"},
	} {
		if _, err := engineering.Git(ctx, dest, args...); err != nil {
			return err
		}
	}
	return nil
}

var benchScorePattern = regexp.MustCompile(`(?m)^BENCH_SCORE:\s*([0-9]+(?:\.[0-9]+)?)\s*$`)

// runBenchCheck lays the hidden tests over the workspace and runs the check.
func runBenchCheck(ctx context.Context, task BenchTask, dir string, runner CommandRunner) (passed bool, score float64, output string) {
	if err := copyBenchTree(filepath.Join(task.Dir, "hidden"), dir); err != nil {
		return false, 0, "could not place hidden tests: " + err.Error()
	}
	timeout := time.Duration(task.TimeoutSeconds) * time.Second
	if timeout == 0 {
		timeout = 2 * time.Minute
	}
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	args := benchCheckArgs(task)
	out, err := runner(checkCtx, dir, args[0], args[1:])
	passed = err == nil
	return passed, benchScore(string(out), passed), benchTail(string(out), err)
}

// benchCheckArgs expands the {task} and {suite} placeholders of the check.
func benchCheckArgs(task BenchTask) []string {
	replacer := strings.NewReplacer("{task}", task.Dir, "{suite}", filepath.Dir(task.Dir))
	args := make([]string, len(task.Check))
	for i, arg := range task.Check {
		args[i] = replacer.Replace(arg)
	}
	return args
}

// benchScore reads the last BENCH_SCORE line of a check's output, clamped to
// 0-100. A check that prints none scores 100 for a pass and 0 for a fail.
func benchScore(output string, passed bool) float64 {
	matches := benchScorePattern.FindAllStringSubmatch(output, -1)
	if len(matches) == 0 {
		if passed {
			return 100
		}
		return 0
	}
	var score float64
	if _, err := fmt.Sscanf(matches[len(matches)-1][1], "%g", &score); err != nil {
		return 0
	}
	return min(max(score, 0), 100)
}

func benchTail(output string, err error) string {
	output = strings.TrimSpace(output)
	if err != nil {
		output += "\n" + err.Error()
	}
	const keep = 1200
	if len(output) > keep {
		output = "…" + output[len(output)-keep:]
	}
	return strings.TrimSpace(output)
}

// ValidateBenchTask proves a task is a fair test: its check must fail on the
// starting files and pass once the reference solution is applied. A check
// that passes at the start measures nothing; one that never passes punishes
// every agent alike.
func ValidateBenchTask(ctx context.Context, task BenchTask, scratch string, runner CommandRunner) error {
	if runner == nil {
		runner = RunCommand
	}
	solution := filepath.Join(task.Dir, "solution")
	if info, err := os.Stat(solution); err != nil || !info.IsDir() {
		return errors.New("solution/ directory is required for validation")
	}
	dir := filepath.Join(scratch, task.ID)
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := materializeBenchTask(ctx, task, dir); err != nil {
		return err
	}
	if passed, _, out := runBenchCheck(ctx, task, dir, runner); passed {
		return errors.New("the check already passes on the starting files")
	} else if strings.HasPrefix(out, "could not place") {
		return errors.New(out)
	}
	if err := copyBenchTree(solution, dir); err != nil {
		return err
	}
	if passed, _, out := runBenchCheck(ctx, task, dir, runner); !passed {
		return fmt.Errorf("the check still fails with the reference solution:\n%s", out)
	}
	return nil
}

// RunBench runs every task under every variant, attempt by attempt, and
// writes the records incrementally to output. Workspaces are preserved.
func RunBench(ctx context.Context, m BenchManifest, program, output string, allowTools bool, runner CommandRunner) ([]BenchRecord, error) {
	tasks, err := LoadBenchSuite(m.Suite, m.Tasks)
	if err != nil {
		return nil, err
	}
	if err := ValidateBench(m, tasks); err != nil {
		return nil, err
	}
	// Fail before spending tokens when a check cannot start: every run would
	// otherwise "fail" for a reason that has nothing to do with the agent.
	if runner == nil {
		for _, task := range tasks {
			if err := checkBenchProgram(task); err != nil {
				return nil, err
			}
		}
	}
	output, err = filepath.Abs(output)
	if err != nil {
		return nil, err
	}
	root := filepath.Join(filepath.Dir(output), ".atlas-bench")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	if runner == nil {
		runner = RunCommand
	}
	timeout := time.Duration(m.TimeoutSeconds) * time.Second
	if timeout == 0 {
		timeout = 10 * time.Minute
	}
	stamp := time.Now().Format("20060102-150405")
	var records []BenchRecord
	for attempt := range benchRepeats(m) {
		for _, task := range tasks {
			for _, variant := range m.Variants {
				if err := ctx.Err(); err != nil {
					return records, err
				}
				dir := filepath.Join(root, fmt.Sprintf("%s-%s-%s-%d", stamp, variant.Name, task.ID, attempt))
				record := runBenchAttempt(ctx, m, task, variant, attempt, dir, program, timeout, allowTools, runner)
				records = append(records, record)
				encoded, err := json.MarshalIndent(records, "", "  ")
				if err != nil {
					return records, err
				}
				if err := engineering.AtomicWrite(output, encoded); err != nil {
					return records, err
				}
			}
		}
	}
	return records, nil
}

func runBenchAttempt(ctx context.Context, m BenchManifest, task BenchTask, variant BenchVariant, attempt int, dir, program string, timeout time.Duration, allowTools bool, runner CommandRunner) (record BenchRecord) {
	record = BenchRecord{Task: task.ID, Variant: variant.Name, Model: m.Model, Attempt: attempt, Workspace: dir}
	started := time.Now()
	defer func() { record.DurationMS = time.Since(started).Milliseconds() }()

	if err := materializeBenchTask(ctx, task, dir); err != nil {
		record.AgentError, record.ErrorKind = "prepare workspace: "+err.Error(), ErrorAgent
		return record
	}
	if len(variant.Config) > 0 {
		if err := os.WriteFile(filepath.Join(dir, ".atlas.json"), variant.Config, 0o644); err != nil {
			record.AgentError, record.ErrorKind = "write variant config: "+err.Error(), ErrorAgent
			return record
		}
	}
	args := []string{"--cwd", dir, "--data-dir", filepath.Join(dir, ".atlas-eval-state"), "run", "--model", m.Model, "--role", "none", "--json", "--quiet", task.Prompt}
	if allowTools {
		args = append([]string{"--yolo"}, args...)
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	data, runErr := runner(runCtx, dir, program, args)
	cancel()

	var result struct {
		SessionID        string  `json:"session_id"`
		PromptTokens     int64   `json:"prompt_tokens"`
		CompletionTokens int64   `json:"completion_tokens"`
		Cost             float64 `json:"cost"`
		ProviderSteps    int64   `json:"provider_steps"`
		ToolCalls        int64   `json:"tool_calls"`
		FinishReason     string  `json:"finish_reason"`
		FinishError      string  `json:"finish_error"`
	}
	decodeErr := json.Unmarshal(data, &result)
	record.SessionID, record.Cost = result.SessionID, result.Cost
	record.Tokens = result.PromptTokens + result.CompletionTokens
	record.ProviderSteps, record.ToolCalls, record.FinishReason = result.ProviderSteps, result.ToolCalls, result.FinishReason
	switch {
	case runErr != nil:
		record.AgentError = runErr.Error()
	case decodeErr != nil:
		record.AgentError = "agent returned invalid JSON: " + decodeErr.Error()
	}
	record.ErrorKind = ClassifyError(record.AgentError, result.FinishError)
	// An error before any token or tool call means the model never answered
	// (a wrong model ID, a 404, an expired login). That is a setup failure,
	// not evidence about the agent, and must not lower a pass rate.
	// A run that died after editing files did reach the model: its JSON
	// summary is missing, not its work, so the workspace decides.
	if record.AgentError != "" && record.ErrorKind == ErrorAgent && record.Tokens == 0 && record.ToolCalls == 0 && !benchWorkspaceChanged(ctx, dir) {
		record.ErrorKind = benchErrorUnreached
	}
	// The check runs even after an agent error: partial work can still pass,
	// and the record keeps both facts.
	record.Passed, record.Score, record.CheckOutput = runBenchCheck(ctx, task, dir, runner)
	return record
}

// benchWorkspaceChanged reports whether the agent left any file changes,
// ignoring the files the bench itself writes.
func benchWorkspaceChanged(ctx context.Context, dir string) bool {
	out, err := engineering.Git(ctx, dir, "status", "--porcelain", "--", ".", ":!.atlas.json", ":!.atlas-eval-state")
	return err == nil && strings.TrimSpace(out) != ""
}

func checkBenchProgram(task BenchTask) error {
	program := benchCheckArgs(task)[0]
	if _, err := exec.LookPath(program); err != nil {
		return fmt.Errorf("task %s: check program %q is not available on PATH: %w", task.ID, program, err)
	}
	return nil
}

// BenchTally counts the attempts of one variant on one task.
type BenchTally struct {
	Valid     int     `json:"valid"`
	Passed    int     `json:"passed"`
	MeanScore float64 `json:"mean_score"`
}

// BenchSummary aggregates one variant. Attempts that failed because the
// provider did (rate limits, outages) are excluded from the pass rate and
// counted separately: they say nothing about the harness.
type BenchSummary struct {
	Variant       string                `json:"variant"`
	Runs          int                   `json:"runs"`
	ProviderFails int                   `json:"provider_failures"`
	Unreached     int                   `json:"unreached"`
	Valid         int                   `json:"valid_runs"`
	Passed        int                   `json:"passed"`
	PassRate      float64               `json:"pass_rate"`
	MeanScore     float64               `json:"mean_score"`
	MeanCost      float64               `json:"mean_cost_usd"`
	MeanTokens    float64               `json:"mean_tokens"`
	MeanToolCalls float64               `json:"mean_tool_calls"`
	MeanSeconds   float64               `json:"mean_seconds"`
	PerTask       map[string]BenchTally `json:"per_task"`
}

// SummarizeBench groups records by variant in order of first appearance.
func SummarizeBench(records []BenchRecord) []BenchSummary {
	var order []string
	by := map[string]*BenchSummary{}
	for _, r := range records {
		s, ok := by[r.Variant]
		if !ok {
			s = &BenchSummary{Variant: r.Variant, PerTask: map[string]BenchTally{}}
			by[r.Variant] = s
			order = append(order, r.Variant)
		}
		s.Runs++
		if r.ErrorKind == benchErrorUnreached {
			s.Unreached++
			continue
		}
		if r.ErrorKind == ErrorProvider {
			s.ProviderFails++
			continue
		}
		s.Valid++
		score := r.Score
		// Records written before scores existed carry 0 for a pass.
		if r.Passed && score == 0 {
			score = 100
		}
		tally := s.PerTask[r.Task]
		tally.Valid++
		tally.MeanScore += score
		if r.Passed {
			s.Passed++
			tally.Passed++
		}
		s.PerTask[r.Task] = tally
		s.MeanScore += score
		s.MeanCost += r.Cost
		s.MeanTokens += float64(r.Tokens)
		s.MeanToolCalls += float64(r.ToolCalls)
		s.MeanSeconds += float64(r.DurationMS) / 1000
	}
	out := make([]BenchSummary, 0, len(order))
	for _, name := range order {
		s := by[name]
		if s.Valid > 0 {
			n := float64(s.Valid)
			s.PassRate = float64(s.Passed) / n
			s.MeanScore /= n
			s.MeanCost /= n
			s.MeanTokens /= n
			s.MeanToolCalls /= n
			s.MeanSeconds /= n
		}
		for task, tally := range s.PerTask {
			tally.MeanScore /= float64(tally.Valid)
			s.PerTask[task] = tally
		}
		out = append(out, *s)
	}
	return out
}

// FormatBenchReport renders summaries as plain text. The first variant is
// the baseline the others are compared with; the result states its own
// limit, since a handful of attempts cannot prove a difference.
func FormatBenchReport(summaries []BenchSummary) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%-16s %6s %7s %9s %6s %10s %9s %8s\n", "variant", "valid", "passed", "pass rate", "score", "cost/run", "tools/run", "sec/run")
	for _, s := range summaries {
		fmt.Fprintf(&sb, "%-16s %6d %7d %8.0f%% %6.1f %10.4f %9.1f %8.0f", s.Variant, s.Valid, s.Passed, s.PassRate*100, s.MeanScore, s.MeanCost, s.MeanToolCalls, s.MeanSeconds)
		if s.ProviderFails > 0 {
			fmt.Fprintf(&sb, "  (%d provider failures excluded)", s.ProviderFails)
		}
		if s.Unreached > 0 {
			fmt.Fprintf(&sb, "  (%d runs never reached the model and were excluded; check the model ID and login)", s.Unreached)
		}
		sb.WriteString("\n")
	}
	if len(summaries) > 1 {
		base := summaries[0]
		sb.WriteString("\nAgainst baseline " + base.Variant + ":\n")
		for _, s := range summaries[1:] {
			fmt.Fprintf(&sb, "- %s: pass rate %+.0f points, score %+.1f", s.Variant, (s.PassRate-base.PassRate)*100, s.MeanScore-base.MeanScore)
			var better, worse []string
			for task, tally := range s.PerTask {
				b, ok := base.PerTask[task]
				if !ok || b.Valid == 0 || tally.Valid == 0 {
					continue
				}
				d := float64(tally.Passed)/float64(tally.Valid) - float64(b.Passed)/float64(b.Valid)
				if d == 0 {
					d = tally.MeanScore - b.MeanScore
				}
				switch {
				case d > 0:
					better = append(better, task)
				case d < 0:
					worse = append(worse, task)
				}
			}
			sort.Strings(better)
			sort.Strings(worse)
			if len(better) > 0 {
				sb.WriteString("; better on " + strings.Join(better, ", "))
			}
			if len(worse) > 0 {
				sb.WriteString("; worse on " + strings.Join(worse, ", "))
			}
			sb.WriteString("\n")
		}
	}
	sb.WriteString("\nSmall samples are noisy: repeat each task several times before trusting a difference of a few points.\n")
	return sb.String()
}

// RescoreBench re-runs the checks of recorded attempts against their
// preserved workspaces, without running any agent. Use it after fixing a
// task's hidden tests. Each workspace first gets the task's own visible test
// files back, because an earlier overlay may have replaced them, and then
// the current hidden tests.
func RescoreBench(ctx context.Context, suite string, records []BenchRecord, runner CommandRunner) ([]BenchRecord, error) {
	tasks, err := LoadBenchSuite(suite, nil)
	if err != nil {
		return nil, err
	}
	byID := map[string]BenchTask{}
	for _, task := range tasks {
		byID[task.ID] = task
		if runner == nil {
			if err := checkBenchProgram(task); err != nil {
				return nil, err
			}
		}
	}
	if runner == nil {
		runner = RunCommand
	}
	rescored := slices.Clone(records)
	for i, record := range rescored {
		task, ok := byID[record.Task]
		if !ok {
			return nil, fmt.Errorf("record %d: task %q is not in the suite", i, record.Task)
		}
		if info, err := os.Stat(record.Workspace); err != nil || !info.IsDir() {
			return nil, fmt.Errorf("record %d: workspace %s is not available", i, record.Workspace)
		}
		if err := restoreVisibleTests(filepath.Join(task.Dir, "repo"), record.Workspace); err != nil {
			return nil, fmt.Errorf("record %d: %w", i, err)
		}
		rescored[i].Passed, rescored[i].Score, rescored[i].CheckOutput = runBenchCheck(ctx, task, record.Workspace, runner)
	}
	return rescored, nil
}

// restoreVisibleTests copies the *_test.go files of the task's starting
// repository back over the workspace.
func restoreVisibleTests(repo, workspace string) error {
	return filepath.WalkDir(repo, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return err
		}
		rel, err := filepath.Rel(repo, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(workspace, rel), data, 0o644)
	})
}
