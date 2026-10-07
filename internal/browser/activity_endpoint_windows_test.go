//go:build windows

package browser

import (
	"context"
	"encoding/binary"
	"net"
	"os"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/activity"
	"github.com/chromedp/chromedp"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	if activity.RunCursorRecovery() {
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestNativeListenerIdentityRequiresExactPIDAndPort(t *testing.T) {
	t.Parallel()
	for _, ipv6 := range []bool{false, true} {
		rowSize, portOffset, pidOffset := 24, 8, 20
		if ipv6 {
			rowSize, portOffset, pidOffset = 56, 20, 52
		}
		table := make([]byte, 4+rowSize)
		binary.LittleEndian.PutUint32(table, 1)
		binary.BigEndian.PutUint16(table[4+portOffset:], 9222)
		binary.LittleEndian.PutUint32(table[4+pidOffset:], 1234)
		require.True(t, tcpTableHasBrowserListener(table, ipv6, 9222, 1234))
		require.False(t, tcpTableHasBrowserListener(table, ipv6, 9223, 1234))
		require.False(t, tcpTableHasBrowserListener(table, ipv6, 9222, 5678))
		require.False(t, tcpTableHasBrowserListener(table[:len(table)-1], ipv6, 9222, 1234))
	}
}

func TestNativeListenerChecksRealWindowsSocketOwnership(t *testing.T) {
	t.Parallel()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	port := uint16(listener.Addr().(*net.TCPAddr).Port)
	pid := uint32(os.Getpid())
	require.True(t, localBrowserListenerOwned(port, pid))
	require.False(t, localBrowserListenerOwned(port, pid+1))
}

func TestNativeOverlayAttachedChromeLive(t *testing.T) {
	endpoint := os.Getenv("ATLAS_BROWSER_OVERLAY_LIVE_URL")
	if endpoint == "" {
		t.Skip("Explicit local Chrome endpoint required for visible overlay test")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	session, err := newChromedpSessionContext(ctx, Options{RemoteURL: endpoint, Headless: false})
	require.NoError(t, err)
	defer session.Close()
	s := session.(*chromedpSession)
	native, ok := s.activityNative.(*nativeBrowserActivity)
	require.True(t, ok, "An attached local Chrome must use the desktop renderer")
	require.NotZero(t, native.window.Load())
	_, _, validWindow := browserWindowGeometry(native.window.Load(), native.pid)
	require.True(t, validWindow)
	_, _, wrongPID := browserWindowGeometry(native.window.Load(), native.pid+1)
	require.False(t, wrongPID)
	// A destroyed document must not freeze a new visual aim when the
	// verified desktop window and DPI remain unchanged.
	failedContext, stopFailed := context.WithCancel(s.currentContext())
	stopFailed()
	recorder := &nativeActivityRecorder{}
	fallback := &nativeBrowserActivity{
		session: &chromedpSession{ctx: failedContext}, renderer: recorder, pid: native.pid,
		projection: native.projection, projectionRect: native.projectionRect,
		projectionDPI: native.projectionDPI, projectionValid: true,
		lastX: native.lastX, lastY: native.lastY, hasLast: true,
	}
	fallback.window.Store(native.window.Load())
	fallback.Render(activity.Event{Visible: true, Persistent: true, Point: true, PointerKind: "aim", PointerRevision: 1, PointerX: 200, PointerY: 150})
	wantX, wantY := native.projection.point(200, 150)
	require.Equal(t, wantX, recorder.events[0].X)
	require.Equal(t, wantY, recorder.events[0].Y)
	require.Equal(t, "aim", recorder.events[0].PointerKind)
	flow, finish := activity.StartFlow(ctx, "attached-overlay-proof")
	defer finish()
	op := s.StartActivity(flow, "attached-overlay-proof", "navigate", "")
	defer op.End()
	m := s.getActivityManager()
	m.Present()
	require.Eventually(t, func() bool { return nativeOverlayCoversMonitor(native.window.Load()) }, 5*time.Second, 20*time.Millisecond,
		"Native strips must reach all four monitor boundaries, including taskbar and browser toolbar")
	var pageOverlay bool
	require.NoError(t, s.run(chromedp.Evaluate(`!!document.querySelector('[data-atlas-activity]')`, &pageOverlay)))
	require.False(t, pageOverlay, "Desktop rendering must not create a second page cursor/banner")
	before := m.Snapshot()
	originalTab := string(chromedp.FromContext(s.currentContext()).Target.TargetID)
	require.NoError(t, s.run(chromedp.Evaluate(`(()=>{const h=document.createElement('div');h.setAttribute('data-atlas-activity','');document.documentElement.append(h);window[Symbol.for('atlas.agent.activity.v1')]={remove(){h.remove()}}})()`, nil)))
	_, _, err = s.Advanced(ctx, Request{Action: "tab_new"})
	require.NoError(t, err)
	m.Present()
	_, _, err = s.Advanced(ctx, Request{Action: "tabs"})
	require.NoError(t, err)
	_, _, err = s.Advanced(ctx, Request{Action: "tab_select", TabID: originalTab})
	require.NoError(t, err)
	require.NoError(t, s.run(chromedp.Evaluate(`!!document.querySelector('[data-atlas-activity]')`, &pageOverlay)))
	require.False(t, pageOverlay, "Selecting an existing tab must remove its legacy document indicator")
	m.Present()
	after := m.Snapshot()
	require.True(t, after.Visible)
	require.True(t, after.Persistent)
	require.Equal(t, before.ID, after.ID, "Tab changes must not restart the activity lifetime")
	require.True(t, nativeOverlayCoversMonitor(native.window.Load()))
}

type overlayCoverageScan struct {
	rects []browserDesktopRect
}

var overlayCoverageCallback = syscall.NewCallback(func(hwnd, parameter uintptr) uintptr {
	if !activity.IsOverlayWindow(hwnd) {
		return 1
	}
	visible, _, _ := browserOverlayUser.NewProc("IsWindowVisible").Call(hwnd)
	if visible != 0 {
		var rect browserDesktopRect
		ok, _, _ := browserOverlayUser.NewProc("GetWindowRect").Call(hwnd, uintptr(unsafe.Pointer(&rect)))
		if ok != 0 {
			scan := (*overlayCoverageScan)(unsafe.Pointer(parameter))
			scan.rects = append(scan.rects, rect)
		}
	}
	return 1
})

func nativeOverlayCoversMonitor(window uintptr) bool {
	monitor, _, _ := browserOverlayUser.NewProc("MonitorFromWindow").Call(window, 2)
	var info struct {
		Size          uint32
		Monitor, Work browserDesktopRect
		Flags         uint32
	}
	info.Size = uint32(unsafe.Sizeof(info))
	ok, _, _ := browserOverlayUser.NewProc("GetMonitorInfoW").Call(monitor, uintptr(unsafe.Pointer(&info)))
	if ok == 0 {
		return false
	}
	scan := overlayCoverageScan{}
	browserOverlayUser.NewProc("EnumWindows").Call(overlayCoverageCallback, uintptr(unsafe.Pointer(&scan)))
	if len(scan.rects) < 6 {
		return false
	}
	var top, right, bottom, left bool
	for _, r := range scan.rects {
		top = top || (r.Left == info.Monitor.Left && r.Right == info.Monitor.Right && r.Top == info.Monitor.Top)
		bottom = bottom || (r.Left == info.Monitor.Left && r.Right == info.Monitor.Right && r.Bottom == info.Monitor.Bottom)
		left = left || (r.Left == info.Monitor.Left && r.Top > info.Monitor.Top && r.Bottom < info.Monitor.Bottom)
		right = right || (r.Right == info.Monitor.Right && r.Top > info.Monitor.Top && r.Bottom < info.Monitor.Bottom)
	}
	return top && right && bottom && left
}
