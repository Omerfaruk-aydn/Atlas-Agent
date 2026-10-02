package engineering

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProjectRefreshInvalidatesChangedAndRemovedFiles(t *testing.T) {
	root := t.TempDir()
	s := NewStore(t.TempDir())
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nimport \"fmt\"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".env"), []byte("secret"), 0o600))
	m, err := s.RefreshProject(t.Context(), root)
	require.NoError(t, err)
	require.Len(t, m.Files, 1)
	require.Equal(t, 1, m.Changed)
	require.Equal(t, []string{"fmt"}, m.Files[0].Imports)
	m, err = s.RefreshProject(t.Context(), root)
	require.NoError(t, err)
	require.Zero(t, m.Changed)
	require.NoError(t, os.Remove(filepath.Join(root, "main.go")))
	m, err = s.RefreshProject(t.Context(), root)
	require.NoError(t, err)
	require.Equal(t, 1, m.Removed)
}
