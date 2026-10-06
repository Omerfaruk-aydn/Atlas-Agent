//go:build windows

package computer

import (
	"context"
	"fmt"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

type desktopChangeWatch struct {
	window uintptr
	events chan struct{}
}

var desktopChangeWatches = struct {
	sync.Mutex
	hooks map[uintptr]desktopChangeWatch
}{hooks: map[uintptr]desktopChangeWatch{}}

// One callback address is shared; Windows callbacks cannot be garbage collected.
var desktopChangeCallback = syscall.NewCallback(func(hook, event, hwnd, object, child, thread, at uintptr) uintptr {
	desktopChangeWatches.Lock()
	defer desktopChangeWatches.Unlock()
	watch, ok := desktopChangeWatches.hooks[hook]
	if !ok || hwnd == 0 {
		return 0
	}
	root, _, _ := modUser32.NewProc("GetAncestor").Call(hwnd, 2) // GA_ROOT.
	if hwnd == watch.window || root == watch.window {
		select {
		case watch.events <- struct{}{}:
		default:
		}
	}
	return 0
})

// WatchChanges owns a message-pump thread and unregisters every hook on exit.
// The channel stays open; the caller's context bounds every wait.
func (b *windowsBackend) WatchChanges(ctx context.Context, id string) (<-chan struct{}, func(), error) {
	n, err := strconv.ParseUint(id, 10, 64)
	if err != nil || n == 0 || uint64(uintptr(n)) != n || strconv.FormatUint(n, 10) != id {
		return nil, nil, fmt.Errorf("invalid change subscription window")
	}
	valid, _, _ := modUser32.NewProc("IsWindow").Call(uintptr(n))
	if valid == 0 {
		return nil, nil, fmt.Errorf("target_missing: change subscription window disappeared")
	}
	ctx, cancel := context.WithCancel(ctx)
	events := make(chan struct{}, 1)
	ready := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(done)
		proc := modUser32.NewProc("SetWinEventHook")
		var hooks []uintptr
		defer func() {
			for _, hook := range hooks {
				modUser32.NewProc("UnhookWinEvent").Call(hook)
				desktopChangeWatches.Lock()
				delete(desktopChangeWatches.hooks, hook)
				desktopChangeWatches.Unlock()
			}
		}()
		// EVENT_SYSTEM_FOREGROUND and EVENT_OBJECT_CREATE..TEXTSELECTIONCHANGED.
		for _, interval := range [][2]uintptr{{3, 3}, {0x8000, 0x8014}} {
			hook, _, _ := proc.Call(interval[0], interval[1], 0, desktopChangeCallback, 0, 0, 0)
			if hook == 0 {
				ready <- fmt.Errorf("native change hooks unavailable")
				return
			}
			hooks = append(hooks, hook)
			desktopChangeWatches.Lock()
			desktopChangeWatches.hooks[hook] = desktopChangeWatch{uintptr(n), events}
			desktopChangeWatches.Unlock()
		}
		ready <- nil
		// MSG alignment and pointer sizes follow the Windows ABI.
		var msg struct {
			Window  windows.Handle
			Message uint32
			WParam  uintptr
			LParam  uintptr
			Time    uint32
			Point   struct{ X, Y int32 }
			Private uint32
		}
		peek := modUser32.NewProc("PeekMessageW")
		for ctx.Err() == nil {
			for {
				found, _, _ := peek.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0, 1)
				if found == 0 {
					break
				}
				modUser32.NewProc("TranslateMessage").Call(uintptr(unsafe.Pointer(&msg)))
				modUser32.NewProc("DispatchMessageW").Call(uintptr(unsafe.Pointer(&msg)))
			}
			// Message wake, with a short bound so cancellation promptly cleans up.
			modUser32.NewProc("MsgWaitForMultipleObjectsEx").Call(0, 0, 50, 0x04ff, 0x0004)
		}
	}()
	select {
	case err := <-ready:
		if err != nil {
			cancel()
			<-done
			return nil, nil, err
		}
	case <-ctx.Done():
		cancel()
		<-done
		return nil, nil, ctx.Err()
	}
	release := func() { cancel(); <-done }
	return events, release, nil
}
