package agent

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workplan"
)

// The diff checks (weakened checks, leftover debug code) and the failure
// corpus compare a file with what it was before the request touched it.
// Turns are not committed, so git cannot say what the request changed; the
// baseline is captured instead, by wrapping the file tools so a file's
// content is read just before the first edit of the request reaches it.
// Files changed by shell commands are not captured, which keeps the checks
// conservative: they can only miss a change, never invent one.

// baselineMaxFiles and baselineMaxBytes bound the memory spent on
// baselines; a larger file is recorded as too large and not analyzed.
const (
	baselineMaxFiles = 300
	baselineMaxBytes = 1 << 20
)

// baselineTools are the file tools whose target is captured before they
// run. Tools that touch many files at once (renames) are left out.
var baselineTools = []string{"edit", "write", "multiedit", "replace_symbol", "lsp_replace_symbol"}

// fileBaseline is a file's content before the request's first edit of it.
type fileBaseline struct {
	content string
	// existed is false for a file the request created.
	existed bool
	// tooLarge marks a file over the size limit, whose content is not kept.
	tooLarge bool
}

func (a *sessionAgent) harnessOptions() *config.Harness {
	if a.taskContextConfig == nil {
		return nil
	}
	if cfg := a.taskContextConfig.Config(); cfg != nil && cfg.Options != nil {
		return cfg.Options.Harness
	}
	return nil
}

// baselinesNeeded reports whether any feature reads the baselines.
func (a *sessionAgent) baselinesNeeded() bool {
	opts := a.harnessOptions()
	return a.completionGateActive() && (opts.CheckGuardEnabled() || opts.DiffHygieneEnabled() || opts.FailureCorpusEnabled() || opts.ErrorMemoryEnabled())
}

// captureBaselines wraps the file tools in palette so the first edit of
// each file in a request records the file's previous content, and a work
// plan written in another folder moves the project root there.
func (a *sessionAgent) captureBaselines(palette []fantasy.AgentTool) []fantasy.AgentTool {
	if a.workingDir == "" || !a.completionGateActive() {
		return palette
	}
	var out []fantasy.AgentTool
	for i, tool := range palette {
		if !slices.Contains(baselineTools, tool.Info().Name) {
			continue
		}
		if out == nil {
			out = slices.Clone(palette)
		}
		out[i] = &baselineTool{AgentTool: tool, agent: a}
	}
	if out == nil {
		return palette
	}
	return out
}

type baselineTool struct {
	fantasy.AgentTool
	agent *sessionAgent
}

func (t *baselineTool) Run(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	path := evaluatorCallPath(fantasy.ToolCallContent{Input: call.Input})
	sessionID := tools.GetSessionFromContext(ctx)
	if sessionID == "" || path == "" {
		return t.AgentTool.Run(ctx, call)
	}
	abs := t.agent.absPath(path)
	if t.agent.baselinesNeeded() {
		t.agent.recordBaseline(sessionID, abs)
	}
	resp, err := t.AgentTool.Run(ctx, call)
	if err == nil && !resp.IsError {
		t.agent.notePlanWrite(sessionID, abs)
	}
	return resp, err
}

// absPath resolves a tool's path argument against the working directory.
func (a *sessionAgent) absPath(path string) string {
	if !filepath.IsAbs(path) {
		path = filepath.Join(a.workingDir, path)
	}
	return filepath.Clean(path)
}

// recordBaseline keeps path's current content unless the request already
// has a baseline for it.
func (a *sessionAgent) recordBaseline(sessionID, path string) {
	ledger := a.evaluatorLedgerFor(sessionID)
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	if _, ok := ledger.baselines[path]; ok || len(ledger.baselines) >= baselineMaxFiles {
		return
	}
	if ledger.baselines == nil {
		ledger.baselines = map[string]fileBaseline{}
	}
	ledger.baselines[path] = readBaseline(path)
}

func readBaseline(path string) fileBaseline {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return fileBaseline{}
	}
	if err != nil || info.IsDir() {
		return fileBaseline{existed: true, tooLarge: true}
	}
	if info.Size() > baselineMaxBytes {
		return fileBaseline{existed: true, tooLarge: true}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fileBaseline{existed: true, tooLarge: true}
	}
	return fileBaseline{content: string(data), existed: true}
}

// fileChange is one captured file with its content before and now.
type fileChange struct {
	path    string // relative to the working directory, slash separated
	before  string
	after   string
	existed bool
	exists  bool
	size    int64
}

// baselineChanges pairs each captured file with its current content.
// Files over the size limit come back with only their size, unchanged
// files are skipped. The caller holds ledger.mu.
func (a *sessionAgent) baselineChanges(ledger *evaluatorLedger) []fileChange {
	paths := make([]string, 0, len(ledger.baselines))
	for path := range ledger.baselines {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	var out []fileChange
	for _, path := range paths {
		base := ledger.baselines[path]
		rel, err := filepath.Rel(a.workingDir, path)
		if err != nil {
			rel = path
		}
		if workplan.IsScratchDir(filepath.ToSlash(rel)) {
			continue // Work in a probe or alternative worktree is not the change.
		}
		change := fileChange{path: filepath.ToSlash(rel), before: base.content, existed: base.existed}
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() {
			change.exists = true
			change.size = info.Size()
			if info.Size() <= baselineMaxBytes && !base.tooLarge {
				data, readErr := os.ReadFile(path)
				if readErr != nil {
					continue
				}
				change.after = string(data)
			}
		}
		if base.tooLarge || change.size > baselineMaxBytes {
			if change.exists && !base.existed {
				out = append(out, change) // A new large file is itself worth a look.
			}
			continue
		}
		if change.exists == base.existed && change.after == base.content {
			continue
		}
		out = append(out, change)
	}
	return out
}
