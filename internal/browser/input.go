package browser

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Input actions dispatch trusted CDP input for pages such as design
// canvases, where DOM targets do not exist. Pointer actions are bound to a
// viewport grant issued by a screenshot so stale coordinates are rejected.
const (
	InputPointerMove  = "pointer_move"
	InputPointerClick = "pointer_click"
	InputPointerDrag  = "pointer_drag"
	InputKeyCombo     = "key_combo"
	InputInsertText   = "insert_text"
)

// InputActions lists the typed input actions in a stable order.
var InputActions = []string{InputPointerMove, InputPointerClick, InputPointerDrag, InputKeyCombo, InputInsertText}

// IsInputAction reports whether action is one of InputActions.
func IsInputAction(action string) bool { return slices.Contains(InputActions, action) }

// IsPointerAction reports whether action needs a viewport grant.
func IsPointerAction(action string) bool {
	return strings.HasPrefix(action, "pointer_") && IsInputAction(action)
}

// viewportGrantTTL bounds how long a screenshot's coordinates stay usable
// even when the viewport identity has not changed.
const viewportGrantTTL = 5 * time.Minute

// maxViewportGrants bounds the per-session grant history.
const maxViewportGrants = 4

// Viewport identifies the visual state a screenshot captured and how its
// image pixels map to CSS pixels.
type Viewport struct {
	ID          string  `json:"viewport_id"`
	TabID       string  `json:"tab_id,omitempty"`
	CSSWidth    float64 `json:"css_width"`
	CSSHeight   float64 `json:"css_height"`
	ImageWidth  int     `json:"image_width"`
	ImageHeight int     `json:"image_height"`
	// Scale is image pixels per CSS pixel.
	Scale    float64   `json:"image_scale"`
	Captured time.Time `json:"-"`
}

// InputResult reports what reached the page.
type InputResult struct {
	Action          string `json:"action"`
	EventsSent      int    `json:"events_sent"`
	ViewportID      string `json:"viewport_id,omitempty"`
	ViewportChanged bool   `json:"viewport_changed,omitempty"`
	CleanupStatus   string `json:"cleanup_status,omitempty"`
	// Focus is the element insert_text was checked against before sending.
	Focus *FocusInfo `json:"focus,omitempty"`
	Next  string     `json:"next"`
}

// InputSession is optional so lightweight and mock drivers remain usable.
type InputSession interface {
	// ViewportScreenshot captures an annotated viewport screenshot and,
	// when the viewport stayed stable during capture, a grant for
	// pointer input against that image.
	ViewportScreenshot(ctx context.Context) ([]byte, Viewport, error)
	// Input runs one typed input action as an ordered sequence.
	Input(ctx context.Context, p Request) (InputResult, error)
}

// ErrStaleViewport reports pointer coordinates that no longer describe the
// page. Nothing was dispatched.
var ErrStaleViewport = errors.New("stale_viewport: the page scrolled, zoomed, resized, navigated or changed tab since that screenshot (or the grant expired); no input sent. Take a new screenshot and aim again from it")

// ErrInputCleanup means at least one held input could not be released.
// Further input must wait until recovery succeeds.
var ErrInputCleanup = errors.New("input_cleanup_failed")

// inputEvent is one CDP input command, optionally preceded by a pause.
type inputEvent struct {
	method string
	params map[string]any
	delay  time.Duration
}

const (
	cdpMouseEvent  = "Input.dispatchMouseEvent"
	cdpKeyEvent    = "Input.dispatchKeyEvent"
	cdpInsertText  = "Input.insertText"
	defaultDragSeg = 12
)

var modifierBits = map[string]int{"alt": 1, "ctrl": 2, "control": 2, "meta": 4, "cmd": 4, "command": 4, "shift": 8}

type keyDef struct {
	key  string
	code string
	vk   int
	text string
}

var modifierKeys = map[int]keyDef{
	1: {key: "Alt", code: "AltLeft", vk: 18},
	2: {key: "Control", code: "ControlLeft", vk: 17},
	4: {key: "Meta", code: "MetaLeft", vk: 91},
	8: {key: "Shift", code: "ShiftLeft", vk: 16},
}

