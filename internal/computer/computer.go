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

// ValidatePoint rejects negative coordinates before they reach the OS.
func ValidatePoint(x, y int) error {
	if x < 0 || y < 0 {
		return fmt.Errorf("invalid screen coordinates (%d, %d): must be non-negative", x, y)
	}
	return nil
}

// ValidatePointIn rejects coordinates outside the given screen bounds.
// The upper bound matters as much as the lower one: an out-of-range
// coordinate would otherwise land silently clamped at the screen edge,
// and the agent would act on the wrong pixel believing it had aimed
// correctly. Callers that do not know the screen size yet fall back to
// ValidatePoint.
func ValidatePointIn(size Size, x, y int) error {
	if err := ValidatePoint(x, y); err != nil {
		return err
	}
	if x >= size.Width || y >= size.Height {
		return fmt.Errorf(
			"invalid screen coordinates (%d, %d): outside the %d x %d screen",
			x, y, size.Width, size.Height,
		)
	}
	return nil
}

// ParseButton maps a user string to a MouseButton. Empty means left,
// matching what a person asking for "a click" expects.
func ParseButton(s string) (MouseButton, error) {
	switch s {
	case "", "left":
		return ButtonLeft, nil
	case "right":
		return ButtonRight, nil
	case "middle":
		return ButtonMiddle, nil
	default:
		return "", fmt.Errorf("unknown mouse button %q: want left, right, or middle", s)
	}
}

// KeyNames maps every named key KeyPress and Hotkey accept to its
// Win32 virtual-key code. Single characters need no entry: backends
// type those as Unicode. The codes are plain numbers so validation
// and tests stay platform-independent; the Windows backend passes
// them to SendInput as-is.
var KeyNames = map[string]uint16{
	"enter": 0x0D, "tab": 0x09, "esc": 0x1B, "escape": 0x1B,
	"space": 0x20, "backspace": 0x08, "delete": 0x2E,
	"insert": 0x2D, "home": 0x24, "end": 0x23,
	"pageup": 0x21, "pagedown": 0x22,
	"up": 0x26, "down": 0x28, "left": 0x25, "right": 0x27,
	"f1": 0x70, "f2": 0x71, "f3": 0x72, "f4": 0x73,
	"f5": 0x74, "f6": 0x75, "f7": 0x76, "f8": 0x77,
	"f9": 0x78, "f10": 0x79, "f11": 0x7A, "f12": 0x7B,
	"shift": 0x10, "ctrl": 0x11, "alt": 0x12, "win": 0x5B,
	"capslock": 0x14, "numlock": 0x90, "scrolllock": 0x91,
	"printscreen": 0x2C, "pause": 0x13,
}

// ResolveKey validates a KeyPress/Hotkey key name. It returns the
// virtual-key code and true for named keys, 0 and true for a single
// character (typed as Unicode by the backend), and false for anything
// else.
func ResolveKey(key string) (uint16, bool) {
	if vk, ok := KeyNames[key]; ok {
		return vk, true
	}
	if len([]rune(key)) == 1 {
		return 0, true
	}
	return 0, false
}

