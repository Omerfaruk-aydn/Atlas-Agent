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

	procSelectObject.Call(memDC, bitmap)
	ok, _, _ := procBitBlt.Call(
		memDC, 0, 0, uintptr(w), uintptr(h),
		screenDC, uintptr(x), uintptr(y), srccopy,
	)
	if ok == 0 {
		return nil, fmt.Errorf("computer-use: BitBlt failed")
	}

	// Top-down 32-bit DIB: rows arrive BGRA, first row first.
	info := bitmapInfo{
		Header: bitmapInfoHeader{
			Size:        uint32(unsafe.Sizeof(bitmapInfoHeader{})),
			Width:       int32(w),
			Height:      -int32(h),
			Planes:      1,
			BitCount:    32,
			Compression: biRGB,
		},
	}
	pixels := make([]byte, 4*w*h)
	ok, _, _ = procGetDIBits.Call(
		memDC, bitmap, 0, uintptr(h),
		uintptr(unsafe.Pointer(&pixels[0])),
		uintptr(unsafe.Pointer(&info)),
		0, // DIB_RGB_COLORS
	)
	if ok == 0 {
		return nil, fmt.Errorf("computer-use: GetDIBits failed")
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

