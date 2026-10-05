//go:build windows

package computer

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/png"
	"log/slog"
	"runtime"
	"sync/atomic"
	"time"
	"unicode/utf16"
	"unsafe"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/activity"

	"golang.org/x/sys/windows"
)

// This backend talks to Win32 directly through golang.org/x/sys — no
// CGO, no third-party input library — so the CGO-disabled release
// builds keep working unchanged.

var (
	modUser32 = windows.NewLazySystemDLL("user32.dll")
	modGdi32  = windows.NewLazySystemDLL("gdi32.dll")

	procGetSystemMetrics = modUser32.NewProc("GetSystemMetrics")
	procGetDC            = modUser32.NewProc("GetDC")
	procReleaseDC        = modUser32.NewProc("ReleaseDC")
	procSetCursorPos     = modUser32.NewProc("SetCursorPos")
	procGetCursorPos     = modUser32.NewProc("GetCursorPos")
	procSendInput        = modUser32.NewProc("SendInput")

	procCreateCompatibleDC = modGdi32.NewProc("CreateCompatibleDC")
	procCreateDIBSection   = modGdi32.NewProc("CreateDIBSection")
	procSelectObject       = modGdi32.NewProc("SelectObject")
	procBitBlt             = modGdi32.NewProc("BitBlt")
	procGdiFlush           = modGdi32.NewProc("GdiFlush")
	procDeleteObject       = modGdi32.NewProc("DeleteObject")
	procDeleteDC           = modGdi32.NewProc("DeleteDC")
)

const (
	smXVirtualScreen  = 76
	smYVirtualScreen  = 77
	smCXVirtualScreen = 78
	smCYVirtualScreen = 79

	srccopy = 0x00CC0020
	biRGB   = 0

	inputMouse    = 0
	inputKeyboard = 1

	mouseMove       = 0x0001
	mouseLeftDown   = 0x0002
	mouseLeftUp     = 0x0004
	mouseRightDown  = 0x0008
	mouseRightUp    = 0x0010
	mouseMiddleDown = 0x0020
	mouseMiddleUp   = 0x0040
	mouseWheel      = 0x0800
	mouseHWheel     = 0x1000

	keyUp      = 0x0002
	keyUnicode = 0x0004

	wheelDelta = 120
)

// winPoint mirrors the Win32 POINT struct.
type winPoint struct {
	X int32
	Y int32
}

// winInput mirrors the Win32 INPUT struct on 64-bit Windows: a DWORD
// type plus 32 payload bytes (the size of MOUSEINPUT, the largest
// union member). The payload is filled with encoding/binary so the
// layout never depends on Go struct padding.
type winInput struct {
	Type    uint32
	_       uint32
	Payload [32]byte
}

func mousePayload(dx, dy int32, data, flags uint32) [32]byte {
	var p [32]byte
	binary.LittleEndian.PutUint32(p[0:4], uint32(dx))
	binary.LittleEndian.PutUint32(p[4:8], uint32(dy))
	binary.LittleEndian.PutUint32(p[8:12], data)
	binary.LittleEndian.PutUint32(p[12:16], flags)
	return p
}

func keyboardPayload(vk, scan uint16, flags uint32) [32]byte {
	var p [32]byte
	binary.LittleEndian.PutUint16(p[0:2], vk)
	binary.LittleEndian.PutUint16(p[2:4], scan)
	binary.LittleEndian.PutUint32(p[4:8], flags)
	return p
}

// bitmapInfoHeader mirrors BITMAPINFOHEADER; bitmapInfo adds the single
// palette entry used by BITMAPINFO even for 32-bit captures.
type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type bitmapInfo struct {
	Header bitmapInfoHeader
	Colors uint32
}

type windowsBackend struct {
	pointerFeedback atomic.Pointer[pointerFeedback]
	pointerHeld     atomic.Bool
}

func openPlatform() (Backend, error) {
	// Use physical desktop pixels consistently on mixed-DPI displays.
	dpi := modUser32.NewProc("SetProcessDpiAwarenessContext")
	if dpi.Find() == nil {
		_, _, _ = dpi.Call(^uintptr(3))
	}
	return &windowsBackend{}, nil
}

