//go:build windows

package browser

import (
	"context"
	"math"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/activity"
	cdpbrowser "github.com/chromedp/cdproto/browser"
	"github.com/chromedp/chromedp"
)

var browserOverlayUser = syscall.NewLazyDLL("user32.dll")

// Scan handles retain Go state across synchronous Win32 callbacks without
// converting an opaque integer back into a Go pointer.
var (
	browserScanIDs atomic.Uintptr
	browserScans   sync.Map
)

func registerBrowserScan(scan any) uintptr {
	id := browserScanIDs.Add(1)
	browserScans.Store(id, scan)
	return id
}

func resolveBrowserScan[T any](id uintptr) *T {
	value, _ := browserScans.Load(id)
	scan, _ := value.(*T)
	return scan
}

type (
	browserDesktopRect  struct{ Left, Top, Right, Bottom int32 }
	browserDesktopPoint struct{ X, Y int32 }
)

// nativeBrowserActivity owns desktop visuals independently of document and
// target lifetimes. Only viewport coordinates come from CDP.
type nativeBrowserActivity struct {
	session         *chromedpSession
	renderer        activity.Renderer
	pid             uint32
	window          atomic.Uintptr
	lastX, lastY    int
	hasLast         bool
	cssX, cssY      float64
	projection      browserScreenTransform
	projectionRect  browserDesktopRect
	projectionDPI   uint32
	projectionValid bool
}

func newNativeBrowserActivity(s *chromedpSession) activity.Renderer {
	c := chromedp.FromContext(s.rootCtx)
	if c == nil || c.Browser == nil {
		return nil
	}
	var pid uint32
	if process := c.Browser.Process(); process != nil {
		pid = uint32(process.Pid)
	} else if port, ok := localBrowserEndpointPort(s.activityEndpoint); ok {
		ctx, cancel := context.WithTimeout(s.currentContext(), 2*time.Second)
		defer cancel()
		info, err := s.command(ctx, "SystemInfo.getProcessInfo", map[string]any{}, true)
		if err == nil {
			processes, _ := info["processInfo"].([]any)
			for _, item := range processes {
				process, _ := item.(map[string]any)
				id, _ := process["id"].(float64)
				if process["type"] == "browser" && id > 0 && id <= math.MaxUint32 && math.Trunc(id) == id && localBrowserListenerOwned(port, uint32(id)) {
					pid = uint32(id)
					break
				}
			}
		}
	}
	if pid == 0 {
		return nil
	}
	view, bounds, err := s.readNativeBrowserViewport()
	if err != nil {
		return nil
	}
	transform, window, valid := nativeBrowserTransform(pid, bounds, view)
	if !valid || window == 0 {
		return nil
	}
	r := &nativeBrowserActivity{
		session: s, renderer: activity.NewNativeRenderer(), pid: pid, hasLast: true,
		cssX: max(0, view.Width-48), cssY: max(0, view.Height-48),
	}
	r.lastX, r.lastY = transform.point(r.cssX, r.cssY)
	r.window.Store(window)
	r.rememberProjection(window, transform)
	return r
}

func (r *nativeBrowserActivity) SetStopHandler(stop func(uint64) bool) {
	if native, ok := r.renderer.(interface{ SetStopHandler(func(uint64) bool) }); ok {
		native.SetStopHandler(func(id uint64) bool {
			foreground, _, _ := browserOverlayUser.NewProc("GetForegroundWindow").Call()
			var pid uint32
			browserOverlayUser.NewProc("GetWindowThreadProcessId").Call(foreground, uintptr(unsafe.Pointer(&pid)))
			// The control island holds focus while the user answers; its
			// Stop and Escape still belong to this browser run.
			return (pid == r.pid || activity.IsOverlayWindow(foreground)) && stop(id)
		})
	}
}

// SetRespondHandler lets the native island answer its displayed request.
func (r *nativeBrowserActivity) SetRespondHandler(respond func(uint64, activity.PromptResponse) error) {
	if native, ok := r.renderer.(interface {
		SetRespondHandler(func(uint64, activity.PromptResponse) error)
	}); ok {
		native.SetRespondHandler(respond)
	}
}

