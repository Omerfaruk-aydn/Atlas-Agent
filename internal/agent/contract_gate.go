package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/harnessmetrics"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/memory"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workplan"
)

// The contract gate holds a turn that changed files to the work plan's
// verification contract (see internal/workplan). Its evidence is the
// commands the agent ran and their exit codes, so it needs no model call
// except for the one-time plan review. In order, at the end of such a turn:
//
//  1. A new or changed plan is reviewed once by a separate model.
//  2. Every required command must have passed after the last code change.
//     A gap sends the work back. Repeated failures escalate: a check that
//     failed twice in a row asks for a step back (hypotheses and one
//     discriminating experiment), three times asks for two genuinely
//     different approaches. A check that passed at the last green
//     checkpoint and fails now is a regression, reported with the files
//     changed since.
//  3. Once the contract holds, the state is checkpointed, and each feature
//     finished since the last follow-up gets two: its check is run against
//     the code from before the feature, where it must fail (a check that
//     cannot fail proves nothing), and an adversarial pass tries to break
//     it. The commands that verified the work are saved to project memory.
//
// Every step fails open: an error in git, the reviewer or the file system
// ends the turn as it would have ended without the gate.

// contractMaxFollowUps bounds the rounds spent on probes and breaker passes
// for one request.
const contractMaxFollowUps = 3

// contractMaxBreakerFeatures bounds the features named in one breaker pass.
const contractMaxBreakerFeatures = 4

// contractProbeAsks is how often a probe that was not run is asked for
// before it is dropped.
const contractProbeAsks = 2

// contractRegressionFiles bounds the changed files listed for a regression.
const contractRegressionFiles = 15

// contractStaleProbe is how old an abandoned probe worktree must be before
// it is removed; younger ones may belong to another session's turn.
const contractStaleProbe = 24 * time.Hour

// contractProbe is a pending proof that a finished feature's check fails on
// the code from before the feature.
type contractProbe struct {
	feature string
	command string
	dir     string
	asks    int
}

type gapKind int

const (
	gapNotRun gapKind = iota
	gapStale
	gapFailed
)

// contractGap is one required command that has not passed since the last
// code change.
type contractGap struct {
	req  workplan.Requirement
	kind gapKind
	tail string
}

func (g contractGap) String() string {
	switch g.kind {
	case gapNotRun:
		return fmt.Sprintf("`%s` (%s): not run", g.req.Command, g.req.Reason)
	case gapStale:
		return fmt.Sprintf("`%s` (%s): last run before the latest code change", g.req.Command, g.req.Reason)
	default:
		text := fmt.Sprintf("`%s` (%s): failed", g.req.Command, g.req.Reason)
		if g.tail != "" {
			text += "; output tail:\n  " + strings.ReplaceAll(g.tail, "\n", "\n  ")
		}
		return text
	}
}

// runKey distinguishes the same command run in different directories, so a
// probe run in an old checkout never stands in for the real one.
func runKey(run evaluatorCommand) string { return run.dir + "\x00" + run.command }

// contractGaps lists each required command that has not passed in the
// project itself since the last code change. Probe runs do not count.
func (l *evaluatorLedger) contractGaps(requirements []workplan.Requirement) []contractGap {
	var gaps []contractGap
	for _, req := range requirements {
		var latest *evaluatorCommand
		for _, run := range l.runs {
			if workplan.IsScratchDir(run.dir) || !workplan.Satisfies(run.command, req.Command) {
				continue
			}
			if latest == nil || run.seq > latest.seq {
				latest = &run
			}
		}
		switch {
		case latest == nil:
			gaps = append(gaps, contractGap{req: req, kind: gapNotRun})
		case latest.seq < l.lastEdit:
			gaps = append(gaps, contractGap{req: req, kind: gapStale})
		case latest.failed:
			gaps = append(gaps, contractGap{req: req, kind: gapFailed, tail: latest.tail})
		}
	}
	return gaps
}

func (a *sessionAgent) workPlanOptions() *config.WorkPlan {
	if a.taskContextConfig == nil {
		return nil
	}
	if cfg := a.taskContextConfig.Config(); cfg != nil && cfg.Options != nil {
		return cfg.Options.WorkPlan
	}
	return nil
}