var namedInputKeys = map[string]keyDef{
	"enter": {key: "Enter", code: "Enter", vk: 13, text: "\r"}, "tab": {key: "Tab", code: "Tab", vk: 9},
	"escape": {key: "Escape", code: "Escape", vk: 27}, "esc": {key: "Escape", code: "Escape", vk: 27},
	"backspace": {key: "Backspace", code: "Backspace", vk: 8}, "delete": {key: "Delete", code: "Delete", vk: 46},
	"space": {key: " ", code: "Space", vk: 32, text: " "}, "home": {key: "Home", code: "Home", vk: 36},
	"end": {key: "End", code: "End", vk: 35}, "pageup": {key: "PageUp", code: "PageUp", vk: 33},
	"pagedown": {key: "PageDown", code: "PageDown", vk: 34}, "arrowleft": {key: "ArrowLeft", code: "ArrowLeft", vk: 37},
	"arrowup": {key: "ArrowUp", code: "ArrowUp", vk: 38}, "arrowright": {key: "ArrowRight", code: "ArrowRight", vk: 39},
	"arrowdown": {key: "ArrowDown", code: "ArrowDown", vk: 40},
	"-":         {key: "-", code: "Minus", vk: 189, text: "-"}, "=": {key: "=", code: "Equal", vk: 187, text: "="},
	"[": {key: "[", code: "BracketLeft", vk: 219, text: "["}, "]": {key: "]", code: "BracketRight", vk: 221, text: "]"},
	"/": {key: "/", code: "Slash", vk: 191, text: "/"}, "\\": {key: "\\", code: "Backslash", vk: 220, text: "\\"},
	",": {key: ",", code: "Comma", vk: 188, text: ","}, ".": {key: ".", code: "Period", vk: 190, text: "."},
	";": {key: ";", code: "Semicolon", vk: 186, text: ";"}, "'": {key: "'", code: "Quote", vk: 222, text: "'"},
	"`": {key: "`", code: "Backquote", vk: 192, text: "`"},
}

func resolveInputKey(name string, shift bool) (keyDef, bool) {
	lower := strings.ToLower(strings.TrimSpace(name))
	if alias, ok := keyAliases[lower]; ok {
		lower = alias
	}
	if def, ok := namedInputKeys[lower]; ok {
		return def, true
	}
	if len(lower) > 1 && lower[0] == 'f' {
		if n, err := strconv.Atoi(lower[1:]); err == nil && n >= 1 && n <= 12 && strconv.Itoa(n) == lower[1:] {
			return keyDef{key: "F" + lower[1:], code: "F" + lower[1:], vk: 111 + n}, true
		}
	}
	if len(lower) != 1 {
		return keyDef{}, false
	}
	c := lower[0]
	switch {
	case c >= 'a' && c <= 'z':
		key := string(c)
		if shift {
			key = strings.ToUpper(key)
		}
		return keyDef{key: key, code: "Key" + strings.ToUpper(string(c)), vk: int(c-'a') + 65, text: key}, true
	case c >= '0' && c <= '9':
		return keyDef{key: string(c), code: "Digit" + string(c), vk: int(c-'0') + 48, text: string(c)}, true
	}
	return keyDef{}, false
}

func parseModifiers(names []string) (int, error) {
	mask := 0
	for _, name := range names {
		bit, ok := modifierBits[strings.ToLower(strings.TrimSpace(name))]
		if !ok {
			return 0, fmt.Errorf("unsupported modifier %q (use alt, ctrl, meta or shift)", name)
		}
		mask |= bit
	}
	return mask, nil
}

var mouseButtons = map[string]int{"left": 1, "right": 2, "middle": 4}

// ValidateInputRequest checks an input action before any event is planned,
// so a workflow can reject a bad later step before its first mutation.
func ValidateInputRequest(p Request) error {
	if !IsInputAction(p.Action) {
		return fmt.Errorf("unsupported input action %q", p.Action)
	}
	if _, err := parseModifiers(p.Modifiers); err != nil {
		return err
	}
	if IsPointerAction(p.Action) && strings.TrimSpace(p.ViewportID) == "" {
		return fmt.Errorf("%s requires viewport_id from a fresh browser screenshot", p.Action)
	}
	if p.Button != "" {
		if _, ok := mouseButtons[p.Button]; !ok {
			return fmt.Errorf("unsupported button %q (use left, right or middle)", p.Button)
		}
	}
	if p.ClickCount < 0 || p.ClickCount > 3 {
		return fmt.Errorf("click_count must be 1-3")
	}
	if p.Steps < 0 || p.Steps > 100 {
		return fmt.Errorf("steps must be 1-100")
	}
	if p.HoldMS < 0 || p.HoldMS > 5000 {
		return fmt.Errorf("hold_ms must be 0-5000")
	}
	switch p.Action {
	case InputKeyCombo:
		mask, _ := parseModifiers(p.Modifiers)
		if _, ok := resolveInputKey(p.Key, mask&8 != 0); !ok {
			return ErrUnsupportedKey{Key: p.Key}
		}
	case InputInsertText:
		if p.Text == "" || len(p.Text) > 64*1024 {
			return fmt.Errorf("insert_text requires text of at most 64 KiB")
		}
	}
	return nil
}

