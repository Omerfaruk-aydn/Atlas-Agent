// Package computer drives the local desktop: screenshots plus mouse
// and keyboard input. It is the execution backend behind the agent's
// computer-use tool, the same way internal/browser backs the browser
// tool. Platform specifics live in the per-OS files; this file holds
// the Backend contract and the platform-independent validation every
// caller gets regardless of OS.
package computer

import (
	"errors"
	"fmt"
)

// ErrUnsupportedPlatform is returned when computer control is used on
// an OS without a backend. Computer-use ships Windows-first; other
// platforms report this error instead of failing obscurely.
var ErrUnsupportedPlatform = errors.New(
	"computer-use is currently supported on Windows only",
)

// Point is a screen coordinate in physical pixels. Coordinates line up
// 1:1 with the pixels of the PNG Screenshot returns.
type Point struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// Size is a screen dimension in physical pixels.
type Size struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// MouseButton selects which button a click or drag uses.
type MouseButton string

const (
	ButtonLeft   MouseButton = "left"
	ButtonRight  MouseButton = "right"
	ButtonMiddle MouseButton = "middle"
)

// Backend moves the pointer, clicks, types, and captures the screen.
// Every method is synchronous: it returns once the OS has accepted the
// input or the capture is complete.
type Backend interface {
	// ScreenSize reports the virtual screen dimensions in pixels.
	ScreenSize() (Size, error)
	// Screenshot captures the virtual screen and returns it as PNG.
	Screenshot() ([]byte, error)
	// CursorPosition reports the current pointer location.
	CursorPosition() (Point, error)
	// MoveTo places the pointer at x, y.
	MoveTo(x, y int) error
	// Click presses and releases button at x, y.
	Click(x, y int, button MouseButton) error
	// DoubleClick presses and releases the left button twice at x, y.
	DoubleClick(x, y int) error
	// Drag holds the left button at x, y, moves to endX, endY, releases.
	Drag(x, y, endX, endY int) error
	// Scroll emits vertical (dy) and horizontal (dx) wheel notches at
	// the current pointer location. Positive dy scrolls up.
	Scroll(dx, dy int) error
	// TypeText types text as Unicode keystrokes at the current focus.
	TypeText(text string) error
	// KeyPress presses and releases one named key (see ResolveKey).
	KeyPress(key string) error
	// Hotkey holds modifiers (any of ctrl, alt, shift, win), presses
	// key, then releases in reverse order.
	Hotkey(modifiers []string, key string) error
}

// Open returns the Backend for the current OS, or
// ErrUnsupportedPlatform where none exists.
func Open() (Backend, error) {
	return openPlatform()
}

// OpenOrNil returns the Backend for the current OS, or nil where none
// exists. Callers that degrade gracefully (the agent tool reports a
// clear error per call) prefer this over Open.
func OpenOrNil() Backend {
	backend, err := openPlatform()
	if err != nil {
		return nil
	}
	return backend
}
