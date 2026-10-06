//go:build windows

package computer

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/activity"
	"golang.org/x/sys/windows"
)

type nativeWindowObservation struct {
	ID             string `json:"window_id"`
	Name           string `json:"name"`
	ProcessID      uint32 `json:"process_id"`
	ProcessName    string `json:"process_name,omitempty"`
	ClassName      string `json:"class_name"`
	Owner          string `json:"owner_window_id"`
	OwnerProcessID uint32 `json:"owner_process_id,omitempty"`
	Foreground     bool   `json:"foreground"`
	Minimized      bool   `json:"minimized"`
	X              int32  `json:"x"`
	Y              int32  `json:"y"`
	Width          int32  `json:"width"`
	Height         int32  `json:"height"`
}

type nativeWindowEnumeration struct {
	ctx        context.Context
	foreground string
	windows    []nativeWindowObservation
	processes  map[uint32]string
}

var (
	nativeEnumerations  sync.Map
	nativeEnumerationID atomic.Uint64
	// Register once: syscall callbacks cannot be freed on Windows.
	nativeWindowCallback = syscall.NewCallback(observeNativeWindow)
)

func observeNativeWindow(hwnd, token uintptr) uintptr {
	if activity.IsOverlayWindow(hwnd) {
		return 1
	}
	value, ok := nativeEnumerations.Load(token)
	if !ok {
		return 0
	}
	enumeration := value.(*nativeWindowEnumeration)
	if enumeration.ctx.Err() != nil || len(enumeration.windows) >= 500 {
		return 0
	}
	visible, _, _ := modUser32.NewProc("IsWindowVisible").Call(hwnd)
	if visible == 0 {
		return 1
	}
	var rect struct{ Left, Top, Right, Bottom int32 }
	valid, _, _ := modUser32.NewProc("GetWindowRect").Call(hwnd, uintptr(unsafe.Pointer(&rect)))
	if valid == 0 {
		return 1
	}
	var title [512]uint16
	_, _, _ = modUser32.NewProc("GetWindowTextW").Call(hwnd, uintptr(unsafe.Pointer(&title[0])), uintptr(len(title)))
	var className [256]uint16
	_, _, _ = modUser32.NewProc("GetClassNameW").Call(hwnd, uintptr(unsafe.Pointer(&className[0])), uintptr(len(className)))
	var pid uint32
	_, _, _ = modUser32.NewProc("GetWindowThreadProcessId").Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	processName, found := enumeration.processes[pid]
	if !found {
		processName = nativeProcessName(pid)
		enumeration.processes[pid] = processName
	}
	owner, _, _ := modUser32.NewProc("GetWindow").Call(hwnd, 4) // GW_OWNER.
	var ownerPID uint32
	if owner != 0 {
		_, _, _ = modUser32.NewProc("GetWindowThreadProcessId").Call(owner, uintptr(unsafe.Pointer(&ownerPID)))
	}
	minimized, _, _ := modUser32.NewProc("IsIconic").Call(hwnd)
	id := strconv.FormatUint(uint64(hwnd), 10)
	enumeration.windows = append(enumeration.windows, nativeWindowObservation{
		ID: id, Name: syscall.UTF16ToString(title[:]), ProcessID: pid, ProcessName: processName, ClassName: syscall.UTF16ToString(className[:]), Owner: strconv.FormatUint(uint64(owner), 10), OwnerProcessID: ownerPID,
		Foreground: id == enumeration.foreground, Minimized: minimized != 0,
		X: rect.Left, Y: rect.Top, Width: rect.Right - rect.Left, Height: rect.Bottom - rect.Top,
	})
	return 1
}

// listNativeWindows avoids starting PowerShell or querying UIA providers.
func (b *windowsBackend) listNativeWindows(ctx context.Context) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	enumeration := &nativeWindowEnumeration{ctx: ctx, foreground: b.ForegroundWindow(), windows: []nativeWindowObservation{}, processes: make(map[uint32]string)}
	token := uintptr(nativeEnumerationID.Add(1))
	nativeEnumerations.Store(token, enumeration)
	defer nativeEnumerations.Delete(token)
	ok, _, callErr := modUser32.NewProc("EnumWindows").Call(nativeWindowCallback, token)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if ok == 0 && len(enumeration.windows) < 500 {
		return nil, fmt.Errorf("window_observation_failed: EnumWindows: %v", callErr)
	}
	return json.Marshal(enumeration.windows)
}

// nativeProcessName reads the image identity without starting a shell or UIA.
func nativeProcessName(pid uint32) string {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(handle)
	var path [32768]uint16
	length := uint32(len(path))
	if windows.QueryFullProcessImageName(handle, 0, &path[0], &length) != nil {
		return ""
	}
	return filepath.Base(windows.UTF16ToString(path[:length]))
}
