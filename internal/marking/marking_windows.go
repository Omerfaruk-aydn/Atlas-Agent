//go:build windows

package marking

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/activity"
	"golang.org/x/sys/windows"
)

var (
	user32  = windows.NewLazySystemDLL("user32.dll")
	gdi32   = windows.NewLazySystemDLL("gdi32.dll")
	shcore  = windows.NewLazySystemDLL("shcore.dll")
	dwmapi  = windows.NewLazySystemDLL("dwmapi.dll")
	kernel  = windows.NewLazySystemDLL("kernel32.dll")
	user    = func(name string) *windows.LazyProc { return user32.NewProc(name) }
	gdi     = func(name string) *windows.LazyProc { return gdi32.NewProc(name) }
	selectM sync.Mutex
)

type winRect struct{ Left, Top, Right, Bottom int32 }

func (r winRect) rect() Rect {
	return Rect{X: int(r.Left), Y: int(r.Top), W: int(r.Right - r.Left), H: int(r.Bottom - r.Top)}
}

type monitorInfoEx struct {
	Size    uint32
	Monitor winRect
	Work    winRect
	Flags   uint32
	Device  [32]uint16
}

// perMonitorAware switches the calling thread to per-monitor-v2 DPI
// awareness so every coordinate is a physical pixel. It returns a restore.
func perMonitorAware() func() {
	p := user("SetThreadDpiAwarenessContext")
	if p.Find() != nil {
		return func() {}
	}
	prev, _, _ := p.Call(^uintptr(3)) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2
	return func() {
		if prev != 0 {
			p.Call(prev)
		}
	}
}

// Desktop is the live Windows desktop.
type Desktop struct{}

// Monitors enumerates displays with their effective DPI.
func (Desktop) Monitors() ([]Monitor, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer perMonitorAware()()
	return monitors()
}

func monitors() ([]Monitor, error) {
	var out []Monitor
	cb := syscall.NewCallback(func(hmon, _, _, _ uintptr) uintptr {
		info := monitorInfoEx{}
		info.Size = uint32(unsafe.Sizeof(info))
		if ok, _, _ := user("GetMonitorInfoW").Call(hmon, uintptr(unsafe.Pointer(&info))); ok == 0 {
			return 1
		}
		var dx, dy uint32 = 96, 96
		if p := shcore.NewProc("GetDpiForMonitor"); p.Find() == nil {
			p.Call(hmon, 0, uintptr(unsafe.Pointer(&dx)), uintptr(unsafe.Pointer(&dy)))
		}
		out = append(out, Monitor{ID: windows.UTF16ToString(info.Device[:]), Bounds: info.Monitor.rect(), DPI: int(dx), Primary: info.Flags&1 != 0})
		return 1
	})
	if ok, _, err := user("EnumDisplayMonitors").Call(0, 0, cb, 0); ok == 0 {
		return nil, err
	}
	if len(out) == 0 {
		return nil, errors.New("no monitors reported")
	}
	return out, nil
}

// WindowInfo reads a top-level window.
func (Desktop) WindowInfo(handle uint64) (Window, bool) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer perMonitorAware()()
	return windowInfo(uintptr(handle))
}

func windowInfo(hwnd uintptr) (Window, bool) {
	if ok, _, _ := user("IsWindow").Call(hwnd); ok == 0 {
		return Window{}, false
	}
	var pid uint32
	user("GetWindowThreadProcessId").Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	var r winRect
	// The DWM frame bounds exclude the invisible resize borders.
	if hr, _, _ := dwmapi.NewProc("DwmGetWindowAttribute").Call(hwnd, 9, uintptr(unsafe.Pointer(&r)), unsafe.Sizeof(r)); hr != 0 {
		user("GetWindowRect").Call(hwnd, uintptr(unsafe.Pointer(&r)))
	}
	buf := make([]uint16, 512)
	user("GetWindowTextW").Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return Window{Handle: uint64(hwnd), Title: windows.UTF16ToString(buf), PID: int(pid), Process: processName(pid), Rect: r.rect()}, true
}

