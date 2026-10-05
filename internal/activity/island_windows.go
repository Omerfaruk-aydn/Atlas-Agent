//go:build windows

package activity

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
	"unicode/utf16"
	"unsafe"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/clipboard"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/i18n"
)

var (
	overlayKernel = syscall.NewLazyDLL("kernel32.dll")
	overlayIMM    = syscall.NewLazyDLL("imm32.dll")
	// islandWindows maps the island and blur windows to their renderer.
	islandWindows sync.Map
	// islandCaptureVisible lets a live verification test record frames;
	// production surfaces are always excluded from screen capture.
	islandCaptureVisible bool
)

const (
	islandWidth     = 560.0
	islandPad       = 20.0
	islandRadius    = 18.0
	islandHeaderH   = 46.0
	islandFooterH   = 64.0
	islandRowGap    = 8.0
	islandMaxHeight = 680.0
	blurFadeIn      = 280 * time.Millisecond
	blurFadeOut     = 240 * time.Millisecond
	blurTint        = 0x38101014 // ABGR: a light dim under the blur.
)

// Palette in RGB; surfaces are composited as premultiplied BGRA.
var (
	islandBase      = [3]float64{23, 25, 32}
	islandInk       = [3]float64{235, 244, 250}
	islandFocusRing = [3]float64{132, 186, 255}
	islandAmber     = [3]float64{255, 196, 82}
)

// islandInput is one posted window message, handled on the render loop.
type islandInput struct {
	msg          uint32
	wparam       uintptr
	lparam       uintptr
	shift, ctrl  bool
	clientX, cly int32
}

type islandTextRun struct {
	text   string
	font   int
	x, y   float64 // Logical, island-local.
	w      float64
	color  [3]float64
	alpha  float64
	region int // 0 header, 1 body, 2 footer.
}

type islandBox struct {
	x, y, w, h, r float64
	fill          [3]float64
	fillAlpha     float64
	stroke        [3]float64
	strokeAlpha   float64
	strokeWidth   float64
	region        int
}

type islandHit struct {
	x, y, w, h float64
	region     int
	control    int // Index in form.controls(); -1 is Stop.
	text       bool
}

type islandLayout struct {
	key                   string
	scale                 float64
	w                     float64 // Logical width.
	headerH, footerH      float64
	bodyH, viewH          float64
	runs                  []islandTextRun
	boxes                 []islandBox
	hits                  []islandHit
	caretX, caretY, lineH float64
	caretOK               bool
	summary               string
}

// nativeIsland is the expanded banner. It is drawn on the banner window so
// the compact strip and the island are one surface.
type nativeIsland struct {
	form          *islandForm
	revision      uint64
	open          bool
	motion        islandMotion
	compactRect   islandRect
	expandedRect  islandRect
	layout        *islandLayout
	content       []byte // Premultiplied BGRA: header, full body, footer.
	contentW      int
	contentH      int
	surface       nativeEdgeSurface
	coverage      []byte
	scroll        float64
	hover, press  int
	version       uint64
	built         uint64
	caretOn       bool
	fonts         [6]uintptr
	fontScale     float64
	blurFrom      float64
	blurTo        float64
	blurAt        time.Time
	blurShown     bool
	blurByte      int
	closingAt     time.Time
	previous      uintptr
	previousPID   uint32
	monitor       uintptr
	area          overlayRect
	scale         float64
	interactive   bool
	surrogate     uint16
	frames        uint64
	frameStart    time.Time
	lastFrameGap  time.Duration
	maxFrameGap   time.Duration
	announcedText string
}

func (r *windowsRenderer) SetRespondHandler(respond func(uint64, PromptResponse) error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onRespond = respond
}

// islandCallback serves the banner/island window and the blur layer. Sent
// messages may arrive while the render loop holds r.mu, so they only use
// atomics; posted input is queued for the loop.
var islandCallback = syscall.NewCallback(func(hwnd uintptr, msg uint32, wparam, lparam uintptr) uintptr {
	value, ok := islandWindows.Load(hwnd)
	if !ok {
		result, _, _ := overlayUser.NewProc("DefWindowProcW").Call(hwnd, uintptr(msg), wparam, lparam)
		return result
	}
	r := value.(*windowsRenderer)
	blur := hwnd == r.blurWindow
	switch msg {
	case 0x84: // WM_NCHITTEST
		if blur || r.islandInteractive.Load() || r.stopClickable.Load() {
			return 1
		}
		return ^uintptr(0)
	case 0x21: // WM_MOUSEACTIVATE
		if blur {
			return 4 // MA_NOACTIVATEANDEAT: the blur swallows stray clicks.
		}
		if r.islandInteractive.Load() {
			return 1 // MA_ACTIVATE
		}
		return 3 // MA_NOACTIVATE: the compact Stop never steals focus.
	case 0x20: // WM_SETCURSOR
		id := uintptr(32512)
		switch r.islandCursor.Load() {
		case 1:
			id = 32649
		case 2:
			id = 32513
		}
		cursor, _, _ := overlayUser.NewProc("LoadCursorW").Call(0, id)
		overlayUser.NewProc("SetCursor").Call(cursor)
		return 1
	case 0x14: // WM_ERASEBKGND
		if blur {
			var rect overlayRect
			overlayUser.NewProc("GetClientRect").Call(hwnd, uintptr(unsafe.Pointer(&rect)))
			brush, _, _ := overlayGDI.NewProc("GetStockObject").Call(4)
			overlayUser.NewProc("FillRect").Call(wparam, uintptr(unsafe.Pointer(&rect)), brush)
		}
		return 1
	case 0x10: // WM_CLOSE belongs to the renderer lifecycle.
		return 0
	case 0x200, 0x201, 0x202, 0x203, 0x20a, 0x2a3, 0x100, 0x102, 0x104, 0x8:
		if blur {
			return 0
		}
		shift, _, _ := overlayUser.NewProc("GetKeyState").Call(0x10)
		ctrl, _, _ := overlayUser.NewProc("GetKeyState").Call(0x11)
		r.inputMu.Lock()
		if len(r.inbox) < 256 {
			r.inbox = append(r.inbox, islandInput{msg: msg, wparam: wparam, lparam: lparam, shift: int16(shift) < 0, ctrl: int16(ctrl) < 0, clientX: int32(int16(lparam)), cly: int32(int16(lparam >> 16))})
		}
		r.inputMu.Unlock()
		if msg == 0x104 && wparam != 0x73 { // Keep Alt+F4 inert; other system keys pass.
			break
		}
		if msg == 0x104 {
			return 0
		}
		if msg != 0x8 {
			return 0
		}
	}
	result, _, _ := overlayUser.NewProc("DefWindowProcW").Call(hwnd, uintptr(msg), wparam, lparam)
	return result
})

// createIslandWindows registers the island and blur window classes. The
// island replaces the banner window; the blur covers the controlled
// monitor beneath every Atlas surface.
func (r *windowsRenderer) createIslandWindows(instance uintptr) func() {
	id := overlayClassID.Add(1)
	islandName, _ := syscall.UTF16PtrFromString(fmt.Sprintf("AtlasAgentControlIsland%d", id))
	blurName, _ := syscall.UTF16PtrFromString(fmt.Sprintf("AtlasAgentControlBlur%d", id))
	brush, _, _ := overlayGDI.NewProc("GetStockObject").Call(4)
	arrow, _, _ := overlayUser.NewProc("LoadCursorW").Call(0, 32512)
	var registered []*uint16
	for _, spec := range []struct {
		name       *uint16
		background uintptr
	}{{islandName, 0}, {blurName, brush}} {
		class := overlayClass{Procedure: islandCallback, Instance: instance, Name: spec.name, Background: spec.background, Cursor: arrow}
		class.Size = uint32(unsafe.Sizeof(class))
		if atom, _, _ := overlayUser.NewProc("RegisterClassExW").Call(uintptr(unsafe.Pointer(&class))); atom != 0 {
			registered = append(registered, spec.name)
		}
	}
	cleanup := func() {
		for _, name := range registered {
			overlayUser.NewProc("UnregisterClassW").Call(uintptr(unsafe.Pointer(name)), instance)
		}
	}
	if len(registered) != 2 {
		return cleanup
	}
	title, _ := syscall.UTF16PtrFromString("Atlas")
	// Layered, topmost, tool, click-through and non-activating until a
	// request is displayed.
	r.bannerWindow, _, _ = overlayUser.NewProc("CreateWindowExW").Call(0x080800a8, uintptr(unsafe.Pointer(islandName)), uintptr(unsafe.Pointer(title)), 0x80000000, 0, 0, 1, 1, 0, 0, instance, 0)
	if r.bannerWindow != 0 {
		overlayWindows.Store(r.bannerWindow, true)
		islandWindows.Store(r.bannerWindow, r)
		r.excludeFromCapture(r.bannerWindow)
	}
	// The blur takes clicks without activating so a stray click cannot
	// reach the controlled application while a request waits.
	r.blurWindow, _, _ = overlayUser.NewProc("CreateWindowExW").Call(0x08080088, uintptr(unsafe.Pointer(blurName)), uintptr(unsafe.Pointer(title)), 0x80000000, 0, 0, 1, 1, 0, 0, instance, 0)
	if r.blurWindow != 0 {
		overlayWindows.Store(r.blurWindow, true)
		islandWindows.Store(r.blurWindow, r)
		r.blurLive = enableLiveBlur(r.blurWindow)
		// Display affinity only takes on a layered window after it entered
		// constant-alpha mode; UpdateLayeredWindow surfaces cannot take it
		// at all on Windows 10, which is why observations wait for answers.
		overlayUser.NewProc("SetLayeredWindowAttributes").Call(r.blurWindow, 0, 0, 2)
		r.excludeFromCapture(r.blurWindow)
	}
	return cleanup
}

