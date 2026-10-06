//go:build windows

package activity

import (
	"math"
	"os"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	if RunCursorRecovery() {
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestNativePersistentCursorReplacementLive(t *testing.T) {
	if os.Getenv("ATLAS_DESKTOP_FIXTURE") != "1" {
		t.Skip("Explicit visible desktop fixture required for cursor replacement")
	}
	r := newPlatformRenderer().(*windowsRenderer)
	m := New(r)
	defer m.Close()
	ctx, finish := StartFlow(t.Context(), "cursor-replacement-proof")
	defer finish()
	op := m.Start(ctx, Event{Resource: "browser", Session: "cursor-replacement-proof", Point: true, X: 400, Y: 300})
	defer op.End()
	m.Pointer(m.Snapshot().ID, "origin", 400, 300)
	m.Present()
	require.Eventually(t, func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.frames > 0 && r.cursor.guard != nil && len(r.cursor.saved) == len(systemCursorIDs) && !r.cursor.disabled
	}, 5*time.Second, 20*time.Millisecond, "All system cursor shapes must be replaced after the agent cursor is painted")
	var original overlayPoint
	read, _, _ := overlayUser.NewProc("GetCursorPos").Call(uintptr(unsafe.Pointer(&original)))
	require.NotZero(t, read)
	defer overlayUser.NewProc("SetCursorPos").Call(uintptr(original.X), uintptr(original.Y))
	moved, _, _ := overlayUser.NewProc("SetCursorPos").Call(450, 330)
	require.NotZero(t, moved)
	require.Eventually(t, func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.pointerSource.physical && math.Abs(r.pointerMotion.x-450) < .5 && math.Abs(r.pointerMotion.y-330) < .5
	}, 2*time.Second, 10*time.Millisecond, "The only visible cursor must follow physical mouse motion while agent input is idle")
	require.True(t, m.Pointer(m.Snapshot().ID, "aim", 700, 400))
	m.Present()
	r.mu.Lock()
	require.False(t, r.pointerSource.physical, "A fresh agent aim resumes agent motion")
	require.InDelta(t, 700, r.pointerMotion.x, .5)
	require.InDelta(t, 400, r.pointerMotion.y, .5)
	r.mu.Unlock()
	finish()
	r.mu.Lock()
	require.Nil(t, r.cursor.guard, "Ending the run must release cursor replacement")
	require.Nil(t, r.cursor.saved)
	require.False(t, r.event.Visible, "Finishing must hide surfaces without closing the reusable renderer")
	r.mu.Unlock()
	next, endNext := StartFlow(t.Context(), "cursor-stop-proof")
	defer endNext()
	m.Start(next, Event{Resource: "browser", Session: "cursor-stop-proof", Point: true, X: 400, Y: 300})
	m.Present()
	require.True(t, m.Stop(m.Snapshot().ID))
	r.mu.Lock()
	defer r.mu.Unlock()
	require.False(t, r.event.Visible)
	require.Nil(t, r.cursor.guard, "Banner Stop must restore the cursor before returning")
	require.Nil(t, r.cursor.saved)
}
