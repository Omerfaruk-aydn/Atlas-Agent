//go:build windows

package activity

import (
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var physicalEscapeWatchers sync.Map

// WatchEscape listens for physical Escape without exposing cancellation to apps.
// The returned function unregisters the hook and waits for its thread to exit.
func WatchEscape(capture func() func()) func() {
	ready, done, quit := make(chan bool, 1), make(chan struct{}), make(chan struct{})
	token := new(byte)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(done)
		instance, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("GetModuleHandleW").Call(0)
		hook, _, _ := overlayUser.NewProc("SetWindowsHookExW").Call(13, escapeCallback, instance, 0)
		if hook == 0 {
			ready <- false
			return
		}
		defer overlayUser.NewProc("UnhookWindowsHookEx").Call(hook)
		physicalEscapeWatchers.Store(token, capture)
		defer physicalEscapeWatchers.Delete(token)
		ready <- true
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-quit:
				return
			case <-ticker.C:
				var message overlayMessage
				for {
					ok, _, _ := overlayUser.NewProc("PeekMessageW").Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0, 1)
					if ok == 0 {
						break
					}
					overlayUser.NewProc("TranslateMessage").Call(uintptr(unsafe.Pointer(&message)))
					overlayUser.NewProc("DispatchMessageW").Call(uintptr(unsafe.Pointer(&message)))
				}
			}
		}
	}()
	if !<-ready {
		<-done
		return nil
	}
	var once sync.Once
	return func() { once.Do(func() { close(quit); <-done }) }
}
