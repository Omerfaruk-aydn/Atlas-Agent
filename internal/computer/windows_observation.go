//go:build windows

package computer

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"
)

type nativeWindowObservation struct {
	ID         string `json:"window_id"`
	Name       string `json:"name"`
	ProcessID  uint32 `json:"process_id"`
	Foreground bool   `json:"foreground"`
	Minimized  bool   `json:"minimized"`
	X          int32  `json:"x"`
	Y          int32  `json:"y"`
	Width      int32  `json:"width"`
	Height     int32  `json:"height"`
}

type nativeWindowEnumeration struct {
	ctx        context.Context
	foreground string
	windows    []nativeWindowObservation
}

var (
	nativeEnumerations  sync.Map
	nativeEnumerationID atomic.Uint64
	// Register once: syscall callbacks cannot be freed on Windows.
	nativeWindowCallback = syscall.NewCallback(observeNativeWindow)
)

func observeNativeWindow(hwnd, token uintptr) uintptr {
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
	var pid uint32
	_, _, _ = modUser32.NewProc("GetWindowThreadProcessId").Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	minimized, _, _ := modUser32.NewProc("IsIconic").Call(hwnd)
	id := strconv.FormatUint(uint64(hwnd), 10)
	enumeration.windows = append(enumeration.windows, nativeWindowObservation{
		ID: id, Name: syscall.UTF16ToString(title[:]), ProcessID: pid,
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
	enumeration := &nativeWindowEnumeration{ctx: ctx, foreground: b.ForegroundWindow(), windows: []nativeWindowObservation{}}
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