// checkContract runs the gate. stop is true when its decision is final for
// this turn; otherwise the reviewer, if configured, runs next. The caller
// holds ledger.mu.
func (a *sessionAgent) checkContract(ctx context.Context, sessionID string, ledger *evaluatorLedger) (evaluatorDecision, bool) {
	if a.workingDir == "" || !ledger.changed {
		return evaluatorDecision{}, false
	}
	plan, _, err := workplan.Load(a.ledgerRoot(ledger))
	if err != nil {
		return a.sendContractBack(ctx, sessionID, ledger, nil, "the work plan cannot be read: "+err.Error()), true
	}
	if plan == nil {
		return evaluatorDecision{}, false
	}
	opts := a.workPlanOptions()
	if opts.PlanReviewEnabled() && len(plan.Features) > 0 {
		if decision, stop := a.reviewPlan(ctx, sessionID, ledger, plan); stop {
			return decision, true
		}
	}
	if problems := plan.CheckProblems(); len(problems) > 0 {
		key := strings.Join(problems, "\n")
		if ledger.checkProblemsRaised != key {
			ledger.checkProblemsRaised = key
			return evaluatorDecision{Continue: checkProblemsPrompt(problems)}, true
		}
	}
	requirements := plan.Requirements()
	if len(requirements) == 0 {
		return evaluatorDecision{}, false
	}
	if gaps := ledger.contractGaps(requirements); len(gaps) > 0 {
		return a.sendContractBack(ctx, sessionID, ledger, gaps, ""), true
	}
	ledger.failStreak = nil
	ledger.greenEdit = ledger.lastEdit + 1
	a.dropAlternatives(ctx, ledger)
	a.recordContractPassed(sessionID, a.ledgerRoot(ledger), requirements)
	return a.followUpContract(ctx, sessionID, ledger, plan, requirements)
}

// sendContractBack turns gaps into the agent's next instruction, escalating
// with repeated failures, or ends the turn once the round limit is spent.
// problem replaces the gaps when the plan itself cannot be read.
func (a *sessionAgent) sendContractBack(ctx context.Context, sessionID string, ledger *evaluatorLedger, gaps []contractGap, problem string) evaluatorDecision {
	lines := make([]string, 0, len(gaps)+1)
	if problem != "" {
		lines = append(lines, problem)
	}
	if ledger.failStreak == nil {
		ledger.failStreak = map[string]int{}
	}
	streak, stuck := 0, ""
	var regressions []string
	flakyKnown := a.harnessOptions().FlakyChecksEnabled() && a.workingDir != ""
	state := workplan.State{}
	if flakyKnown {
		state = workplan.LoadState(a.ledgerRoot(ledger))
	}
	for _, gap := range gaps {
		line := gap.String()
		flaky := gap.kind == gapFailed && flakyKnown && (ledger.isFlaky(gap.req.Command) || state.IsFlaky(gap.req.Command))
		if gap.kind == gapFailed {
			ledger.failStreak[gap.req.Command]++
		}
		switch {
		case flaky && ledger.failStreak[gap.req.Command] == 1:
			// A single failure of a flaky check says little about the
			// last change, so it neither escalates nor counts as a
			// regression. Failing again in the next round makes it real.
			line += "\n  " + contractFlakyText
		case gap.kind == gapFailed:
			if flaky {
				line += "\n  This check is known to be flaky, but it failed again: treat the failure as real."
			}
			if ledger.failStreak[gap.req.Command] > streak {
				streak, stuck = ledger.failStreak[gap.req.Command], gap.req.Command
			}
			if note := a.regressionNote(ctx, a.ledgerRoot(ledger), gap.req.Command); note != "" {
				line += "\n  " + strings.ReplaceAll(note, "\n", "\n  ")
				regressions = append(regressions, gap.req.Command)
			}
		default:
			delete(ledger.failStreak, gap.req.Command)
		}
		lines = append(lines, line)
	}
	findings := "- " + strings.Join(lines, "\n- ")
	limit := contractMaxRounds + ledger.extraRounds
	if ledger.contractRounds >= limit {
		slog.Warn("Verification contract still unmet after the round limit", "session_id", sessionID, "rounds", ledger.contractRounds)
		a.recordHarness(harnessmetrics.ContractUnresolved, sessionID, nil)
		a.dropAlternatives(ctx, ledger)
		ledger.changed = false
		return evaluatorDecision{Unresolved: "Verification contract not met:\n" + findings}
	}
	ledger.contractRounds++
	for _, command := range regressions {
		a.recordHarness(harnessmetrics.Regression, sessionID, map[string]string{"command": command})
	}
	a.recordHarness(harnessmetrics.ContractSentBack, sessionID, map[string]string{"round": fmt.Sprint(ledger.contractRounds), "streak": fmt.Sprint(streak)})
	escalation := a.settleAlternatives(ctx, sessionID, ledger)
	switch {
	case escalation != "":
	case streak >= 3:
		a.recordHarness(harnessmetrics.Alternatives, sessionID, nil)
		if alt := a.prepareAlternatives(ctx, sessionID, ledger, stuck); alt != nil {
			ledger.alternatives = alt
			escalation = a.alternativesRequest(alt)
			limit = contractMaxRounds + ledger.extraRounds
		} else {
			escalation = contractAlternativesText
		}
	case streak == 2:
		escalation = contractStepBackText
		a.recordHarness(harnessmetrics.StepBack, sessionID, nil)
	}
	slog.Info("Verification contract sent the turn back", "session_id", sessionID, "round", ledger.contractRounds, "streak", streak)
	return evaluatorDecision{Continue: contractContinuationPrompt(findings, escalation, ledger.contractRounds, limit)}
}

