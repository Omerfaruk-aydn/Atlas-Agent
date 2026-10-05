//go:build windows

package activity

import (
	"log/slog"
	"math"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

var (
	overlayUser    = syscall.NewLazyDLL("user32.dll")
	overlayGDI     = syscall.NewLazyDLL("gdi32.dll")
	overlayDWM     = syscall.NewLazyDLL("dwmapi.dll")
	overlayScaling = syscall.NewLazyDLL("shcore.dll")
	overlayWindows sync.Map
	overlayClassID atomic.Uint64
)

// IsOverlayWindow excludes our noninteractive surfaces from target discovery.
func IsOverlayWindow(hwnd uintptr) bool { _, ok := overlayWindows.Load(hwnd); return ok }

type (
	overlayRect    struct{ Left, Top, Right, Bottom int32 }
	overlayPoint   struct{ X, Y int32 }
	overlayMessage struct {
		Window  uintptr
		Message uint32
		WParam  uintptr
		LParam  uintptr
		Time    uint32
		Point   overlayPoint
		Private uint32
	}
	overlayClass struct {
		Size, Style                        uint32
		Procedure                          uintptr
		ClassExtra, WindowExtra            int32
		Instance, Icon, Cursor, Background uintptr
		Menu, Name                         *uint16
		SmallIcon                          uintptr
	}
)

var overlayCallback = syscall.NewCallback(func(hwnd uintptr, msg uint32, wparam, lparam uintptr) uintptr {
	if msg == 0x84 {
		return ^uintptr(0)
	} // HTTRANSPARENT.
	if msg == 0x21 {
		return 3
	} // MA_NOACTIVATE.
	if msg == 0x14 {
		return 1
	} // No background erasure.
	result, _, _ := overlayUser.NewProc("DefWindowProcW").Call(hwnd, uintptr(msg), wparam, lparam)
	return result
})

type windowsRenderer struct {
	mu            sync.Mutex
	event         Event
	windows       []uintptr
	started       bool
	stopped       bool
	stop          chan struct{}
	done          chan struct{}
	hide          chan chan struct{}
	present       chan chan struct{}
	imageDC       uintptr
	cursorPixels  []byte
	cursorIdle    []byte
	cursorPressed []byte
	cursorHeld    []byte
	pointerMotion pointerMotion
	bitmap        uintptr
	oldBitmap     uintptr
	cursor        cursorOverride
	scale         float64
	edges         [4]nativeEdgeSurface
	edgeArea      overlayRect
	edgeScale     float64
	epoch         time.Time
	frames        uint64
	pointerFrames uint64
	lastPointer   overlayPoint
	bannerWindow  uintptr
	banner        nativeBanner
	onStop        func(uint64) bool
	keyboardHook  uintptr
	escapeEvent   atomic.Pointer[Event]
}

func (r *windowsRenderer) SetStopHandler(stop func(uint64) bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onStop = stop
}

func newPlatformRenderer() Renderer {
	return &windowsRenderer{stop: make(chan struct{}), done: make(chan struct{}), hide: make(chan chan struct{}), present: make(chan chan struct{})}
}

func (r *windowsRenderer) Render(e Event) {
	r.mu.Lock()
	if r.stopped {
		r.mu.Unlock()
		return
	}
	if !e.Visible && time.Until(e.FinishedUntil) > 0 {
		e.Visible = true
	}
	r.event = e
	if e.Visible && e.CanStop {
		r.escapeEvent.Store(&e)
	} else {
		r.escapeEvent.Store(nil)
	}
	if e.Visible && !r.started {
		r.started = true
		go r.loop()
	}
	started := r.started
	r.mu.Unlock()
	if e.Visible && (e.PointerKind == "aim" || e.PointerKind == "origin" || e.PointerKind == "click") && started {
		ack := make(chan struct{})
		select {
		case r.present <- ack:
			select {
			case <-ack:
			case <-r.done:
			}
		case <-r.done:
		}
	}
	if !e.Visible && started {
		r.mu.Lock()
		r.pointerMotion = pointerMotion{}
		r.mu.Unlock()
		ack := make(chan struct{})
		select {
		case r.hide <- ack:
			select {
			case <-ack:
			case <-r.done:
			}
		case <-r.done:
		}
	}
}

func (r *windowsRenderer) Close() {
	r.mu.Lock()
	if r.stopped {
		r.mu.Unlock()
		return
	}
	r.stopped = true
	close(r.stop)
	started := r.started
	r.mu.Unlock()
	if started {
		<-r.done
	}
}

func (r *windowsRenderer) loop() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(r.done)
	if dpi := overlayUser.NewProc("SetThreadDpiAwarenessContext"); dpi.Find() == nil {
		previous, _, _ := dpi.Call(^uintptr(3))
		if previous != 0 {
			defer dpi.Call(previous)
		}
	}
	name, _ := syscall.UTF16PtrFromString("AtlasAgentActivityOverlay" + strconv.FormatUint(overlayClassID.Add(1), 10))
	instance, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("GetModuleHandleW").Call(0)
	class := overlayClass{Procedure: overlayCallback, Instance: instance, Name: name}
	class.Size = uint32(unsafe.Sizeof(class))
	atom, _, _ := overlayUser.NewProc("RegisterClassExW").Call(uintptr(unsafe.Pointer(&class)))
	if atom == 0 {
		return
	}
	defer overlayUser.NewProc("UnregisterClassW").Call(uintptr(unsafe.Pointer(name)), instance)
	r.mu.Lock()
	for range 5 {
		hwnd, _, _ := overlayUser.NewProc("CreateWindowExW").Call(0x080800a8, uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(name)), 0x80000000, 0, 0, 1, 1, 0, 0, instance, 0)
		if hwnd == 0 {
			break
		}
		r.windows = append(r.windows, hwnd)
		overlayWindows.Store(hwnd, true)
		overlayUser.NewProc("SetWindowDisplayAffinity").Call(hwnd, 0x11)
	}
	if len(r.windows) == 5 {
		r.bannerWindow, _, _ = overlayUser.NewProc("CreateWindowExW").Call(0x080800a8, uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(name)), 0x80000000, 0, 0, 1, 1, 0, 0, instance, 0)
		if r.bannerWindow != 0 {
			overlayWindows.Store(r.bannerWindow, true)
			overlayUser.NewProc("SetWindowDisplayAffinity").Call(r.bannerWindow, 0x11)
		}
	}
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		for _, w := range r.windows {
			overlayWindows.Delete(w)
			overlayUser.NewProc("DestroyWindow").Call(w)
		}
		r.windows = nil
		if r.bannerWindow != 0 {
			overlayWindows.Delete(r.bannerWindow)
			overlayUser.NewProc("DestroyWindow").Call(r.bannerWindow)
			r.bannerWindow = 0
		}
		r.banner.close()
		r.onStop = nil
		r.escapeEvent.Store(nil)
		r.cursor.restore()
		for i := range r.edges {
			r.edges[i].close()
		}
		if r.imageDC != 0 {
			overlayGDI.NewProc("SelectObject").Call(r.imageDC, r.oldBitmap)
			overlayGDI.NewProc("DeleteObject").Call(r.bitmap)
			overlayGDI.NewProc("DeleteDC").Call(r.imageDC)
		}
	}()
	if !r.createCursorSurface(1) {
		slog.Warn("Failed to create activity cursor surface")
		return
	}
	r.keyboardHook, _, _ = overlayUser.NewProc("SetWindowsHookExW").Call(13, escapeCallback, instance, 0)
	if r.keyboardHook != 0 {
		keyboardRenderers.Store(r, true)
		defer func() {
			keyboardRenderers.Delete(r)
			overlayUser.NewProc("UnhookWindowsHookEx").Call(r.keyboardHook)
		}()
	} else {
		slog.Warn("Desktop activity Escape shortcut unavailable")
	}
	r.epoch = time.Now()
	ticker := time.NewTicker(edgeFrame)
	defer ticker.Stop()
	for {
		select {
		case <-r.stop:
			return
		case ack := <-r.present:
			deadline := time.Now().Add(400 * time.Millisecond)
			for {
				pumpOverlayMessages()
				r.mu.Lock()
				r.draw()
				moving := r.event.PointerKind == "aim" && !r.event.ReducedMotion && (math.Abs(r.pointerMotion.x-float64(r.event.X)) > .5 || math.Abs(r.pointerMotion.y-float64(r.event.Y)) > .5)
				overlayDWM.NewProc("DwmFlush").Call()
				r.mu.Unlock()
				if !moving || time.Now().After(deadline) {
					break
				}
				select {
				case <-r.stop:
					close(ack)
					return
				case <-ticker.C:
				}
			}
			close(ack)
		case ack := <-r.hide:
			r.mu.Lock()
			for _, w := range r.windows {
				overlayUser.NewProc("ShowWindow").Call(w, 0)
			}
			overlayUser.NewProc("ShowWindow").Call(r.bannerWindow, 0)
			r.cursor.restore()
			overlayDWM.NewProc("DwmFlush").Call()
			r.mu.Unlock()
			close(ack)
		case <-ticker.C:
			pumpOverlayMessages()
			r.mu.Lock()
			r.draw()
			r.mu.Unlock()
		}
	}
}

