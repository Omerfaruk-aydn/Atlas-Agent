package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/csync"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/errmemory"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workplan"
	"github.com/stretchr/testify/require"
)

func newLedgerAgent() *sessionAgent {
	return &sessionAgent{evaluatorLedgers: csync.NewMap[string, *evaluatorLedger](), tools: csync.NewSlice[fantasy.AgentTool]()}
}

func TestBudgetLine(t *testing.T) {
	t.Parallel()
	require.Empty(t, budgetLine(200_000, 10_000, 0, 2), "short turns with little context pay nothing")
	require.Equal(t, int64(160_000), compactionPoint(200_000, 0))
	require.Equal(t, int64(750_000), compactionPoint(1_000_000, 0))
	require.Equal(t, int64(500_000), compactionPoint(1_000_000, 0.5))
	require.Zero(t, compactionPoint(0, 0))

	line := budgetLine(1_000_000, 400_000, 0, 3)
	require.Contains(t, line, "Context: 400k of 1000k tokens used (40%); the conversation is summarized at about 750k, 350k from now.")
	require.Contains(t, line, "This turn has taken 3 steps.")

	require.Contains(t, budgetLine(1_000_000, 700_000, 0, 1), "A summary is close")
	require.Contains(t, budgetLine(0, 0, 0, 30), "This turn has taken 30 steps. If the last steps")
}

func TestWithBudgetLineReplacesThePreviousOne(t *testing.T) {
	t.Parallel()
	messages := []fantasy.Message{fantasy.NewUserMessage("hi"), fantasy.NewSystemMessage(budgetLineMarker + "\nold\n</context_budget>")}
	out := withBudgetLine(messages, budgetLineMarker+"\nnew\n</context_budget>")
	require.Len(t, out, 2)
	require.Contains(t, out[1].Content[0].(fantasy.TextPart).Text, "new")
	require.Len(t, withBudgetLine(out, ""), 1)
}

func TestLedgerMarksFlakyCommands(t *testing.T) {
	t.Parallel()
	l := &evaluatorLedger{}
	run := func(failed bool) {
		l.seq++
		r := evaluatorCommand{command: "npm test", failed: failed, seq: l.seq}
		l.noteFlaky(r)
		l.remember(r)
	}
	run(true)
	l.seq++
	l.lastEdit = l.seq // A fix in between: passing next is not flaky.
	run(false)
	require.False(t, l.isFlaky("npm test"))
	run(true) // No edit since the pass.
	require.True(t, l.isFlaky("npm test"))
	require.Equal(t, []string{"npm test"}, l.newFlaky)
}

func TestLedgerLearnsAFixFromFailureEditsAndPass(t *testing.T) {
	t.Parallel()
	l := &evaluatorLedger{}
	l.seq++
	failed := evaluatorCommand{command: "go build ./...", failed: true, seq: l.seq}
	l.trackError(failed, "# app\n./main.go:12:3: undefined: helper\n")
	l.seq++
	l.edits = append(l.edits, editRecord{seq: l.seq, path: "main.go", summary: "main.go: +`func helper() {}`"})
	l.seq++
	l.trackError(evaluatorCommand{command: "go build ./...", seq: l.seq}, "")
	require.Len(t, l.learned, 1)
	require.Equal(t, []string{"main.go: +`func helper() {}`"}, l.learned[0].fixes)
	require.Contains(t, l.learned[0].signature, "undefined: helper")

	// A pass with no edits since the failure is a flake, not a fix.
	l2 := &evaluatorLedger{}
	l2.trackError(evaluatorCommand{command: "make", failed: true, seq: 1}, "error: boom happened\n")
	l2.trackError(evaluatorCommand{command: "make", seq: 2}, "")
	require.Empty(t, l2.learned)

	// A failing test passed by changing only tests is not a reusable fix.
	l3 := &evaluatorLedger{}
	l3.trackError(evaluatorCommand{command: "npm run test:e2e", failed: true, seq: 1}, "Error: a visible planet should be available for selection\n")
	l3.edits = append(l3.edits, editRecord{seq: 2, path: "D:/Orbitarium/e2e/solar-system.spec.ts", summary: "spec: -`goto('/')` +`goto('/?e2e')`"})
	l3.trackError(evaluatorCommand{command: "npm run test:e2e", seq: 3}, "")
	require.Empty(t, l3.learned)
}