func (r *windowsRenderer) excludeFromCapture(hwnd uintptr) {
	if !islandCaptureVisible {
		overlayUser.NewProc("SetWindowDisplayAffinity").Call(hwnd, 0x11)
	}
}

// enableLiveBlur asks DWM to blur what lies behind the window. Without it
// the layer is only a dim, and never claims to blur.
func enableLiveBlur(hwnd uintptr) bool {
	proc := overlayUser.NewProc("SetWindowCompositionAttribute")
	if proc.Find() != nil {
		return false
	}
	accent := struct{ State, Flags, Gradient, Animation uint32 }{State: 3, Flags: 2, Gradient: blurTint}
	data := struct {
		Attribute uint32
		Data      unsafe.Pointer
		Size      uintptr
	}{19, unsafe.Pointer(&accent), unsafe.Sizeof(accent)}
	ok, _, _ := proc.Call(hwnd, uintptr(unsafe.Pointer(&data)))
	return ok != 0
}

func (r *windowsRenderer) destroyIslandWindows() {
	r.island.restoreForeground(r.bannerWindow)
	for _, hwnd := range []uintptr{r.bannerWindow, r.blurWindow} {
		if hwnd != 0 {
			overlayWindows.Delete(hwnd)
			islandWindows.Delete(hwnd)
			overlayUser.NewProc("DestroyWindow").Call(hwnd)
		}
	}
	r.bannerWindow, r.blurWindow = 0, 0
	r.island.release()
	r.islandInteractive.Store(false)
	r.stopClickable.Store(false)
}

func (n *nativeIsland) release() {
	n.surface.close()
	for i, font := range n.fonts {
		if font != 0 {
			overlayGDI.NewProc("DeleteObject").Call(font)
			n.fonts[i] = 0
		}
	}
	*n = nativeIsland{}
}

// islandWanted reports whether the run displays a request on this surface.
func (r *windowsRenderer) islandWanted() bool {
	e := r.event
	return e.Visible && e.Persistent && e.Prompt != nil && e.Prompt.Revision != 0 && r.bannerWindow != 0
}

// islandBusy reports whether the island owns the banner window this frame.
func (r *windowsRenderer) islandBusy(now time.Time) bool {
	if r.island.open {
		return true
	}
	if !r.island.motion.ready {
		return false
	}
	if !r.island.closingAt.IsZero() && now.Sub(r.island.closingAt) > islandCloseDeadline+500*time.Millisecond {
		return false
	}
	_, settled := r.island.motion.at(now)
	return !settled || r.blurAlpha(now) > 0
}

// resetIsland tears the island down at once: cancellation, hiding and
// shutdown never animate and never leave blur or focus behind.
func (r *windowsRenderer) resetIsland() {
	n := &r.island
	r.setInteractive(false)
	n.restoreForeground(r.bannerWindow)
	if r.blurWindow != 0 {
		overlayUser.NewProc("ShowWindow").Call(r.blurWindow, 0)
		overlayUser.NewProc("SetLayeredWindowAttributes").Call(r.blurWindow, 0, 0, 2)
	}
	surface, fonts := n.surface, n.fonts
	*n = nativeIsland{surface: surface, fonts: fonts, fontScale: n.fontScale}
	r.inputMu.Lock()
	r.inbox = nil
	r.inputMu.Unlock()
	r.stopClickable.Store(false)
	if r.bannerWindow != 0 {
		r.setTransparent(true)
	}
}

func (r *windowsRenderer) setTransparent(transparent bool) {
	style, _, _ := overlayUser.NewProc("GetWindowLongPtrW").Call(r.bannerWindow, ^uintptr(19))
	next := style | 0x20
	if !transparent {
		next = style &^ 0x20
	}
	if next != style {
		overlayUser.NewProc("SetWindowLongPtrW").Call(r.bannerWindow, ^uintptr(19), next)
	}
}

// setInteractive makes the island a normal, focusable window only while a
// request is displayed.
func (r *windowsRenderer) setInteractive(on bool) {
	if r.bannerWindow == 0 || r.island.interactive == on {
		return
	}
	r.island.interactive = on
	r.islandInteractive.Store(on)
	style, _, _ := overlayUser.NewProc("GetWindowLongPtrW").Call(r.bannerWindow, ^uintptr(19))
	if on {
		style &^= 0x20 | 0x08000000
	} else {
		style |= 0x20 | 0x08000000
	}
	overlayUser.NewProc("SetWindowLongPtrW").Call(r.bannerWindow, ^uintptr(19), style)
	if !on {
		r.islandCursor.Store(0)
	}
}

// takeForeground focuses the island so keyboard and IME input reach it.
// The previous foreground window is remembered with its process.
func (n *nativeIsland) takeForeground(island uintptr) {
	foreground, _, _ := overlayUser.NewProc("GetForegroundWindow").Call()
	if foreground != 0 && foreground != island && !IsOverlayWindow(foreground) {
		n.previous = foreground
		overlayUser.NewProc("GetWindowThreadProcessId").Call(foreground, uintptr(unsafe.Pointer(&n.previousPID)))
	}
	current, _, _ := overlayKernel.NewProc("GetCurrentThreadId").Call()
	other, _, _ := overlayUser.NewProc("GetWindowThreadProcessId").Call(foreground, 0)
	attached := false
	if other != 0 && other != current {
		ok, _, _ := overlayUser.NewProc("AttachThreadInput").Call(current, other, 1)
		attached = ok != 0
	}
	overlayUser.NewProc("BringWindowToTop").Call(island)
	overlayUser.NewProc("SetForegroundWindow").Call(island)
	overlayUser.NewProc("SetFocus").Call(island)
	if attached {
		overlayUser.NewProc("AttachThreadInput").Call(current, other, 0)
	}
}

// restoreForeground returns focus only to the same, still-open window of
// the same process, and only if the island still holds focus.
func (n *nativeIsland) restoreForeground(island uintptr) {
	previous, pid := n.previous, n.previousPID
	n.previous, n.previousPID = 0, 0
	if previous == 0 || island == 0 {
		return
	}
	foreground, _, _ := overlayUser.NewProc("GetForegroundWindow").Call()
	if foreground != island {
		return
	}
	valid, _, _ := overlayUser.NewProc("IsWindow").Call(previous)
	visible, _, _ := overlayUser.NewProc("IsWindowVisible").Call(previous)
	var owner uint32
	overlayUser.NewProc("GetWindowThreadProcessId").Call(previous, uintptr(unsafe.Pointer(&owner)))
	if valid == 0 || visible == 0 || owner != pid {
		return
	}
	overlayUser.NewProc("SetForegroundWindow").Call(previous)
}

// blurAlpha is the blur layer's opacity at now, eased over time.
func (r *windowsRenderer) blurAlpha(now time.Time) float64 {
	n := &r.island
	if n.blurAt.IsZero() {
		return n.blurTo
	}
	duration := blurFadeOut
	if n.blurTo > n.blurFrom {
		duration = blurFadeIn
	}
	if r.event.ReducedMotion {
		duration = islandReducedFade
	}
	t := min(1, float64(now.Sub(n.blurAt))/float64(duration))
	eased := 1 - (1-t)*(1-t)*(1-t)
	return n.blurFrom + (n.blurTo-n.blurFrom)*eased
}