const contractFlakyText = "This check has both passed and failed with no code change in between, so it is flaky: this failure may not be caused by your change. " +
	"Run it again before debugging. If it keeps flipping, fix the cause of the flakiness (timing, test order, shared state, the network) instead of the code under test."

const contractStepBackText = `The same check has failed twice in a row, so the current idea is probably wrong. Stop patching and step back:
1. Read the whole failure again (if the output was saved to a log, grep it for the first error, not the last).
2. Write two or three distinct hypotheses about the cause in .atlas/progress.md.
3. Run the one cheap experiment that tells them apart: a print, a minimal reproduction, a single test.
4. Only then change code, and note what did not work so it is not tried again.`

const contractAlternativesText = `The same check has failed three times in a row: change approach rather than refine this one. Try two genuinely different approaches. If the agent or worktree tools are available, give each approach to its own sub-agent or worktree with this check as its success criterion, then keep the one whose check passes and discard the other. Otherwise implement the most different alternative yourself. Record both attempts and the outcome in .atlas/progress.md.`

func contractContinuationPrompt(findings, escalation string, round, limit int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "The verification contract in %s/%s is not met yet (check %d of %d):\n\n%s\n\n", workplan.Dir, workplan.PlanFile, round, limit, findings)
	if escalation != "" {
		b.WriteString(escalation + "\n\n")
	}
	fmt.Fprintf(&b, "Run each command, fix what fails and re-run it after your last change. If a check itself is wrong for "+
		"the project, correct it in the plan and say why; never weaken it just to pass. Update %s/%s, then give "+
		"the final report with each command and its result.", workplan.Dir, workplan.ProgressFile)
	return b.String()
}

// regressionNote explains a failing check that passed at a green
// checkpoint: when it last passed and which files changed since.
func (a *sessionAgent) regressionNote(ctx context.Context, root, command string) string {
	if !a.workPlanOptions().CheckpointsEnabled() {
		return ""
	}
	greens, err := workplan.Greens(root)
	if err != nil {
		return ""
	}
	green, ok := workplan.LastGreenPassing(greens, command)
	if !ok {
		return ""
	}
	changed, err := workplan.ChangedSince(ctx, root, green.Commit, contractRegressionFiles)
	if err != nil || len(changed) == 0 {
		return ""
	}
	ago := time.Since(time.UnixMilli(green.Time)).Round(time.Minute)
	return fmt.Sprintf("Regression: this check passed at checkpoint %s (%s ago). Files changed since then:\n%s\n"+
		"The cause is in these changes; `git diff %s` shows them in full. If the cause is not obvious, bisect: undo half of the change, re-run, repeat.",
		short(green.Commit), ago, strings.Join(changed, "\n"), short(green.Commit))
}

func short(commit string) string { return commit[:min(len(commit), 10)] }

