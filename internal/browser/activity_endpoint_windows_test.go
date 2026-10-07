//go:build windows

package browser

import (
	"encoding/binary"

	"os"
	"syscall"
	"testing"

	"unsafe"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/activity"

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
