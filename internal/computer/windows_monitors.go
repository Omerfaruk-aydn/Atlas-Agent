//go:build windows

package computer

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"syscall"
	"unsafe"
)

// Monitors are enumerated in this process, which is per-monitor DPI aware
// (see openPlatform), so bounds are physical pixels in the same virtual
// screen space as screenshots, window rectangles, the cursor and input.
// The UI Automation worker is a separate PowerShell process without DPI
// awareness; at 125% scaling it reported a 1920×1080 monitor as 1536×864.

var (
	modShcore            = syscall.NewLazyDLL("shcore.dll")
	procGetDpiForMonitor = modShcore.NewProc("GetDpiForMonitor")
	monitorCallback      = syscall.NewCallback(observeMonitor)
	monitorMu            sync.Mutex
	monitorTarget        *[]MonitorObservation
)

func observeMonitor(hmonitor, _, _, _ uintptr) uintptr {
	if monitorTarget == nil || len(*monitorTarget) >= 32 {
		return 0
	}
	info := struct {
		Size    uint32
		Monitor struct{ Left, Top, Right, Bottom int32 }
		Work    struct{ Left, Top, Right, Bottom int32 }
		Flags   uint32
		Device  [32]uint16
	}{}
	info.Size = uint32(unsafe.Sizeof(info))
	ok, _, _ := modUser32.NewProc("GetMonitorInfoW").Call(hmonitor, uintptr(unsafe.Pointer(&info)))
	if ok == 0 {
		return 1
	}
	m := MonitorObservation{
		Name:    syscall.UTF16ToString(info.Device[:]),
		Primary: info.Flags&1 != 0, // MONITORINFOF_PRIMARY.
		X:       int(info.Monitor.Left), Y: int(info.Monitor.Top),
		Width:  int(info.Monitor.Right - info.Monitor.Left),
		Height: int(info.Monitor.Bottom - info.Monitor.Top),
		WorkX:  int(info.Work.Left), WorkY: int(info.Work.Top),
		WorkWidth:  int(info.Work.Right - info.Work.Left),
		WorkHeight: int(info.Work.Bottom - info.Work.Top),
		DPI:        96,
	}
	if procGetDpiForMonitor.Find() == nil {
		var dpiX, dpiY uint32
		// MDT_EFFECTIVE_DPI.
		if hr, _, _ := procGetDpiForMonitor.Call(hmonitor, 0, uintptr(unsafe.Pointer(&dpiX)), uintptr(unsafe.Pointer(&dpiY))); hr == 0 && dpiX > 0 {
			m.DPI = int(dpiX)
		}
	}
	m.ScaleFactor = float64(m.DPI) / 96
	*monitorTarget = append(*monitorTarget, m)
	return 1
}

func (b *windowsBackend) listNativeMonitors(ctx context.Context) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	monitorMu.Lock()
	defer monitorMu.Unlock()
	out := []MonitorObservation{}
	monitorTarget = &out
	defer func() { monitorTarget = nil }()
	ok, _, callErr := modUser32.NewProc("EnumDisplayMonitors").Call(0, 0, monitorCallback, 0)
	if ok == 0 && len(out) == 0 {
		return nil, fmt.Errorf("monitor_observation_failed: EnumDisplayMonitors: %v", callErr)
	}
	for i := range out {
		out[i].CoordinateSpace = CoordinateSpacePhysical
	}
	return json.Marshal(out)
}