// PumpOverlayMessages keeps physical Escape responsive during pointer travel.
func pumpOverlayMessages() {
	var message overlayMessage
	for {
		ok, _, _ := overlayUser.NewProc("PeekMessageW").Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0, 1)
		if ok == 0 {
			return
		}
		overlayUser.NewProc("TranslateMessage").Call(uintptr(unsafe.Pointer(&message)))
		overlayUser.NewProc("DispatchMessageW").Call(uintptr(unsafe.Pointer(&message)))
	}
}

func (r *windowsRenderer) draw() {
	r.cursor.tick()
	if !r.event.Persistent {
		r.cursor.restore()
	}
	if !r.event.FinishedUntil.IsZero() && time.Now().After(r.event.FinishedUntil) {
		r.event.Visible = false
		r.escapeEvent.Store(nil)
		for _, w := range r.windows {
			overlayUser.NewProc("ShowWindow").Call(w, 0)
		}
		overlayUser.NewProc("ShowWindow").Call(r.bannerWindow, 0)
	}
	if !r.event.Visible || len(r.windows) == 0 || r.imageDC == 0 {
		r.cursor.restore()
		return
	}
	var point overlayPoint
	overlayUser.NewProc("GetCursorPos").Call(uintptr(unsafe.Pointer(&point)))
	if r.event.Point {
		point = overlayPoint{int32(r.event.X), int32(r.event.Y)}
	}
	// Resolve DPI from the target point without moving the displayed surface.
	scale := float64(1)
	packed := uintptr(uint32(point.X)) | uintptr(uint32(point.Y))<<32
	monitor, _, _ := overlayUser.NewProc("MonitorFromPoint").Call(packed, 2)
	if dpi := overlayScaling.NewProc("GetDpiForMonitor"); dpi.Find() == nil {
		var xDPI, yDPI uint32
		result, _, _ := dpi.Call(monitor, 0, uintptr(unsafe.Pointer(&xDPI)), uintptr(unsafe.Pointer(&yDPI)))
		if result == 0 && xDPI != 0 {
			scale = max(1, min(4, float64(xDPI)/96))
		}
	}
	if scale != r.scale && !r.createCursorSurface(scale) {
		r.cursor.restore()
		return
	}
	now := time.Now()
	finishingMove := r.event.PointerKind == "move" && now.Sub(r.event.PointerAt) < pointerMoveDuration && int(point.X) == r.event.X && int(point.Y) == r.event.Y
	snap := r.event.ReducedMotion || (!r.event.Point && !finishingMove) || (r.event.PointerKind != "" && r.event.PointerKind != "move" && r.event.PointerKind != "aim")
	x, y := r.pointerMotion.position(float64(point.X), float64(point.Y), snap, now)
	press, held := pointerCue(r.event, now)
	source := r.cursorIdle
	if held {
		source = r.cursorHeld
		press = 0
	}
	for i := 0; i < len(source); i += 4 {
		for c := range 4 {
			channel := [4]int{2, 1, 0, 3}[c]
			r.cursorPixels[i+c] = byte(float64(source[i+channel])*(1-press) + float64(r.cursorPressed[i+channel])*press)
		}
	}
	hotspot := int32(math.Round(float64(cursorHotspot) * scale))
	position := overlayPoint{int32(math.Round(x)) - hotspot, int32(math.Round(y)) - hotspot}
	size := overlayPoint{int32(math.Round(float64(cursorSize) * scale)), int32(math.Round(float64(cursorSize) * scale))}
	origin := overlayPoint{}
	blend := uint32(0x01ff0000)
	ok, _, _ := overlayUser.NewProc("UpdateLayeredWindow").Call(r.windows[0], 0, uintptr(unsafe.Pointer(&position)), uintptr(unsafe.Pointer(&size)), r.imageDC, uintptr(unsafe.Pointer(&origin)), 0, uintptr(unsafe.Pointer(&blend)), 2)
	if ok == 0 {
		r.cursor.restore()
		return
	}
	if position != r.lastPointer {
		r.pointerFrames++
		r.lastPointer = position
	}
	overlayUser.NewProc("SetWindowPos").Call(r.windows[0], ^uintptr(0), 0, 0, 0, 0, 0x0053)
	r.drawEdges(monitor, scale)
	r.drawBanner(scale, time.Since(r.epoch))
	r.frames++
	if r.event.Persistent {
		if err := r.cursor.hide(); err != nil {
			slog.Warn("Failed to hide system cursor", "error", err)
		}
	}
}

