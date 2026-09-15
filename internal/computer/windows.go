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

