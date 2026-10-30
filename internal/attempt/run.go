package attempt

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// defaultStrategies give attempts different angles so they are not N copies
// of the same answer. They are hints, not constraints.
var defaultStrategies = []string{
	"Approach: make the smallest correct change that satisfies the task, reusing existing helpers and following the conventions of the surrounding code.",
	"Approach: work test-first. Add or extend a test that captures the requirement, watch it fail, then implement until the project's tests pass.",
	"Approach: find the root cause or the right abstraction before editing. Prefer a robust fix over patching the symptom, and check the callers of anything you change.",
}

// Run executes the attempts, scores them and persists the report. The
// user's working tree is never modified.
func Run(ctx context.Context, opts Options) (*Report, error) {
	if err := normalize(&opts); err != nil {
		return nil, err
	}
	snap, err := takeSnapshot(ctx, opts.Root)
	if err != nil {
		return nil, err
	}
	id, err := newID(time.Now())
	if err != nil {
		return nil, err
	}
	runDir := filepath.Join(opts.StateDir, id)
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		return nil, err
	}
	workBase := filepath.Join(opts.WorkDir, id)
	if err := os.MkdirAll(workBase, 0o700); err != nil {
		return nil, err
	}
	defer func() {
		if opts.Keep {
			return
		}
		// Worktrees are already removed; this drops the agents' private
		// state directories and any worktree whose removal failed.
		_ = os.RemoveAll(workBase)
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		_, _ = git(cleanup, opts.Root, nil, "worktree", "prune")
	}()

	verify, source := ResolveVerify(opts.Root, opts.Verify)
	report := &Report{
		ID:           id,
		Task:         opts.Task,
		CreatedAt:    time.Now().UTC(),
		Root:         opts.Root,
		BaseHead:     snap.Head,
		VerifySource: source,
		Verify:       verify,
		Lint:         ResolveLint(opts.Root, opts.Lint),
		Attempts:     make([]Attempt, opts.N),
	}

	r := &runner{opts: opts, snap: snap, report: report, runDir: runDir, workBase: workBase}
	r.runAttempts(ctx)
	if err := ctx.Err(); err != nil {
		report.Warnings = append(report.Warnings, "the run was canceled; results are partial")
	}
	r.finish(ctx)
	if err := Save(opts.StateDir, report); err != nil {
		return report, err
	}
	return report, nil
}

// ChildEnv is set in the environment of every agent an attempt run starts.
// An agent inside an attempt must not start attempts of its own: the
// fan-out would multiply cost and worktrees without bound.
const ChildEnv = "ATLAS_ATTEMPT_CHILD"

func normalize(o *Options) error {
	if os.Getenv(ChildEnv) != "" {
		return errors.New("attempts cannot be started from inside an attempt")
	}
	if strings.TrimSpace(o.Task) == "" {
		return errors.New("a task is required")
	}
	if o.N < 1 || o.N > MaxAttempts {
		return fmt.Errorf("attempts must be between 1 and %d", MaxAttempts)
	}
	if o.Root == "" || o.StateDir == "" {
		return errors.New("root and state directory are required")
	}
	root, err := filepath.Abs(o.Root)
	if err != nil {
		return err
	}
	o.Root = root
	if o.StateDir, err = filepath.Abs(o.StateDir); err != nil {
		return err
	}
	if o.WorkDir == "" {
		o.WorkDir = filepath.Join(os.TempDir(), "atlas-attempts")
	}
	if o.WorkDir, err = filepath.Abs(o.WorkDir); err != nil {
		return err
	}
	if o.Parallel < 1 {
		o.Parallel = defaultParallel
	}
	if o.Timeout <= 0 {
		o.Timeout = defaultTimeout
	}
	if o.CheckTimeout <= 0 {
		o.CheckTimeout = defaultCheckTimeout
	}
	if o.Check == nil {
		o.Check = ExecCheck
	}
	if o.Agent == nil {
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("locating the Atlas binary: %w", err)
		}
		o.Agent = ExecAgent(exe)
	}
	if o.Progress == nil {
		o.Progress = func(string) {}
	}
	return nil
}

