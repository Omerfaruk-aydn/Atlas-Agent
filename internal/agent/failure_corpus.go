package agent

import (
	"context"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/harnessmetrics"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workplan"
)

// saveFailureCase keeps a request that ended with unmet checks or findings
// in the failure corpus, so `atlas eval corpus` can turn it into a bench
// task. The caller holds ledger.mu.
func (a *sessionAgent) saveFailureCase(ctx context.Context, sessionID string, ledger *evaluatorLedger, reason, findings string) {
	if a.workingDir == "" || a.isSubAgent || !a.harnessOptions().FailureCorpusEnabled() {
		return
	}
	root := a.harness.Root()
	if root == "" || strings.TrimSpace(ledger.request) == "" {
		return
	}
	now := time.Now()
	c := harnessmetrics.Case{
		ID:         harnessmetrics.NewCaseID(now, sessionID),
		Time:       now.UnixMilli(),
		Reason:     reason,
		Request:    ledger.request,
		WorkingDir: a.ledgerRoot(ledger),
		Findings:   findings,
	}
	if plan, _, err := workplan.Load(a.ledgerRoot(ledger)); err == nil && plan != nil {
		for _, r := range plan.Requirements() {
			c.Checks = append(c.Checks, r.Command)
		}
	}
	if workplan.IsGitRepo(ctx, a.ledgerRoot(ledger)) {
		if commit, err := workplan.Snapshot(ctx, a.ledgerRoot(ledger), "atlas: failure corpus "+c.ID); err == nil {
			if err := workplan.KeepCommit(ctx, a.ledgerRoot(ledger), harnessmetrics.CorpusRef+c.ID, commit); err == nil {
				c.Commit = commit
			}
		}
	}
	paths := make([]string, 0, len(ledger.baselines))
	for path := range ledger.baselines {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	for _, path := range paths {
		base := ledger.baselines[path]
		rel, err := filepath.Rel(a.ledgerRoot(ledger), path)
		if err != nil || strings.HasPrefix(rel, "..") || workplan.IsScratchDir(filepath.ToSlash(rel)) {
			continue // Outside the project: not part of a replay.
		}
		c.Files = append(c.Files, harnessmetrics.CaseFile{
			Path: filepath.ToSlash(rel), Before: base.content, Existed: base.existed, Unknown: base.tooLarge,
		})
	}
	if err := harnessmetrics.SaveCase(root, c); err != nil {
		slog.Debug("Could not save a failure corpus case", "error", err)
		return
	}
	a.recordHarness(harnessmetrics.CorpusSaved, sessionID, map[string]string{"reason": reason})
}

// recordContractPassed records a holding contract with the project's kinds
// and commands, the evidence behind strategy hints.
func (a *sessionAgent) recordContractPassed(sessionID, root string, requirements []workplan.Requirement) {
	commands := make([]string, 0, len(requirements))
	for _, r := range requirements {
		commands = append(commands, r.Command)
	}
	a.recordHarness(harnessmetrics.ContractPassed, sessionID, harnessmetrics.CheckAttrs(harnessmetrics.ProjectKinds(root), commands))
}

// strategyHintLimit bounds the commands suggested.
const strategyHintLimit = 4

// strategyHintsBlock suggests the checks that verified projects of the same
// kind before, while the project has no verification contract of its own.
func (a *sessionAgent) strategyHintsBlock(sessionID string) string {
	if a.workingDir == "" || a.isSubAgent || !a.harnessOptions().StrategyHintsEnabled() {
		return ""
	}
	root := a.projectRoot(sessionID)
	plan, _, err := workplan.Load(root)
	if err != nil || plan != nil && len(plan.Verify) > 0 {
		return ""
	}
	kinds := harnessmetrics.ProjectKinds(root)
	hints := harnessmetrics.CheckHints(a.harness.Root(), kinds, strategyHintLimit)
	if len(hints) == 0 {
		return ""
	}
	return "\n\n<known_checks>\nCommands that verified work on " + strings.Join(kinds, "/") + " projects here before: `" +
		strings.Join(hints, "`, `") + "`. Consider them when choosing how to verify substantial work; check they apply first.\n</known_checks>"
}
