package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/harnessmetrics"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/secrets"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/verification"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workplan"
	"github.com/google/uuid"
)

// completeTurn is the sole normal-ending entry point: the ordered gates feed
// the existing aggregate decision and one continuation, never a second verdict.
func (a *sessionAgent) completeTurn(ctx context.Context, id, report string) evaluatorDecision {
	a.syncProjectEvidence(ctx, id)
	decision := a.evaluateTurn(ctx, id, report)
	decision, _ = a.decideTurnCompletion(ctx, id, decision)
	return decision
}

func (a *sessionAgent) recordStallCompletion(ctx context.Context, id, reason string) {
	input, ok := a.completionInput(ctx, id, evaluatorDecision{})
	if !ok {
		return
	}
	input.Blocker = reason
	decision := decideCompletion(input)
	if _, err := a.persistCompletion(ctx, id, input, decision, false); err != nil {
		slog.Warn("Failed to persist blocked completion", "session_id", id, "error", err)
	}
	a.sessionTrace(id).record(harnessmetrics.TraceEvent{Kind: "gate", Name: "aggregate_completion", Outcome: "blocked", Reason: "strategy_budget_exhausted"})
}

// invokeCheck uses the original hook/permission-aware tools, with runtime
// ownership, budget and trace wrappers. It never runs an unguarded subprocess.
func (a *sessionAgent) invokeCheck(ctx context.Context, id, dir, command string) (fantasy.ToolResponse, error) {
	ctx = context.WithValue(ctx, tools.SessionIDContextKey, id)
	input, _ := json.Marshal(tools.BashParams{Command: command, WorkingDir: dir, Description: "Configured completion verification", AutoBackgroundAfter: 600})
	for _, tool := range observedTools(trackSourceRoot(guardTools(a.tools.Copy(), a.engineering, a.sessions), a.workingDir)) {
		if tool.Info().Name == tools.BashToolName {
			return tool.Run(ctx, fantasy.ToolCall{ID: uuid.NewString(), Name: tools.BashToolName, Input: string(input)})
		}
	}
	return fantasy.ToolResponse{}, fmt.Errorf("bash tool unavailable for configured checks")
}

func (a *sessionAgent) deterministicChecks(ctx context.Context, id string, l *evaluatorLedger) (evaluatorDecision, bool) {
	_, gate := startGate(ctx, "deterministic_checks")
	if l.projectError != "" {
		gate.finish("unverified", "source_snapshot_unavailable")
		return evaluatorDecision{Unverified: "Project change evidence is unavailable: " + l.projectError}, true
	}
	if len(l.qualityErrors) > 0 {
		gate.finish("send_back", "new_lint_errors")
		return a.qualitySendBack(l, "New errors must be fixed before completion:\n"+strings.Join(l.qualityErrors, "\n")), true
	}
	if decision, stop := a.scanChangedSecrets(l); stop {
		gate.finish("send_back", "changed_file_secret")
		return decision, true
	}
	opts := a.harnessOptions()
	if opts == nil || len(opts.CompletionCommands) == 0 {
		gate.finish("skipped", "no_configured_commands")
		return evaluatorDecision{}, false
	}
	for _, command := range opts.CompletionCommands {
		checkCtx, cancel := context.WithTimeout(ctx, opts.CompletionTimeout())
		resp, err := a.invokeCheck(checkCtx, id, a.ledgerRoot(l), command)
		cancel()
		exit, finished := bashExitCode(resp.Metadata)
		if err != nil || resp.StopTurn || !finished {
			gate.finish("unverified", "check_unavailable")
			slog.Warn("Configured completion check could not finish", "session_id", id)
			return evaluatorDecision{Unverified: "Configured deterministic check could not finish; inspect permissions, timeout and tool availability"}, true
		}
		l.seq++
		l.remember(evaluatorCommand{command: command, failed: exit != 0 || resp.IsError, seq: l.seq})
		if exit != 0 || resp.IsError {
			gate.finish("send_back", "check_failed")
			return a.qualitySendBack(l, "Configured deterministic check failed: "+command+"\n"+evaluatorTruncate(resp.Content)), true
		}
	}
	gate.finish("pass", "configured_checks_passed")
	return evaluatorDecision{}, false
}

// scanChangedSecrets applies the full credential scanner to every existing
// file changed during the request. Findings are counted, never copied into a
// continuation, so a detected credential cannot leak through gate output.
func (a *sessionAgent) scanChangedSecrets(l *evaluatorLedger) (evaluatorDecision, bool) {
	return a.scanChangedSecretsWithStat(l, os.Stat)
}

