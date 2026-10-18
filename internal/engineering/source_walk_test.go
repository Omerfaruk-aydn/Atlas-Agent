package engineering

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSourceFingerprintWorksOutsideGit(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if insideGitWorkTree(t.Context(), root) {
		t.Skip("temp dir is inside a git work tree")
	}
	write := func(rel, body string) {
		path := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	}
	write("src/main.ts", "one")
	write("node_modules/three/index.js", "dep")

	paths, err := walkSourceFiles(t.Context(), root)
	require.NoError(t, err)
	require.Equal(t, []string{"src/main.ts"}, paths)

	first, err := SourceFingerprint(t.Context(), root)
	require.NoError(t, err)
	write("node_modules/three/index.js", "dep changed")
	same, err := SourceFingerprint(t.Context(), root)
	require.NoError(t, err)
	require.Equal(t, first, same, "generated directories do not change the fingerprint")
	write("src/main.ts", "two")
	changed, err := SourceFingerprint(t.Context(), root)
	require.NoError(t, err)
	require.NotEqual(t, first, changed)
}

func TestRenameRetryReplacesTarget(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tmp, path := filepath.Join(dir, "a.tmp"), filepath.Join(dir, "a")
	require.NoError(t, os.WriteFile(path, []byte("old"), 0o644))
	require.NoError(t, os.WriteFile(tmp, []byte("new"), 0o644))
	require.NoError(t, renameRetry(tmp, path))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "new", string(data))
	require.Error(t, renameRetry(filepath.Join(dir, "missing"), path))
}