func getSystemMetrics(n int) int {
	r, _, _ := procGetSystemMetrics.Call(uintptr(n))
	return int(r)
}

func (b *windowsBackend) ScreenSize() (Size, error) {
	w := getSystemMetrics(smCXVirtualScreen)
	h := getSystemMetrics(smCYVirtualScreen)
	if w <= 0 || h <= 0 {
		return Size{}, fmt.Errorf("computer-use: unexpected screen size %dx%d", w, h)
	}
	return Size{Width: w, Height: h}, nil
}

func (b *windowsBackend) Screenshot() ([]byte, error) {
	defer activity.Default.SuspendForObservation()()
	// Screen DCs must be released on the thread that acquired them.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	x := getSystemMetrics(smXVirtualScreen)
	y := getSystemMetrics(smYVirtualScreen)
	w := getSystemMetrics(smCXVirtualScreen)
	h := getSystemMetrics(smCYVirtualScreen)
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("computer-use: unexpected screen size %dx%d", w, h)
	}

	screenDC, _, _ := procGetDC.Call(0)
	if screenDC == 0 {
		return nil, fmt.Errorf("computer-use: GetDC failed")
	}
	defer procReleaseDC.Call(0, screenDC)

	memDC, _, _ := procCreateCompatibleDC.Call(screenDC)
	if memDC == 0 {
		return nil, fmt.Errorf("computer-use: CreateCompatibleDC failed")
	}

	info := bitmapInfo{Header: dibHeader(w, h)}
	var bits unsafe.Pointer
	bitmap, _, callErr := procCreateDIBSection.Call(
		screenDC, uintptr(unsafe.Pointer(&info)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0,
	)
	if bitmap == 0 || bits == nil {
		procDeleteDC.Call(memDC)
		if bitmap != 0 {
			procDeleteObject.Call(bitmap)
		}
		return nil, fmt.Errorf("computer-use: CreateDIBSection failed: %v", callErr)
	}
	defer func() {
		// Delete the DC first even if restoring its bitmap failed.
		procDeleteDC.Call(memDC)
		procDeleteObject.Call(bitmap)
	}()

	pixels := unsafe.Slice((*byte)(bits), 4*w*h)
	// One retry around the blit plus readback: a frame can tear if
	// the display mode changes mid-capture (resolution switch, monitor
	// plug/unplug, RDP reconnect), and the second attempt then lands
	// on a stable desktop.
	if err := captureWithRetry(func() error {
		return blitBitmap(memDC, bitmap, screenDC, x, y, w, h)
	}); err != nil {
		return nil, err
	}

	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < w*h; i++ {
		// BGRA to NRGBA. Screenshots are opaque, so no un-premultiplying.
		img.Pix[4*i] = pixels[4*i+2]
		img.Pix[4*i+1] = pixels[4*i+1]
		img.Pix[4*i+2] = pixels[4*i]
		img.Pix[4*i+3] = 0xFF
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("computer-use: PNG encode failed: %w", err)
	}
	return buf.Bytes(), nil
}

// captureWithRetry runs capture, retrying once when the first attempt
// fails. A single retry covers transient frames; a persistent failure
// is returned as-is so a dead display fails fast instead of hanging
// the agent loop.
func captureWithRetry(capture func() error) error {
	if err := capture(); err != nil {
		return capture()
	}
	return nil
}

// blitBitmap copies the screen and synchronizes direct DIB memory access.
func blitBitmap(memDC, bitmap, screenDC uintptr, x, y, w, h int) error {
	err := withCaptureBitmap(func() (uintptr, error) {
		old, _, callErr := procSelectObject.Call(memDC, bitmap)
		if old == 0 || old == ^uintptr(0) {
			return 0, fmt.Errorf("computer-use: SelectObject failed: %v", callErr)
		}
		return old, nil
	}, func(old uintptr) error {
		previous, _, callErr := procSelectObject.Call(memDC, old)
		if previous == 0 || previous == ^uintptr(0) {
			return fmt.Errorf("computer-use: bitmap restoration failed: %v", callErr)
		}
		return nil
	}, func() error {
		ok, _, callErr := procBitBlt.Call(
			memDC, 0, 0, uintptr(w), uintptr(h),
			screenDC, uintptr(x), uintptr(y), srccopy,
		)
		if ok == 0 {
			return fmt.Errorf("computer-use: BitBlt failed: %v (%s)", callErr, captureHint())
		}
		return nil
	})
	if err != nil {
		return err
	}
	ok, _, callErr := procGdiFlush.Call()
	if ok == 0 {
		return fmt.Errorf("computer-use: GdiFlush failed: %v", callErr)
	}
	return nil
}

