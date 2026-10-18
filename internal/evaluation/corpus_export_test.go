package evaluation

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/harnessmetrics"
	"github.com/stretchr/testify/require"
)

func TestExportCaseRestoresTheStartingState(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
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
	require.NoError(t, os.MkdirAll(filepath.Join(project, "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(project, "src", "a.txt"), []byte("end state"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(project, "new.txt"), []byte("created by the request"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(project, "keep.txt"), []byte("untouched"), 0o644))
	git("add", "-A")
	git("commit", "-q", "-m", "end")
	commit := git("rev-parse", "HEAD")

	c := harnessmetrics.Case{
		ID: "20261016-141500-abcd1234", Request: "make it work", WorkingDir: project, Commit: commit,
		Checks: []string{"make test", "make lint"},
		Files: []harnessmetrics.CaseFile{
			{Path: "src/a.txt", Before: "start state", Existed: true},
			{Path: "new.txt"},
			{Path: "../outside.txt", Before: "x", Existed: true},
		},
	}
	suite := t.TempDir()
	dir, err := ExportCase(t.Context(), c, suite, nil)
	require.NoError(t, err)
	repo := filepath.Join(dir, "repo")
	data, err := os.ReadFile(filepath.Join(repo, "src", "a.txt"))
	require.NoError(t, err)
	require.Equal(t, "start state", string(data))
	require.NoFileExists(t, filepath.Join(repo, "new.txt"))
	require.FileExists(t, filepath.Join(repo, "keep.txt"))
	require.NoFileExists(t, filepath.Join(dir, "outside.txt"))

	var task BenchTask
	raw, err := os.ReadFile(filepath.Join(dir, "task.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &task))
	require.Equal(t, "make it work", task.Prompt)
	require.Equal(t, []string{"bash", "-c", "make test && make lint"}, task.Check)

	_, err = ExportCase(t.Context(), c, suite, nil)
	require.Error(t, err, "an existing task is not overwritten")

	noChecks := c
	noChecks.ID, noChecks.Checks = "20261016-141501-abcd1234", nil
	_, err = ExportCase(t.Context(), noChecks, suite, nil)
	require.ErrorContains(t, err, "pass one with --check")
	_, err = ExportCase(t.Context(), noChecks, suite, []string{"go", "test", "./..."})
	require.NoError(t, err)
}
