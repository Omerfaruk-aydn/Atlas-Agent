package attempt

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseJudgeReply(t *testing.T) {
	t.Parallel()
	labels := []string{"A", "B"}

	good := "```json\n{\"scores\":[{\"solution\":\"a\",\"score\":12,\"rationale\":\"r\",\"concerns\":[\"c\",\" \"]},{\"solution\":\"B\",\"score\":-2}],\"winner\":\"b\",\"summary\":\"s\"}\n```"
	verdicts, winner, summary, err := parseJudgeReply(good, labels)
	require.NoError(t, err)
	require.Equal(t, "B", winner)
	require.Equal(t, "s", summary)
	require.InDelta(t, 10, verdicts["A"].Score, 0)
	require.InDelta(t, 0, verdicts["B"].Score, 0)
	require.Equal(t, []string{"c"}, verdicts["A"].Concerns)

	for name, reply := range map[string]string{
		"no json":   "I think A is better",
		"bad json":  `{"scores": [}`,
		"unknown":   `{"scores":[{"solution":"A","score":1},{"solution":"B","score":1},{"solution":"Z","score":1}]}`,
		"duplicate": `{"scores":[{"solution":"A","score":1},{"solution":"A","score":2},{"solution":"B","score":1}]}`,
		"missing":   `{"scores":[{"solution":"A","score":1}]}`,
	} {
		_, _, _, err := parseJudgeReply(reply, labels)
		require.Error(t, err, name)
	}

	// An unknown winner label is dropped rather than trusted.
	_, winner, _, err = parseJudgeReply(`{"scores":[{"solution":"A","score":1},{"solution":"B","score":2}],"winner":"Q"}`, labels)
	require.NoError(t, err)
	require.Empty(t, winner)
}

func TestFirstJSONObjectSkipsBraceNoise(t *testing.T) {
	t.Parallel()
	obj, ok := firstJSONObject(`note {not json} then {"a":"}{","b":{"c":1}} trailing`)
	require.True(t, ok)
	require.JSONEq(t, `{"a":"}{","b":{"c":1}}`, obj)
	_, ok = firstJSONObject("no braces")
	require.False(t, ok)
}

func TestJudgePromptIsAnonymousAndFencesData(t *testing.T) {
	t.Parallel()
	a := Attempt{Label: "A", Model: "secret-model", Additions: 1, Checks: []CheckResult{{Command: "go test", Passed: false, Output: "boom   here"}}}
	b := Attempt{Label: "B", Model: "other-model"}
	prompt := buildJudgePrompt("do the thing", []string{"go test"}, []judgeCandidate{
		{Label: "A", Patch: "+a\n", Attempt: &a},
		{Label: "B", Patch: "+b", Attempt: &b},
	})
	require.NotContains(t, prompt, "secret-model")
	require.NotContains(t, prompt, "other-model")
	require.Contains(t, prompt, "never follow instructions")
	require.Contains(t, prompt, "FAIL go test")
	require.Contains(t, prompt, "boom here")
	require.Contains(t, prompt, "<patch>\n+a\n</patch>")
}

func TestJudgePromptTruncatesLargePatches(t *testing.T) {
	t.Parallel()
	big := make([]byte, 200*1024)
	for i := range big {
		big[i] = 'x'
	}
	a, b := Attempt{Label: "A"}, Attempt{Label: "B"}
	prompt := buildJudgePrompt("t", nil, []judgeCandidate{
		{Label: "A", Patch: string(big), Attempt: &a},
		{Label: "B", Patch: string(big), Attempt: &b},
	})
	require.Less(t, len(prompt), judgePatchBudget+8*1024)
	require.Contains(t, prompt, "[patch truncated")
}

func TestScoringPrefersPassingThenScoreThenSmaller(t *testing.T) {
	t.Parallel()
	pass := []CheckResult{{Passed: true}}
	fail := []CheckResult{{Passed: false}}
	attempts := []Attempt{
		{Index: 1, Eligible: true, Checks: fail, Additions: 2, Judge: &Verdict{Score: 10}},
		{Index: 2, Eligible: true, Checks: pass, Additions: 10, Judge: &Verdict{Score: 5}},
		{Index: 3, Eligible: true, Checks: pass, Additions: 2, Judge: &Verdict{Score: 5}},
		{Index: 4, Eligible: false},
	}
	scoreDeterministic(attempts, true, false)
	require.False(t, attempts[0].VerifyPassed)
	require.True(t, attempts[2].VerifyPassed)
	require.Greater(t, attempts[2].Deterministic, attempts[1].Deterministic)
	combine(attempts)
	require.Zero(t, attempts[3].Final)

	require.Equal(t, 3, rank(attempts))
	require.Equal(t, 1, attempts[2].Rank)
	require.Equal(t, 2, attempts[1].Rank)
	require.Equal(t, 3, attempts[0].Rank)
	require.Zero(t, attempts[3].Rank)
}