func (r *windowsRenderer) fadeBlur(to float64, now time.Time) {
	n := &r.island
	if n.blurTo == to {
		return
	}
	n.blurFrom, n.blurTo, n.blurAt = r.blurAlpha(now), to, now
}

// drawBlur keeps the blur on the controlled monitor, under every Atlas
// surface and above everything else.
func (r *windowsRenderer) drawBlur(now time.Time) {
	n := &r.island
	if r.blurWindow == 0 {
		return
	}
	alpha := r.blurAlpha(now)
	if alpha <= 0.001 {
		if n.blurShown {
			overlayUser.NewProc("ShowWindow").Call(r.blurWindow, 0)
			n.blurShown = false
		}
		return
	}
	area := n.area
	if !n.blurShown {
		overlayUser.NewProc("SetWindowPos").Call(r.blurWindow, ^uintptr(0), uintptr(area.Left), uintptr(area.Top), uintptr(area.Right-area.Left), uintptr(area.Bottom-area.Top), 0x0050)
		n.blurShown = true
		n.blurByte = -1
	}
	// Every attribute change makes DWM resample the blur, so an unchanged
	// opacity is never reapplied: doing so each frame makes it flicker.
	if b := int(math.Round(alpha * 255)); b != n.blurByte {
		overlayUser.NewProc("SetLayeredWindowAttributes").Call(r.blurWindow, 0, uintptr(b), 2)
		n.blurByte = b
	}
	// The edges hang in a chain beneath the island, so the blur goes
	// directly beneath the last one and never covers the island's text.
	placeWindow(r.blurWindow, r.windows[len(r.windows)-1], 0x0013)
}

// monitorArea resolves a monitor's full rectangle and scale.
func monitorArea(monitor uintptr) (overlayRect, float64, bool) {
	var info struct {
		Size          uint32
		Monitor, Work overlayRect
		Flags         uint32
	}
	info.Size = uint32(unsafe.Sizeof(info))
	if ok, _, _ := overlayUser.NewProc("GetMonitorInfoW").Call(monitor, uintptr(unsafe.Pointer(&info))); ok == 0 {
		return overlayRect{}, 1, false
	}
	scale := 1.0
	if dpi := overlayScaling.NewProc("GetDpiForMonitor"); dpi.Find() == nil {
		var x, y uint32
		if result, _, _ := dpi.Call(monitor, 0, uintptr(unsafe.Pointer(&x)), uintptr(unsafe.Pointer(&y))); result == 0 && x != 0 {
			scale = max(1, min(4, float64(x)/96))
		}
	}
	return info.Monitor, scale, true
}

// drawIsland renders one frame of the island: opening, waiting, closing.
// Called with r.mu held from draw.
func (r *windowsRenderer) drawIsland(monitor uintptr, now time.Time) {
	n := &r.island
	wanted := r.islandWanted()
	area, scale, ok := monitorArea(monitor)
	if !ok {
		return
	}
	moved := n.monitor != 0 && (n.monitor != monitor || n.scale != scale || n.area != area)
	if n.monitor != monitor || n.scale != scale || n.area != area {
		// Monitor or DPI changed: re-layout at the new size.
		n.monitor, n.scale, n.area = monitor, scale, area
		n.layout = nil
	}
	r.edgeArea = area
	if wanted && (n.form == nil || n.revision != r.event.Prompt.Revision) {
		first := n.form == nil
		n.form = newIslandForm(r.event.Prompt, now)
		n.revision = r.event.Prompt.Revision
		n.scroll, n.hover, n.press = 0, -2, -2
		n.layout = nil
		n.version++
		if first {
			r.drainInput()
		}
	}
	// Compact geometry comes from the real banner so the morph starts and
	// ends on exactly what was on screen.
	r.prepareBannerText(now)
	if !r.banner.prepare(area, scale, r.bannerEvent()) {
		return
	}
	cw, ch := float64(r.banner.surface.model.width), float64(r.banner.surface.model.height)
	n.compactRect = islandRect{CX: float64(area.Left+area.Right) / 2, Top: float64(area.Top) + math.Round(20*scale), W: cw, H: ch, R: bannerRadius * scale}
	if n.form != nil {
		r.layoutIsland(now)
	}
	if wanted && n.layout == nil && !n.open {
		// Without fonts the request stays answerable in the terminal; the
		// strip keeps showing that the run is waiting.
		r.drawBanner(scale, time.Since(r.epoch))
		return
	}
	if n.layout != nil {
		viewH := n.layout.headerH + n.layout.viewH + n.layout.footerH
		n.expandedRect = islandRect{CX: n.compactRect.CX, Top: n.compactRect.Top, W: math.Round(n.layout.w * scale), H: math.Round(viewH * scale), R: islandRadius * scale}
	}
	if moved && n.motion.ready {
		// Follow the controlled monitor at once rather than sliding across.
		if n.open {
			n.motion.snap(n.expandedRect)
		} else {
			n.motion.snap(n.compactRect)
		}
	}
	if wanted && !n.open {
		n.open = true
		if !n.motion.ready {
			n.motion.snap(n.compactRect)
		}
		n.motion.retarget(n.expandedRect, now, true, r.event.ReducedMotion)
		r.fadeBlur(1, now)
		r.setInteractive(true)
		r.cursor.show()
		n.takeForeground(r.bannerWindow)
	} else if !wanted && n.open {
		n.open = false
		n.closingAt = now
		r.setInteractive(false)
		n.restoreForeground(r.bannerWindow)
		n.motion.retarget(n.compactRect, now, false, r.event.ReducedMotion)
		r.fadeBlur(0, now)
	} else if n.open && n.motion.to != n.expandedRect {
		// A queued request or a re-layout resizes from the current shape.
		n.motion.retarget(n.expandedRect, now, true, r.event.ReducedMotion)
	} else if !n.open && n.motion.to != n.compactRect {
		n.motion.retarget(n.compactRect, now, false, r.event.ReducedMotion)
	}
	if n.open {
		r.handleIslandInput(now)
	} else {
		r.drainInput()
	}
	r.drawBlur(now)
	geometry, settled := n.motion.at(now)
	progress := islandProgress(geometry, n.compactRect, n.expandedRect)
	compactAlpha, expandedAlpha := islandOpacities(progress)
	if r.event.ReducedMotion && !settled {
		// Reduced motion: a plain cross-fade over the target shape.
		t := float64(now.Sub(n.motion.start)) / float64(max(1, n.motion.duration))
		if n.open {
			compactAlpha, expandedAlpha = 1-t, t
		} else {
			compactAlpha, expandedAlpha = t, 1-t
		}
	}
	if !n.open {
		expandedAlpha = min(expandedAlpha, 1-smoothstep(0, .35, float64(now.Sub(n.motion.start))/float64(max(1, n.motion.duration))))
	}
	r.paintIsland(geometry, compactAlpha, expandedAlpha, now, settled && n.open)
	// A closing island always finishes: whatever happens to motion or
	// layout, blur and input never outlive the request by more than this.
	overdue := !n.open && !n.closingAt.IsZero() && now.Sub(n.closingAt) > islandCloseDeadline
	if !n.open && (overdue || (settled && r.blurAlpha(now) <= 0.001)) {
		if overdue {
			r.resetIsland()
			return
		}
		// Fully compact again: hand the window back to the banner.
		n.form, n.layout, n.revision, n.motion = nil, nil, 0, islandMotion{}
		n.blurAt, n.closingAt = time.Time{}, time.Time{}
		n.blurFrom, n.blurTo = 0, 0
	}
}

// islandCloseDeadline bounds the collapse whatever its motion does.
const islandCloseDeadline = 1200 * time.Millisecond

func (r *windowsRenderer) drainInput() {
	r.inputMu.Lock()
	r.inbox = nil
	r.inputMu.Unlock()
}

// bannerEvent is the event used for the compact strip's own layout.
func (r *windowsRenderer) bannerEvent() Event {
	e := r.event
	e.CanStop = e.CanStop && r.keyboardHook != 0
	return e
}