// planInput converts a validated request into ordered CDP events. For
// pointer actions vp converts screenshot pixels into CSS pixels and bounds
// the coordinates to the captured image.
func planInput(p Request, vp Viewport) ([]inputEvent, error) {
	if err := ValidateInputRequest(p); err != nil {
		return nil, err
	}
	mask, _ := parseModifiers(p.Modifiers)
	switch p.Action {
	case InputInsertText:
		return []inputEvent{{method: cdpInsertText, params: map[string]any{"text": p.Text}}}, nil
	case InputKeyCombo:
		key, _ := resolveInputKey(p.Key, mask&8 != 0)
		var events []inputEvent
		held := 0
		for _, bit := range []int{2, 1, 4, 8} {
			if mask&bit == 0 {
				continue
			}
			held |= bit
			m := modifierKeys[bit]
			events = append(events, keyEvent("rawKeyDown", m, held, ""))
		}
		down, text := "rawKeyDown", ""
		// Printable keys only produce text when no command modifier is
		// held, matching what a physical keyboard delivers.
		if key.text != "" && mask&(1|2|4) == 0 {
			down, text = "keyDown", key.text
		}
		events = append(events, keyEvent(down, key, mask, text), keyEvent("keyUp", key, mask, ""))
		for _, bit := range []int{8, 4, 1, 2} {
			if mask&bit == 0 {
				continue
			}
			held &^= bit
			events = append(events, keyEvent("keyUp", modifierKeys[bit], held, ""))
		}
		return events, nil
	}
	if vp.Scale <= 0 || vp.ImageWidth <= 0 || vp.ImageHeight <= 0 {
		return nil, ErrStaleViewport
	}
	toCSS := func(x, y float64, field string) (float64, float64, error) {
		if math.IsNaN(x) || math.IsNaN(y) || x < 0 || y < 0 || x >= float64(vp.ImageWidth) || y >= float64(vp.ImageHeight) {
			return 0, 0, fmt.Errorf("%s (%.0f, %.0f) is outside the %dx%d screenshot; aim inside the captured image", field, x, y, vp.ImageWidth, vp.ImageHeight)
		}
		return x / vp.Scale, y / vp.Scale, nil
	}
	x, y, err := toCSS(p.X, p.Y, "x/y")
	if err != nil {
		return nil, err
	}
	button := p.Button
	if button == "" {
		button = "left"
	}
	buttons := mouseButtons[button]
	move := func(x, y float64, pressed bool) inputEvent {
		b := 0
		if pressed {
			b = buttons
		}
		return mouseEvent("mouseMoved", x, y, "none", b, 0, mask)
	}
	hold := time.Duration(p.HoldMS) * time.Millisecond
	switch p.Action {
	case InputPointerMove:
		return []inputEvent{move(x, y, false)}, nil
	case InputPointerClick:
		count := max(p.ClickCount, 1)
		events := []inputEvent{move(x, y, false)}
		for i := 1; i <= count; i++ {
			events = append(events, mouseEvent("mousePressed", x, y, button, buttons, i, mask))
			release := mouseEvent("mouseReleased", x, y, button, 0, i, mask)
			if i == 1 {
				release.delay = hold
			}
			events = append(events, release)
		}
		return events, nil
	case InputPointerDrag:
		tx, ty, err := toCSS(p.ToX, p.ToY, "to_x/to_y")
		if err != nil {
			return nil, err
		}
		steps := p.Steps
		if steps == 0 {
			steps = defaultDragSeg
		}
		events := []inputEvent{move(x, y, false), mouseEvent("mousePressed", x, y, button, buttons, 1, mask)}
		for i := 1; i <= steps; i++ {
			f := float64(i) / float64(steps)
			step := move(x+(tx-x)*f, y+(ty-y)*f, true)
			if i == 1 {
				step.delay = hold
			} else {
				// Canvas apps sample pointer motion per frame.
				step.delay = 16 * time.Millisecond
			}
			events = append(events, step)
		}
		events = append(events, mouseEvent("mouseReleased", tx, ty, button, 0, 1, mask))
		return events, nil
	}
	return nil, fmt.Errorf("unsupported input action %q", p.Action)
}

