//go:build windows

package computer

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNativeChangeSubscriptionCleansUp(t *testing.T) {
	b := &windowsBackend{}
	data, err := b.listNativeWindows(t.Context())
	require.NoError(t, err)
	var windows []nativeWindowObservation
	require.NoError(t, json.Unmarshal(data, &windows))
	if len(windows) == 0 {
		t.Skip("No interactive native window available")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	events, release, err := b.WatchChanges(ctx, windows[0].ID)
	require.NoError(t, err)
	require.NotNil(t, events)
	hwnd, err := strconv.ParseUint(windows[0].ID, 10, 64)
	require.NoError(t, err)
	// Emit a native notification without changing application state or focus.
	modUser32.NewProc("NotifyWinEvent").Call(0x800c, uintptr(hwnd), 0, 0)
	select {
	case <-events:
	case <-ctx.Done():
		t.Fatal("Native event was not delivered to the subscription")
	}
	release()
	desktopChangeWatches.Lock()
	count := len(desktopChangeWatches.hooks)
	desktopChangeWatches.Unlock()
	require.Zero(t, count)
	// Cleanup is safe even when a caller's deadline has already fired.
	release()
}