func (a *sessionAgent) scanChangedSecretsWithStat(l *evaluatorLedger, stat func(string) (os.FileInfo, error)) (evaluatorDecision, bool) {
	var findings int
	for _, name := range l.files {
		if workplan.IsAtlasPath(name) {
			continue
		}
		path := name
		if !filepath.IsAbs(path) {
			path = filepath.Join(a.ledgerRoot(l), filepath.FromSlash(path))
		}
		info, err := stat(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil || info == nil || !info.Mode().IsRegular() {
			return evaluatorDecision{Unverified: "Changed-file secret scan could not finish"}, true
		}
		result, err := secrets.Scan(path, secrets.Options{})
		if err != nil {
			return evaluatorDecision{Unverified: "Changed-file secret scan could not finish"}, true
		}
		findings += len(result.Findings)
	}
	if findings > 0 {
		return a.qualitySendBack(l, fmt.Sprintf("Changed-file secret scan reported %d possible credential(s). Remove them from source, rotate exposed values, and rerun the scan.", findings)), true
	}
	return evaluatorDecision{}, false
}

func (a *sessionAgent) qualitySendBack(l *evaluatorLedger, reason string) evaluatorDecision {
	if l.qualityRounds >= 2 {
		return evaluatorDecision{Unresolved: reason}
	}
	l.qualityRounds++
	return evaluatorDecision{Continue: reason + "\nFix the implementation and rerun the same checks; do not weaken the checks."}
}

func (a *sessionAgent) mutationCheck(ctx context.Context, id string, l *evaluatorLedger) (evaluatorDecision, bool) {
	ctx, gate := startGate(ctx, "mutation")
	opts := a.harnessOptions()
	if !opts.MutationEnabled() {
		gate.finish("skipped", "disabled")
		return evaluatorDecision{}, false
	}
	changes := a.baselineChanges(l)
	modifiedTests := slices.ContainsFunc(changes, func(c fileChange) bool { return isTestFile(c.path, c.before) })
	if !modifiedTests {
		gate.finish("skipped", "no_changed_tests")
		return evaluatorDecision{}, false
	}
	var sources []verification.Change
	for _, c := range changes {
		if isTestFile(c.path, c.before) || !c.exists {
			continue
		}
		if !slices.Contains([]string{".go", ".py", ".ts", ".tsx", ".js", ".jsx", ".rs", ".java", ".cs"}, filepath.Ext(c.path)) {
			continue
		}
		lines := []int{}
		for _, line := range addedLines(c.before, c.after) {
			lines = append(lines, line.n)
		}
		if len(lines) > 0 {
			sources = append(sources, verification.Change{Path: c.path, Before: c.before, After: c.after, Lines: lines})
		}
	}
	timeout := opts.MutationTimeoutSeconds
	if timeout == 0 {
		timeout = 60
	}
	r, err := verification.RunMutations(ctx, a.ledgerRoot(l), sources, verification.MutationOptions{Max: opts.MutationMax, Timeout: time.Duration(timeout) * time.Second, Command: opts.MutationCommand}, func(ctx context.Context, dir, command string) (int, string, error) {
		resp, err := a.invokeCheck(ctx, id, dir, command)
		if err != nil {
			return 0, "", err
		}
		exit, finished := bashExitCode(resp.Metadata)
		if !finished || resp.StopTurn {
			return 0, "", fmt.Errorf("mutation test did not complete")
		}
		return exit, resp.Content, nil
	})
	if err != nil {
		gate.finish("unverified", "mutation_unavailable")
		slog.Warn("Mutation check could not finish", "session_id", id, "error", err)
		return evaluatorDecision{Unverified: "Mutation verification could not finish: " + err.Error()}, true
	}
	if r.Invalid > 0 || r.Unsupported > 0 || r.Generated == 0 {
		gate.finish("unverified", "mutation_not_measured")
		return evaluatorDecision{Unverified: fmt.Sprintf("Mutation coverage is unverified: generated=%d invalid=%d unsupported=%d", r.Generated, r.Invalid, r.Unsupported)}, true
	}
	if r.Survived > 0 {
		gate.finish("send_back", "surviving_mutants")
		return a.qualitySendBack(l, fmt.Sprintf("%d changed-line Go mutant(s) survived. Strengthen behavior assertions and rerun tests.", r.Survived)), true
	}
	gate.finish("pass", "all_mutants_killed")
	return evaluatorDecision{}, false
}
