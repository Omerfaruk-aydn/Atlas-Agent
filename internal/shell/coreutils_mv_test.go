//go:build !solaris && !illumos

package shell

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMoveAcrossVolumesCopiesThenRemoves(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	src := filepath.Join(root, "project")
	require.NoError(t, os.MkdirAll(filepath.Join(src, "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(src, "package.json"), []byte("{}"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(src, "src", "main.ts"), []byte("x"), 0o644))

	require.NoError(t, copyTree(src, filepath.Join(root, "copy")))
	data, err := os.ReadFile(filepath.Join(root, "copy", "src", "main.ts"))
	require.NoError(t, err)
	require.Equal(t, "x", string(data))

	target := filepath.Join(root, "moved")
	require.NoError(t, moveAcrossVolumes(src, target, false))
	require.NoDirExists(t, src)
	require.FileExists(t, filepath.Join(target, "package.json"))

	require.Error(t, moveAcrossVolumes(filepath.Join(root, "copy"), target, false), "a directory is never merged into another")
	require.NoError(t, moveAcrossVolumes(filepath.Join(root, "copy"), target, true), "-n skips an existing target")
	require.DirExists(t, filepath.Join(root, "copy"))
}

func TestMvWithinOneVolumeIsLeftToCoreutils(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a"), []byte("a"), 0o644))
	stderr, err := runCoreutilsCommand(t, t.Context(), "mv '"+filepath.ToSlash(filepath.Join(dir, "a"))+"' '"+filepath.ToSlash(filepath.Join(dir, "b"))+"'")
	require.NoError(t, err, stderr)
	require.FileExists(t, filepath.Join(dir, "b"))
}

func TestMvAcrossDrives(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "windows" {
		t.Skip("drive letters are Windows only")
	}
	src := t.TempDir()
	var other string
	for _, drive := range []string{"D:", "E:", "C:"} {
		if !filepath.IsAbs(src) || filepath.VolumeName(src) == drive {
			continue
		}
		if info, err := os.Stat(drive + `\`); err == nil && info.IsDir() {
			other = drive
			break
		}
	}
	if other == "" {
		t.Skip("no second drive")
	}
	dest, err := os.MkdirTemp(other+`\`, "atlas-mv-test-")
	if err != nil {
		t.Skip("second drive not writable")
	}
	t.Cleanup(func() { _ = os.RemoveAll(dest) })
	require.NoError(t, os.WriteFile(filepath.Join(src, "f.txt"), []byte("moved"), 0o644))
	stderr, err := runCoreutilsCommand(t, t.Context(), "mv '"+filepath.ToSlash(filepath.Join(src, "f.txt"))+"' '"+filepath.ToSlash(dest)+"'")
	require.NoError(t, err, stderr)
	require.NoFileExists(t, filepath.Join(src, "f.txt"))
	data, err := os.ReadFile(filepath.Join(dest, "f.txt"))
	require.NoError(t, err)
	require.Equal(t, "moved", string(data))
}
