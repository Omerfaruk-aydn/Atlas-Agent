//go:build windows

package activity

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCursorOwnershipIsExclusiveUntilRestoration(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	release, ok := acquireCursorOwnership()
	require.True(t, ok)
	defer release()
	other := make(chan bool, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		releaseOther, acquired := acquireCursorOwnership()
		if acquired {
			releaseOther()
		}
		other <- acquired
	}()
	require.False(t, <-other)
}

func TestStandardCursorHandlesCanBePreserved(t *testing.T) {
	for _, id := range systemCursorIDs {
		original, _, _ := overlayUser.NewProc("LoadCursorW").Call(0, uintptr(id))
		require.NotZero(t, original, "Cursor %d", id)
		copy, _, _ := overlayUser.NewProc("CopyImage").Call(original, 2, 0, 0, 0)
		require.NotZero(t, copy, "Cursor %d", id)
		ok, _, _ := overlayUser.NewProc("DestroyCursor").Call(copy)
		require.NotZero(t, ok)
	}
}
