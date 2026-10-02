package engineering

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func testRepository(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	root := t.TempDir()
	for _, args := range [][]string{{"init"}, {"config", "user.name", "Atlas test"}, {"config", "user.email", "atlas@example.invalid"}, {"config", "commit.gpgsign", "false"}, {"config", "core.autocrlf", "false"}, {"config", "core.hooksPath", filepath.Join(root, "no-hooks")}} {
		_, err := Git(t.Context(), root, args...)
		require.NoError(t, err)
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, "file.txt"), []byte("base\n"), 0o644))
	_, err := Git(t.Context(), root, "add", "file.txt")
	require.NoError(t, err)
	_, err = Git(t.Context(), root, "commit", "-m", "test: baseline")
	require.NoError(t, err)
	return root
}

func TestWorktreeIntegrationPreservesDirtyChangesAndChecksConflicts(t *testing.T) {
	root := testRepository(t)
	s := NewStore(t.TempDir())
	w, err := s.CreateWorkspace(t.Context(), root, "s", "task")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(w.Path, "file.txt"), []byte("feature\n"), 0o644))
	require.ErrorContains(t, s.RemoveWorkspace(t.Context(), root, "s", w.ID), "dirty")
	require.NoError(t, s.ApplyWorkspace(t.Context(), root, "s", w.ID))
	applied, patch, err := s.WorkspacePatch(t.Context(), "s", w.ID)
	require.NoError(t, err)
	require.Equal(t, Hash(patch), applied.AppliedPatchHash)
	data, err := os.ReadFile(filepath.Join(root, "file.txt"))
	require.NoError(t, err)
	require.Equal(t, "feature\n", string(data))
	require.NoError(t, os.WriteFile(filepath.Join(root, "file.txt"), []byte("user change\n"), 0o644))
	require.Error(t, s.ApplyWorkspace(t.Context(), root, "s", w.ID))
	data, err = os.ReadFile(filepath.Join(root, "file.txt"))
	require.NoError(t, err)
	require.Equal(t, "user change\n", string(data))
	_, err = Git(t.Context(), w.Path, "restore", "file.txt")
	require.NoError(t, err)
	require.NoError(t, s.RemoveWorkspace(t.Context(), root, "s", w.ID))
}

func TestWorktreeRegistryRejectsEscapingPaths(t *testing.T) {
	s := NewStore(t.TempDir())
	root := testRepository(t)
	w, err := s.CreateWorkspace(t.Context(), root, "s", "")
	require.NoError(t, err)
	require.NoError(t, s.Update(t.Context(), "s", func(st *State) error { st.Workspaces[0].Path = root; return nil }))
	_, err = s.Workspace(t.Context(), "s", w.ID)
	require.ErrorContains(t, err, "escapes")
	_, err = Git(t.Context(), root, "worktree", "remove", w.Path)
	require.NoError(t, err)
}