func processName(pid uint32) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_PATH)
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n); err != nil {
		return ""
	}
	path := windows.UTF16ToString(buf[:n])
	if i := strings.LastIndexAny(path, `\/`); i >= 0 {
		return path[i+1:]
	}
	return path
}

// CaptureHash captures r and hashes it.
func (d Desktop) CaptureHash(r Rect) (uint64, error) {
	img, err := d.Capture(r)
	if err != nil {
		return 0, err
	}
	return AverageHash(img), nil
}

// Capture grabs r from the screen. Atlas overlays use display affinity
// WDA_EXCLUDEFROMCAPTURE, so they never appear in it.
func (Desktop) Capture(r Rect) (*image.RGBA, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer perMonitorAware()()
	c, err := captureDIB(r)
	if err != nil {
		return nil, err
	}
	defer c.release()
	return c.image(), nil
}

type bitmapInfoHeader struct {
	Size                         uint32
	Width, Height                int32
	Planes, BitCount             uint16
	Compression, SizeImage       uint32
	XPelsPerMeter, YPelsPerMeter int32
	ClrUsed, ClrImportant        uint32
}

type dibCapture struct {
	dc, bitmap, old uintptr
	bits            unsafe.Pointer
	w, h            int
}

func captureDIB(r Rect) (*dibCapture, error) {
	if r.Empty() {
		return nil, errors.New("empty capture rectangle")
	}
	screen, _, _ := user("GetDC").Call(0)
	if screen == 0 {
		return nil, errors.New("GetDC failed")
	}
	defer user("ReleaseDC").Call(0, screen)
	dc, _, _ := gdi("CreateCompatibleDC").Call(screen)
	hdr := bitmapInfoHeader{Width: int32(r.W), Height: -int32(r.H), Planes: 1, BitCount: 32}
	hdr.Size = uint32(unsafe.Sizeof(hdr))
	var bits unsafe.Pointer
	bmp, _, _ := gdi("CreateDIBSection").Call(dc, uintptr(unsafe.Pointer(&hdr)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bmp == 0 || bits == nil {
		gdi("DeleteDC").Call(dc)
		return nil, errors.New("CreateDIBSection failed")
	}
	old, _, _ := gdi("SelectObject").Call(dc, bmp)
	const srcCopyCaptureBlt = 0x00CC0020 | 0x40000000
	if ok, _, err := gdi("BitBlt").Call(dc, 0, 0, uintptr(r.W), uintptr(r.H), screen, uintptr(int32(r.X)), uintptr(int32(r.Y)), srcCopyCaptureBlt); ok == 0 {
		c := &dibCapture{dc: dc, bitmap: bmp, old: old}
		c.release()
		return nil, fmt.Errorf("BitBlt failed: %v", err)
	}
	return &dibCapture{dc: dc, bitmap: bmp, old: old, bits: bits, w: r.W, h: r.H}, nil
}

func (c *dibCapture) image() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, c.w, c.h))
	src := unsafe.Slice((*byte)(c.bits), c.w*c.h*4)
	for i := 0; i < len(src); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = src[i+2], src[i+1], src[i], 0xFF
	}
	return img
}

func (c *dibCapture) release() {
	if c.dc != 0 {
		gdi("SelectObject").Call(c.dc, c.old)
		gdi("DeleteObject").Call(c.bitmap)
		gdi("DeleteDC").Call(c.dc)
		c.dc = 0
	}
}