// prepareBannerText chooses the caption and timer for the compact strip.
func (r *windowsRenderer) prepareBannerText(now time.Time) {
	caption := r.status.text(r.event, now)
	timer := ""
	if !r.event.RunStarted.IsZero() {
		timer = FormatElapsed(r.event.Elapsed(now))
	}
	if r.bannerRun != r.event.RunStarted {
		r.bannerRun, r.banner.floor = r.event.RunStarted, 0
	}
	r.banner.wantCaption, r.banner.wantTimer = caption, timer
}

// islandFontSpecs are size in logical pixels and weight.
var islandFontSpecs = [6][2]int32{{17, 600}, {13, 400}, {14, 500}, {12, 400}, {12, 600}, {13, 600}}

const (
	fontTitle = iota
	fontBody
	fontLabel
	fontSmall
	fontKicker
	fontButton
)

func (n *nativeIsland) ensureFonts(scale float64) bool {
	if n.fontScale == scale && n.fonts[0] != 0 {
		return true
	}
	for i, font := range n.fonts {
		if font != 0 {
			overlayGDI.NewProc("DeleteObject").Call(font)
			n.fonts[i] = 0
		}
	}
	face, _ := syscall.UTF16PtrFromString("Segoe UI")
	for i, spec := range islandFontSpecs {
		height := -int32(math.Round(float64(spec[0]) * scale))
		n.fonts[i], _, _ = overlayGDI.NewProc("CreateFontW").Call(uintptr(height), 0, 0, 0, uintptr(spec[1]), 0, 0, 0, 1, 0, 0, 4, 0, uintptr(unsafe.Pointer(face)))
		if n.fonts[i] == 0 {
			return false
		}
	}
	n.fontScale = scale
	return true
}

// islandMeasurer measures text with the island's fonts.
type islandMeasurer struct {
	dc    uintptr
	n     *nativeIsland
	scale float64
}

func (m islandMeasurer) width(font int, text string) float64 {
	if text == "" {
		return 0
	}
	overlayGDI.NewProc("SelectObject").Call(m.dc, m.n.fonts[font])
	encoded := utf16.Encode([]rune(text))
	var size overlayPoint
	overlayGDI.NewProc("GetTextExtentPoint32W").Call(m.dc, uintptr(unsafe.Pointer(&encoded[0])), uintptr(len(encoded)), uintptr(unsafe.Pointer(&size)))
	return float64(size.X) / m.scale
}

func (m islandMeasurer) lineHeight(font int) float64 {
	return math.Ceil(float64(islandFontSpecs[font][0]) * 1.42)
}

// wrap breaks text into lines no wider than width; long words break by
// rune so nothing is cut off.
func (m islandMeasurer) wrap(font int, text string, width float64) []string {
	var lines []string
	for _, paragraph := range strings.Split(text, "\n") {
		words := strings.Fields(paragraph)
		if len(words) == 0 {
			lines = append(lines, "")
			continue
		}
		line := ""
		for _, word := range words {
			candidate := word
			if line != "" {
				candidate = line + " " + word
			}
			if m.width(font, candidate) <= width {
				line = candidate
				continue
			}
			if line != "" {
				lines = append(lines, line)
				line = ""
			}
			for m.width(font, word) > width {
				runes := []rune(word)
				cut := len(runes) - 1
				for cut > 1 && m.width(font, string(runes[:cut])) > width {
					cut--
				}
				lines = append(lines, string(runes[:cut]))
				word = string(runes[cut:])
			}
			line = word
		}
		lines = append(lines, line)
	}
	return lines
}

// plainText removes lightweight markdown markers from model-authored text.
func plainText(s string) string {
	return strings.NewReplacer("**", "", "__", "", "`", "").Replace(s)
}

func capitalized(s string) string {
	for i, r := range s {
		return string(unicode.ToUpper(r)) + s[i+len(string(r)):]
	}
	return s
}