// withCaptureBitmap restores the DC before readback or bitmap destruction.
func withCaptureBitmap(selectBitmap func() (uintptr, error), restore func(uintptr) error, blit func() error) (err error) {
	old, err := selectBitmap()
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, restore(old)) }()
	return blit()
}

// captureHint suggests checks without claiming an environmental diagnosis.
func captureHint() string {
	return "cause not determined; check whether the workstation is locked, the RDP window minimized, or a secure desktop (UAC prompt) is active, then retry"
}

// dibHeader builds the top-down 32-bit DIB descriptor CreateDIBSection
// expects: rows arrive BGRA, first row first.
func dibHeader(w, h int) bitmapInfoHeader {
	return bitmapInfoHeader{
		Size:        uint32(unsafe.Sizeof(bitmapInfoHeader{})),
		Width:       int32(w),
		Height:      -int32(h),
		Planes:      1,
		BitCount:    32,
		Compression: biRGB,
	}
}

func (b *windowsBackend) CursorPosition() (Point, error) {
	var pt winPoint
	ok, _, _ := procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	if ok == 0 {
		return Point{}, fmt.Errorf("computer-use: GetCursorPos failed")
	}
	return Point{X: int(pt.X) - getSystemMetrics(smXVirtualScreen), Y: int(pt.Y) - getSystemMetrics(smYVirtualScreen)}, nil
}

func (b *windowsBackend) MoveTo(x, y int) error {
	if err := ValidatePoint(x, y); err != nil {
		return err
	}
	if b.pointerFeedback.Load() != nil && !b.pointerHeld.Load() {
		if start, err := b.CursorPosition(); err == nil {
			if err := b.presentPointer("origin", start.X, start.Y); err != nil {
				return err
			}
		}
	}
	if err := setCursorPos(x, y); err != nil {
		return err
	}
	// Verify the pointer actually arrived: remote-desktop overlays,
	// cursor clipping (ClipCursor), and UAC prompts can all swallow a
	// move that the API reported as successful. One retry covers the
	// transient cases; a persistent mismatch is reported with both
	// positions so the caller can re-aim instead of clicking blind.
	pos, err := b.CursorPosition()
	if err != nil {
		return err
	}
	if pos.X == x && pos.Y == y {
		b.emitPointer("move", x, y)
		return nil
	}
	if err := setCursorPos(x, y); err != nil {
		return err
	}
	pos, err = b.CursorPosition()
	if err != nil {
		return err
	}
	if pos.X != x || pos.Y != y {
		return fmt.Errorf(
			"computer-use: pointer is at (%d, %d) after moving to (%d, %d)",
			pos.X, pos.Y, x, y,
		)
	}
	b.emitPointer("move", x, y)
	return nil
}

func setCursorPos(x, y int) error {
	x += getSystemMetrics(smXVirtualScreen)
	y += getSystemMetrics(smYVirtualScreen)
	ok, _, _ := procSetCursorPos.Call(uintptr(x), uintptr(y))
	if ok == 0 {
		return fmt.Errorf("computer-use: SetCursorPos(%d, %d) failed", x, y)
	}
	return nil
}

// sendInputs retries only the unaccepted tail, avoiding duplicate keystrokes.
func sendInputs(inputs []winInput) error {
	return sendInputBatch(inputs, sendInputsOnce)
}

func sendInputBatch(inputs []winInput, submit func([]winInput) (int, error)) error {
	if len(inputs) == 0 {
		return nil
	}
	if accepted, err := submit(inputs); err != nil {
		if accepted < 0 || accepted >= len(inputs) {
			return err
		}
		slog.Warn("SendInput partially accepted, retrying remaining input", "error", err)
		time.Sleep(50 * time.Millisecond)
		_, err = submit(inputs[accepted:])
		return err
	}
	return nil
}

