package rehearsal

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRestoreRejectsSymlinkOutsideDestination(t *testing.T) {
	t.Parallel()
	src, dst, outside := t.TempDir(), t.TempDir(), t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(src, "nested"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(src, "nested", "file.txt"), []byte("original"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(outside, "file.txt"), []byte("untouched"), 0o644))
	if err := os.Symlink(outside, filepath.Join(dst, "nested")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	base := manifest{"nested/file.txt": {Mode: 0o644}}
	require.Error(t, restore(t.Context(), src, dst, base))
	data, err := os.ReadFile(filepath.Join(outside, "file.txt"))
	require.NoError(t, err)
	require.Equal(t, "untouched", string(data))
}

func TestApplyRejectsSymlinkOutsideProject(t *testing.T) {
	t.Parallel()
	root, store := newWorld(t, map[string]string{"keep.txt": "keep"})
	report, err := Run(t.Context(), Options{Root: root, Store: store, Run: fakeRunner, Steps: []string{"write nested/file.txt changed"}})
	require.NoError(t, err)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "nested")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	_, err = store.Apply(t.Context(), report.ID, ApplyOptions{Root: root})
	require.Error(t, err)
	require.NoFileExists(t, filepath.Join(outside, "file.txt"))
}

func TestRestoreDoesNotOverwriteHardlinkedFile(t *testing.T) {
	t.Parallel()
	src, dst, outside := t.TempDir(), t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(src, "file.txt"), []byte("original"), 0o644))
	other := filepath.Join(outside, "file.txt")
	require.NoError(t, os.WriteFile(other, []byte("untouched"), 0o644))
	if err := os.Link(other, filepath.Join(dst, "file.txt")); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	base := manifest{"file.txt": {Hash: hashBytes([]byte("original")), Size: 8, Mode: 0o644}}
	require.NoError(t, restore(t.Context(), src, dst, base))
	require.Equal(t, "original", read(t, dst, "file.txt"))
	require.Equal(t, "untouched", read(t, outside, "file.txt"))
}
