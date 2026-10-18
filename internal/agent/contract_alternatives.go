package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
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
	for _, name := range []string{"a", "b"} {
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
	first, second := alt.relDir(alt.dirs[0]), alt.relDir(alt.dirs[1])
	var b strings.Builder
	fmt.Fprintf(&b, "The same check has failed three times in a row: change approach rather than refine this one. "+
		"Atlas checked the current project out twice, into %s and %s (installed dependencies and other ignored files are not copied; install them there first if the check needs them). ", first, second)
	brief := func(dir, which string) string {
		return fmt.Sprintf("Work only inside %s (absolute path %s): pass it as working_dir to bash and edit files under it. "+
			"Take %s approach to make `%s` pass there, genuinely different from what has been tried so far (read %s/%s first). "+
			"Stop when the check passes or the approach proves wrong, and report what you changed and the check's result.",
			dir, filepath.ToSlash(filepath.Join(alt.root, dir)), which, alt.command, workplan.Dir, workplan.ProgressFile)
	}
	if a.agentToolAvailable() {
		fmt.Fprintf(&b, "Start two sub-agents in parallel with the agent tool, one per worktree.\n\nBrief for the first: %q\n\nBrief for the second: %q\n\n",
			brief(first, "one"), brief(second, "a second, different"))
	} else {
		fmt.Fprintf(&b, "Try one approach in each worktree yourself, one after the other.\n\nFirst: %s\n\nSecond: %s\n\n",
			brief(first, "one"), brief(second, "a second, different"))
	}
	fmt.Fprintf(&b, "When both are done, run `%s` yourself with working_dir set to each worktree, so Atlas can tell which one passes. "+
		"Do not change the project itself meanwhile. Record both approaches and their outcome in %s/%s.", alt.command, workplan.Dir, workplan.ProgressFile)
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
	var passed string
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
		if !latest.failed && passed == "" {
			passed = dir
		}
	}
	switch {
	case passed != "":
		alt.winner = passed
		for _, dir := range alt.dirs {
			if dir != passed {
				_ = workplan.RemoveProbe(ctx, a.ledgerRoot(ledger), dir)
			}
		}
		a.recordHarness(harnessmetrics.AlternativeWon, sessionID, nil)
		return a.applyWinnerRequest(alt)
	case ran < len(alt.dirs) && alt.asks+1 < contractAlternativeAsks:
		alt.asks++
		return a.alternativesRequest(alt)
	default:
		a.dropAlternatives(ctx, ledger)
		return "Neither alternative made the check pass. Compare what the attempts had in common: the cause is probably an assumption all of them share. " +
			"Question it explicitly (re-read the failing output and the code it points to, check the environment and the check itself), then fix the project directly."
	}
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
