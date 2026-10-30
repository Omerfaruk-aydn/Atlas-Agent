package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/harnessmetrics"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workplan"
)

// When the same check has failed three rounds in a row, the current idea is
// unlikely to work and the cheapest way out is to try others side by side.
// Asking the agent to "try two approaches" rarely changes anything, so Atlas
// makes it concrete: it snapshots the project, checks the snapshot out into
// two git worktrees, and asks for one genuinely different approach in each,
// in parallel sub-agents when the agent tool is available. The gate then
// judges by the check's runs in each worktree, names the one that passes and
// gives the commands that bring it into the project. The user's branch,
// index and files are never touched by Atlas; applying the winner is an
// ordinary change the agent makes.

// contractAlternativeRounds are the extra contract rounds granted when
// alternatives are prepared: one to try them, one to bring the winner in.
const contractAlternativeRounds = 2

// contractAlternativeAsks is how often alternatives that were not run are
// asked for before they are dropped.
const contractAlternativeAsks = 2

// contractAlternatives are the worktrees prepared for one stuck check.
type contractAlternatives struct {
	command string
	// root is the project the worktrees were checked out from.
	root string
	dirs []string
	asks int
	// winner is the worktree whose check passed, kept until the project's
	// own check passes.
	winner string
	// replayed bounds green-checkpoint recovery to one fresh strategy.
	replayed bool
}

// prepareAlternatives creates the worktrees for command, or returns nil
// where that is not possible (no git, an error): the plain escalation text
// is used then. The caller holds ledger.mu.
func (a *sessionAgent) prepareAlternatives(ctx context.Context, sessionID string, ledger *evaluatorLedger, command string) *contractAlternatives {
	if ledger.alternativesTried || !a.harnessOptions().ParallelAlternativesEnabled() || !workplan.IsGitRepo(ctx, a.ledgerRoot(ledger)) {
		return nil
	}
	ledger.alternativesTried = true
	workplan.PruneProbes(ctx, a.ledgerRoot(ledger), contractStaleProbe)
	base, err := workplan.Snapshot(ctx, a.ledgerRoot(ledger), "atlas: alternatives base")
	if err != nil {
		slog.Warn("Failed to snapshot the project for alternative approaches", "error", err)
		return nil
	}
	prefix := probeName(sessionID)
	prefix = prefix[:min(len(prefix), 8)]
	alt := &contractAlternatives{command: command, root: a.ledgerRoot(ledger)}
	attempts := 3
	if a.taskContextConfig != nil && a.taskContextConfig.Config() != nil && a.taskContextConfig.Config().Options != nil {
		if configured := a.taskContextConfig.Config().Options.MaxConcurrentSubAgents; configured > 3 {
			attempts = min(5, configured)
		}
	}
	for _, name := range []string{"a", "b", "c", "d", "e"}[:attempts] {
		dir, err := workplan.AddWorktree(ctx, a.ledgerRoot(ledger), workplan.AlternativeDir, base, prefix+"-"+name)
		if err != nil {
			slog.Warn("Failed to prepare an alternative worktree", "error", err)
			for _, made := range alt.dirs {
				_ = workplan.RemoveProbe(ctx, a.ledgerRoot(ledger), made)
			}
			return nil
		}
		alt.dirs = append(alt.dirs, dir)
	}
	ledger.extraRounds = contractAlternativeRounds
	a.recordHarness(harnessmetrics.AlternativesPrepared, sessionID, nil)
	return alt
}

func (alt *contractAlternatives) relDir(dir string) string {
	rel, err := filepath.Rel(alt.root, dir)
	if err != nil {
		return filepath.ToSlash(dir)
	}
	return filepath.ToSlash(rel)
}

// alternativesRequest is the instruction that goes with fresh worktrees.
func (a *sessionAgent) alternativesRequest(alt *contractAlternatives) string {
	var b strings.Builder
	fmt.Fprintf(&b, "The same check has failed three times in a row. Atlas prepared %d isolated worktrees. Use a genuinely different approach in each; installed dependencies and ignored files are not copied.\n", len(alt.dirs))
	for i, dir := range alt.dirs {
		rel := alt.relDir(dir)
		fmt.Fprintf(&b, "\nAttempt %c (%s, absolute %s): work only there. First map entry points, interfaces, consumers, tests and traps with codemap/project_map/impact_analysis; use that shared map to enrich the implementation. Run `%s`, inspect the full diff and write .atlas/alternative-summary.json with id, approach, files, checks, regressions, risks and diff. Use id %q.\n", 'A'+i, rel, filepath.ToSlash(filepath.Join(alt.root, dir)), alt.command, rel)
	}
	if a.agentToolAvailable() {
		fmt.Fprintf(&b, "\nRun these attempts as parallel sub-agents, bounded by MaxConcurrentSubAgents and the existing cost budget. Do not change the parent checkout.\n")
	} else {
		fmt.Fprintf(&b, "\nTry the attempts sequentially. Do not change the parent checkout.\n")
	}
	fmt.Fprintf(&b, "When all are done, rerun `%s` in every worktree. Candidates with failing checks are eliminated; eligible candidates enter pairwise tournament selection. The winner will be applied and the full contract rerun in the parent.", alt.command)
	return b.String()
}