func (r *nativeBrowserActivity) Render(e activity.Event) {
	if !e.Visible {
		r.renderer.Render(e)
		if e.FinishedUntil.IsZero() {
			r.hasLast = false
		}
		return
	}
	view, bounds, err := r.session.readNativeBrowserViewport()
	var transform browserScreenTransform
	var valid bool
	if err == nil {
		var window uintptr
		transform, window, valid = nativeBrowserTransform(r.pid, bounds, view)
		if valid {
			r.window.Store(window)
			r.rememberProjection(window, transform)
		}
	}
	if !valid && r.projectionValid {
		if rect, dpi, ok := browserWindowGeometry(r.window.Load(), r.pid); ok && rect == r.projectionRect && dpi == r.projectionDPI {
			transform, valid = r.projection, true
			// Document replacement must not imply a confirmed click at the
			// old viewport mapping. Movement can retain its visual continuity.
			if e.PointerKind != "move" && e.PointerKind != "aim" {
				e.PointerKind = ""
			}
		}
	}
	if valid {
		if !e.Point {
			// Keep the indicator on its browser monitor when the window is
			// moved, without making the physical mouse drive the AI cursor.
			if !r.hasLast {
				if view.valid() {
					r.cssX, r.cssY = max(0, view.Width-48), max(0, view.Height-48)
				}
			}
			e.Point, e.PointerRevision = true, 0
			e.X, e.Y = int(math.Round(r.cssX)), int(math.Round(r.cssY))
			e.PointerX, e.PointerY = r.cssX, r.cssY
		} else if e.PointerRevision != 0 {
			r.cssX, r.cssY = e.PointerX, e.PointerY
		} else {
			r.cssX, r.cssY = float64(e.X), float64(e.Y)
		}
	}
	e = desktopBrowserEvent(e, transform, valid, r.lastX, r.lastY, r.hasLast)
	if e.Point {
		r.lastX, r.lastY, r.hasLast = e.X, e.Y, true
	}
	if window := r.window.Load(); window != 0 {
		e.WindowID = strconv.FormatUint(uint64(window), 10)
	}
	r.renderer.Render(e)
}

func (r *nativeBrowserActivity) Close() { r.renderer.Close() }

func (r *nativeBrowserActivity) rememberProjection(window uintptr, projection browserScreenTransform) {
	rect, dpi, ok := browserWindowGeometry(window, r.pid)
	if ok {
		r.projection, r.projectionRect, r.projectionDPI, r.projectionValid = projection, rect, dpi, true
	}
}

// Only an unchanged, still-visible window in the verified browser process may
// reuse a visual projection while its document's CDP context is unavailable.
func browserWindowGeometry(window uintptr, expectedPID uint32) (browserDesktopRect, uint32, bool) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if awareness := browserOverlayUser.NewProc("SetThreadDpiAwarenessContext"); awareness.Find() == nil {
		previous, _, _ := awareness.Call(^uintptr(3))
		if previous != 0 {
			defer awareness.Call(previous)
		}
	}
	var pid uint32
	browserOverlayUser.NewProc("GetWindowThreadProcessId").Call(window, uintptr(unsafe.Pointer(&pid)))
	visible, _, _ := browserOverlayUser.NewProc("IsWindowVisible").Call(window)
	iconic, _, _ := browserOverlayUser.NewProc("IsIconic").Call(window)
	if pid != expectedPID || pid == 0 || visible == 0 || iconic != 0 {
		return browserDesktopRect{}, 0, false
	}
	var rect browserDesktopRect
	ok, _, _ := browserOverlayUser.NewProc("GetWindowRect").Call(window, uintptr(unsafe.Pointer(&rect)))
	if ok == 0 {
		return browserDesktopRect{}, 0, false
	}
	dpi := uint32(96)
	if query := browserOverlayUser.NewProc("GetDpiForWindow"); query.Find() == nil {
		value, _, _ := query.Call(window)
		if value != 0 {
			dpi = uint32(value)
		}
	}
	return rect, dpi, true
}

type browserWindowScan struct {
	pid    uint32
	bounds *cdpbrowser.Bounds
	window uintptr
	score  float64
}

// One permanent callback avoids allocating a new Win32 callback each frame.
var browserWindowScanCallback = syscall.NewCallback(func(hwnd, parameter uintptr) uintptr {
	scan := resolveBrowserScan[browserWindowScan](parameter)
	if scan == nil {
		return 0
	}
	var pid uint32
	browserOverlayUser.NewProc("GetWindowThreadProcessId").Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	visible, _, _ := browserOverlayUser.NewProc("IsWindowVisible").Call(hwnd)
	iconic, _, _ := browserOverlayUser.NewProc("IsIconic").Call(hwnd)
	if pid != scan.pid || visible == 0 || iconic != 0 {
		return 1
	}
	var name [128]uint16
	browserOverlayUser.NewProc("GetClassNameW").Call(hwnd, uintptr(unsafe.Pointer(&name[0])), uintptr(len(name)))
	if !strings.HasPrefix(syscall.UTF16ToString(name[:]), "Chrome_WidgetWin_") {
		return 1
	}
	var rect browserDesktopRect
	ok, _, _ := browserOverlayUser.NewProc("GetWindowRect").Call(hwnd, uintptr(unsafe.Pointer(&rect)))
	if ok == 0 {
		return 1
	}
	scale := float64(1)
	if dpi := browserOverlayUser.NewProc("GetDpiForWindow"); dpi.Find() == nil {
		value, _, _ := dpi.Call(hwnd)
		if value != 0 {
			scale = float64(value) / 96
		}
	}
	// Chromium bounds vary with its DPI mode. Accept physical or DIP bounds,
	// but only within the launched browser process and a bounded tolerance.
	score := browserBoundsScore(rect, scan.bounds, scale)
	if score < scan.score {
		scan.score, scan.window = score, hwnd
	}
	return 1
})

