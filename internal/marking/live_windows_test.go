//go:build windows

package marking

import (
	"context"
	"errors"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/require"
)

// findOverlay waits for the marking overlay window and returns it.
func findOverlay(t *testing.T) uintptr {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		var found uintptr
		cb := syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
			buf := make([]uint16, 128)
			user("GetClassNameW").Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), 128)
			if strings.HasPrefix(syscall.UTF16ToString(buf), "AtlasMarkOverlay") {
				found = hwnd
				return 0
			}
			return 1
		})
		user("EnumWindows").Call(cb, 0)
		if found != 0 {
			return found
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("marking overlay did not appear")
	return 0
}

func lparam(x, y int) uintptr { return uintptr(uint16(int16(x))) | uintptr(uint16(int16(y)))<<16 }

// TestLiveMarkOverlay drives the real overlay on this desktop by posting
// mouse messages to the overlay window itself, so the user's pointer never
// moves. Opt in with ATLAS_LIVE_MARK=1.
func TestLiveMarkOverlay(t *testing.T) {
	if os.Getenv("ATLAS_LIVE_MARK") != "1" {
		t.Skip("set ATLAS_LIVE_MARK=1 to show the overlay on this desktop")
	}
	mons, err := Desktop{}.Monitors()
	require.NoError(t, err)
	vb := VirtualBounds(mons)
	t.Logf("monitors: %+v virtual=%+v", mons, vb)

	type result struct {
		m    Mark
		crop []byte
		err  error
	}
	done := make(chan result, 1)
	go func() {
		m, crop, err := CaptureMark(context.Background(), "live test")
		done <- result{m, crop, err}
	}()
	hwnd := findOverlay(t)
	// Drag a 300x200 region from the primary monitor's centre, in overlay
	// client coordinates (virtual minus origin).
	var primary Monitor
	for _, m := range mons {
		if m.Primary {
			primary = m
		}
	}
	cx, cy := primary.Bounds.Center()
	x1, y1 := cx-150-vb.X, cy-100-vb.Y
	x2, y2 := x1+300, y1+200
	user("PostMessageW").Call(hwnd, wmLButtonDown, 1, lparam(x1, y1))
	user("PostMessageW").Call(hwnd, wmMouseMove, 1, lparam(x1+100, y1+80))
	user("PostMessageW").Call(hwnd, wmMouseMove, 1, lparam(x2, y2))
	user("PostMessageW").Call(hwnd, wmLButtonUp, 0, lparam(x2, y2))
	var r result
	select {
	case r = <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("selection did not complete")
	}
	require.NoError(t, r.err)
	want := Rect{X: x1 + vb.X, Y: y1 + vb.Y, W: 300, H: 200}
	require.Equal(t, want, r.m.Rect, "client coordinates map back to virtual-screen coordinates")
	require.Equal(t, primary.ID, r.m.Monitor.ID)
	require.True(t, r.m.OverlayExcluded)
	t.Logf("mark: rect=%+v monitor=%s dpi=%d window=%+v element=%+v", r.m.Rect, r.m.Monitor.ID, r.m.Monitor.DPI, r.m.Window, r.m.Element)

	store := Store{Dir: t.TempDir()}
	saved, err := store.Save(r.m, r.crop)
	require.NoError(t, err)
	v := Validate(saved, Desktop{}, time.Now())
	t.Logf("validity right after marking: %+v", v)
	require.NotEqual(t, Unverified, v.State)

	// Escape cancels without a mark.
	go func() {
		m, crop, err := CaptureMark(context.Background(), "")
		done <- result{m, crop, err}
	}()
	hwnd = findOverlay(t)
	user("PostMessageW").Call(hwnd, wmKeyDown, vkEscape, 0)
	select {
	case r = <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("cancel did not complete")
	}
	require.True(t, errors.Is(r.err, ErrCanceled))
}

func TestLiveElementAt(t *testing.T) {
	if os.Getenv("ATLAS_LIVE_MARK") != "1" {
		t.Skip("live only")
	}
	el, err := elementAt(context.Background(), 960, 540)
	t.Logf("element=%+v err=%v", el, err)
	require.NoError(t, err)
}