// layoutIsland computes the expanded island from the request and form.
func (r *windowsRenderer) layoutIsland(now time.Time) {
	n := &r.island
	f := n.form
	e := r.event
	language := e.Language
	tr := func(s string) string { return i18n.Text(language, s) }
	runTime := FormatElapsed(e.Elapsed(now))
	waited := FormatElapsed(now.Sub(f.prompt.Since))
	key := fmt.Sprintf("%d|%d|%.3f|%d|%s|%s|%s|%d|%d|%d|%t", n.revision, n.version, n.scale, n.area.Right-n.area.Left, language, runTime, waited, n.hover, f.focus, f.page, f.submitted)
	if n.layout != nil && n.layout.key == key {
		return
	}
	if !n.ensureFonts(n.scale) {
		n.layout = nil
		return
	}
	dc, _, _ := overlayGDI.NewProc("CreateCompatibleDC").Call(0)
	if dc == 0 {
		return
	}
	defer overlayGDI.NewProc("DeleteDC").Call(dc)
	m := islandMeasurer{dc: dc, n: n, scale: n.scale}
	areaW := float64(n.area.Right-n.area.Left) / n.scale
	areaH := float64(n.area.Bottom-n.area.Top) / n.scale
	l := &islandLayout{key: key, scale: n.scale, w: min(islandWidth, areaW-32), headerH: islandHeaderH, footerH: islandFooterH}
	inner := l.w - 2*islandPad
	controls := f.controls()
	controlIndex := func(match func(islandControl) bool) int {
		for i, c := range controls {
			if match(c) {
				return i
			}
		}
		return -1
	}
	add := func(region int, font int, text string, x, y, w float64, color [3]float64, alpha float64) {
		l.runs = append(l.runs, islandTextRun{text: text, font: font, x: x, y: y, w: w, color: color, alpha: alpha, region: region})
	}
	paragraph := func(region, font int, text string, x, y, w float64, color [3]float64, alpha float64) float64 {
		lh := m.lineHeight(font)
		lines := m.wrap(font, text, w)
		for i, line := range lines {
			add(region, font, line, x, y+float64(i)*lh, w, color, alpha)
		}
		return float64(len(lines)) * lh
	}

	// Header: what is being asked, the stop control and elapsed time.
	kicker := tr("Permission needed")
	if f.prompt.Kind == KindQuestion {
		kicker = tr("Question")
		if len(f.prompt.Questions) > 1 {
			kicker = fmt.Sprintf(tr("Question %d of %d"), f.page+1, len(f.prompt.Questions))
		}
	}
	l.boxes = append(l.boxes, islandBox{x: islandPad, y: 19, w: 8, h: 8, r: 4, fill: islandAmber, fillAlpha: .95})
	add(0, fontKicker, kicker, islandPad+16, 14, 220, islandInk, .78)
	stopLabel := tr("Stop")
	stopW := m.width(fontButton, stopLabel) + m.width(fontSmall, "Esc") + 34
	stopX := l.w - islandPad - stopW
	stopHover := n.hover == -1
	l.boxes = append(l.boxes, islandBox{x: stopX, y: 9, w: stopW, h: 28, r: 14, fill: islandInk, fillAlpha: map[bool]float64{true: .16, false: .08}[stopHover], stroke: islandInk, strokeAlpha: .14, strokeWidth: 1})
	add(0, fontSmall, "Esc", stopX+12, 14, 30, islandInk, .55)
	add(0, fontButton, stopLabel, stopX+18+m.width(fontSmall, "Esc"), 13, stopW, islandInk, .95)
	l.hits = append(l.hits, islandHit{x: stopX, y: 9, w: stopW, h: 28, region: 0, control: -1})
	timing := fmt.Sprintf(tr("Run time %s"), runTime) + "  ·  " + fmt.Sprintf(tr("Waiting %s"), waited)
	timingW := m.width(fontSmall, timing)
	if islandPad+16+m.width(fontKicker, kicker)+24+timingW < stopX-12 {
		add(0, fontSmall, timing, stopX-12-timingW, 15, timingW+4, islandInk, .5)
	}

	// Body: the request itself.
	y := 4.0
	if f.prompt.Kind == KindPermission {
		p := f.prompt.Permission
		title := p.Tool
		if p.Action != "" {
			title += " · " + p.Action
		}
		y += paragraph(1, fontTitle, title, islandPad, y, inner, islandInk, 1) + 6
		if p.Detail != "" {
			y += paragraph(1, fontBody, p.Detail, islandPad, y, inner, islandInk, .72) + 6
		}
		if p.Hidden {
			y += paragraph(1, fontSmall, tr("Typed content is hidden"), islandPad, y, inner, islandInk, .55) + 6
		}
		if p.Scope != "" {
			label := tr("Scope") + ": "
			lw := m.width(fontSmall, label)
			add(1, fontSmall, label, islandPad, y, lw, islandInk, .5)
			y += paragraph(1, fontSmall, p.Scope, islandPad+lw, y, inner-lw, islandInk, .72)
		}
		y += 8
	} else {
		q := f.question()
		y += paragraph(1, fontTitle, plainText(q.Text), islandPad, y, inner, islandInk, 1) + 4
		if q.Description != "" {
			y += paragraph(1, fontBody, plainText(q.Description), islandPad, y, inner, islandInk, .68) + 8
		}
		hint := ""
		switch q.Type {
		case QuestionSingleChoice, QuestionYesNo:
			hint = tr("Choose one")
		case QuestionMultiChoice:
			hint = tr("Select all that apply")
		}
		if hint != "" {
			y += paragraph(1, fontSmall, hint, islandPad, y, inner, islandInk, .5) + 6
		}
		for _, c := range f.choices() {
			index := controlIndex(func(k islandControl) bool { return k.kind == controlChoice && k.choice == c.ID })
			label := plainText(c.Label)
			if q.Type == QuestionYesNo {
				label = capitalized(tr(c.Label))
			}
			textX, textW := islandPad+44, inner-56
			labelLines := m.wrap(fontLabel, label, textW)
			descLines := []string(nil)
			if c.Description != "" {
				descLines = m.wrap(fontSmall, plainText(c.Description), textW)
			}
			rowH := max(44, 22+float64(len(labelLines))*m.lineHeight(fontLabel)+float64(len(descLines))*m.lineHeight(fontSmall))
			selected := f.isSelected(c.ID)
			focused := index == f.focus
			hovered := index == n.hover
			fill := .04
			if hovered {
				fill = .08
			}
			if selected {
				fill = .12
			}
			strokeAlpha := .1
			if selected {
				strokeAlpha = .5
			}
			l.boxes = append(l.boxes, islandBox{x: islandPad, y: y, w: inner, h: rowH, r: 10, fill: islandInk, fillAlpha: fill, stroke: islandInk, strokeAlpha: strokeAlpha, strokeWidth: 1, region: 1})
			if focused {
				l.boxes = append(l.boxes, islandBox{x: islandPad - 3, y: y - 3, w: inner + 6, h: rowH + 6, r: 13, stroke: islandFocusRing, strokeAlpha: .95, strokeWidth: 2, region: 1})
			}
			// Radio for one answer, checkbox for many.
			gy := y + 13
			if q.Type == QuestionMultiChoice {
				l.boxes = append(l.boxes, islandBox{x: islandPad + 14, y: gy, w: 18, h: 18, r: 5, fill: islandInk, fillAlpha: map[bool]float64{true: .95, false: 0}[selected], stroke: islandInk, strokeAlpha: .6, strokeWidth: 1.5, region: 1})
				if selected {
					add(1, fontKicker, "✓", islandPad+17.5, gy-1, 14, islandBase, 1)
				}
			} else {
				l.boxes = append(l.boxes, islandBox{x: islandPad + 14, y: gy, w: 18, h: 18, r: 9, stroke: islandInk, strokeAlpha: .6, strokeWidth: 1.5, region: 1})
				if selected {
					l.boxes = append(l.boxes, islandBox{x: islandPad + 18.5, y: gy + 4.5, w: 9, h: 9, r: 4.5, fill: islandInk, fillAlpha: 1, region: 1})
				}
			}
			ly := y + 11
			for i, line := range labelLines {
				add(1, fontLabel, line, textX, ly+float64(i)*m.lineHeight(fontLabel), textW, islandInk, .96)
			}
			ly += float64(len(labelLines)) * m.lineHeight(fontLabel)
			for i, line := range descLines {
				add(1, fontSmall, line, textX, ly+float64(i)*m.lineHeight(fontSmall), textW, islandInk, .58)
			}
			l.hits = append(l.hits, islandHit{x: islandPad, y: y, w: inner, h: rowH, region: 1, control: index})
			y += rowH + islandRowGap
		}
		if q.Type == QuestionFreeText {
			index := controlIndex(func(k islandControl) bool { return k.kind == controlText })
			text := f.text[f.page]
			lines := m.wrap(fontLabel, string(text), inner-28)
			lh := m.lineHeight(fontLabel)
			fieldH := max(3, float64(len(lines)))*lh + 22
			focused := index == f.focus
			l.boxes = append(l.boxes, islandBox{x: islandPad, y: y, w: inner, h: fieldH, r: 10, fill: islandInk, fillAlpha: .05, stroke: islandInk, strokeAlpha: map[bool]float64{true: .45, false: .16}[focused], strokeWidth: 1, region: 1})
			if focused {
				l.boxes = append(l.boxes, islandBox{x: islandPad - 3, y: y - 3, w: inner + 6, h: fieldH + 6, r: 13, stroke: islandFocusRing, strokeAlpha: .95, strokeWidth: 2, region: 1})
			}
			if len(text) == 0 {
				add(1, fontLabel, tr("Type your answer…"), islandPad+14, y+11, inner-28, islandInk, .38)
			}
			for i, line := range lines {
				add(1, fontLabel, line, islandPad+14, y+11+float64(i)*lh, inner-28, islandInk, .96)
			}
			// Caret position follows the same wrapping as the text.
			caret := f.caret[f.page]
			before := m.wrap(fontLabel, string(text[:caret]), inner-28)
			last := before[len(before)-1]
			if strings.HasSuffix(string(text[:caret]), " ") && !strings.HasSuffix(last, " ") {
				last += " "
			}
			l.caretX, l.caretY, l.lineH, l.caretOK = islandPad+14+m.width(fontLabel, last), y+11+float64(len(before)-1)*lh, lh, focused
			l.hits = append(l.hits, islandHit{x: islandPad, y: y, w: inner, h: fieldH, region: 1, control: index, text: true})
			y += fieldH + islandRowGap
		}
	}
	l.bodyH = y + 4
	maxView := min(islandMaxHeight, areaH*.78) - l.headerH - l.footerH
	l.viewH = min(l.bodyH, max(80, maxView))

	// Footer: queue, then actions with the primary action last.
	if f.prompt.Queued > 0 {
		add(2, fontSmall, fmt.Sprintf(tr("+%d more waiting"), f.prompt.Queued), islandPad, 24, 160, islandInk, .55)
	}
	bx := l.w - islandPad
	for i := len(controls) - 1; i >= 0; i-- {
		c := controls[i]
		label := ""
		primary := false
		switch c.kind {
		case controlBack:
			label = tr("Back")
		case controlNext:
			label, primary = tr("Next"), true
		case controlSubmit:
			label, primary = tr("Submit"), true
			if f.submitted {
				label = tr("Sending…")
			}
		case controlDecision:
			switch c.decision {
			case DecisionAllowOnce:
				label, primary = tr("Allow Once"), true
			case DecisionAllowSession:
				label = tr("Allow for Session")
			case DecisionDeny:
				label = tr("Deny")
			}
		default:
			continue
		}
		w := m.width(fontButton, label) + 32
		bx -= w
		enabled := f.enabled(c)
		alpha := 1.0
		if !enabled {
			alpha = .4
		}
		box := islandBox{x: bx, y: 14, w: w, h: 36, r: 18, region: 2}
		color := islandInk
		if primary {
			box.fill, box.fillAlpha = islandInk, .95*alpha
			color = islandBase
		} else {
			box.fill, box.fillAlpha, box.stroke, box.strokeAlpha, box.strokeWidth = islandInk, .07*alpha, islandInk, .2*alpha, 1
		}
		if i == n.hover && enabled {
			box.fillAlpha = min(1, box.fillAlpha+.08)
		}
		l.boxes = append(l.boxes, box)
		if i == f.focus {
			l.boxes = append(l.boxes, islandBox{x: bx - 3, y: 11, w: w + 6, h: 42, r: 21, stroke: islandFocusRing, strokeAlpha: .95, strokeWidth: 2, region: 2})
		}
		add(2, fontButton, label, bx+16, 22, w-32, color, alpha)
		l.hits = append(l.hits, islandHit{x: bx, y: 14, w: w, h: 36, region: 2, control: i})
		bx -= 8
	}
	if e.Language == "ar" {
		mirrorLayout(l)
	}
	l.summary = islandSummary(f, kicker, tr)
	n.layout = l
	n.layoutContent()
}

