//go:build windows

package computer

import (
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