func newID(now time.Time) (string, error) {
	var b [2]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return now.UTC().Format("20060102-150405") + "-" + hex.EncodeToString(b[:]), nil
}

type runner struct {
	opts     Options
	snap     snapshot
	report   *Report
	runDir   string
	workBase string
	// createMu serializes "git worktree add", which updates shared
	// repository metadata.
	createMu sync.Mutex
}

func label(i int) string {
	return string(rune('A' + i))
}

func (r *runner) runAttempts(ctx context.Context) {
	sem := make(chan struct{}, r.opts.Parallel)
	var wg sync.WaitGroup
	for i := range r.report.Attempts {
		a := &r.report.Attempts[i]
		a.Index = i + 1
		a.Label = label(i)
		if len(r.opts.Models) > 0 {
			a.Model = r.opts.Models[i%len(r.opts.Models)]
		}
		if r.opts.N > 1 {
			strategies := r.opts.Strategies
			if len(strategies) == 0 {
				strategies = defaultStrategies
			}
			a.Strategy = strategies[i%len(strategies)]
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				a.Status = StatusCanceled
				return
			}
			defer func() {
				if p := recover(); p != nil {
					a.Status, a.Error = StatusInfra, fmt.Sprintf("internal error: %v", p)
				}
			}()
			r.runOne(ctx, a)
		}()
	}
	wg.Wait()
}

func attemptPrompt(task, strategy string) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(task))
	b.WriteString("\n\n---\nYou are one of several independent attempts at this task, working in your own disposable copy of the repository. ")
	b.WriteString("Make the change by editing files directly in the current directory. Do not commit, push, or touch anything outside this directory. ")
	b.WriteString("Run the project's checks before you finish and fix what they report.")
	if strategy != "" {
		b.WriteString("\n" + strategy)
	}
	return b.String()
}