// followUpContract runs once the contract holds: it settles pending probes,
// checkpoints the state, and follows up features finished since the last
// follow-up.
func (a *sessionAgent) followUpContract(ctx context.Context, sessionID string, ledger *evaluatorLedger, plan *workplan.Plan, requirements []workplan.Requirement) (evaluatorDecision, bool) {
	opts := a.workPlanOptions()
	var notes []string
	notes = append(notes, a.settleProbes(ctx, sessionID, ledger)...)

	state := workplan.LoadState(a.ledgerRoot(ledger))
	unsettled := state.Unsettled(plan)
	git := opts.CheckpointsEnabled() && workplan.IsGitRepo(ctx, a.ledgerRoot(ledger))
	var greens []workplan.Green
	if git {
		greens, _ = workplan.Greens(a.ledgerRoot(ledger))
	}
	if git && opts.ProveChecksEnabled() {
		notes = append(notes, a.startProbes(ctx, ledger, unsettled, greens)...)
	}
	if git {
		passed := make([]string, 0, len(requirements))
		for _, r := range requirements {
			passed = append(passed, r.Command)
		}
		if _, err := workplan.RecordGreen(ctx, a.ledgerRoot(ledger), plan.DoneIDs(), passed); err != nil {
			slog.Warn("Failed to record a green checkpoint", "error", err)
		}
	}
	if opts.LearnRecipesEnabled() {
		a.learnRecipes(sessionID, requirements)
	}
	if opts.BreakerEnabled() && len(unsettled) > 0 {
		notes = append(notes, breakerNote(unsettled, a.agentToolAvailable()))
		a.recordHarness(harnessmetrics.BreakerPass, sessionID, map[string]string{"features": fmt.Sprint(len(unsettled))})
	}
	if len(unsettled) > 0 {
		for _, f := range unsettled {
			state.Settled = append(state.Settled, f.ID)
		}
		if err := workplan.SaveState(a.ledgerRoot(ledger), state); err != nil {
			slog.Warn("Failed to save work plan state", "error", err)
		}
	}
	if len(notes) == 0 {
		return evaluatorDecision{}, false
	}
	if ledger.followUps >= contractMaxFollowUps {
		a.dropProbes(ctx, ledger)
		return evaluatorDecision{}, false
	}
	ledger.followUps++
	return evaluatorDecision{Continue: "The verification contract holds. Before you report:\n\n" + strings.Join(notes, "\n\n") +
		"\n\nThen re-run the contract and give the final report with each command and its result."}, true
}