// topWindowAt walks top-level windows in z-order and returns the first
// visible, non-transparent, non-Atlas window containing the point.
func topWindowAt(x, y int, skip uintptr) (uintptr, bool) {
	var found uintptr
	cb := syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		if hwnd == skip || activity.IsOverlayWindow(hwnd) {
			return 1
		}
		if v, _, _ := user("IsWindowVisible").Call(hwnd); v == 0 {
			return 1
		}
		ex, _, _ := user("GetWindowLongPtrW").Call(hwnd, ^uintptr(19)) // GWL_EXSTYLE
		if ex&0x20 != 0 {                                              // WS_EX_TRANSPARENT
			return 1
		}
		var cloaked uint32
		dwmapi.NewProc("DwmGetWindowAttribute").Call(hwnd, 14, uintptr(unsafe.Pointer(&cloaked)), 4)
		if cloaked != 0 {
			return 1
		}
		w, ok := windowInfo(hwnd)
		if ok && w.Rect.Contains(x, y) && !w.Rect.Empty() {
			found = hwnd
			return 0
		}
		return 1
	})
	user("EnumWindows").Call(cb, 0)
	return found, found != 0
}

// elementAt asks UI Automation for the element under a physical point. It
// runs out of process (PowerShell + UIAutomationClient), which keeps COM out
// of Atlas; the result is verified against the point before it is trusted.
func elementAt(ctx context.Context, x, y int) (*Element, error) {
	script := fmt.Sprintf(`Add-Type -AssemblyName UIAutomationClient,UIAutomationTypes,WindowsBase
Add-Type -Namespace AtlasDpi -Name T -MemberDefinition '[DllImport("user32.dll")] public static extern IntPtr SetThreadDpiAwarenessContext(IntPtr c);'
[void][AtlasDpi.T]::SetThreadDpiAwarenessContext([IntPtr](-4))
$e=[System.Windows.Automation.AutomationElement]::FromPoint([System.Windows.Point]::new(%d,%d))
if($e){$r=$e.Current.BoundingRectangle;[pscustomobject]@{name=$e.Current.Name;role=$e.Current.ControlType.ProgrammaticName;id=$e.Current.AutomationId;x=[int]$r.X;y=[int]$r.Y;w=[int]$r.Width;h=[int]$r.Height}|ConvertTo-Json -Compress}`, x, y)
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	// -EncodedCommand keeps the script's quotes intact; -Command would let
	// the Windows command line strip them.
	units := utf16.Encode([]rune(script))
	encoded := make([]byte, len(units)*2)
	for i, v := range units {
		binary.LittleEndian.PutUint16(encoded[i*2:], v)
	}
	cmd := exec.CommandContext(cctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-EncodedCommand", base64.StdEncoding.EncodeToString(encoded))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var v struct {
		Name, Role, ID string
		X, Y, W, H     int
	}
	if err := json.Unmarshal(bytes.TrimSpace(out), &v); err != nil {
		return nil, err
	}
	el := &Element{Name: v.Name, Role: v.Role, AutomationID: v.ID, Bounds: Rect{X: v.X, Y: v.Y, W: v.W, H: v.H}}
	el.Verified = el.Bounds.Contains(x, y)
	return el, nil
}

// selection state shared with the window procedure (one selection at a
// time, guarded by selectM).
var sel struct {
	origin         Rect
	capture        *dibCapture
	down, dragging bool
	startX, startY int
	curX, curY     int
	result         *Rect
	click          bool
	canceled       bool
	done           bool
	font           uintptr
}

const (
	wmDestroy     = 0x0002
	wmPaint       = 0x000F
	wmSetCursor   = 0x0020
	wmKeyDown     = 0x0100
	wmMouseMove   = 0x0200
	wmLButtonDown = 0x0201
	wmLButtonUp   = 0x0202
	wmRButtonDown = 0x0204
	wmEraseBkgnd  = 0x0014
	vkEscape      = 0x1B
)

type paintStruct struct {
	HDC                uintptr
	Erase              int32
	Paint              winRect
	Restore, IncUpdate int32
	Reserved           [32]byte
}

func lparamXY(l uintptr) (int, int) { return int(int16(l & 0xFFFF)), int(int16((l >> 16) & 0xFFFF)) }