func browserBoundsScore(rect browserDesktopRect, bounds *cdpbrowser.Bounds, scale float64) float64 {
	if bounds == nil || bounds.Width <= 0 || bounds.Height <= 0 {
		return math.Inf(1)
	}
	best := math.Inf(1)
	for _, factor := range []float64{1, scale} {
		score := math.Abs(float64(rect.Left)-float64(bounds.Left)*factor) +
			math.Abs(float64(rect.Top)-float64(bounds.Top)*factor) +
			math.Abs(float64(rect.Right-rect.Left)-float64(bounds.Width)*factor) +
			math.Abs(float64(rect.Bottom-rect.Top)-float64(bounds.Height)*factor)
		best = min(best, score)
	}
	return best
}

type browserContentScan struct {
	width, height float64
	origin        browserDesktopPoint
	found         bool
}

var browserContentScanCallback = syscall.NewCallback(func(hwnd, parameter uintptr) uintptr {
	scan := resolveBrowserScan[browserContentScan](parameter)
	if scan == nil {
		return 0
	}
	var name [128]uint16
	browserOverlayUser.NewProc("GetClassNameW").Call(hwnd, uintptr(unsafe.Pointer(&name[0])), uintptr(len(name)))
	if syscall.UTF16ToString(name[:]) != "Chrome_RenderWidgetHostHWND" {
		return 1
	}
	visible, _, _ := browserOverlayUser.NewProc("IsWindowVisible").Call(hwnd)
	var rect browserDesktopRect
	ok, _, _ := browserOverlayUser.NewProc("GetWindowRect").Call(hwnd, uintptr(unsafe.Pointer(&rect)))
	if visible != 0 && ok != 0 && math.Abs(float64(rect.Right-rect.Left)-scan.width) <= 2 && math.Abs(float64(rect.Bottom-rect.Top)-scan.height) <= 2 {
		scan.origin, scan.found = browserDesktopPoint{rect.Left, rect.Top}, true
		return 0
	}
	return 1
})

func nativeBrowserTransform(pid uint32, bounds *cdpbrowser.Bounds, view browserViewport) (browserScreenTransform, uintptr, bool) {
	if !view.valid() || bounds == nil || bounds.WindowState == cdpbrowser.WindowStateMinimized {
		return browserScreenTransform{}, 0, false
	}
	// Physical coordinates must not be DPI-virtualized on this Go thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if dpi := browserOverlayUser.NewProc("SetThreadDpiAwarenessContext"); dpi.Find() == nil {
		previous, _, _ := dpi.Call(^uintptr(3))
		if previous != 0 {
			defer dpi.Call(previous)
		}
	}
	scan := browserWindowScan{pid: pid, bounds: bounds, score: 64}
	windowScanHandle := registerBrowserScan(&scan)
	browserOverlayUser.NewProc("EnumWindows").Call(browserWindowScanCallback, windowScanHandle)
	browserScans.Delete(windowScanHandle)
	if scan.window == 0 {
		return browserScreenTransform{}, 0, false
	}
	content := browserContentScan{width: view.Width * view.DPR, height: view.Height * view.DPR}
	contentScanHandle := registerBrowserScan(&content)
	browserOverlayUser.NewProc("EnumChildWindows").Call(scan.window, browserContentScanCallback, contentScanHandle)
	browserScans.Delete(contentScanHandle)
	if content.found {
		return browserScreenTransform{float64(content.origin.X), float64(content.origin.Y), view.DPR}, scan.window, true
	}
	// A dedicated Chrome window without side panels docks its viewport at
	// the bottom of the client area. Never guess around docked DevTools or
	// sidebars when the content HWND cannot be resolved.
	var client browserDesktopRect
	var origin browserDesktopPoint
	gotClient, _, _ := browserOverlayUser.NewProc("GetClientRect").Call(scan.window, uintptr(unsafe.Pointer(&client)))
	gotOrigin, _, _ := browserOverlayUser.NewProc("ClientToScreen").Call(scan.window, uintptr(unsafe.Pointer(&origin)))
	header := float64(client.Bottom-client.Top) - content.height
	if gotClient == 0 || gotOrigin == 0 || math.Abs(float64(client.Right-client.Left)-content.width) > 2 || header < 0 || header > 300 {
		return browserScreenTransform{}, scan.window, false
	}
	return browserScreenTransform{float64(origin.X), float64(origin.Y) + header, view.DPR}, scan.window, true
}
