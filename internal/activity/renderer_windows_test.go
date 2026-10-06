//go:build windows

package activity

import (
	"context"
	"io"
	"math"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/require"
)

func TestWindowsBannerOrderBetweenSurfaceUpdates(t *testing.T) {
	if os.Getenv("ATLAS_DESKTOP_FIXTURE") != "1" {
		t.Skip("Set ATLAS_DESKTOP_FIXTURE=1 for native overlay verification")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	name, err := syscall.UTF16PtrFromString(t.Name())
	require.NoError(t, err)
	instance, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("GetModuleHandleW").Call(0)
	class := overlayClass{Procedure: overlayCallback, Instance: instance, Name: name}
	class.Size = uint32(unsafe.Sizeof(class))
	atom, _, _ := overlayUser.NewProc("RegisterClassExW").Call(uintptr(unsafe.Pointer(&class)))
	require.NotZero(t, atom)
	defer overlayUser.NewProc("UnregisterClassW").Call(uintptr(unsafe.Pointer(name)), instance)
	r := &windowsRenderer{event: Event{Visible: true, Point: true, X: 40, Y: 40}, epoch: time.Now()}
	for range 6 {
		hwnd, _, _ := overlayUser.NewProc("CreateWindowExW").Call(0x080800a8, uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(name)), 0x80000000, 0, 0, 1, 1, 0, 0, instance, 0)
		require.NotZero(t, hwnd)
		defer overlayUser.NewProc("DestroyWindow").Call(hwnd)
		if len(r.windows) == 5 {
			r.bannerWindow = hwnd
		} else {
			r.windows = append(r.windows, hwnd)
		}
	}
	require.True(t, r.createCursorSurface(1))
	defer func() {
		r.banner.close()
		for i := range r.edges {
			r.edges[i].close()
		}
		overlayGDI.NewProc("SelectObject").Call(r.imageDC, r.oldBitmap)
		overlayGDI.NewProc("DeleteObject").Call(r.bitmap)
		overlayGDI.NewProc("DeleteDC").Call(r.imageDC)
	}()
	r.draw()
	visible, _, _ := overlayUser.NewProc("IsWindowVisible").Call(r.bannerWindow)
	require.NotZero(t, visible)
	monitor, _, _ := overlayUser.NewProc("MonitorFromWindow").Call(r.windows[0], 2)
	// Start the next frame by raising the cursor, as the renderer does.
	positioned, _, _ := overlayUser.NewProc("SetWindowPos").Call(r.windows[0], ^uintptr(0), 0, 0, 0, 0, 0x13)
	require.NotZero(t, positioned)
	// Stop before repainting the banner: the compositor may present this stack.
	r.drawEdges(monitor, r.scale)
	for above, _, _ := overlayUser.NewProc("GetWindow").Call(r.bannerWindow, 3); above != 0; {
		require.NotContains(t, r.windows[1:], above, "Smoke must stay below the banner while updating independently")
		above, _, _ = overlayUser.NewProc("GetWindow").Call(above, 3)
	}
}