var overlayProc = syscall.NewCallback(func(hwnd uintptr, msg uint32, wparam, lparam uintptr) uintptr {
	switch msg {
	case wmEraseBkgnd:
		return 1
	case wmSetCursor:
		c, _, _ := user("LoadCursorW").Call(0, 32515) // IDC_CROSS
		user("SetCursor").Call(c)
		return 1
	case wmLButtonDown:
		sel.down = true
		sel.startX, sel.startY = lparamXY(lparam)
		sel.curX, sel.curY = sel.startX, sel.startY
		user("SetCapture").Call(hwnd)
		return 0
	case wmMouseMove:
		if sel.down {
			sel.curX, sel.curY = lparamXY(lparam)
			if absInt(sel.curX-sel.startX) > 4 || absInt(sel.curY-sel.startY) > 4 {
				sel.dragging = true
			}
			user("InvalidateRect").Call(hwnd, 0, 0)
		}
		return 0
	case wmLButtonUp:
		if !sel.down {
			return 0
		}
		user("ReleaseCapture").Call()
		x, y := lparamXY(lparam)
		r := FromDrag(sel.startX, sel.startY, x, y)
		if !sel.dragging {
			sel.click = true
			r = Rect{X: x, Y: y, W: 1, H: 1}
		}
		r.X += sel.origin.X
		r.Y += sel.origin.Y
		sel.result = &r
		sel.done = true
		user("DestroyWindow").Call(hwnd)
		return 0
	case wmRButtonDown:
		sel.canceled, sel.done = true, true
		user("DestroyWindow").Call(hwnd)
		return 0
	case wmKeyDown:
		if wparam == vkEscape {
			sel.canceled, sel.done = true, true
			user("DestroyWindow").Call(hwnd)
		}
		return 0
	case wmPaint:
		paintOverlay(hwnd)
		return 0
	case wmDestroy:
		user("PostQuitMessage").Call(0)
		return 0
	}
	r, _, _ := user("DefWindowProcW").Call(hwnd, uintptr(msg), wparam, lparam)
	return r
})

func paintOverlay(hwnd uintptr) {
	var ps paintStruct
	hdc, _, _ := user("BeginPaint").Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	defer user("EndPaint").Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	w, h := sel.origin.W, sel.origin.H
	back, _, _ := gdi("CreateCompatibleDC").Call(hdc)
	bmp, _, _ := gdi("CreateCompatibleBitmap").Call(hdc, uintptr(w), uintptr(h))
	old, _, _ := gdi("SelectObject").Call(back, bmp)
	gdi("BitBlt").Call(back, 0, 0, uintptr(w), uintptr(h), sel.capture.dc, 0, 0, 0x00CC0020)
	// Selection rectangle.
	if sel.down {
		pen, _, _ := gdi("CreatePen").Call(0, 3, 0x00FF8D4C) // BGR of #4C8DFF
		oldPen, _, _ := gdi("SelectObject").Call(back, pen)
		nullBrush, _, _ := gdi("GetStockObject").Call(5)
		oldBrush, _, _ := gdi("SelectObject").Call(back, nullBrush)
		r := FromDrag(sel.startX, sel.startY, sel.curX, sel.curY)
		gdi("Rectangle").Call(back, uintptr(int32(r.X)), uintptr(int32(r.Y)), uintptr(int32(r.Right())), uintptr(int32(r.Bottom())))
		gdi("SelectObject").Call(back, oldBrush)
		gdi("SelectObject").Call(back, oldPen)
		gdi("DeleteObject").Call(pen)
	}
	// Instruction banner on the primary monitor (virtual origin 0,0).
	text, _ := syscall.UTF16FromString("  Atlas: drag to mark a region, or click an element.  Esc or right-click cancels.  ")
	bx, by := -sel.origin.X+24, -sel.origin.Y+24
	tr := winRect{Left: int32(bx), Top: int32(by), Right: int32(bx + 900), Bottom: int32(by + 44)}
	if sel.font != 0 {
		gdi("SelectObject").Call(back, sel.font)
	}
	gdi("SetBkMode").Call(back, 2) // OPAQUE
	gdi("SetBkColor").Call(back, 0x00201A17)
	gdi("SetTextColor").Call(back, 0x00FFFFFF)
	user("DrawTextW").Call(back, uintptr(unsafe.Pointer(&text[0])), uintptr(len(text)-1), uintptr(unsafe.Pointer(&tr)), 0x0100|0x0020|0x0004) // NOCLIP|SINGLELINE|VCENTER
	gdi("BitBlt").Call(hdc, 0, 0, uintptr(w), uintptr(h), back, 0, 0, 0x00CC0020)
	gdi("SelectObject").Call(back, old)
	gdi("DeleteObject").Call(bmp)
	gdi("DeleteDC").Call(back)
}