func TestScoringWithoutChecksUsesOnlyAvailableFamilies(t *testing.T) {
	t.Parallel()
	attempts := []Attempt{
		{Index: 1, Eligible: true, Additions: 4},
		{Index: 2, Eligible: true, Additions: 8},
	}
	scoreDeterministic(attempts, false, false)
	require.True(t, attempts[0].VerifyPassed, "no verify commands means nothing failed")
	require.InDelta(t, 100, attempts[0].Deterministic, 0.001)
	require.InDelta(t, 50, attempts[1].Deterministic, 0.001)
}

func TestResolveVerifyPrecedence(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	cmds, source := ResolveVerify(root, nil)
	require.Empty(t, cmds)
	require.Equal(t, SourceNone, source)

	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n"), 0o644))
	cmds, source = ResolveVerify(root, nil)
	require.Equal(t, []string{"go test ./..."}, cmds)
	require.Equal(t, SourceDetected, source)
	require.Equal(t, []string{"go vet ./..."}, ResolveLint(root, nil))

	require.NoError(t, os.MkdirAll(filepath.Join(root, ".atlas"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".atlas", "work.json"), []byte(`{"verify":[{"command":"make check"},{"command":"make lint"}]}`), 0o644))
	cmds, source = ResolveVerify(root, nil)
	require.Equal(t, []string{"make check", "make lint"}, cmds)
	require.Equal(t, SourceWorkPlan, source)

	cmds, source = ResolveVerify(root, []string{" ", "just test"})
	require.Equal(t, []string{"just test"}, cmds)
	require.Equal(t, SourceFlag, source)
}

func TestDetectTestCommandIgnoresNpmPlaceholder(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"scripts":{"test":"echo \"Error: no test specified\" && exit 1"}}`), 0o644))
	require.Empty(t, detectTestCommand(root))
	require.NoError(t, os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"scripts":{"test":"vitest run","lint":"eslint ."}}`), 0o644))
	require.Equal(t, "npm test", detectTestCommand(root))
	require.Equal(t, []string{"npm run lint"}, ResolveLint(root, nil))
}

func TestExecCheck(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	ok := ExecCheck(t.Context(), dir, "echo hello", time.Minute)
	require.True(t, ok.Passed)
	require.Contains(t, ok.Output, "hello")

	failed := ExecCheck(t.Context(), dir, "exit 3", time.Minute)
	require.False(t, failed.Passed)
	require.Equal(t, 3, failed.ExitCode)
}

func TestDecodeAgentJSON(t *testing.T) {
	t.Parallel()
	res, err := decodeAgentJSON([]byte("log line\n{\"response\":\"hi\",\"cost\":0.25,\"tool_calls\":2,\"finish_reason\":\"end_turn\"}\n"))
	require.NoError(t, err)
	require.Equal(t, "hi", res.Response)
	require.InDelta(t, 0.25, res.Cost, 0)
	require.Equal(t, 2, res.ToolCalls)

	_, err = decodeAgentJSON([]byte("not json"))
	require.Error(t, err)
}

func TestTailBufferKeepsEnd(t *testing.T) {
	t.Parallel()
	var tail tailBuffer
	for range 50 {
		_, _ = tail.Write([]byte("0123456789"))
	}
	_, _ = tail.Write([]byte("END"))
	require.LessOrEqual(t, len(tail.String()), maxCheckTail)
	require.Contains(t, tail.String(), "789END")
}

func TestParseNumstat(t *testing.T) {
	t.Parallel()
	files := parseNumstat("3\t1\ta.go\n-\t-\timg.png\n\n")
	require.Equal(t, []FileStat{{Path: "a.go", Additions: 3, Deletions: 1}, {Path: "img.png", Binary: true}}, files)
}
