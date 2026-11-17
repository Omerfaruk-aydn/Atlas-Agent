package tools

import (
	"os"
	"path/filepath"
	"testing"

	atlaspolicy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/policy"
	"github.com/stretchr/testify/require"
)

func TestWatchedChangesAreReported(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".github", "workflows"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".env"), []byte("A=1"), 0o644))
	before := snapshotWatched(root)
	require.Contains(t, before, ".env")

	require.NoError(t, os.WriteFile(filepath.Join(root, ".env"), []byte("A=2222"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".github", "workflows", "ci.yml"), []byte("on: push"), 0o644))
	changed := watchedChanges(before, snapshotWatched(root))
	require.Equal(t, []string{".env (modified)", ".github/workflows/ci.yml (created)"}, changed)
	require.Contains(t, monitorNote(changed), "Policy monitor")
	require.Empty(t, monitorNote(nil))

	withEnv := snapshotWatched(root)
	require.NoError(t, os.Remove(filepath.Join(root, ".env")))
	require.Contains(t, watchedChanges(withEnv, snapshotWatched(root)), ".env (removed)")
}

func TestVerdictContainedBySandbox(t *testing.T) {
	t.Parallel()
	e := atlaspolicy.New("")
	require.True(t, verdictContainedBySandbox(e.Check("sudo apt update", atlaspolicy.Context{})))
	require.True(t, verdictContainedBySandbox(e.Check("echo x >> ~/.bashrc", atlaspolicy.Context{})))
	require.False(t, verdictContainedBySandbox(e.Check("git reset --hard", atlaspolicy.Context{})), "the project is mounted: not contained")
	require.False(t, verdictContainedBySandbox(e.Check("ls", atlaspolicy.Context{})))
}