// settleAlternatives judges the alternatives by the check's runs in their
// worktrees and returns the instruction that replaces the escalation text,
// or "" once they are done with. The caller holds ledger.mu.
func (a *sessionAgent) settleAlternatives(ctx context.Context, sessionID string, ledger *evaluatorLedger) string {
	alt := ledger.alternatives
	if alt == nil {
		return ""
	}
	if alt.winner != "" {
		return a.applyWinnerRequest(alt)
	}
	var candidates []AlternativeSummary
	ran := 0
	for _, dir := range alt.dirs {
		var latest *evaluatorCommand
		for _, run := range ledger.runs {
			if sameDir(a.ledgerRoot(ledger), run.dir, dir) && workplan.Satisfies(run.command, alt.command) && (latest == nil || run.seq > latest.seq) {
				latest = &run
			}
		}
		if latest == nil {
			continue
		}
		ran++
		if !latest.failed {
			candidates = append(candidates, readAlternativeSummary(dir, alt.relDir(dir), alt.command))
		}
	}
	winner := a.tournamentWinner(ctx, sessionID, candidates)
	switch {
	case winner != "":
		for _, dir := range alt.dirs {
			if alt.relDir(dir) == winner {
				alt.winner = dir
				break
			}
		}
		for _, dir := range alt.dirs {
			if dir != alt.winner {
				_ = workplan.RemoveProbe(ctx, a.ledgerRoot(ledger), dir)
			}
		}
		a.recordHarness(harnessmetrics.AlternativeWon, sessionID, nil)
		return a.applyWinnerRequest(alt)
	case ran < len(alt.dirs) && alt.asks+1 < contractAlternativeAsks:
		alt.asks++
		return a.alternativesRequest(alt)
	default:
		if request := a.replayFromGreen(ctx, sessionID, ledger, alt); request != "" {
			return request
		}
		a.dropAlternatives(ctx, ledger)
		return "Neither alternative made the check pass. Compare what the attempts had in common: the cause is probably an assumption all of them share. " +
			"Question it explicitly (re-read the failing output and the code it points to, check the environment and the check itself), then fix the project directly."
	}
}

// replayFromGreen gives one failed attempt a clean checkpoint and a different
// strategy. It only replaces Atlas-owned worktrees, never the user's checkout.
func (a *sessionAgent) replayFromGreen(ctx context.Context, sessionID string, ledger *evaluatorLedger, alt *contractAlternatives) string {
	if alt.replayed {
		return ""
	}
	greens, err := workplan.Greens(a.ledgerRoot(ledger))
	if err != nil {
		return ""
	}
	green, ok := workplan.LastGreenPassing(greens, alt.command)
	if !ok {
		return ""
	}
	var failed *evaluatorCommand
	for _, recorded := range ledger.runs {
		run := recorded
		if run.failed && workplan.Satisfies(run.command, alt.command) && slices.ContainsFunc(alt.dirs, func(dir string) bool { return sameDir(a.ledgerRoot(ledger), run.dir, dir) }) && (failed == nil || run.seq > failed.seq) {
			failed = &run
		}
	}
	if failed == nil {
		return ""
	}
	dir := alt.dirs[0]
	if _, err := workplan.AddWorktree(ctx, a.ledgerRoot(ledger), workplan.AlternativeDir, green.Commit, filepath.Base(dir)+"-replay"); err != nil {
		return ""
	}
	alt.replayed = true
	alt.dirs = []string{filepath.Join(a.ledgerRoot(ledger), workplan.Dir, workplan.AlternativeDir, filepath.Base(dir)+"-replay")}
	alt.asks = 0
	ledger.extraRounds++
	a.recordHarness(harnessmetrics.AlternativesPrepared, sessionID, map[string]string{"replay": "true", "checkpoint": short(green.Commit)})
	return fmt.Sprintf("All isolated approaches failed. Replay from the last green checkpoint %s instead of restarting. Work only in %s. Previous failure:\n%s\nChoose a materially different strategy, preserve the checks, write .atlas/alternative-summary.json with id %q, then run `%s` with working_dir set to that worktree.", short(green.Commit), alt.relDir(alt.dirs[0]), failed.tail, alt.relDir(alt.dirs[0]), alt.command)
}

func (a *sessionAgent) applyWinnerRequest(alt *contractAlternatives) string {
	dir := alt.relDir(alt.winner)
	patch := dir + ".patch"
	return fmt.Sprintf("The approach in %s makes `%s` pass. Bring it into the project and drop the other one: run\n"+
		"  git -C %s add -A\n  git -C %s diff --cached --binary HEAD > %s\n  git apply %s\n"+
		"from the project root (resolve any conflict by hand), then run the whole contract in the project itself.",
		dir, alt.command, dir, dir, patch, patch)
}

// dropAlternatives removes the worktrees. The caller holds ledger.mu.
func (a *sessionAgent) dropAlternatives(ctx context.Context, ledger *evaluatorLedger) {
	if ledger.alternatives == nil {
		return
	}
	for _, dir := range ledger.alternatives.dirs {
		_ = workplan.RemoveProbe(ctx, a.ledgerRoot(ledger), dir)
		_ = os.Remove(dir + ".patch")
	}
	ledger.alternatives = nil
}
