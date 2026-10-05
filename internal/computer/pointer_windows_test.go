//go:build windows

package computer

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPointerFeedbackScopeAndDesktopOrigin(t *testing.T) {
	var b windowsBackend
	oldCalled, currentCalled := false, false
	old := b.BindPointerFeedback(t.Context(), func(string, int, int) { oldCalled = true })
	current := b.BindPointerFeedback(t.Context(), func(kind string, x, y int) {
		currentCalled = true
		require.Equal(t, "drag_move", kind)
		require.Equal(t, 30+getSystemMetrics(smXVirtualScreen), x)
		require.Equal(t, 40+getSystemMetrics(smYVirtualScreen), y)
	})
	old()
	b.pointerHeld.Store(true)
	b.emitPointer("move", 30, 40)
	require.False(t, oldCalled)
	require.True(t, currentCalled)
	currentCalled = false
	current()
	b.emitPointer("move", 30, 40)
	require.False(t, currentCalled)
}

func TestPointerAimRejectsCancellationDuringPresentation(t *testing.T) {
	var b windowsBackend
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	defer b.BindPointerFeedback(ctx, func(string, int, int) { cancel() })()
	require.ErrorIs(t, b.aimPointer(30, 40), context.Canceled)
}

func TestPointerOriginRejectsCancellationBeforeMovement(t *testing.T) {
	var b windowsBackend
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	defer b.BindPointerFeedback(ctx, func(string, int, int) { cancel() })()
	require.ErrorIs(t, b.presentPointer("origin", 30, 40), context.Canceled)
}
