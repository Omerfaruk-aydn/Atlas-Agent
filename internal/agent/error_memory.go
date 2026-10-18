package agent

import (
	"cmp"
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/errmemory"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/harnessmetrics"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workplan"
)

// Error memory: a failing command followed by edits and then a passing run
// of the same command is a fix. Its error signature and a summary of the
// edits are saved to the project (see internal/errmemory), and when the
// same error shows up again its fix is appended to the failing output.

// pendingError is the latest failure of one command, waiting for a pass.
type pendingError struct {
	signature string
	line      string
	command   string
	seq       int
}

// learnedFix is a fix observed in this request, not yet saved.
type learnedFix struct {
	signature string
	line      string
	command   string
	fixes     []string
	files     []string
}

// editSummaryChars bounds one edit's summary.
const editSummaryChars = 160

// editSummary describes one code change in a line: the file and the first
// line it removed and added.
func editSummary(tc fantasy.ToolCallContent) string {
	var p struct {
		FilePath    string `json:"file_path"`
		Path        string `json:"path"`
		OldString   string `json:"old_string"`
		NewString   string `json:"new_string"`
		Replacement string `json:"replacement"`
		Symbol      string `json:"symbol"`
		Edits       []struct {
			OldString string `json:"old_string"`
			NewString string `json:"new_string"`
		} `json:"edits"`
	}
	if json.Unmarshal([]byte(tc.Input), &p) != nil {
		return ""
	}
	file := filepath.ToSlash(cmp.Or(p.FilePath, p.Path))
	if len(p.Edits) > 0 {
		p.OldString, p.NewString = p.Edits[0].OldString, p.Edits[0].NewString
	}
	switch {
	case p.Symbol != "":
		return file + ": rewrote " + p.Symbol
	case p.OldString == "" && p.NewString == "":
		return file + ": rewrote the file"
	}
	removed, added := firstDifferentLine(p.OldString, p.NewString), firstDifferentLine(p.NewString, p.OldString)
	summary := file + ":"
	if removed != "" {
		summary += " -`" + removed + "`"
	}
	if added != "" {
		summary += " +`" + added + "`"
	}
	if len(summary) > editSummaryChars {
		summary = summary[:editSummaryChars] + "…"
	}
	return summary
}

// firstDifferentLine returns the first non-blank line of a that b lacks.
func firstDifferentLine(a, b string) string {
	have := map[string]bool{}
	for line := range strings.SplitSeq(b, "\n") {
		have[strings.TrimSpace(line)] = true
	}
	for line := range strings.SplitSeq(a, "\n") {
		if text := strings.TrimSpace(line); text != "" && !have[text] {
			return text
		}
	}
	return ""
}

// trackError notes a run's failure, or learns a fix when a failed command
// now passes after edits.
func (l *evaluatorLedger) trackError(run evaluatorCommand, output string) {
	if workplan.IsScratchDir(run.dir) {
		return
	}
	key := runKey(run)
	if run.failed {
		signature, line, ok := errmemory.Signature(output)
		if !ok {
			return
		}
		if l.pendingErrors == nil {
			l.pendingErrors = map[string]pendingError{}
		}
		if prev, seen := l.pendingErrors[key]; seen && prev.signature == signature {
			return // Keep the first failure: edits since it are the fix.
		}
		l.pendingErrors[key] = pendingError{signature: signature, line: line, command: run.command, seq: run.seq}
		return
	}
	failure, ok := l.pendingErrors[key]
	if !ok {
		return
	}
	delete(l.pendingErrors, key)
	var fixes, files []string
	for _, edit := range l.edits {
		if edit.seq <= failure.seq || edit.seq >= run.seq || workplan.IsPlanFile(edit.path) || workplan.IsScratchDir(edit.path) {
			continue
		}
		if edit.summary != "" && !slices.Contains(fixes, edit.summary) {
			fixes = append(fixes, edit.summary)
		}
		if edit.path != "" && !slices.Contains(files, edit.path) {
			files = append(files, edit.path)
		}
	}
	if len(fixes) == 0 {
		return // Passing again without edits is a flake, not a fix.
	}
	if onlyTestFiles(files) {
		// A failing test made to pass by changing only tests is about that
		// test, not a fix another project could reuse.
		return
	}
	l.learned = append(l.learned, learnedFix{signature: failure.signature, line: failure.line, command: failure.command, fixes: fixes, files: files})
}