func TestWindowsActivityOwnedFixture(t *testing.T) {
	if os.Getenv("ATLAS_DESKTOP_FIXTURE") != "1" {
		t.Skip("Set ATLAS_DESKTOP_FIXTURE=1 for native overlay verification")
	}
	foreground, _, _ := overlayUser.NewProc("GetForegroundWindow").Call()
	r := newPlatformRenderer().(*windowsRenderer)
	m := New(r)
	defer m.Close()
	finish := m.Begin(context.Background(), Event{Session: "fixture", Action: "click", WindowID: "", Point: true, X: 40, Y: 40})
	require.Eventually(t, func() bool { r.mu.Lock(); defer r.mu.Unlock(); return len(r.windows) == 5 }, 2*time.Second, 10*time.Millisecond)
	require.Eventually(t, func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		v, _, _ := overlayUser.NewProc("IsWindowVisible").Call(r.windows[0])
		return v != 0
	}, time.Second, 10*time.Millisecond)
	r.mu.Lock()
	windows := append([]uintptr(nil), r.windows...)
	badge := r.bannerWindow
	r.mu.Unlock()
	require.NotZero(t, badge)
	require.Eventually(t, func() bool {
		v, _, _ := overlayUser.NewProc("IsWindowVisible").Call(badge)
		return v != 0
	}, time.Second, 10*time.Millisecond)
	require.True(t, IsOverlayWindow(badge))
	for _, w := range windows {
		require.Eventually(t, func() bool {
			v, _, _ := overlayUser.NewProc("IsWindowVisible").Call(w)
			return v != 0
		}, time.Second, 10*time.Millisecond)
		require.True(t, IsOverlayWindow(w))
		style, _, _ := overlayUser.NewProc("GetWindowLongPtrW").Call(w, ^uintptr(19))
		require.NotZero(t, style&0x08000000)
		require.NotZero(t, style&0x20)
		require.NotZero(t, style&0x8, "Every overlay surface must stay in the topmost band")
	}
	// No smoke strip may be above the replacement pointer in the Z order.
	checkCursorOrder := func() {
		for above, _, _ := overlayUser.NewProc("GetWindow").Call(windows[0], 3); above != 0; {
			require.NotContains(t, windows[1:], above, "Smoke must not cover the replacement cursor")
			require.NotEqual(t, badge, above, "The banner must not cover the replacement cursor")
			above, _, _ = overlayUser.NewProc("GetWindow").Call(above, 3)
		}
	}
	// Monitor the live stack without the renderer lock to catch mid-frame
	// inversions that the compositor can present as banner flicker.
	checkBannerOrder := func() {
		for above, _, _ := overlayUser.NewProc("GetWindow").Call(badge, 3); above != 0; {
			require.NotContains(t, windows[1:], above, "Smoke must never cover the status banner, including between frame updates")
			above, _, _ = overlayUser.NewProc("GetWindow").Call(above, 3)
		}
	}
	checkCursorOrder()
	checkBannerOrder()
	after, _, _ := overlayUser.NewProc("GetForegroundWindow").Call()
	require.Equal(t, foreground, after)
	r.mu.Lock()
	startFrames := r.frames
	r.mu.Unlock()
	start := time.Now()
	for time.Since(start) < 2*time.Second {
		checkCursorOrder()
		checkBannerOrder()
		time.Sleep(time.Millisecond)
	}
	r.mu.Lock()
	fps := float64(r.frames-startFrames) / time.Since(start).Seconds()
	r.mu.Unlock()
	t.Logf("Native cursor and RGB edges: %.1f presented frames/s", fps)
	require.True(t, m.Pointer(m.Snapshot().ID, "origin", 40, 40))
	m.Present()
	r.mu.Lock()
	startPointerFrames := r.pointerFrames
	r.mu.Unlock()
	require.True(t, m.Pointer(m.Snapshot().ID, "aim", 600, 300))
	aimed := make(chan struct{})
	go func() { m.Present(); close(aimed) }()
	positions := make(map[int32]bool)
	start = time.Now()
	for {
		var position overlayRect
		read, _, _ := overlayUser.NewProc("GetWindowRect").Call(windows[0], uintptr(unsafe.Pointer(&position)))
		require.NotZero(t, read)
		r.mu.Lock()
		tipX := position.Left + int32(math.Round(cursorHotspot*r.scale))
		r.mu.Unlock()
		if tipX > 40 && tipX < 599 {
			positions[tipX] = true
		}
		select {
		case <-aimed:
			goto arrived
		case <-time.After(5 * time.Millisecond):
		}
	}
arrived:
	r.mu.Lock()
	paintedPointerFrames := r.pointerFrames - startPointerFrames
	r.mu.Unlock()
	require.GreaterOrEqual(t, paintedPointerFrames, uint64(4), "The native surface must present at least three intermediate positions plus arrival before input")
	require.NotEmpty(t, positions, "An external window observer must also see intermediate positions")
	require.Greater(t, time.Since(start), 80*time.Millisecond)
	require.True(t, m.Pointer(m.Snapshot().ID, "click", 600, 300))
	m.Present()
	r.mu.Lock()
	ringIndex := (int(math.Round(61*r.scale))*int(math.Round(cursorSize*r.scale))+int(math.Round(81*r.scale)))*4 + 3
	ringAlpha := r.cursorPixels[ringIndex]
	r.mu.Unlock()
	require.Greater(t, ringAlpha, uint8(80), "Confirmed clicks must paint a visible response on the native surface")
	require.True(t, m.Pointer(m.Snapshot().ID, "aim", 90, 80))
	m.Present()
	var rect struct{ Left, Top, Right, Bottom int32 }
	read, _, _ := overlayUser.NewProc("GetWindowRect").Call(windows[0], uintptr(unsafe.Pointer(&rect)))
	require.NotZero(t, read)
	r.mu.Lock()
	hotspot := int32(math.Round(cursorHotspot * r.scale))
	r.mu.Unlock()
	require.Equal(t, int32(90)-hotspot, rect.Left)
	require.Equal(t, int32(80)-hotspot, rect.Top)
	restore := m.Suspend()
	v, _, _ := overlayUser.NewProc("IsWindowVisible").Call(badge)
	require.Zero(t, v)
	for _, w := range windows {
		v, _, _ := overlayUser.NewProc("IsWindowVisible").Call(w)
		require.Zero(t, v)
	}
	restore()
	finish()
	m.Close()
	require.False(t, IsOverlayWindow(badge))
	for _, w := range windows {
		v, _, _ := overlayUser.NewProc("IsWindow").Call(w)
		require.Zero(t, v)
		require.False(t, IsOverlayWindow(w))
	}
}

