package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/codereview"
	"github.com/stretchr/testify/require"
)

func TestRunReviewEndToEndWithFakeRunner(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	git := func(args ...string) {
		out, err := exec.CommandContext(t.Context(), "git", append([]string{"-C", root}, args...)...).CombinedOutput()
		require.NoError(t, err, string(out))
	}
	git("init", "-q")
	git("config", "user.name", "Fixture")
	git("config", "user.email", "fixture@example.invalid")
	path := filepath.Join(root, "source.txt")
	require.NoError(t, os.WriteFile(path, []byte("baseline\n"), 0o644))
	git("add", "source.txt")
	git("commit", "-q", "-m", "feat: fixture")
	require.NoError(t, os.WriteFile(path, []byte("baseline\nworking-change\n"), 0o644))

	runner := codereview.RunnerFunc(func(_ context.Context, _ codereview.Request) (string, error) {
		return `{"summary":"One change.","findings":[{"path":"source.txt","line":2,"severity":"high","title":"suspicious","scenario":"it breaks","confidence":0.9}]}`, nil
	})
	var out, errOut bytes.Buffer
	fixed := ""
	err := runReview(t.Context(), reviewOptions{
		root: root, effort: codereview.EffortLow, asJSON: true, fix: true,
		stdout: &out, stderr: &errOut, resolver: codereview.NewResolver(), runner: runner,
		fixRun: func(_ context.Context, prompt string) error { fixed = prompt; return nil },
	})
	require.NoError(t, err)
	var report codereview.Report
	require.NoError(t, json.Unmarshal(out.Bytes(), &report))
	require.Len(t, report.Findings, 1)
	require.Equal(t, "source.txt", report.Findings[0].Path)
	require.Contains(t, fixed, "source.txt:2")

	err = runReview(t.Context(), reviewOptions{root: root, comment: true, stdout: &out, stderr: &errOut, resolver: codereview.NewResolver(), runner: runner})
	require.ErrorContains(t, err, "pull request")
	err = runReview(t.Context(), reviewOptions{root: root, spec: "12", fix: true, stdout: &out, stderr: &errOut, resolver: codereview.NewResolver(), runner: runner})
	require.ErrorContains(t, err, "--fix")
}

func TestPrepareReviewUsesSelectedGitScope(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	git := func(args ...string) {
		command := exec.CommandContext(t.Context(), "git", append([]string{"-C", root}, args...)...)
		output, err := command.CombinedOutput()
		require.NoError(t, err, string(output))
	}
	git("init")
	git("config", "user.name", "Fixture")
	git("config", "user.email", "fixture@example.invalid")
	path := filepath.Join(root, "source.txt")
	require.NoError(t, os.WriteFile(path, []byte("baseline\n"), 0o644))
	git("add", "source.txt")
	git("commit", "-m", "feat: fixture")
	require.NoError(t, os.WriteFile(path, []byte("working-change\n"), 0o644))
	prompt, truncated, err := prepareReview(t.Context(), root, "", "")
	require.NoError(t, err)
	require.False(t, truncated)
	require.Contains(t, prompt, "+working-change")
	prompt, _, err = prepareReview(t.Context(), root, "HEAD", "")
	require.NoError(t, err)
	require.Contains(t, prompt, "+baseline")
	require.NotContains(t, prompt, "+working-change")
	_, _, err = prepareReview(t.Context(), root, "--help", "")
	require.Error(t, err)
	_, _, err = prepareReview(t.Context(), root, "HEAD", "HEAD")
	require.Error(t, err)
}

func TestReviewPatchBoundsOutput(t *testing.T) {
	t.Parallel()
	var patch reviewPatch
	data := make([]byte, 64*1024)
	n, err := patch.Write(data)
	require.NoError(t, err)
	require.Equal(t, len(data), n)
	require.Equal(t, 48*1024, patch.Len())
	require.True(t, patch.Truncated)
}