// mirrorLayout reflects positions for right-to-left languages.
func mirrorLayout(l *islandLayout) {
	for i := range l.runs {
		l.runs[i].x = l.w - l.runs[i].x - l.runs[i].w
	}
	for i := range l.boxes {
		l.boxes[i].x = l.w - l.boxes[i].x - l.boxes[i].w
	}
	for i := range l.hits {
		l.hits[i].x = l.w - l.hits[i].x - l.hits[i].w
	}
	l.caretX = l.w - l.caretX
}

// islandSummary is the accessible name: the request, then the focused
// control and whether it is selected.
func islandSummary(f *islandForm, kicker string, tr func(string) string) string {
	parts := []string{"Atlas", kicker}
	if q := f.question(); q != nil {
		parts = append(parts, plainText(q.Text))
	} else if f.prompt.Kind == KindPermission {
		parts = append(parts, f.prompt.Permission.Tool+" "+f.prompt.Permission.Action, f.prompt.Permission.Detail)
	}
	if c, ok := f.focused(); ok {
		switch c.kind {
		case controlChoice:
			for _, choice := range f.choices() {
				if choice.ID == c.choice {
					label := choice.Label
					if f.question().Type == QuestionYesNo {
						label = capitalized(tr(label))
					}
					state := ""
					if f.isSelected(c.choice) {
						state = " ✓"
					}
					parts = append(parts, label+state)
				}
			}
		case controlText:
			parts = append(parts, tr("Type your answer…"))
		case controlSubmit:
			parts = append(parts, tr("Submit"))
		case controlNext:
			parts = append(parts, tr("Next"))
		case controlBack:
			parts = append(parts, tr("Back"))
		case controlDecision:
			parts = append(parts, map[PermissionDecision]string{DecisionAllowOnce: tr("Allow Once"), DecisionAllowSession: tr("Allow for Session"), DecisionDeny: tr("Deny")}[c.decision])
		}
	}
	return strings.Join(parts, " — ")
}

// layoutContent renders the expanded content once per layout change into a
// premultiplied layer; frames only composite it.
func (n *nativeIsland) layoutContent() {
	l := n.layout
	s := n.scale
	w := int(math.Ceil(l.w * s))
	h := int(math.Ceil((l.headerH + l.bodyH + l.footerH) * s))
	if cap(n.content) < w*h*4 {
		n.content = make([]byte, w*h*4)
	}
	n.content = n.content[:w*h*4]
	clear(n.content)
	n.contentW, n.contentH = w, h
	offset := func(region int) float64 {
		switch region {
		case 1:
			return l.headerH
		case 2:
			return l.headerH + l.bodyH
		}
		return 0
	}
	// Hairlines between header, body and footer.
	for _, line := range []float64{l.headerH, l.headerH + l.bodyH} {
		y := int(math.Round(line * s))
		for x := int(islandPad * s); x < w-int(islandPad*s); x++ {
			if y >= 0 && y < h {
				blendPixel(n.content, (y*w+x)*4, islandInk, .07)
			}
		}
	}
	for _, b := range l.boxes {
		paintIslandBox(n.content, w, h, s, b, offset(b.region))
	}
	if len(l.runs) == 0 {
		return
	}
	var mask nativeEdgeSurface
	if !mask.create(&edgePixels{width: w, height: int(math.Ceil(40 * s))}) {
		return
	}
	defer mask.close()
	overlayGDI.NewProc("SetBkMode").Call(mask.dc, 1)
	overlayGDI.NewProc("SetTextColor").Call(mask.dc, 0xffffff)
	for _, run := range l.runs {
		if run.text == "" {
			continue
		}
		clear(mask.pixels)
		overlayGDI.NewProc("SelectObject").Call(mask.dc, n.fonts[run.font])
		encoded := utf16.Encode([]rune(run.text))
		rect := overlayRect{int32(math.Round(run.x * s)), 0, int32(math.Round((run.x + run.w + 2) * s)), int32(mask.model.height)}
		flags := uintptr(0x0820) // DT_SINGLELINE|DT_NOPREFIX, top-left.
		if strings.ContainsFunc(run.text, func(r rune) bool { return unicode.Is(unicode.Arabic, r) }) {
			flags |= 0x20000 | 2 // DT_RTLREADING, right aligned within its box.
		}
		overlayUser.NewProc("DrawTextW").Call(mask.dc, uintptr(unsafe.Pointer(&encoded[0])), uintptr(len(encoded)), uintptr(unsafe.Pointer(&rect)), flags)
		overlayGDI.NewProc("GdiFlush").Call()
		top := int(math.Round((run.y + offset(run.region)) * s))
		for y := range mask.model.height {
			row := top + y
			if row < 0 || row >= h {
				continue
			}
			for x := max(0, int(rect.Left)); x < min(w, int(rect.Right)); x++ {
				k := (y*w + x) * 4
				a := max(mask.pixels[k], mask.pixels[k+1], mask.pixels[k+2])
				if a != 0 {
					blendPixel(n.content, (row*w+x)*4, run.color, float64(a)/255*run.alpha)
				}
			}
		}
	}
}

// blendPixel composites a color over a premultiplied BGRA pixel.
func blendPixel(p []byte, i int, color [3]float64, alpha float64) {
	if alpha <= 0 {
		return
	}
	alpha = min(1, alpha)
	keep := 1 - alpha
	p[i] = uint8(float64(p[i])*keep + color[2]*alpha + .5)
	p[i+1] = uint8(float64(p[i+1])*keep + color[1]*alpha + .5)
	p[i+2] = uint8(float64(p[i+2])*keep + color[0]*alpha + .5)
	p[i+3] = uint8(float64(p[i+3])*keep + 255*alpha + .5)
}

// roundedDepth is the signed distance inside a rounded rectangle.
func roundedDepth(px, py, x, y, w, h, r float64) float64 {
	dx := math.Abs(px-(x+w/2)) - (w/2 - r)
	dy := math.Abs(py-(y+h/2)) - (h/2 - r)
	return -(math.Hypot(max(0, dx), max(0, dy)) + min(0, max(dx, dy)) - r)
}

func paintIslandBox(p []byte, w, h int, s float64, b islandBox, offset float64) {
	x0, y0 := int(math.Floor((b.x-2)*s)), int(math.Floor((b.y+offset-2)*s))
	x1, y1 := int(math.Ceil((b.x+b.w+2)*s)), int(math.Ceil((b.y+offset+b.h+2)*s))
	for y := max(0, y0); y < min(h, y1); y++ {
		for x := max(0, x0); x < min(w, x1); x++ {
			px, py := (float64(x)+.5)/s, (float64(y)+.5)/s-offset
			depth := roundedDepth(px, py, b.x, b.y, b.w, b.h, b.r) * s
			inside := max(0, min(1, depth+.5))
			if inside == 0 {
				continue
			}
			i := (y*w + x) * 4
			if b.fillAlpha > 0 {
				blendPixel(p, i, b.fill, b.fillAlpha*inside)
			}
			if b.strokeWidth > 0 {
				band := max(0, min(1, b.strokeWidth*s-depth+.5)) * inside
				blendPixel(p, i, b.stroke, b.strokeAlpha*band)
			}
		}
	}
}

