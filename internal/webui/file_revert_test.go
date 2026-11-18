package webui

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// Undoing an edit puts the old text back, only while the file still holds
// what the agent wrote, and never outside the project.
func TestFileRevertRestoresOnlyWhatTheAgentWrote(t *testing.T) {
	h := newHarness(t)
	b := h.signIn()
	ws := b.open(h.project)
	path := wsPath(ws.ID, "/files/revert")

	file := filepath.Join(h.project, "a.txt")
	require.NoError(t, os.WriteFile(file, []byte("new\n"), 0o644))

	status, _ := b.raw(http.MethodPost, path, map[string]any{"path": "a.txt", "old_content": "old\n", "new_content": "changed\n"})
	require.Equal(t, http.StatusConflict, status, "a file the person changed since is left alone")
	got, _ := os.ReadFile(file)
	require.Equal(t, "new\n", string(got))

	status, _ = b.raw(http.MethodPost, path, map[string]any{"path": "a.txt", "old_content": "old\n", "new_content": "new\n"})
	require.Equal(t, http.StatusOK, status)
	got, _ = os.ReadFile(file)
	require.Equal(t, "old\n", string(got))

	created := filepath.Join(h.project, "sub", "b.txt")
	require.NoError(t, os.MkdirAll(filepath.Dir(created), 0o755))
	require.NoError(t, os.WriteFile(created, []byte("made\n"), 0o644))
	status, _ = b.raw(http.MethodPost, path, map[string]any{"path": "sub/b.txt", "old_content": "", "new_content": "made\n"})
	require.Equal(t, http.StatusOK, status)
	_, err := os.Stat(created)
	require.ErrorIs(t, err, os.ErrNotExist, "undoing a creation removes the file")

	outside := filepath.Join(t.TempDir(), "secret.txt")
	require.NoError(t, os.WriteFile(outside, []byte("x"), 0o644))
	for _, p := range []string{outside, "../secret.txt", ""} {
		status, _ = b.raw(http.MethodPost, path, map[string]any{"path": p, "old_content": "y", "new_content": "x"})
		require.Equal(t, http.StatusBadRequest, status, p)
	}
	got, _ = os.ReadFile(outside)
	require.Equal(t, "x", string(got))
}