// saveHarnessLearning writes what the request learned to the project:
// flaky commands to the plan state and fixes to the error memory. The
// caller holds ledger.mu.
func (a *sessionAgent) saveHarnessLearning(sessionID string, ledger *evaluatorLedger) {
	if a.workingDir == "" {
		return
	}
	opts := a.harnessOptions()
	if len(ledger.newFlaky) > 0 && opts.FlakyChecksEnabled() {
		state := workplan.LoadState(a.ledgerRoot(ledger))
		added := false
		for _, command := range ledger.newFlaky {
			if state.AddFlaky(command) {
				added = true
				a.recordHarness(harnessmetrics.FlakyCheck, sessionID, map[string]string{"command": command})
			}
		}
		if added {
			if err := workplan.SaveState(a.ledgerRoot(ledger), state); err != nil {
				slog.Debug("Could not save flaky checks", "error", err)
			}
		}
	}
	ledger.newFlaky = nil
	if len(ledger.learned) > 0 && opts.ErrorMemoryEnabled() {
		for _, fix := range ledger.learned {
			if err := errmemory.Learn(a.ledgerRoot(ledger), fix.signature, fix.line, fix.command, fix.fixes, fix.files); err != nil {
				slog.Debug("Could not save an error fix", "error", err)
				continue
			}
			a.recordHarness(harnessmetrics.ErrorLearned, sessionID, nil)
		}
	}
	ledger.learned = nil
}

// recallErrors wraps the bash tool in palette so a failing command whose
// error was fixed before shows that fix.
func (a *sessionAgent) recallErrors(palette []fantasy.AgentTool) []fantasy.AgentTool {
	if a.workingDir == "" || a.isSubAgent || !a.harnessOptions().ErrorMemoryEnabled() {
		return palette
	}
	i := slices.IndexFunc(palette, func(t fantasy.AgentTool) bool { return t.Info().Name == tools.BashToolName })
	if i < 0 {
		return palette
	}
	out := slices.Clone(palette)
	out[i] = &errorRecallTool{AgentTool: palette[i], agent: a}
	return out
}

type errorRecallTool struct {
	fantasy.AgentTool
	agent *sessionAgent
}

func (t *errorRecallTool) Run(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	resp, err := t.AgentTool.Run(ctx, call)
	if err != nil {
		return resp, err
	}
	exit, finished := bashExitCode(resp.Metadata)
	if !resp.IsError && (!finished || exit == 0) {
		return resp, nil
	}
	signature, _, ok := errmemory.Signature(resp.Content)
	if !ok {
		return resp, nil
	}
	sessionID := tools.GetSessionFromContext(ctx)
	if sessionID != "" {
		ledger := t.agent.evaluatorLedgerFor(sessionID)
		ledger.mu.Lock()
		shown := ledger.recalled[signature]
		if ledger.recalled == nil {
			ledger.recalled = map[string]bool{}
		}
		ledger.recalled[signature] = true
		ledger.mu.Unlock()
		if shown {
			return resp, nil
		}
	}
	entry, found := errmemory.Lookup(t.agent.workingDir, signature)
	if !found {
		return resp, nil
	}
	resp.Content += "\n\n" + errmemory.Note(entry)
	t.agent.recordHarness(harnessmetrics.ErrorRecalled, sessionID, nil)
	return resp, nil
}

// onlyTestFiles reports whether every path is a test file.
func onlyTestFiles(paths []string) bool {
	if len(paths) == 0 {
		return false
	}
	for _, path := range paths {
		if !testPathPattern.MatchString(filepath.ToSlash(path)) {
			return false
		}
	}
	return true
}