func (r *windowsRenderer) createCursorSurface(scale float64) bool {
	dc, _, _ := overlayGDI.NewProc("CreateCompatibleDC").Call(0)
	if dc == 0 {
		return false
	}
	var info struct {
		Size                        uint32
		Width, Height               int32
		Planes, BitCount            uint16
		Compression, ImageSize      uint32
		XPels, YPels                int32
		ColorsUsed, ColorsImportant uint32
	}
	info.Size = uint32(unsafe.Sizeof(info))
	size := int32(math.Round(float64(cursorSize) * scale))
	info.Width, info.Height = size, -size
	info.Planes, info.BitCount = 1, 32
	var bits unsafe.Pointer
	bitmap, _, _ := overlayGDI.NewProc("CreateDIBSection").Call(dc, uintptr(unsafe.Pointer(&info)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bitmap == 0 || bits == nil {
		overlayGDI.NewProc("DeleteDC").Call(dc)
		return false
	}
	old, _, _ := overlayGDI.NewProc("SelectObject").Call(dc, bitmap)
	if r.imageDC != 0 {
		overlayGDI.NewProc("SelectObject").Call(r.imageDC, r.oldBitmap)
		overlayGDI.NewProc("DeleteObject").Call(r.bitmap)
		overlayGDI.NewProc("DeleteDC").Call(r.imageDC)
	}
	r.imageDC, r.bitmap, r.oldBitmap, r.scale = dc, bitmap, old, scale
	r.cursorIdle = cursorImageAtScale(scale).Pix
	r.cursorPressed = cursorStateImage(scale, true, false).Pix
	r.cursorHeld = cursorStateImage(scale, false, true).Pix
	r.cursorPixels = unsafe.Slice((*byte)(bits), len(r.cursorIdle))
	return true
}
