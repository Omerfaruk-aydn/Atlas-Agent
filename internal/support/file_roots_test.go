package support

import (
	"os"
	"path/filepath"
	"testing"

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