// startProbes checks out, for each newly finished feature with its own
// check, the last green state without the feature, and asks the agent to
// run the check there, where it must fail.
func (a *sessionAgent) startProbes(ctx context.Context, ledger *evaluatorLedger, finished []workplan.Feature, greens []workplan.Green) []string {
	workplan.PruneProbes(ctx, a.ledgerRoot(ledger), contractStaleProbe)
	var notes []string
	for _, feature := range finished {
		if feature.Verify == "" {
			continue
		}
		base, ok := workplan.LastGreenWithout(greens, feature.ID)
		if !ok {
			continue
		}
		dir, err := workplan.AddProbe(ctx, a.ledgerRoot(ledger), base.Commit, probeName(feature.ID))
		if err != nil {
			slog.Warn("Failed to prepare a probe worktree", "feature", feature.ID, "error", err)
			continue
		}
		if ledger.probes == nil {
			ledger.probes = map[string]contractProbe{}
		}
		probe := contractProbe{feature: feature.ID, command: feature.Verify, dir: dir}
		ledger.probes[feature.ID] = probe
		notes = append(notes, probeRequest(probe, a.ledgerRoot(ledger)))
	}
	return notes
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

func probeName(feature string) string {
	return unsafeName.ReplaceAllString(feature, "_")
}

func probeRequest(p contractProbe, root string) string {
	rel, err := filepath.Rel(root, p.dir)
	if err != nil {
		rel = p.dir
	}
	return fmt.Sprintf("Prove the check for %s can fail: %s holds the code from before %s. Run `%s` with working_dir %q. "+
		"It is expected to FAIL there; a pass means the check does not test %s.",
		p.feature, filepath.ToSlash(rel), p.feature, p.command, filepath.ToSlash(rel), p.feature)
}

// settleProbes judges pending probes by the runs made in their worktrees.
func (a *sessionAgent) settleProbes(ctx context.Context, sessionID string, ledger *evaluatorLedger) []string {
	var notes []string
	for feature, probe := range ledger.probes {
		var latest *evaluatorCommand
		for _, run := range ledger.runs {
			if sameDir(a.ledgerRoot(ledger), run.dir, probe.dir) && workplan.Satisfies(run.command, probe.command) && (latest == nil || run.seq > latest.seq) {
				latest = &run
			}
		}
		switch {
		case latest == nil && probe.asks+1 < contractProbeAsks:
			probe.asks++
			ledger.probes[feature] = probe
			notes = append(notes, probeRequest(probe, a.ledgerRoot(ledger)))
			continue
		case latest != nil && !latest.failed:
			a.recordHarness(harnessmetrics.VacuousCheck, sessionID, map[string]string{"feature": feature})
			notes = append(notes, fmt.Sprintf("The check for %s (`%s`) passed on the code from before %s existed, so it does not test %s. "+
				"Strengthen it so it fails without the feature and passes with it, update the plan, and run it again.", feature, probe.command, feature, feature))
		}
		_ = workplan.RemoveProbe(ctx, a.ledgerRoot(ledger), probe.dir)
		delete(ledger.probes, feature)
	}
	return notes
}

func (a *sessionAgent) dropProbes(ctx context.Context, ledger *evaluatorLedger) {
	for feature, probe := range ledger.probes {
		_ = workplan.RemoveProbe(ctx, a.ledgerRoot(ledger), probe.dir)
		delete(ledger.probes, feature)
	}
}

// sameDir compares a run's working directory, as the agent wrote it, with a
// probe's absolute path.
func sameDir(root, ran, probe string) bool {
	if ran == "" {
		return false
	}
	if !filepath.IsAbs(ran) {
		ran = filepath.Join(root, ran)
	}
	a, b := filepath.Clean(ran), filepath.Clean(probe)
	if os.PathSeparator == '\\' {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// agentToolAvailable reports whether the session can delegate to a
// sub-agent.
func (a *sessionAgent) agentToolAvailable() bool {
	return slices.ContainsFunc(a.tools.Copy(), func(t fantasy.AgentTool) bool {
		name := t.Info().Name
		return name == AgentToolName || name == "delegate"
	})
}

func breakerNote(features []workplan.Feature, delegate bool) string {
	shown := features[:min(len(features), contractMaxBreakerFeatures)]
	names := make([]string, 0, len(shown))
	for _, f := range shown {
		names = append(names, f.ID+" ("+f.Title+")")
	}
	brief := "Find inputs and situations that make " + strings.Join(names, ", ") + " misbehave: edge cases, invalid or hostile input, " +
		"empty and very large data, repeated and concurrent actions, interrupted operations, slow or failing dependencies, and, for " +
		"anything with a user interface, keyboard-only use and narrow screens. Write a failing test or a reproducible script for each " +
		"real problem; do not fix the code."
	text := "Try to break what was just finished. "
	if delegate {
		text += "Delegate this to a sub-agent with a fresh context, since the author is the worst judge of their own work. Brief: \"" + brief + "\""
	} else {
		text += brief
	}
	return text + " Then fix each real problem, keep the new tests, and add the ones worth keeping to the contract."
}

// learnRecipes saves the commands that verified the work to project memory
// once, so the next session starts with known-good checks.
func (a *sessionAgent) learnRecipes(sessionID string, requirements []workplan.Requirement) {
	if a.taskContextConfig == nil {
		return
	}
	store := memoryStore(a.taskContextConfig)
	current, err := store.Read(memory.ScopeProject)
	if err != nil {
		return
	}
	var fresh []string
	for _, r := range requirements {
		if !strings.Contains(current, "`"+r.Command+"`") {
			fresh = append(fresh, "`"+r.Command+"`")
		}
	}
	if len(fresh) == 0 {
		return
	}
	entry := "Verified checks (passed " + time.Now().Format("2006-01-02") + "): " + strings.Join(fresh, ", ")
	if _, err := store.Add(memory.ScopeProject, entry); err != nil {
		slog.Debug("Could not save verified checks to project memory", "error", err)
		return
	}
	a.recordHarness(harnessmetrics.RecipeLearned, sessionID, map[string]string{"commands": fmt.Sprint(len(fresh))})
}

// checkProblemsPrompt asks the agent to fix plan checks that can never pass.
func checkProblemsPrompt(problems []string) string {
	return fmt.Sprintf("Some checks in %s/%s cannot work as checks, so the verification contract ignores them:\n- %s\n\n"+
		"A check is a command that exits 0 when the work is right. Replace each with such a command, or remove it; check a "+
		"web page with the browser tool rather than a test suite. Then run the contract's checks again and give the final report.",
		workplan.Dir, workplan.PlanFile, strings.Join(problems, "\n- "))
}