type wndClassEx struct {
	Size, Style                        uint32
	Proc                               uintptr
	ClsExtra, WndExtra                 int32
	Instance, Icon, Cursor, Background uintptr
	Menu, Name                         *uint16
	IconSm                             uintptr
}

type msgT struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct{ X, Y int32 }
	Private uint32
}

// Selection is the raw result of the overlay.
type Selection struct {
	Rect     Rect
	Click    bool
	Monitors []Monitor
	Full     *image.RGBA
	Origin   Rect
}

// Select shows the marking overlay and blocks until the user marks a
// region (drag), an element (click) or cancels. The screen is captured
// before the overlay appears and the overlay shows that frozen image, so
// nothing the user does reaches the application underneath.
func Select(ctx context.Context) (Selection, error) {
	selectM.Lock()
	defer selectM.Unlock()
	type out struct {
		s   Selection
		err error
	}
	ch := make(chan out, 1)
	go func() {
		// The thread is intentionally never unlocked: it may still hold a
		// WM_QUIT for the destroyed overlay, and a locked goroutine that exits
		// takes its OS thread with it instead of returning it to the pool.
		runtime.LockOSThread()
		defer perMonitorAware()()
		s, err := runSelect(ctx)
		ch <- out{s, err}
	}()
	r := <-ch
	return r.s, r.err
}

func runSelect(ctx context.Context) (Selection, error) {
	mons, err := monitors()
	if err != nil {
		return Selection{}, err
	}
	vb := VirtualBounds(mons)
	capture, err := captureDIB(vb)
	if err != nil {
		return Selection{}, err
	}
	defer capture.release()
	full := capture.image()
	sel.origin, sel.capture = vb, capture
	sel.down, sel.dragging, sel.click, sel.canceled, sel.done, sel.result = false, false, false, false, false, nil
	face, _ := syscall.UTF16PtrFromString("Segoe UI")
	font, _, _ := gdi("CreateFontW").Call(^uintptr(21), 0, 0, 0, 600, 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(face)))
	sel.font = font
	defer func() {
		if font != 0 {
			gdi("DeleteObject").Call(font)
		}
		sel.font, sel.capture = 0, nil
	}()
	instance, _, _ := kernel.NewProc("GetModuleHandleW").Call(0)
	name, _ := syscall.UTF16PtrFromString(fmt.Sprintf("AtlasMarkOverlay%d", time.Now().UnixNano()))
	wc := wndClassEx{Proc: overlayProc, Instance: instance, Name: name}
	wc.Size = uint32(unsafe.Sizeof(wc))
	if atom, _, err := user("RegisterClassExW").Call(uintptr(unsafe.Pointer(&wc))); atom == 0 {
		return Selection{}, fmt.Errorf("register overlay class: %v", err)
	}
	defer user("UnregisterClassW").Call(uintptr(unsafe.Pointer(name)), instance)
	const wsPopup, wsVisible = 0x80000000, 0x10000000
	const wsExTopmost, wsExToolWindow = 0x00000008, 0x00000080
	hwnd, _, err := user("CreateWindowExW").Call(wsExTopmost|wsExToolWindow, uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(name)), wsPopup|wsVisible,
		uintptr(int32(vb.X)), uintptr(int32(vb.Y)), uintptr(vb.W), uintptr(vb.H), 0, 0, instance, 0)
	if hwnd == 0 {
		return Selection{}, fmt.Errorf("create overlay: %v", err)
	}
	// The overlay itself must never appear in any later capture.
	user("SetWindowDisplayAffinity").Call(hwnd, 0x11)
	user("SetForegroundWindow").Call(hwnd)
	user("SetFocus").Call(hwnd)
	deadline := time.Now().Add(2 * time.Minute)
	var m msgT
	for !sel.done {
		if ctx.Err() != nil || time.Now().After(deadline) {
			sel.canceled, sel.done = true, true
			user("DestroyWindow").Call(hwnd)
		}
		// PeekMessage keeps the loop responsive to ctx and the deadline.
		if ok, _, _ := user("PeekMessageW").Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0, 1); ok != 0 {
			if m.Message == 0x0012 { // WM_QUIT
				break
			}
			user("TranslateMessage").Call(uintptr(unsafe.Pointer(&m)))
			user("DispatchMessageW").Call(uintptr(unsafe.Pointer(&m)))
			continue
		}
		user("MsgWaitForMultipleObjects").Call(0, 0, 0, 50, 0x04FF)
	}
	if sel.canceled || sel.result == nil {
		return Selection{}, ErrCanceled
	}
	return Selection{Rect: *sel.result, Click: sel.click, Monitors: mons, Full: full, Origin: vb}, nil
}

