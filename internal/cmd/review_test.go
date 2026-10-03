package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

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