func TestSaveHarnessLearningWritesFlakyAndFixes(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	a := newLedgerAgent()
	a.workingDir = root
	ledger := &evaluatorLedger{
		newFlaky: []string{"npm test"},
		learned:  []learnedFix{{signature: "error: x is undefined", line: "error: x is undefined", command: "npm test", fixes: []string{"a.js: +`let x`"}}},
	}
	a.saveHarnessLearning("s1", ledger)
	require.True(t, workplan.LoadState(root).IsFlaky("npm test"))
	entry, ok := errmemory.Lookup(root, "error: x is undefined")
	require.True(t, ok)
	require.Equal(t, []string{"a.js: +`let x`"}, entry.Fixes)
	require.Nil(t, ledger.learned)
	require.Nil(t, ledger.newFlaky)
}

func TestEditSummary(t *testing.T) {
	t.Parallel()
	summary := editSummary(fantasy.ToolCallContent{Input: `{"file_path":"src/a.go","old_string":"x := 1\nreturn x","new_string":"x := 2\nreturn x"}`})
	require.Equal(t, "src/a.go: -`x := 1` +`x := 2`", summary)
	require.Equal(t, "b.go: rewrote the file", editSummary(fantasy.ToolCallContent{Input: `{"file_path":"b.go","content":"package b"}`}))
}

func TestBaselineChangesSeeEditsAndSkipScratchDirs(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	a := newLedgerAgent()
	a.workingDir = root
	file := filepath.Join(root, "a.txt")
	require.NoError(t, os.WriteFile(file, []byte("one\n"), 0o644))
	scratch := filepath.Join(root, workplan.Dir, workplan.AlternativeDir, "x", "b.txt")
	require.NoError(t, os.MkdirAll(filepath.Dir(scratch), 0o755))

	a.recordBaseline("s1", file)
	a.recordBaseline("s1", scratch)
	a.recordBaseline("s1", filepath.Join(root, "new.txt"))
	require.NoError(t, os.WriteFile(file, []byte("one\ntwo\n"), 0o644))
	require.NoError(t, os.WriteFile(scratch, []byte("x\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "new.txt"), []byte("n\n"), 0o644))
	a.recordBaseline("s1", file) // A later edit keeps the first baseline.

	ledger := a.evaluatorLedgerFor("s1")
	changes := a.baselineChanges(ledger)
	require.Len(t, changes, 2)
	require.Equal(t, "a.txt", changes[0].path)
	require.Equal(t, "one\n", changes[0].before)
	require.Equal(t, "new.txt", changes[1].path)
	require.False(t, changes[1].existed)
}

func TestSettleAlternativesPicksThePassingWorktree(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	a := newLedgerAgent()
	a.workingDir = root
	dirA := filepath.Join(root, workplan.Dir, workplan.AlternativeDir, "s-a")
	dirB := filepath.Join(root, workplan.Dir, workplan.AlternativeDir, "s-b")
	ledger := &evaluatorLedger{alternatives: &contractAlternatives{command: "make test", root: root, dirs: []string{dirA, dirB}}}

	// Nothing run yet: asked again once, then given up.
	note := a.settleAlternatives(t.Context(), "s1", ledger)
	require.Contains(t, note, "run `make test` yourself with working_dir")

	ledger.remember(evaluatorCommand{command: "make test", failed: true, seq: 1, dir: filepath.ToSlash(filepath.Join(workplan.Dir, workplan.AlternativeDir, "s-a"))})
	ledger.remember(evaluatorCommand{command: "make test", seq: 2, dir: dirB})
	note = a.settleAlternatives(t.Context(), "s1", ledger)
	require.Contains(t, note, "The approach in .atlas/alternatives/s-b makes `make test` pass")
	require.Contains(t, note, "git -C .atlas/alternatives/s-b diff --cached --binary HEAD > .atlas/alternatives/s-b.patch")
	require.Equal(t, dirB, ledger.alternatives.winner)

	a.dropAlternatives(t.Context(), ledger)
	require.Nil(t, ledger.alternatives)
}

func TestContractFlakyFailureDoesNotEscalateOnce(t *testing.T) {
	t.Parallel()
	a := newLedgerAgent()
	a.workingDir = t.TempDir()
	ledger := &evaluatorLedger{
		flaky: map[string]bool{runKey(evaluatorCommand{command: "npm test"}): true},
		runs:  map[string]evaluatorCommand{runKey(evaluatorCommand{command: "npm test"}): {command: "npm test", failed: true}},
	}
	gap := contractGap{req: workplan.Requirement{Command: "npm test", Reason: "contract check"}, kind: gapFailed}
	decision := a.sendContractBack(t.Context(), "s1", ledger, []contractGap{gap}, "")
	require.Contains(t, decision.Continue, "so it is flaky")
	decision = a.sendContractBack(t.Context(), "s1", ledger, []contractGap{gap}, "")
	require.Contains(t, decision.Continue, "failed again: treat the failure as real")
	require.True(t, strings.Contains(decision.Continue, "step back") || strings.Contains(decision.Continue, "Step back"))
}