func mouseEvent(kind string, x, y float64, button string, buttons, clicks, modifiers int) inputEvent {
	params := map[string]any{"type": kind, "x": x, "y": y, "button": button, "buttons": buttons, "modifiers": modifiers}
	if clicks > 0 {
		params["clickCount"] = clicks
	}
	return inputEvent{method: cdpMouseEvent, params: params}
}

func keyEvent(kind string, key keyDef, modifiers int, text string) inputEvent {
	params := map[string]any{"type": kind, "key": key.key, "code": key.code, "windowsVirtualKeyCode": key.vk, "nativeVirtualKeyCode": key.vk, "modifiers": modifiers}
	if text != "" {
		params["text"] = text
		params["unmodifiedText"] = text
	}
	return inputEvent{method: cdpKeyEvent, params: params}
}

// inputSender delivers one CDP command to the selected tab.
type inputSender func(ctx context.Context, method string, params map[string]any) error

// dispatchInput sends events strictly in order. On failure, it attempts
// cancellation-independent cleanup and retains any unconfirmed releases.
func dispatchInput(ctx context.Context, send inputSender, events []inputEvent) (sent int, err error) {
	var pressed map[string]any
	var lastX, lastY float64
	held := map[string]map[string]any{}
	defer func() {
		if err == nil {
			return
		}
		var releases []inputEvent
		if pressed != nil {
			release := map[string]any{"type": "mouseReleased", "x": lastX, "y": lastY, "button": pressed["button"], "buttons": 0, "clickCount": pressed["clickCount"], "modifiers": 0}
			releases = append(releases, inputEvent{method: cdpMouseEvent, params: release})
		}
		// Release ordinary keys before modifiers, in a stable order.
		codes := make([]string, 0, len(held))
		for code := range held {
			codes = append(codes, code)
		}
		modifier := func(code string) bool {
			return code == "ControlLeft" || code == "AltLeft" || code == "MetaLeft" || code == "ShiftLeft"
		}
		slices.SortFunc(codes, func(a, b string) int {
			if modifier(a) != modifier(b) {
				if modifier(a) {
					return 1
				}
				return -1
			}
			return strings.Compare(a, b)
		})
		for _, code := range codes {
			down := held[code]
			releases = append(releases, inputEvent{method: cdpKeyEvent, params: map[string]any{"type": "keyUp", "key": down["key"], "code": code, "windowsVirtualKeyCode": down["windowsVirtualKeyCode"], "nativeVirtualKeyCode": down["nativeVirtualKeyCode"], "modifiers": 0}})
		}
		failure := &inputDispatchFailure{cause: err, cleanupAttempted: len(releases) > 0}
		if len(releases) > 0 {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
			defer cancel()
			failure.pending, failure.cleanupErr = releaseInput(cleanup, send, releases)
		}
		err = failure
	}()
	for _, ev := range events {
		if err = ctx.Err(); err != nil {
			return sent, err
		}
		if ev.delay > 0 {
			timer := time.NewTimer(ev.delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				err = ctx.Err()
				return sent, err
			case <-timer.C:
			}
		}
		// Track state before sending: a failed send may still have
		// reached the page, so cleanup must assume it did.
		switch ev.method {
		case cdpMouseEvent:
			if x, ok := ev.params["x"].(float64); ok {
				lastX, lastY = x, ev.params["y"].(float64)
			}
			switch ev.params["type"] {
			case "mousePressed":
				pressed = ev.params
			}
		case cdpKeyEvent:
			code, _ := ev.params["code"].(string)
			switch ev.params["type"] {
			case "keyDown", "rawKeyDown":
				held[code] = ev.params
			}
		}
		if err = send(ctx, ev.method, ev.params); err != nil {
			return sent, err
		}
		sent++
		switch ev.method {
		case cdpMouseEvent:
			if ev.params["type"] == "mouseReleased" {
				pressed = nil
			}
		case cdpKeyEvent:
			if ev.params["type"] == "keyUp" {
				code, _ := ev.params["code"].(string)
				delete(held, code)
			}
		}
	}
	return sent, nil
}

// grantFor returns the stored grant matching id, if still fresh.
func grantFor(grants []Viewport, id string, now time.Time) (Viewport, bool) {
	for _, g := range grants {
		if g.ID == id && now.Sub(g.Captured) <= viewportGrantTTL {
			return g, true
		}
	}
	return Viewport{}, false
}