// paintIsland composes the surface for geometry and presents it.
func (r *windowsRenderer) paintIsland(g islandRect, compactAlpha, expandedAlpha float64, now time.Time, interactive bool) {
	n := &r.island
	maxW := int(math.Ceil(max(n.expandedRect.W, n.compactRect.W, g.W))) + 2
	maxH := int(math.Ceil(max(n.expandedRect.H, n.compactRect.H, g.H))) + 2
	if n.surface.dc == 0 || n.surface.model.width < maxW || n.surface.model.height < maxH {
		if !n.surface.create(&edgePixels{width: maxW, height: maxH}) {
			return
		}
	}
	stride := n.surface.model.width
	w, h := int(math.Ceil(g.W)), int(math.Ceil(g.H))
	left := int(math.Round(g.CX - g.W/2))
	top := int(math.Round(g.Top))
	pixels := n.surface.pixels
	if cap(n.coverage) < w*h {
		n.coverage = make([]byte, w*h)
	}
	coverage := n.coverage[:w*h]
	phase := float64(time.Since(r.epoch)%edgeCycle) / float64(edgeCycle)
	if r.event.ReducedMotion {
		phase = 0
	}
	// Shape: SDF only near the border; the interior is a flat fill.
	band := g.R + 2
	for y := range h {
		row := pixels[y*stride*4 : (y*stride+w)*4]
		fy := float64(y) + .5
		nearY := fy < band || fy > g.H-band
		for x := range w {
			fx := float64(x) + .5
			i := x * 4
			if !nearY && fx >= band && fx <= g.W-band {
				row[i], row[i+1], row[i+2], row[i+3] = uint8(islandBase[2]), uint8(islandBase[1]), uint8(islandBase[0]), 255
				coverage[y*w+x] = 255
				continue
			}
			depth := roundedDepth(fx, fy, 0, 0, g.W, g.H, g.R)
			a := max(0, min(1, depth+.5))
			coverage[y*w+x] = uint8(a * 255)
			if a == 0 {
				row[i], row[i+1], row[i+2], row[i+3] = 0, 0, 0, 0
				continue
			}
			rim := max(0, min(1, 1.1*r.island.scale-depth)) / max(1, r.island.scale*.6)
			rim = min(1, rim)
			c := edgeColor(fx/g.W + .15 + phase)
			for ch, base := range [3]float64{islandBase[2], islandBase[1], islandBase[0]} {
				row[i+ch] = uint8((base*(1-rim) + float64(c[2-ch])*rim) * a)
			}
			row[i+3] = uint8(255 * a)
		}
	}
	// Compact content: the live banner, centered at the top.
	if compactAlpha > 0.001 {
		b := &r.banner
		r.mascot.Advance(r.event, now, Look{Y: -.25})
		b.paint(time.Since(r.epoch), r.mascot, r.event.CanStop && !n.open)
		bw, bh := b.surface.model.width, b.surface.model.height
		ox := (w - bw) / 2
		for y := range min(bh, h) {
			for x := range bw {
				tx := ox + x
				if tx < 0 || tx >= w {
					continue
				}
				k := (y*bw + x) * 4
				a := float64(b.surface.pixels[k+3]) / 255 * compactAlpha * float64(coverage[y*w+tx]) / 255
				if a <= 0 {
					continue
				}
				i := (y*stride + tx) * 4
				keep := 1 - a
				for ch := range 4 {
					src := float64(b.surface.pixels[k+ch]) * compactAlpha * float64(coverage[y*w+tx]) / 255
					pixels[i+ch] = uint8(min(255, float64(pixels[i+ch])*keep+src))
				}
			}
		}
	}
	// Expanded content, laid out at its final size and clipped by the shape.
	if expandedAlpha > 0.001 && n.layout != nil && n.content != nil {
		l := n.layout
		s := n.scale
		cw := n.contentW
		ox := int(math.Round(g.W/2 - float64(cw)/2))
		headerPx := int(math.Round(l.headerH * s))
		viewPx := int(math.Round(l.viewH * s))
		bodyPx := int(math.Round(l.bodyH * s))
		scrollPx := int(math.Round(n.scroll * s))
		for y := range h {
			var src int
			switch {
			case y < headerPx:
				src = y
			case y < headerPx+viewPx:
				src = y + scrollPx
			default:
				src = y - headerPx - viewPx + headerPx + bodyPx
			}
			if src < 0 || src >= n.contentH {
				continue
			}
			for x := range w {
				sx := x - ox
				if sx < 0 || sx >= cw {
					continue
				}
				k := (src*cw + sx) * 4
				ca := n.content[k+3]
				if ca == 0 {
					continue
				}
				cov := float64(coverage[y*w+x]) / 255
				a := float64(ca) / 255 * expandedAlpha * cov
				i := (y*stride + x) * 4
				keep := 1 - a
				for ch := range 4 {
					pixels[i+ch] = uint8(min(255, float64(pixels[i+ch])*keep+float64(n.content[k+ch])*expandedAlpha*cov))
				}
			}
		}
		// Caret: a steady bar while typing, blinking when idle.
		if l.caretOK && interactive {
			cx := ox + int(math.Round(l.caretX*s))
			cy := int(math.Round((l.headerH+l.caretY-n.scroll)*s)) + int(2*s)
			ch := int(math.Round((l.lineH - 4) * s))
			if (now.UnixMilli()/530)%2 == 0 {
				for y := max(cy, headerPx); y < min(cy+ch, headerPx+viewPx, h); y++ {
					for x := cx; x < cx+max(1, int(math.Round(1.5*s))) && x < w; x++ {
						if x >= 0 {
							blendPixel(pixels, (y*stride+x)*4, islandInk, expandedAlpha)
						}
					}
				}
			}
			r.placeIME(left+cx, top+cy, ch)
		}
		// Scroll indicator for long requests.
		if l.bodyH > l.viewH {
			trackH := float64(viewPx)
			thumbH := max(24*s, trackH*l.viewH/l.bodyH)
			thumbY := float64(headerPx) + (trackH-thumbH)*n.scroll/max(1, l.bodyH-l.viewH)
			x := w - int(6*s)
			for y := int(thumbY); y < int(thumbY+thumbH) && y < h; y++ {
				for dx := range max(2, int(3*s)) {
					blendPixel(pixels, (y*stride+x+dx)*4, islandInk, .28*expandedAlpha)
				}
			}
		}
	}
	position := overlayPoint{int32(left), int32(top)}
	size, origin := overlayPoint{int32(w), int32(h)}, overlayPoint{}
	blend := uint32(0x01ff0000)
	ok, _, _ := overlayUser.NewProc("UpdateLayeredWindow").Call(r.bannerWindow, 0, uintptr(unsafe.Pointer(&position)), uintptr(unsafe.Pointer(&size)), n.surface.dc, uintptr(unsafe.Pointer(&origin)), 0, uintptr(unsafe.Pointer(&blend)), 2)
	if ok != 0 {
		if n.open {
			placeWindow(r.bannerWindow, ^uintptr(0), 0x0053)
		} else {
			placeWindow(r.bannerWindow, r.windows[0], 0x0053)
		}
	}
	n.frames++
	if !n.frameStart.IsZero() {
		gap := now.Sub(n.frameStart)
		n.lastFrameGap = gap
		if !n.motion.ready || now.Sub(n.motion.start) < n.motion.duration {
			n.maxFrameGap = max(n.maxFrameGap, gap)
		}
	}
	n.frameStart = now
	if n.layout != nil && n.layout.summary != n.announcedText && n.open {
		title, _ := syscall.UTF16PtrFromString(n.layout.summary)
		overlayUser.NewProc("SetWindowTextW").Call(r.bannerWindow, uintptr(unsafe.Pointer(title)))
		n.announcedText = n.layout.summary
		// Let assistive technology know the focused description changed.
		overlayUser.NewProc("NotifyWinEvent").Call(0x800C, r.bannerWindow, 0, 0)
	}
}

// placeIME keeps the IME composition window at the caret.
func (r *windowsRenderer) placeIME(x, y, h int) {
	get := overlayIMM.NewProc("ImmGetContext")
	if get.Find() != nil {
		return
	}
	context, _, _ := get.Call(r.bannerWindow)
	if context == 0 {
		return
	}
	defer overlayIMM.NewProc("ImmReleaseContext").Call(r.bannerWindow, context)
	var window overlayRect
	overlayUser.NewProc("GetWindowRect").Call(r.bannerWindow, uintptr(unsafe.Pointer(&window)))
	form := struct {
		Style uint32
		Point overlayPoint
		Area  overlayRect
	}{Style: 2, Point: overlayPoint{int32(x) - window.Left, int32(y+h) - window.Top}}
	overlayIMM.NewProc("ImmSetCompositionWindow").Call(context, uintptr(unsafe.Pointer(&form)))
}

// hitIsland maps a client point to a control; -2 is none, -1 is Stop.
func (n *nativeIsland) hitIsland(clientX, clientY int32) (int, bool) {
	l := n.layout
	if l == nil {
		return -2, false
	}
	s := n.scale
	g, _ := n.motion.at(time.Now())
	cw := float64(n.contentW) / s
	x := float64(clientX)/s - (g.W/s/2 - cw/2)
	y := float64(clientY) / s
	for _, hit := range l.hits {
		hy := y
		switch hit.region {
		case 1:
			if y < l.headerH || y >= l.headerH+l.viewH {
				continue
			}
			hy = y - l.headerH + n.scroll
		case 2:
			if y < l.headerH+l.viewH {
				continue
			}
			hy = y - l.headerH - l.viewH
		default:
			if y >= l.headerH {
				continue
			}
		}
		if x >= hit.x && x < hit.x+hit.w && hy >= hit.y && hy < hit.y+hit.h {
			return hit.control, hit.text
		}
	}
	return -2, false
}

