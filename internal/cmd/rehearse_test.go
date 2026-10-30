package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

func runRehearse(t *testing.T, root, dataDir string, args ...string) (string, error) {
	t.Helper()
	command := newRehearseCommand()
	command.PersistentFlags().String("cwd", "", "")
	command.PersistentFlags().String("data-dir", "", "")
	var out bytes.Buffer
	command.SetOut(&out)
	command.SetErr(&out)
	command.SetArgs(append([]string{"--cwd", root, "--data-dir", dataDir}, args...))
	err := command.ExecuteContext(t.Context())
	return out.String(), err
}

func TestRehearseCommandPreviewsThenAppliesByID(t *testing.T) {
	t.Setenv("AI_AGENT", "")
	t.Setenv("AGENT", "")
	root, data := t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.txt"), []byte("old\n"), 0o644))

	out, err := runRehearse(t, root, data, "make it new", "--run", "echo new > a.txt", "--check", "test -f a.txt")
	require.NoError(t, err, out)
	require.Contains(t, out, "verdict: ok")
	require.Contains(t, out, "a.txt")
	content, err := os.ReadFile(filepath.Join(root, "a.txt"))
	require.NoError(t, err)
	require.Equal(t, "old\n", string(content))

	id := regexp.MustCompile(`rh-[0-9]{8}-[0-9]{6}-[0-9a-f]{6}`).FindString(out)
	require.NotEmpty(t, id)
	list, err := runRehearse(t, root, data, "list")
	require.NoError(t, err)
	require.Contains(t, list, id)

	out, err = runRehearse(t, root, data, "apply", id)
	require.NoError(t, err, out)
	content, err = os.ReadFile(filepath.Join(root, "a.txt"))
	require.NoError(t, err)
	require.Equal(t, "new\n", string(content))
}

func TestRehearseCommandNeedsAChange(t *testing.T) {
	t.Setenv("AI_AGENT", "")
	t.Setenv("AGENT", "")
	_, err := runRehearse(t, t.TempDir(), t.TempDir(), "do something")
	require.ErrorContains(t, err, "--run")
}
