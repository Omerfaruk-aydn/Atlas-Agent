package evaluation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/errmemory"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/harnessmetrics"
)

// Mining turns the failure corpus into candidate bench tasks. A case is
// worth keeping when it is hard in a way a harness change could matter for:
// the request touched several files, the request leaves room for
// interpretation, or the error it ended on keeps coming back in the error
// memory. Easy cases would only push a suite toward the ceiling.

const (
	// DefaultMinHardness is the lowest hardness score a case needs to be
	// mined.
	DefaultMinHardness = 3

	minedRedRuns = 2
)

// MinedTask describes one candidate task and what happened to it.
type MinedTask struct {
	ID       string   `json:"id"`
	Hardness int      `json:"hardness"`
	Reasons  []string `json:"reasons"`
	// Valid is true when the task was exported and its check fails on the
	// starting files, consistently.
	Valid bool `json:"valid"`
	// Rejected explains why a case was skipped or an exported task dropped.
	Rejected string `json:"rejected,omitempty"`
	// HiddenTests lists test files from the case's end state that were
	// placed in hidden/. They are derived, not hand-written, and need review.
	HiddenTests []string `json:"hidden_tests,omitempty"`
}

var (
	testFilePattern = regexp.MustCompile(`(?i)(_test\.go|(^|/)test_[^/]+\.py|_test\.py|\.(test|spec)\.[cm]?[jt]sx?|(^|/)tests?/[^/]+)$`)
	codePathHint    = regexp.MustCompile("[\\w./-]+\\.\\w{1,5}\\b|`[^`]+`|\\b[a-z]+[A-Z]\\w*\\b|\\b\\w+_\\w+\\b")
	vagueRequest    = regexp.MustCompile(`(?i)\b(somehow|maybe|better|nicer|cleaner|improve|clean ?up|make it work|fix it|something|etc|whatever|daha iyi|düzelt|iyileştir|bir şekilde)\b`)
)

// HardnessOf scores a case. The score is deterministic so the same corpus
// always mines the same tasks. recurring is the set of error signatures the
// error memory has seen more than once.
func HardnessOf(c harnessmetrics.Case, recurring map[string]bool) (score int, reasons []string) {
	known := 0
	dirs := map[string]bool{}
	for _, f := range c.Files {
		if f.Unknown {
			continue
		}
		known++
		dirs[path.Dir(f.Path)] = true
	}
	switch {
	case known >= 4:
		score += 3
		reasons = append(reasons, fmt.Sprintf("multi-file: %d files changed", known))
	case known >= 2:
		score += 2
		reasons = append(reasons, fmt.Sprintf("multi-file: %d files changed", known))
	}
	if len(dirs) >= 2 {
		score++
		reasons = append(reasons, fmt.Sprintf("spans %d directories", len(dirs)))
	}
	request := strings.TrimSpace(c.Request)
	if vagueRequest.MatchString(request) {
		score++
		reasons = append(reasons, "request uses vague wording")
	}
	if len(request) > 0 && len(request) < 200 && !codePathHint.MatchString(request) {
		score++
		reasons = append(reasons, "short request naming no file or identifier")
	}
	if c.Findings != "" {
		score++
		reasons = append(reasons, "evaluator findings were left unresolved")
	}
	if len(c.Checks) > 0 && strings.Contains(strings.ToLower(c.Reason), "check") {
		score++
		reasons = append(reasons, "recorded checks were still unmet")
	}
	if sig, _, ok := errmemory.Signature(c.Findings); ok && recurring[sig] {
		score += 2
		reasons = append(reasons, "ends on an error the error memory has seen repeatedly")
	}
	return score, reasons
}

// RecurringSignatures returns the signatures the error memory under root
// holds that were fixed more than once, which marks them as hard to get
// right the first time.
func RecurringSignatures(root string) map[string]bool {
	out := map[string]bool{}
	for _, e := range errmemory.List(root) {
		if e.Fixed > 1 {
			out[e.Signature] = true
		}
	}
	return out
}

// MineOptions control MineCorpus.
type MineOptions struct {
	// MinHardness is the lowest score a case needs; zero means
	// DefaultMinHardness.
	MinHardness int
	// MaxTasks caps how many tasks are exported; zero means no cap beyond
	// the bench limit.
	MaxTasks int
	// Check replaces the recorded checks of every mined case.
	Check []string
	// Recurring is the error memory's recurring signature set.
	Recurring map[string]bool
	// Runner runs the red-at-start checks; nil uses the real command runner.
	Runner CommandRunner
}

// MineCorpus exports the hardest cases as bench tasks into suite and keeps
// only those whose check fails on the starting files. Tasks already in the
// suite are validated again instead of exported twice. The result lists
// every case considered, hardest first.
func MineCorpus(ctx context.Context, cases []harnessmetrics.Case, suite string, opts MineOptions) ([]MinedTask, error) {
	minHardness := opts.MinHardness
	if minHardness <= 0 {
		minHardness = DefaultMinHardness
	}
	limit := opts.MaxTasks
	if limit <= 0 || limit > benchMaxTasks {
		limit = benchMaxTasks
	}
	suite, err := filepath.Abs(suite)
	if err != nil {
		return nil, err
	}
	type scored struct {
		c       harnessmetrics.Case
		score   int
		reasons []string
	}
	ranked := make([]scored, 0, len(cases))
	for _, c := range cases {
		score, reasons := HardnessOf(c, opts.Recurring)
		ranked = append(ranked, scored{c, score, reasons})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].c.ID > ranked[j].c.ID
	})

	scratch, err := os.MkdirTemp("", "atlas-improve-red-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(scratch)

	var mined []MinedTask
	exported := 0
	for _, s := range ranked {
		task := MinedTask{ID: s.c.ID, Hardness: s.score, Reasons: s.reasons}
		switch {
		case s.score < minHardness:
			task.Rejected = fmt.Sprintf("hardness %d is below the minimum %d", s.score, minHardness)
		case exported >= limit:
			task.Rejected = "task limit reached"
		default:
			if err := ctx.Err(); err != nil {
				return mined, err
			}
			exported++
			if reason := mineOne(ctx, s.c, suite, scratch, opts, &task); reason != "" {
				task.Rejected = reason
				exported--
			} else {
				task.Valid = true
			}
		}
		mined = append(mined, task)
	}
	return mined, nil
}