func sendInputsOnce(inputs []winInput) (int, error) {
	size := unsafe.Sizeof(winInput{})
	n, _, err := procSendInput.Call(
		uintptr(len(inputs)),
		uintptr(unsafe.Pointer(&inputs[0])),
		size,
	)
	if int(n) != len(inputs) {
		return int(n), fmt.Errorf("computer-use: SendInput accepted %d of %d inputs: %v", n, len(inputs), err)
	}
	return int(n), nil
}

func mouseClick(button MouseButton) []winInput {
	var down, up uint32
	switch button {
	case ButtonRight:
		down, up = mouseRightDown, mouseRightUp
	case ButtonMiddle:
		down, up = mouseMiddleDown, mouseMiddleUp
	default:
		down, up = mouseLeftDown, mouseLeftUp
	}
	return []winInput{
		{Type: inputMouse, Payload: mousePayload(0, 0, 0, down)},
		{Type: inputMouse, Payload: mousePayload(0, 0, 0, up)},
	}
}

func (b *windowsBackend) Click(x, y int, button MouseButton) error {
	if err := ValidatePoint(x, y); err != nil {
		return err
	}
	if err := b.MoveTo(x, y); err != nil {
		return err
	}
	// A beat between down and up: some targets ignore zero-width clicks.
	time.Sleep(10 * time.Millisecond)
	if err := b.aimPointer(x, y); err != nil {
		return err
	}
	err := sendInputs(mouseClick(button))
	if err == nil {
		b.emitPointer("click", x, y)
	}
	return err
}

func (b *windowsBackend) DoubleClick(x, y int) error {
	if err := ValidatePoint(x, y); err != nil {
		return err
	}
	if err := b.MoveTo(x, y); err != nil {
		return err
	}
	time.Sleep(10 * time.Millisecond)
	if err := b.aimPointer(x, y); err != nil {
		return err
	}
	clicks := append(mouseClick(ButtonLeft), mouseClick(ButtonLeft)...)
	err := sendInputs(clicks)
	if err == nil {
		b.emitPointer("click", x, y)
	}
	return err
}

func (b *windowsBackend) Drag(x, y, endX, endY int) (resultErr error) {
	if err := ValidatePoint(x, y); err != nil {
		return err
	}
	if err := ValidatePoint(endX, endY); err != nil {
		return err
	}
	if err := b.MoveTo(x, y); err != nil {
		return err
	}
	time.Sleep(10 * time.Millisecond)
	if err := b.aimPointer(x, y); err != nil {
		return err
	}
	// Press the button first: the down stroke must already be down
	// while the pointer walks, or the target sees a click at the
	// release point instead of a drag.
	if err := sendInputs([]winInput{
		{Type: inputMouse, Payload: mousePayload(0, 0, 0, mouseLeftDown)},
	}); err != nil {
		return err
	}
	b.pointerHeld.Store(true)
	b.emitPointer("drag", x, y)
	lastX, lastY := x, y
	defer func() {
		releaseErr := sendInputs([]winInput{{Type: inputMouse, Payload: mousePayload(0, 0, 0, mouseLeftUp)}})
		if releaseErr == nil {
			b.pointerHeld.Store(false)
			b.emitPointer("release", lastX, lastY)
		}
		resultErr = errors.Join(resultErr, releaseErr)
	}()
	// Walk the pointer in small steps so hover states track the drag.
	const steps = 20
	for i := 1; i <= steps; i++ {
		if owner := b.pointerFeedback.Load(); owner != nil && owner.ctx.Err() != nil {
			return owner.ctx.Err()
		}
		ix := x + (endX-x)*i/steps
		iy := y + (endY-y)*i/steps
		if err := b.MoveTo(ix, iy); err != nil {
			return err
		}
		lastX, lastY = ix, iy
		pace := time.Millisecond
		if b.pointerFeedback.Load() != nil {
			pace = 8 * time.Millisecond
		}
		time.Sleep(pace)
	}
	return nil
}

