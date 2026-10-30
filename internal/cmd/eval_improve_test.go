package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/evaluation"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/harnessmetrics"
	"github.com/stretchr/testify/require"
)

// improveCommand returns an eval command with the root's persistent flags
// that the improve command reads.
func improveCommand(out *bytes.Buffer, args ...string) error {
	command := newEvalCommand()
	command.PersistentFlags().String("data-dir", "", "")
	command.SetOut(out)
	command.SetErr(out)
	command.SetArgs(append([]string{"improve"}, args...))
	return command.Execute()
}

// writeImproveTask writes a task the mock agent can fix. The hidden
// expected.txt holds the answer; the check compares it with a.txt.
func writeImproveTask(t *testing.T, suite, id string) {
	t.Helper()
	for path, content := range map[string]string{
		"task.json":           `{"id":"` + id + `","prompt":"Fix a.txt.","check":["verify"]}`,
		"repo/a.txt":          "broken",
		"hidden/expected.txt": "fixed",
	} {
		full := filepath.Join(suite, id, filepath.FromSlash(path))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
	}
}

func mockImproveRunner(_ context.Context, dir, program string, _ []string) ([]byte, error) {
	switch program {
	case "verify":
		got, _ := os.ReadFile(filepath.Join(dir, "a.txt"))
		want, _ := os.ReadFile(filepath.Join(dir, "expected.txt"))
		if string(got) != string(want) {
			return []byte("a.txt is " + string(got)), errors.New("exit status 1")
		}
		return []byte("ok"), nil
	case "atlas":
		if config, _ := os.ReadFile(filepath.Join(dir, ".atlas.json")); strings.Contains(string(config), "fixes") {
			_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte("fixed"), 0o644)
		}
		return []byte(`{"session_id":"s","prompt_tokens":10,"completion_tokens":5,"cost":0.01,"tool_calls":2,"provider_steps":2}`), nil
	}
	return nil, errors.New("unexpected program " + program)
}

// This test replaces a package variable, so it does not run in parallel.
func TestEvalImproveRunsMockABAndWritesProposal(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	improveRunner = mockImproveRunner
	t.Cleanup(func() { improveRunner = nil })

	dir := t.TempDir()
	suite := filepath.Join(dir, "suite")
	for i := range 6 {
		writeImproveTask(t, suite, fmt.Sprintf("task-%d", i))
	}
	candidate := filepath.Join(dir, "candidate.json")
	require.NoError(t, os.WriteFile(candidate, []byte(`{"options":{"note":"fixes"}}`), 0o644))
	proposalPath := filepath.Join(dir, "proposal.json")

	var out bytes.Buffer
	err := improveCommand(&out, "--suite", suite, "--model", "mock", "--candidate-config", candidate,
		"--proposal", proposalPath, "--results", filepath.Join(dir, "results.json"))
	require.NoError(t, err, out.String())
	require.Contains(t, out.String(), "Verdict: IMPROVED")
	require.Contains(t, out.String(), "Nothing was applied.")

	var proposal evaluation.Proposal
	data, err := os.ReadFile(proposalPath)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &proposal))
	require.False(t, proposal.Applied)
	require.Equal(t, 6, proposal.Comparison.Wins)
}

func TestEvalImproveRequiresSuiteModelAndCandidate(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	require.ErrorContains(t, improveCommand(&out), "--suite is required")

	suite := t.TempDir()
	writeImproveTask(t, suite, "one")
	require.ErrorContains(t, improveCommand(&out, "--suite", suite), "--model is required")
	require.ErrorContains(t, improveCommand(&out, "--suite", suite, "--model", "m"), "--candidate-config is required")

	bad := filepath.Join(t.TempDir(), "bad.json")
	require.NoError(t, os.WriteFile(bad, []byte(`[1]`), 0o644))
	require.ErrorContains(t, improveCommand(&out, "--suite", suite, "--model", "m", "--candidate-config", bad), "JSON object")
}

func TestEvalImproveMineAndPlanRunsNoAgent(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	// A red check without a model: the runner fails every check.
	improveRunner = func(context.Context, string, string, []string) ([]byte, error) {
		return []byte("FAIL: expected 3 got 2"), errors.New("exit status 1")
	}
	t.Cleanup(func() { improveRunner = nil })

	project := t.TempDir()
	git := func(args ...string) string {
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = project
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	for _, path := range []string{"a/x.go", "b/y.go"} {
		require.NoError(t, os.MkdirAll(filepath.Join(project, filepath.Dir(path)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(project, path), []byte("package x"), 0o644))
	}
	git("add", "-A")
	git("commit", "-q", "-m", "end")

	dataDir := t.TempDir()
	require.NoError(t, harnessmetrics.SaveCase(dataDir, harnessmetrics.Case{
		ID: "20261016-141500-abcd1234", Request: "make the search better somehow", WorkingDir: project,
		Commit: git("rev-parse", "HEAD"), Reason: "checks unmet", Checks: []string{"make test"},
		Files: []harnessmetrics.CaseFile{
			{Path: "a/x.go", Before: "package old", Existed: true},
			{Path: "b/y.go", Before: "package old", Existed: true},
		},
	}))

	suite := filepath.Join(t.TempDir(), "mined")
	var out bytes.Buffer
	err := improveCommand(&out, "--suite", suite, "--mine", "--plan", "--data-dir", dataDir)
	require.NoError(t, err, out.String())
	require.Contains(t, out.String(), "1 of 1 corpus cases are valid")
	require.Contains(t, out.String(), "Plan only: no agent was run.")
	require.DirExists(t, filepath.Join(suite, "20261016-141500-abcd1234", "hidden"))
	require.NoFileExists(t, "improve-proposal.json")
}
