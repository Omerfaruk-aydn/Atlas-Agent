package transfer

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReceiveVerifiesSizeAndHashBeforePublishing(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	data := []byte("hello node")
	sum := HashBytes(data)

	_, err := Receive(root, "out/a.txt", bytes.NewReader(data), int64(len(data)), HashBytes([]byte("other")), 0, false)
	require.Equal(t, "hash_mismatch", Code(err))
	require.NoFileExists(t, filepath.Join(root, "out", "a.txt"))

	_, err = Receive(root, "out/a.txt", bytes.NewReader(data[:4]), int64(len(data)), sum, 0, false)
	require.Equal(t, "size_mismatch", Code(err))
	require.NoFileExists(t, filepath.Join(root, "out", "a.txt"))

	path, err := Receive(root, "out/a.txt", bytes.NewReader(data), int64(len(data)), sum, 0, false)
	require.NoError(t, err)
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, data, got)

	_, err = Receive(root, "out/a.txt", bytes.NewReader(data), int64(len(data)), sum, 0, false)
	require.Equal(t, "exists", Code(err))
}

func TestReceiveEnforcesLimit(t *testing.T) {
	t.Parallel()
	data := bytes.Repeat([]byte("x"), 100)
	_, err := Receive(t.TempDir(), "big", bytes.NewReader(data), 100, HashBytes(data), 10, false)
	require.Equal(t, "too_large", Code(err))
	// A sender that lies about the size is still cut off.
	_, err = Receive(t.TempDir(), "big", bytes.NewReader(data), 10, HashBytes(data), 10, false)
	require.Equal(t, "too_large", Code(err))
}

func TestResolveRejectsTraversalAndAbsolutePaths(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, rel := range []string{"../x", "a/../../x", `..\x`, "/etc/passwd", `C:\Windows\x`, "C:x", `\\server\share\x`, ""} {
		_, err := Resolve(root, rel)
		require.Error(t, err, rel)
	}
	p, err := Resolve(root, "a/b/c.txt")
	require.NoError(t, err)
	real, _ := filepath.EvalSymlinks(root)
	require.Equal(t, filepath.Join(real, "a", "b", "c.txt"), p)
}

func TestResolveRejectsSymlinkEscape(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_, err := Resolve(root, "link/file.txt")
	require.Equal(t, "symlink_escape", Code(err))
	data := []byte("x")
	_, err = Receive(root, "link/file.txt", bytes.NewReader(data), 1, HashBytes(data), 0, false)
	require.Equal(t, "symlink_escape", Code(err))
	require.NoFileExists(t, filepath.Join(outside, "file.txt"))
}

func TestOpenReturnsHash(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "f"), []byte("abc"), 0o644))
	f, size, sum, err := Open(root, "f", 0)
	require.NoError(t, err)
	defer f.Close()
	require.EqualValues(t, 3, size)
	require.Equal(t, HashBytes([]byte("abc")), sum)
	_, _, _, err = Open(root, "missing", 0)
	require.Equal(t, "not_found", Code(err))
}