func (b *windowsBackend) Scroll(dx, dy int) error {
	var inputs []winInput
	for i := 0; i < abs(dy); i++ {
		// Negation applies to the variable, not the constant: negating
		// the constant itself overflows uint32 at compile time, while
		// negating a uint32 value wraps around to the two's-complement
		// encoding SendInput expects for a downward notch.
		data := uint32(wheelDelta)
		if dy < 0 {
			data = -data
		}
		inputs = append(inputs, winInput{
			Type:    inputMouse,
			Payload: mousePayload(0, 0, data, mouseWheel),
		})
	}
	for i := 0; i < abs(dx); i++ {
		data := uint32(wheelDelta)
		if dx < 0 {
			data = -data
		}
		inputs = append(inputs, winInput{
			Type:    inputMouse,
			Payload: mousePayload(0, 0, data, mouseHWheel),
		})
	}
	return sendInputs(inputs)
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// typeUnicode sends one UTF-16 code unit as a key down/up pair.
func typeUnicode(unit uint16) []winInput {
	return []winInput{
		{Type: inputKeyboard, Payload: keyboardPayload(0, unit, keyUnicode)},
		{Type: inputKeyboard, Payload: keyboardPayload(0, unit, keyUnicode|keyUp)},
	}
}

func (b *windowsBackend) TypeText(text string) error {
	if text == "" {
		return fmt.Errorf("computer-use: nothing to type")
	}
	var inputs []winInput
	for _, unit := range utf16.Encode([]rune(text)) {
		inputs = append(inputs, typeUnicode(unit)...)
	}
	return sendInputs(inputs)
}

func pressVK(vk uint16) []winInput {
	return []winInput{
		{Type: inputKeyboard, Payload: keyboardPayload(vk, 0, 0)},
		{Type: inputKeyboard, Payload: keyboardPayload(vk, 0, keyUp)},
	}
}

func (b *windowsBackend) KeyPress(key string) error {
	vk, ok := ResolveKey(key)
	if !ok {
		return fmt.Errorf("computer-use: unknown key %q", key)
	}
	if vk == 0 {
		// A single character: type it as Unicode.
		return b.TypeText(key)
	}
	return sendInputs(pressVK(vk))
}

var modifierVKs = map[string]uint16{
	ModCtrl:  0x11,
	ModAlt:   0x12,
	ModShift: 0x10,
	ModWin:   0x5B,
}

func (b *windowsBackend) Hotkey(modifiers []string, key string) error {
	inputs, err := hotkeyInputs(modifiers, key)
	if err != nil {
		return err
	}
	return sendInputs(inputs)
}

// hotkeyInputs uses physical virtual keys so modifiers trigger shortcuts.
func hotkeyInputs(modifiers []string, key string) ([]winInput, error) {
	mods, err := ParseModifiers(modifiers)
	if err != nil {
		return nil, err
	}
	if len(mods) == 0 {
		return nil, fmt.Errorf("computer-use: hotkey needs at least one modifier")
	}
	vk, ok := ResolveKey(key)
	if !ok {
		return nil, fmt.Errorf("computer-use: unknown key %q", key)
	}
	if vk == 0 {
		if len(key) == 1 && key[0] >= 'a' && key[0] <= 'z' {
			vk = uint16(key[0] - 'a' + 'A')
		} else if len(key) == 1 && (key[0] >= 'A' && key[0] <= 'Z' || key[0] >= '0' && key[0] <= '9') {
			vk = uint16(key[0])
		} else {
			return nil, fmt.Errorf("computer-use: hotkey requires a named key or ASCII letter/digit, got %q; Unicode text is not a keyboard shortcut", key)
		}
	}
	var inputs []winInput
	for _, m := range mods {
		mv := modifierVKs[m]
		inputs = append(inputs, winInput{
			Type:    inputKeyboard,
			Payload: keyboardPayload(mv, 0, 0),
		})
	}
	inputs = append(inputs, pressVK(vk)...)
	for i := len(mods) - 1; i >= 0; i-- {
		mv := modifierVKs[mods[i]]
		inputs = append(inputs, winInput{
			Type:    inputKeyboard,
			Payload: keyboardPayload(mv, 0, keyUp),
		})
	}
	return inputs, nil
}
