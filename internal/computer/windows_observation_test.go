//go:build windows

package computer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNativeProcessIdentityUsesObservedPID(t *testing.T) {
	t.Parallel()
	executable, err := os.Executable()
	require.NoError(t, err)
	require.Equal(t, filepath.Base(executable), nativeProcessName(uint32(os.Getpid())))
	require.Empty(t, nativeProcessName(0), "Unavailable process identity must remain unknown")
}

func TestNativeWindowEnumerationReturnsOwnershipMetadata(t *testing.T) {
	t.Parallel()
	data, err := (&windowsBackend{}).listNativeWindows(t.Context())
	require.NoError(t, err)
	var observed []nativeWindowObservation
	require.NoError(t, json.Unmarshal(data, &observed))
	for _, window := range observed {
		require.NotEmpty(t, window.ID)
		require.NotEmpty(t, window.ClassName, "Native class identity must distinguish folder windows from desktop shell")
		require.NotEmpty(t, window.Owner, "A native window must report its owner, including zero for an unowned window")
		if window.ProcessName != "" {
			require.Equal(t, filepath.Base(window.ProcessName), window.ProcessName, "Expose the image name, not its full path")
		}
	}
}