// mineOne exports and validates a single case. It returns why the case was
// rejected, or an empty string when the task is valid; a rejected export is
// removed so the suite only holds valid tasks.
func mineOne(ctx context.Context, c harnessmetrics.Case, suite, scratch string, opts MineOptions, task *MinedTask) string {
	taskDir := filepath.Join(suite, c.ID)
	preexisting := false
	if _, err := os.Stat(taskDir); err == nil {
		preexisting = true
	} else {
		if err := os.MkdirAll(suite, 0o755); err != nil {
			return err.Error()
		}
		if _, err := ExportCase(ctx, c, suite, opts.Check); err != nil {
			return err.Error()
		}
		hidden, err := writeHiddenTests(ctx, c, taskDir)
		if err != nil {
			_ = os.RemoveAll(taskDir)
			return "hidden tests: " + err.Error()
		}
		task.HiddenTests = hidden
	}
	loaded, err := loadBenchTask(taskDir)
	if err != nil {
		if !preexisting {
			_ = os.RemoveAll(taskDir)
		}
		return err.Error()
	}
	if err := ValidateRedAtStart(ctx, loaded, scratch, opts.Runner); err != nil {
		if !preexisting {
			_ = os.RemoveAll(taskDir)
		}
		return err.Error()
	}
	return ""
}

// writeHiddenTests places the test files the case changed, as they were in
// its end state, under the task's hidden/ directory. The directory always
// exists because a bench task requires it.
func writeHiddenTests(ctx context.Context, c harnessmetrics.Case, taskDir string) ([]string, error) {
	hidden := filepath.Join(taskDir, "hidden")
	if err := os.MkdirAll(hidden, 0o755); err != nil {
		return nil, err
	}
	var placed []string
	for _, f := range c.Files {
		rel := path.Clean(strings.ReplaceAll(f.Path, `\`, "/"))
		if f.Unknown || !testFilePattern.MatchString(rel) {
			continue
		}
		target, err := safeJoin(hidden, rel)
		if err != nil {
			continue
		}
		content, err := engineering.Git(ctx, c.WorkingDir, "show", c.Commit+":"+rel)
		if err != nil || len(content) > benchMaxFileSize {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
			return nil, err
		}
		placed = append(placed, rel)
	}
	if len(placed) == 0 {
		// An empty directory is not kept by every tool; a marker keeps the
		// requirement satisfied when the suite is copied or committed.
		if err := os.WriteFile(filepath.Join(hidden, ".gitkeep"), nil, 0o644); err != nil {
			return nil, err
		}
	}
	return placed, nil
}

// ValidateRedAtStart proves a mined task can measure something: its check
// must fail on the starting files, every time it is run. A check that passes
// already measures nothing, and one that fails for a reason that is not the
// missing work (the program is absent, it timed out, the hidden tests could
// not be placed) would punish every agent alike. Mined tasks have no
// reference solution, so unlike ValidateBenchTask this cannot show that the
// check can pass; the report says so.
func ValidateRedAtStart(ctx context.Context, task BenchTask, scratch string, runner CommandRunner) error {
	if runner == nil {
		runner = RunCommand
		if err := checkBenchProgram(task); err != nil {
			return err
		}
	}
	dir := filepath.Join(scratch, task.ID)
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := materializeBenchTask(ctx, task, dir); err != nil {
		return err
	}
	if err := copyBenchTree(filepath.Join(task.Dir, "hidden"), dir); err != nil {
		return fmt.Errorf("could not place hidden tests: %w", err)
	}
	timeout := checkTimeout(task)
	args := benchCheckArgs(task)
	for range minedRedRuns {
		checkCtx, cancel := context.WithTimeout(ctx, timeout)
		out, err := runner(checkCtx, dir, args[0], args[1:])
		timedOut := checkCtx.Err() != nil
		cancel()
		switch {
		case err == nil:
			return errors.New("the check already passes on the starting files")
		case timedOut:
			return fmt.Errorf("the check timed out after %s, so its failure proves nothing", timeout)
		case errors.Is(err, exec.ErrNotFound):
			return fmt.Errorf("check program %q is not available: %w", args[0], err)
		case looksLikeMissingProgram(string(out)):
			return fmt.Errorf("the check failed because a program is missing, not because work is missing: %s", benchTail(string(out), err))
		}
	}
	return nil
}

func checkTimeout(task BenchTask) time.Duration {
	if task.TimeoutSeconds > 0 {
		return time.Duration(task.TimeoutSeconds) * time.Second
	}
	return 2 * time.Minute
}

func looksLikeMissingProgram(out string) bool {
	lower := strings.ToLower(out)
	for _, marker := range []string{"command not found", "is not recognized as an internal or external command", "executable file not found", "no such file or directory: '"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
