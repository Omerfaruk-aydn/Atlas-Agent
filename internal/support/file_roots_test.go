package support

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/nodes"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/transfer"
	"github.com/stretchr/testify/require"
)

func TestComputerFileRootsFindsAccessibleUniqueRoots(t *testing.T) {
	t.Parallel()
	roots, err := ComputerFileRoots()
	require.NoError(t, err)
	require.NotEmpty(t, roots)
	seen := map[string]bool{}
	for _, root := range roots {
		require.True(t, filepath.IsAbs(root.Path))
		require.True(t, rootLabel.MatchString(root.Label))
		require.False(t, seen[root.Label])
		seen[root.Label] = true
		info, err := os.Stat(root.Path)
		require.NoError(t, err)
		require.True(t, info.IsDir())
	}
	h := startHost(t, nil)
	info, err := h.Offer(OfferOptions{Capabilities: Presets["full"], Roots: roots, LAN: true})
	require.NoError(t, err)
	require.NotEmpty(t, info.Link)
}

// A drive root is labeled "C" for display but its meaning is the absolute
// C:\ directory. A controller that names a file by its absolute path
// (C:\..., C:/...) must reach it; a drive-relative C:foo must not.
func TestDriveRootAcceptsAbsoluteWindowsPaths(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "windows" {
		t.Skip("drive letters exist only on Windows")
	}
	dir := t.TempDir()
	volume := filepath.VolumeName(dir)
	if len(volume) != 2 || volume[1] != ':' {
		t.Skipf("temp dir %q is not on a lettered drive", dir)
	}
	label := volume[:1]
	src := filepath.Join(dir, "in", "shot.png")
	require.NoError(t, os.MkdirAll(filepath.Dir(src), 0o755))
	payload := []byte("screenshot bytes")
	require.NoError(t, os.WriteFile(src, payload, 0o600))

	h := startHost(t, nil)
	c := startController(t, nil)
	r := connect(t, h, c, []Capability{CapFileRead, CapFileSend, CapFileReceive}, nodes.Root{Label: label, Path: volume + `\`})
	cl := client(t, c)
	local := t.TempDir()
	sum := sha256.Sum256(payload)

	forward := filepath.ToSlash(src)
	for i, remote := range []string{forward, src, label + "/" + strings.TrimPrefix(forward, volume+"/")} {
		got, err := cl.Pull(t.Context(), remote, local, "copy"+strconv.Itoa(i)+".png", false, 0)
		require.NoError(t, err, remote)
		require.Equal(t, hex.EncodeToString(sum[:]), got.SHA256, remote)
	}

	// Drive-relative and traversal forms stay refused.
	for _, remote := range []string{volume + "foo.png", volume + `\..\..\x.png`, forward + "/../../../../x"} {
		_, err := cl.Pull(t.Context(), remote, local, "bad.png", false, 0)
		require.Error(t, err, remote)
		require.Equal(t, CodePathDenied, codeOf(t, err, r.RemoteDeviceID), remote)
	}

	// Uploading by absolute path lands inside the drive root and is
	// hash-verified.
	up := filepath.Join(local, "up.txt")
	require.NoError(t, os.WriteFile(up, []byte("upload"), 0o600))
	dest := filepath.ToSlash(filepath.Join(dir, "out", "up.txt"))
	res, err := cl.Upload(t.Context(), "op-abs-up-0", up, dest, transfer.ConflictError, 0, nil)
	require.NoError(t, err)
	require.Equal(t, "complete", res.State)
	got, err := os.ReadFile(filepath.Join(dir, "out", "up.txt"))
	require.NoError(t, err)
	require.Equal(t, "upload", string(got))
}

// A root outside the drive is not reachable by naming an absolute path on
// another root.
func TestAbsolutePathOutsideEveryRootIsDenied(t *testing.T) {
	t.Parallel()
	root := nodes.Root{Label: "Work", Path: t.TempDir()}
	outside := filepath.Join(t.TempDir(), "secret.txt")
	require.NoError(t, os.WriteFile(outside, []byte("no"), 0o600))
	h := startHost(t, nil)
	c := startController(t, nil)
	r := connect(t, h, c, []Capability{CapFileRead, CapFileSend}, root)
	_, err := client(t, c).Pull(t.Context(), filepath.ToSlash(outside), t.TempDir(), "x.txt", false, 0)
	require.Equal(t, CodePathDenied, codeOf(t, err, r.RemoteDeviceID))
	// The error names the folder's real path, not only its label.
	require.Contains(t, err.Error(), root.Path)
}