func (r *runner) runOne(ctx context.Context, a *Attempt) {
	started := time.Now()
	defer func() { a.DurationMS = time.Since(started).Milliseconds() }()

	path := filepath.Join(r.workBase, fmt.Sprintf("a%d", a.Index))
	r.createMu.Lock()
	wt, err := createWorktree(ctx, r.opts.Root, path, r.snap)
	r.createMu.Unlock()
	if wt != nil && !r.opts.Keep {
		defer func() {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
			defer cancel()
			if err := wt.remove(cleanup); err != nil {
				a.Notes = append(a.Notes, "worktree cleanup failed: "+err.Error())
			}
		}()
	}
	if err != nil {
		if ctx.Err() != nil {
			a.Status = StatusCanceled
			return
		}
		a.Status, a.Error = StatusInfra, err.Error()
		return
	}
	if r.opts.Keep {
		a.Notes = append(a.Notes, "worktree kept at "+wt.Path)
	}

	if setup := strings.TrimSpace(r.opts.Setup); setup != "" {
		r.opts.Progress(fmt.Sprintf("attempt %s: running setup", a.Label))
		res := r.opts.Check(ctx, wt.Path, setup, r.opts.CheckTimeout)
		if !res.Passed {
			a.Status = StatusSetupFailed
			a.Error = fmt.Sprintf("setup command failed (exit %d): %s", res.ExitCode, clip(res.Output, 400))
			return
		}
	}

	r.opts.Progress(fmt.Sprintf("attempt %s: running agent%s", a.Label, modelSuffix(a.Model)))
	agentCtx, cancel := context.WithTimeout(ctx, r.opts.Timeout)
	res, agentErr := r.opts.Agent(agentCtx, AgentJob{
		Dir:      wt.Path,
		StateDir: filepath.Join(r.workBase, fmt.Sprintf("state-a%d", a.Index)),
		Model:    a.Model,
		Prompt:   attemptPrompt(r.opts.Task, a.Strategy),
		Yolo:     r.opts.Yolo,
	})
	timedOut := errors.Is(agentCtx.Err(), context.DeadlineExceeded)
	cancel()
	a.Response = clip(strings.TrimSpace(res.Response), 4000)
	a.SessionID = res.SessionID
	a.PromptTokens, a.CompletionTokens, a.Cost = res.PromptTokens, res.CompletionTokens, res.Cost
	a.ToolCalls = res.ToolCalls
	switch {
	case ctx.Err() != nil:
		a.Status = StatusCanceled
	case timedOut:
		a.Status, a.Error = StatusTimeout, fmt.Sprintf("the agent did not finish within %s", r.opts.Timeout)
	case agentErr != nil:
		a.Status, a.Error = StatusAgentError, clip(agentErr.Error(), 600)
	case res.FinishReason == "error" || res.FinishError != "":
		a.Status = StatusAgentError
		a.Error = clip(strings.TrimSpace("agent stopped with an error: "+res.FinishError), 600)
	default:
		a.Status = StatusCompleted
	}
	if a.Status == StatusCanceled {
		return
	}

	// Capture whatever the agent produced, even after a failure, so the
	// partial work stays inspectable.
	captureCtx, captureCancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	defer captureCancel()
	patch, files, err := wt.capture(captureCtx)
	if err != nil {
		a.Status, a.Error = StatusInfra, "capturing the change: "+err.Error()
		return
	}
	a.Files = files
	for _, f := range files {
		a.Additions += f.Additions
		a.Deletions += f.Deletions
	}
	a.PatchBytes = len(patch)
	if patch != "" {
		sum := sha256.Sum256([]byte(patch))
		a.PatchHash = hex.EncodeToString(sum[:])
		a.PatchFile = fmt.Sprintf("attempt-%d.patch", a.Index)
		if err := os.WriteFile(filepath.Join(r.runDir, a.PatchFile), []byte(patch), 0o600); err != nil {
			a.Status, a.Error = StatusInfra, "saving the patch: "+err.Error()
			return
		}
	}
	if a.Status != StatusCompleted {
		return
	}
	if patch == "" {
		a.Status = StatusNoChanges
		return
	}
	a.Eligible = true

	for _, command := range r.report.Verify {
		r.opts.Progress(fmt.Sprintf("attempt %s: verify %s", a.Label, command))
		a.Checks = append(a.Checks, r.opts.Check(ctx, wt.Path, command, r.opts.CheckTimeout))
	}
	for _, command := range r.report.Lint {
		r.opts.Progress(fmt.Sprintf("attempt %s: lint %s", a.Label, command))
		a.Lint = append(a.Lint, r.opts.Check(ctx, wt.Path, command, r.opts.CheckTimeout))
	}
	if ctx.Err() != nil {
		a.Status, a.Eligible = StatusCanceled, false
	}
}

func modelSuffix(model string) string {
	if model == "" {
		return ""
	}
	return " (" + model + ")"
}

// finish dedupes, judges, scores and ranks.
func (r *runner) finish(ctx context.Context) {
	rep := r.report
	for i := range rep.Attempts {
		a := &rep.Attempts[i]
		rep.TotalCost += a.Cost
		if !a.Eligible {
			continue
		}
		patch, err := os.ReadFile(filepath.Join(r.runDir, a.PatchFile))
		if err != nil {
			a.Eligible, a.Notes = false, append(a.Notes, "stored patch unreadable: "+err.Error())
			continue
		}
		applies := patchApplies(ctx, rep.Root, patch) == nil
		a.Applies = &applies
		if !applies {
			a.Eligible = false
			a.Notes = append(a.Notes, "the patch no longer applies to the tree the run started from (it changed during the run)")
		}
	}
	// Identical patches are one candidate for the judge and share a score.
	first := map[string]int{}
	for i := range rep.Attempts {
		a := &rep.Attempts[i]
		if !a.Eligible {
			continue
		}
		if prev, ok := first[a.PatchHash]; ok {
			a.DuplicateOf = prev
			a.Notes = append(a.Notes, fmt.Sprintf("identical to attempt %s", label(prev-1)))
		} else {
			first[a.PatchHash] = a.Index
		}
	}

	r.judge(ctx)

	scoreDeterministic(rep.Attempts, len(rep.Verify) > 0, len(rep.Lint) > 0)
	// Duplicates inherit the judge verdict of the attempt they copy.
	for i := range rep.Attempts {
		a := &rep.Attempts[i]
		if a.DuplicateOf > 0 {
			a.Judge = rep.Attempts[a.DuplicateOf-1].Judge
		}
	}
	combine(rep.Attempts)
	rep.Winner = rank(rep.Attempts)
	rep.Confidence, rep.ConfidenceReasons = assess(rep)
	rep.Warnings = append(rep.Warnings, warnings(rep)...)
}