// handleIslandInput applies queued input to the displayed request only.
func (r *windowsRenderer) handleIslandInput(now time.Time) {
	r.inputMu.Lock()
	inbox := r.inbox
	r.inbox = nil
	r.inputMu.Unlock()
	n := &r.island
	f := n.form
	if f == nil {
		return
	}
	_, settled := n.motion.at(now)
	for _, in := range inbox {
		switch in.msg {
		case 0x200: // WM_MOUSEMOVE
			hit, text := n.hitIsland(in.clientX, in.cly)
			cursor := int32(0)
			if text {
				cursor = 2
			} else if hit != -2 {
				cursor = 1
			}
			r.islandCursor.Store(cursor)
			if hit != n.hover {
				n.hover = hit
				n.version++
			}
			// Track leaving so hover never sticks.
			track := struct {
				Size, Flags  uint32
				Window       uintptr
				HoverTimeout uint32
			}{Flags: 2, Window: r.bannerWindow}
			track.Size = uint32(unsafe.Sizeof(track))
			overlayUser.NewProc("TrackMouseEvent").Call(uintptr(unsafe.Pointer(&track)))
		case 0x2a3: // WM_MOUSELEAVE
			if n.hover != -2 {
				n.hover = -2
				n.version++
			}
		case 0x201, 0x203: // WM_LBUTTONDOWN, double click as a fresh press.
			if settled {
				n.press, _ = n.hitIsland(in.clientX, in.cly)
			}
		case 0x202: // WM_LBUTTONUP: act only on a press that began here.
			hit, _ := n.hitIsland(in.clientX, in.cly)
			press := n.press
			n.press = -2
			if !settled || hit != press || hit == -2 || now.Sub(f.opened) < islandInputGuard {
				continue
			}
			if hit == -1 {
				r.requestStop()
				continue
			}
			controls := f.controls()
			if hit < 0 || hit >= len(controls) {
				continue
			}
			f.focus = hit
			n.version++
			if controls[hit].kind == controlText {
				f.caret[f.page] = len(f.text[f.page])
				continue
			}
			if response, send := f.activate(controls[hit]); send {
				r.deliver(response)
			}
		case 0x20a: // WM_MOUSEWHEEL
			delta := float64(int16(in.wparam>>16)) / 120
			r.scrollIsland(-delta * 48)
		case 0x100, 0x104: // WM_KEYDOWN
			if in.wparam == 0x56 && in.ctrl { // Ctrl+V pastes text.
				f.insert(readClipboardText())
				n.version++
				continue
			}
			if k, ok := islandKeyFor(in.wparam, in.shift); ok {
				if k == keyPageUp || k == keyPageDown {
					page := (n.layoutViewH() - 40) * map[bool]float64{true: -1, false: 1}[k == keyPageUp]
					r.scrollIsland(page)
					continue
				}
				if response, send := f.key(k, now); send {
					r.deliver(response)
				}
				n.version++
				r.revealFocus()
			}
		case 0x102: // WM_CHAR, including IME results.
			unit := uint16(in.wparam)
			if c, ok := f.focused(); !ok || c.kind != controlText || now.Sub(f.opened) < islandInputGuard {
				continue
			}
			if utf16.IsSurrogate(rune(unit)) {
				if unit < 0xdc00 {
					n.surrogate = unit
					continue
				}
				if n.surrogate != 0 {
					f.insert(string(utf16.DecodeRune(rune(n.surrogate), rune(unit))))
					n.surrogate = 0
					n.version++
				}
				continue
			}
			if unit >= 0x20 && unit != 0x7f {
				f.insert(string(rune(unit)))
				n.version++
			}
		case 0x8: // WM_KILLFOCUS: keep the request; nothing is answered.
		}
	}
}

func (n *nativeIsland) layoutViewH() float64 {
	if n.layout == nil {
		return 0
	}
	return n.layout.viewH
}

func (r *windowsRenderer) scrollIsland(delta float64) {
	n := &r.island
	if n.layout == nil {
		return
	}
	limit := max(0, n.layout.bodyH-n.layout.viewH)
	next := max(0, min(limit, n.scroll+delta))
	if next != n.scroll {
		n.scroll = next
	}
}

// revealFocus scrolls the focused body control into view.
func (r *windowsRenderer) revealFocus() {
	n := &r.island
	if n.layout == nil || n.form == nil {
		return
	}
	for _, hit := range n.layout.hits {
		if hit.region == 1 && hit.control == n.form.focus {
			if hit.y < n.scroll {
				n.scroll = hit.y
			} else if hit.y+hit.h > n.scroll+n.layout.viewH {
				n.scroll = min(n.layout.bodyH-n.layout.viewH, hit.y+hit.h-n.layout.viewH)
			}
		}
	}
}

func islandKeyFor(vk uintptr, shift bool) (islandKey, bool) {
	switch vk {
	case 0x09:
		if shift {
			return keyShiftTab, true
		}
		return keyTab, true
	case 0x26:
		return keyUp, true
	case 0x28:
		return keyDown, true
	case 0x25:
		return keyLeft, true
	case 0x27:
		return keyRight, true
	case 0x24:
		return keyHome, true
	case 0x23:
		return keyEnd, true
	case 0x20:
		return keySpace, true
	case 0x0d:
		if shift {
			return keyShiftEnter, true
		}
		return keyEnter, true
	case 0x08:
		return keyBackspace, true
	case 0x2e:
		return keyDelete, true
	case 0x21:
		return keyPageUp, true
	case 0x22:
		return keyPageDown, true
	}
	return 0, false
}

var clipboardReady = sync.OnceValue(clipboard.Init)

// readClipboardText returns the clipboard's text, if any.
func readClipboardText() string {
	if clipboardReady() != nil {
		return ""
	}
	data, err := clipboard.Read(clipboard.FormatText)
	if err != nil {
		return ""
	}
	return string(data)
}

// deliver sends a response off the render thread. Only a rejected payload
// re-enables the form; a stale or busy request stays sent.
func (r *windowsRenderer) deliver(response PromptResponse) {
	respond, revision := r.onRespond, r.island.revision
	if respond == nil {
		return
	}
	form := r.island.form
	go func() {
		err := respond(revision, response)
		if errors.Is(err, ErrPromptInvalid) {
			r.mu.Lock()
			if r.island.form == form {
				form.submitted = false
				r.island.version++
			}
			r.mu.Unlock()
		}
	}()
}

// requestStop cancels the run owning the visible surface.
func (r *windowsRenderer) requestStop() {
	stop, id := r.onStop, r.event.ID
	if stop != nil && r.event.CanStop {
		go stop(id)
	}
}

// updateStopClick makes the compact Stop clickable only for the physical
// mouse, never for a pointer the agent is driving.
func (r *windowsRenderer) updateStopClick(position overlayPoint, scale float64) {
	b := &r.banner
	clickable := false
	if r.event.CanStop && b.canStop && r.pointerSource.physical && time.Since(r.event.PointerAt) > 400*time.Millisecond {
		var cursor overlayPoint
		if ok, _, _ := overlayUser.NewProc("GetCursorPos").Call(uintptr(unsafe.Pointer(&cursor))); ok != 0 {
			left := position.X + int32(b.surface.model.width) - b.tail
			clickable = cursor.X >= left && cursor.X < position.X+int32(b.surface.model.width) && cursor.Y >= position.Y && cursor.Y < position.Y+int32(b.surface.model.height)
		}
	}
	if clickable != r.stopClickable.Load() {
		r.stopClickable.Store(clickable)
		r.setTransparent(!clickable)
	}
	r.inputMu.Lock()
	inbox := r.inbox
	r.inbox = nil
	r.inputMu.Unlock()
	for _, in := range inbox {
		if in.msg == 0x201 {
			r.stopPressed = clickable
		}
		if in.msg == 0x202 && clickable && r.stopPressed {
			r.stopPressed = false
			r.requestStop()
		}
	}
}

// IslandFrameStats reports frame pacing of the most recent island motion.
func (r *windowsRenderer) islandFrameStats() (frames uint64, worst time.Duration) {
	return r.island.frames, r.island.maxFrameGap
}