// Capture builds a Mark from a user selection: crop from the frozen frame,
// window under the region, and (for click marks) the UIA element, whose
// bounds then become the mark.
func CaptureMark(ctx context.Context, note string) (Mark, []byte, error) {
	s, err := Select(ctx)
	if err != nil {
		return Mark{}, nil, err
	}
	m := Mark{ID: NewID(), CreatedAt: time.Now().UnixMilli(), Source: "user_mark", Mode: "region", Note: strings.TrimSpace(note), Rect: s.Rect, Monitors: s.Monitors, OverlayExcluded: true}
	cx, cy := s.Rect.Center()
	if s.Click {
		m.Mode = "element"
		if el, err := elementAt(ctx, cx, cy); err == nil && el.Verified && !el.Bounds.Empty() {
			m.Element = el
			m.Rect = el.Bounds.Intersect(VirtualBounds(s.Monitors))
		} else {
			// Without a verified element a click marks a small region.
			m.Mode = "point"
			m.Rect = Rect{X: cx - 24, Y: cy - 24, W: 48, H: 48}.Intersect(VirtualBounds(s.Monitors))
			if el != nil {
				m.Element = el
			}
		}
	} else if el, err := elementAt(ctx, cx, cy); err == nil {
		m.Element = el
	}
	if m.Rect.Empty() {
		return Mark{}, nil, errors.New("the marked region is empty")
	}
	m.Monitor, _ = MonitorFor(m.Rect, s.Monitors)
	runtime.LockOSThread()
	restore := perMonitorAware()
	if hwnd, ok := topWindowAt(cx, cy, 0); ok {
		if w, ok := windowInfo(hwnd); ok {
			m.Window = &w
			m.Offset = &Rect{X: m.Rect.X - w.Rect.X, Y: m.Rect.Y - w.Rect.Y, W: m.Rect.W, H: m.Rect.H}
		}
	}
	restore()
	runtime.UnlockOSThread()
	crop := s.Full.SubImage(image.Rect(m.Rect.X-s.Origin.X, m.Rect.Y-s.Origin.Y, m.Rect.Right()-s.Origin.X, m.Rect.Bottom()-s.Origin.Y))
	var buf bytes.Buffer
	if err := png.Encode(&buf, crop); err != nil {
		return Mark{}, nil, err
	}
	m.Validity = Validity{State: Fresh, CheckedAt: time.Now().UnixMilli()}
	return m, buf.Bytes(), nil
}
