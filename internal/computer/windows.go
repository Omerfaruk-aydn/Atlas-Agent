//go:build windows

package computer

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"log/slog"
	"time"
	"unicode/utf16"
	"unsafe"

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

	procCreateCompatibleDC     = modGdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap = modGdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject           = modGdi32.NewProc("SelectObject")
	procBitBlt                 = modGdi32.NewProc("BitBlt")
	procGetDIBits              = modGdi32.NewProc("GetDIBits")
	procDeleteObject           = modGdi32.NewProc("DeleteObject")
	procDeleteDC               = modGdi32.NewProc("DeleteDC")
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
// palette entry GetDIBits requires even for 32-bit captures.
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

type windowsBackend struct{}

func openPlatform() (Backend, error) {
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
	defer procDeleteDC.Call(memDC)

	bitmap, _, _ := procCreateCompatibleBitmap.Call(
		screenDC, uintptr(w), uintptr(h),
	)
	if bitmap == 0 {
		return nil, fmt.Errorf("computer-use: CreateCompatibleBitmap failed")
	}
	defer procDeleteObject.Call(bitmap)

	old, _, _ := procSelectObject.Call(memDC, bitmap)
	if old == 0 {
		return nil, fmt.Errorf("computer-use: SelectObject failed")
	}
	pixels := make([]byte, 4*w*h)
	// One retry around the blit plus readback: a frame can tear if
	// the display mode changes mid-capture (resolution switch, monitor
	// plug/unplug, RDP reconnect), and the second attempt then lands
	// on a stable desktop.
	if err := captureWithRetry(func() error {
		return blitAndRead(memDC, bitmap, screenDC, x, y, w, h, pixels)
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

// blitAndRead copies the virtual screen into bitmap through memDC and
// reads the pixels back into a top-down 32-bit buffer. A failure here
// with a working cursor and screen size almost always means there is
// no readable desktop right now, so the errors name the usual causes
// instead of just the API name.
func blitAndRead(memDC, bitmap, screenDC uintptr, x, y, w, h int, pixels []byte) error {
	ok, _, _ := procBitBlt.Call(
		memDC, 0, 0, uintptr(w), uintptr(h),
		screenDC, uintptr(x), uintptr(y), srccopy,
	)
	if ok == 0 {
		return fmt.Errorf("computer-use: BitBlt failed (%s)", captureHint())
	}
	info := bitmapInfo{Header: dibHeader(w, h)}
	ok, _, _ = procGetDIBits.Call(
		memDC, bitmap, 0, uintptr(h),
		uintptr(unsafe.Pointer(&pixels[0])),
		uintptr(unsafe.Pointer(&info)),
		0, // DIB_RGB_COLORS
	)
	if ok == 0 {
		return fmt.Errorf("computer-use: GetDIBits failed (%s)", captureHint())
	}
	return nil
}

// captureHint names the environmental causes of a capture failure.
// Screen size and cursor reads need no video output, so they keep
// working while the desktop itself is unreadable.
func captureHint() string {
	return "no readable desktop: the workstation may be locked, the RDP window minimized, or a secure desktop (UAC prompt) on screen; restore and retry"
}

// dibHeader builds the top-down 32-bit DIB descriptor GetDIBits
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
	return Point{X: int(pt.X), Y: int(pt.Y)}, nil
}

func (b *windowsBackend) MoveTo(x, y int) error {
	if err := ValidatePoint(x, y); err != nil {
		return err
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
	return nil
}

func setCursorPos(x, y int) error {
	ok, _, _ := procSetCursorPos.Call(uintptr(x), uintptr(y))
	if ok == 0 {
		return fmt.Errorf("computer-use: SetCursorPos(%d, %d) failed", x, y)
	}
	return nil
}

// sendInputs submits inputs through SendInput and reports how many
// the system accepted. A partial acceptance is retried once: elevated
// (UAC) windows and secure-desktop transitions can drop the first
// batch while the input queue settles.
func sendInputs(inputs []winInput) error {
	if len(inputs) == 0 {
		return nil
	}
	if err := sendInputsOnce(inputs); err != nil {
		slog.Warn("SendInput partially accepted, retrying", "error", err)
		time.Sleep(50 * time.Millisecond)
		return sendInputsOnce(inputs)
	}
	return nil
}

func sendInputsOnce(inputs []winInput) error {
	size := unsafe.Sizeof(winInput{})
	n, _, err := procSendInput.Call(
		uintptr(len(inputs)),
		uintptr(unsafe.Pointer(&inputs[0])),
		size,
	)
	if int(n) != len(inputs) {
		return fmt.Errorf("computer-use: SendInput accepted %d of %d inputs: %v", n, len(inputs), err)
	}
	return nil
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
	return sendInputs(mouseClick(button))
}

func (b *windowsBackend) DoubleClick(x, y int) error {
	if err := ValidatePoint(x, y); err != nil {
		return err
	}
	if err := b.MoveTo(x, y); err != nil {
		return err
	}
	time.Sleep(10 * time.Millisecond)
	clicks := append(mouseClick(ButtonLeft), mouseClick(ButtonLeft)...)
	return sendInputs(clicks)
}

func (b *windowsBackend) Drag(x, y, endX, endY int) error {
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
	// Press the button first: the down stroke must already be down
	// while the pointer walks, or the target sees a click at the
	// release point instead of a drag.
	if err := sendInputs([]winInput{
		{Type: inputMouse, Payload: mousePayload(0, 0, 0, mouseLeftDown)},
	}); err != nil {
		return err
	}
	// Walk the pointer in small steps so hover states track the drag.
	const steps = 20
	for i := 1; i <= steps; i++ {
		ix := x + (endX-x)*i/steps
		iy := y + (endY-y)*i/steps
		if err := b.MoveTo(ix, iy); err != nil {
			return err
		}
		time.Sleep(time.Millisecond)
	}
	return sendInputs([]winInput{
		{Type: inputMouse, Payload: mousePayload(0, 0, 0, mouseLeftUp)},
	})
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
	mods, err := ParseModifiers(modifiers)
	if err != nil {
		return err
	}
	if len(mods) == 0 {
		return fmt.Errorf("computer-use: hotkey needs at least one modifier")
	}
	vk, ok := ResolveKey(key)
	if !ok {
		return fmt.Errorf("computer-use: unknown key %q", key)
	}
	var inputs []winInput
	for _, m := range mods {
		mv := modifierVKs[m]
		inputs = append(inputs, winInput{
			Type:    inputKeyboard,
			Payload: keyboardPayload(mv, 0, 0),
		})
	}
	if vk == 0 {
		for _, unit := range utf16.Encode([]rune(key)) {
			inputs = append(inputs, typeUnicode(unit)...)
		}
	} else {
		inputs = append(inputs, pressVK(vk)...)
	}
	for i := len(mods) - 1; i >= 0; i-- {
		mv := modifierVKs[mods[i]]
		inputs = append(inputs, winInput{
			Type:    inputKeyboard,
			Payload: keyboardPayload(mv, 0, keyUp),
		})
	}
	return sendInputs(inputs)
}
