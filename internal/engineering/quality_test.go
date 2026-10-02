package engineering

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSourceFingerprintIncludesUntrackedDeletedAndChangedFiles(t *testing.T) {
	root := testRepository(t)
	baseline, err := SourceFingerprint(t.Context(), root)
	require.NoError(t, err)
	untracked := filepath.Join(root, "new file.txt")
	require.NoError(t, os.WriteFile(untracked, []byte("new"), 0o644))
	changed, err := SourceFingerprint(t.Context(), root)
	require.NoError(t, err)
	require.NotEqual(t, baseline, changed)
	require.NoError(t, os.Remove(untracked))
	same, err := SourceFingerprint(t.Context(), root)
	require.NoError(t, err)
	require.Equal(t, baseline, same)
	require.NoError(t, os.Remove(filepath.Join(root, "file.txt")))
	deleted, err := SourceFingerprint(t.Context(), root)
	require.NoError(t, err)
	require.NotEqual(t, baseline, deleted)
	store := NewStore(filepath.Join(root, "state"))
	require.NoError(t, store.SetBudget(t.Context(), "s", "", Limits{MaxTokens: 100}))
	first, err := SourceFingerprint(t.Context(), root, store.Dir())
	require.NoError(t, err)
	require.NoError(t, store.Charge(t.Context(), "s", "", 5, 0, 0))
	second, err := SourceFingerprint(t.Context(), root, store.Dir())
	require.NoError(t, err)
	require.Equal(t, first, second)
}

func TestEnvironmentCachesDoNotChangeSourceEvidence(t *testing.T) {
	root := testRepository(t)
	before, err := SourceFingerprint(t.Context(), root)
	require.NoError(t, err)
	for _, name := range []string{".atlas-env", ".venv"} {
		require.NoError(t, os.Mkdir(filepath.Join(root, name), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, name, "dependency.go"), []byte("package dependency\n"), 0o644))
	}
	after, err := SourceFingerprint(t.Context(), root)
	require.NoError(t, err)
	require.Equal(t, before, after)
	m, err := NewStore(t.TempDir()).RefreshProject(t.Context(), root)
	require.NoError(t, err)
	for _, file := range m.Files {
		require.NotContains(t, file.Path, ".atlas-env")
		require.NotContains(t, file.Path, ".venv")
	}
	_, err = Git(t.Context(), root, "add", "-f", ".atlas-env/dependency.go")
	require.NoError(t, err)
	tracked, err := SourceFingerprint(t.Context(), root)
	require.NoError(t, err)
	require.NotEqual(t, before, tracked, "Tracked files remain part of source evidence even inside cache directories")
}