func (r *runner) judge(ctx context.Context) {
	rep := r.report
	rep.Judge.Model = r.opts.Judge
	var candidates []judgeCandidate
	for i := range rep.Attempts {
		a := &rep.Attempts[i]
		if !a.Eligible || a.DuplicateOf > 0 {
			continue
		}
		patch, err := os.ReadFile(filepath.Join(r.runDir, a.PatchFile))
		if err != nil {
			continue
		}
		candidates = append(candidates, judgeCandidate{Label: a.Label, Patch: string(patch), Attempt: a})
	}
	switch {
	case r.opts.NoJudge:
		rep.Judge.Skipped = "judging was disabled"
		return
	case len(candidates) < 2:
		rep.Judge.Skipped = "fewer than two distinct candidates to compare"
		return
	}
	scratch := filepath.Join(r.workBase, "judge")
	if err := os.MkdirAll(scratch, 0o700); err != nil {
		rep.Judge.Error = err.Error()
		return
	}
	defer os.RemoveAll(scratch)
	r.opts.Progress(fmt.Sprintf("judging %d candidates%s", len(candidates), modelSuffix(r.opts.Judge)))
	timeout := min(r.opts.Timeout, 10*time.Minute)
	judgeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	res, err := r.opts.Agent(judgeCtx, AgentJob{
		Dir:      scratch,
		StateDir: filepath.Join(r.workBase, "state-judge"),
		Model:    r.opts.Judge,
		Prompt:   buildJudgePrompt(r.opts.Task, rep.Verify, candidates),
	})
	rep.Judge.Cost = res.Cost
	rep.TotalCost += res.Cost
	if err != nil {
		rep.Judge.Error = clip(err.Error(), 400)
		return
	}
	labels := make([]string, len(candidates))
	for i, c := range candidates {
		labels[i] = c.Label
	}
	verdicts, winner, summary, err := parseJudgeReply(res.Response, labels)
	if err != nil {
		rep.Judge.Error = err.Error()
		return
	}
	rep.Judge.Ran, rep.Judge.Winner, rep.Judge.Summary = true, winner, summary
	for _, c := range candidates {
		v := verdicts[c.Label]
		c.Attempt.Judge = &v
	}
}

func warnings(rep *Report) []string {
	var out []string
	if rep.Judge.Error != "" {
		out = append(out, "the judge failed ("+rep.Judge.Error+"); ranking uses deterministic checks only")
	}
	if len(rep.Verify) == 0 {
		out = append(out, "no verify command was found; pass --verify or add verify commands to .atlas/work.json for a ranking grounded in execution")
	}
	anyPassed, anyEligible := false, false
	for _, a := range rep.Attempts {
		anyEligible = anyEligible || a.Eligible
		anyPassed = anyPassed || (a.Eligible && a.VerifyPassed)
	}
	if len(rep.Verify) > 0 && anyEligible && !anyPassed {
		out = append(out, "no attempt passed the verify commands; if they also fail on the unmodified tree, fix that first")
	}
	if rep.Judge.Ran && len(rep.Attempts) > 1 {
		same := true
		for _, a := range rep.Attempts {
			if a.Model != rep.Judge.Model {
				same = false
			}
		}
		if same && rep.Judge.Model != "" {
			out = append(out, "the judge uses the same model as every attempt, which tends to favour its own style; use a different judge model when possible")
		}
	}
	return out
}