func TestSuspendedPersistentOverlayRestoresSystemCursor(t *testing.T) {
	if os.Getenv("ATLAS_DESKTOP_FIXTURE") != "1" {
		t.Skip("Set ATLAS_DESKTOP_FIXTURE=1 for native overlay verification")
	}
	r := newPlatformRenderer().(*windowsRenderer)
	defer r.Close()
	r.Render(Event{Visible: true})
	require.Eventually(t, func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.frames > 0
	}, 2*time.Second, 10*time.Millisecond)
	// Exercise guard teardown without changing any user's system cursors.
	executable, err := os.Executable()
	require.NoError(t, err)
	cmd := exec.CommandContext(t.Context(), executable, "-test.run=^$")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	require.NoError(t, cmd.Start())
	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	defer reader.Close()
	r.mu.Lock()
	r.cursor.guard = &cursorGuard{input: writer, cmd: cmd}
	r.cursor.heartbeat = time.Now()
	r.mu.Unlock()
	r.Render(Event{Visible: false, Persistent: true})
	r.mu.Lock()
	guardReleased := r.cursor.guard == nil
	r.mu.Unlock()
	require.True(t, guardReleased, "A hidden replacement must release system cursor suppression")
}

// The banner character on a real desktop: frame rate, a stable banner
// rectangle, the banner-only completion and leak-free teardown. It uses a
// standalone operation so the user's system cursors are never replaced.
func TestWindowsBannerMascotFixture(t *testing.T) {
	if os.Getenv("ATLAS_DESKTOP_FIXTURE") != "1" {
		t.Skip("Set ATLAS_DESKTOP_FIXTURE=1 for native overlay verification")
	}
	baseline := runtime.NumGoroutine()
	r := newPlatformRenderer().(*windowsRenderer)
	now := time.Now()
	r.Render(Event{ID: 1, Visible: true, Resource: "desktop", Action: "click", Point: true, X: 300, Y: 500, PhaseAt: now})
	require.Eventually(t, func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.mascot.Frames > 3 && r.bannerWindow != 0
	}, 3*time.Second, 10*time.Millisecond)
	r.mu.Lock()
	badge, cursor := r.bannerWindow, r.windows[0]
	startFrames, startMascot, scale := r.frames, r.mascot.Frames, r.scale
	r.mu.Unlock()
	var first overlayRect
	overlayUser.NewProc("GetWindowRect").Call(badge, uintptr(unsafe.Pointer(&first)))
	start := time.Now()
	moods := []Event{
		{ID: 1, Visible: true, Action: "click", Point: true, X: 300, Y: 500, PointerKind: "click", PointerAt: time.Now()},
		{ID: 1, Visible: true, Phase: PhaseThinking, PhaseAt: time.Now().Add(-time.Second)},
		{ID: 1, Visible: true, Phase: PhaseFailed, PhaseAt: time.Now()},
		{ID: 1, Visible: true, Action: "type"},
	}
	for i := 0; time.Since(start) < 2*time.Second; i++ {
		if i%400 == 0 {
			e := moods[(i/400)%len(moods)]
			e.Resource = "desktop"
			r.Render(e)
		}
		var rect overlayRect
		overlayUser.NewProc("GetWindowRect").Call(badge, uintptr(unsafe.Pointer(&rect)))
		require.Equal(t, first, rect, "The banner rectangle stays fixed while the character animates")
		time.Sleep(time.Millisecond)
	}
	r.mu.Lock()
	elapsed := time.Since(start).Seconds()
	fps, mascotFPS := float64(r.frames-startFrames)/elapsed, float64(r.mascot.Frames-startMascot)/elapsed
	r.mu.Unlock()
	t.Logf("Monitor scale %.2f: %.1f presented frames/s, %.1f character frames/s", scale, fps, mascotFPS)
	require.Greater(t, mascotFPS, 50.0)

	// Completion keeps only the banner, then removes it.
	finished := time.Now()
	r.Render(Event{ID: 2, Resource: "desktop", Phase: PhaseDone, PhaseAt: finished, FinishedUntil: finished.Add(mascotDoneLinger)})
	time.Sleep(120 * time.Millisecond)
	v, _, _ := overlayUser.NewProc("IsWindowVisible").Call(cursor)
	require.Zero(t, v, "The agent cursor leaves as soon as the run completes")
	v, _, _ = overlayUser.NewProc("IsWindowVisible").Call(badge)
	require.NotZero(t, v, "The banner stays for the completion motion")
	var done overlayRect
	overlayUser.NewProc("GetWindowRect").Call(badge, uintptr(unsafe.Pointer(&done)))
	require.Equal(t, first, done, "Completion does not resize the banner")
	require.Eventually(t, func() bool {
		v, _, _ := overlayUser.NewProc("IsWindowVisible").Call(badge)
		return v == 0
	}, 2*time.Second, 10*time.Millisecond)
	require.Eventually(t, func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		return !r.mascot.anim.ready
	}, time.Second, 10*time.Millisecond, "A hidden banner forgets its pose")

	r.Render(Event{ID: 3, Visible: true, Resource: "desktop", Action: "observe"})
	r.Close()
	require.False(t, IsOverlayWindow(badge))
	v, _, _ = overlayUser.NewProc("IsWindow").Call(badge)
	require.Zero(t, v)
	require.Eventually(t, func() bool { return runtime.NumGoroutine() <= baseline+1 }, 2*time.Second, 10*time.Millisecond, "No goroutine outlives the renderer")
}
