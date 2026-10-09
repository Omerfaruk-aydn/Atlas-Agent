package android

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

// Screen is the device's current geometry.
type Screen struct {
	Width    int `json:"width"`
	Height   int `json:"height"`
	Density  int `json:"density"`
	Rotation int `json:"rotation"`
}

// Focus is the foreground app and activity.
type Focus struct {
	Package  string `json:"package"`
	Activity string `json:"activity,omitempty"`
}

// Element is one accessibility node from a UI dump.
type Element struct {
	Index       int    `json:"index"`
	Text        string `json:"text,omitempty"`
	ResourceID  string `json:"resource_id,omitempty"`
	ContentDesc string `json:"content_desc,omitempty"`
	Class       string `json:"class,omitempty"`
	Package     string `json:"package,omitempty"`
	Clickable   bool   `json:"clickable,omitempty"`
	Focused     bool   `json:"focused,omitempty"`
	Enabled     bool   `json:"enabled"`
	Password    bool   `json:"password,omitempty"`
	Bounds      [4]int `json:"bounds"`
}

// Center returns the element's tap point.
func (e Element) Center() (int, int) {
	return (e.Bounds[0] + e.Bounds[2]) / 2, (e.Bounds[1] + e.Bounds[3]) / 2
}

// Observation binds what was seen to the geometry and app it was seen in.
type Observation struct {
	ID         string    `json:"observation_id"`
	Serial     string    `json:"serial"`
	Screen     Screen    `json:"screen"`
	Focus      Focus     `json:"focus"`
	Elements   []Element `json:"elements,omitempty"`
	UIHash     string    `json:"ui_hash,omitempty"`
	PNG        []byte    `json:"-"`
	CapturedAt int64     `json:"captured_at"`
}

var (
	sizeRe     = regexp.MustCompile(`(Physical|Override) size:\s*(\d+)x(\d+)`)
	densityRe  = regexp.MustCompile(`(Physical|Override) density:\s*(\d+)`)
	rotationRe = regexp.MustCompile(`SurfaceOrientation:\s*(\d)`)
	displayRot = regexp.MustCompile(`mCurrentOrientation=(\d)`)
	focusRe    = regexp.MustCompile(`mCurrentFocus=Window\{[^ ]+ [^ ]+ ([^/\s}]+)(?:/([^\s}]+))?`)
	resumedRe  = regexp.MustCompile(`(?:mResumedActivity|ResumedActivity|topResumedActivity)[:=] ?ActivityRecord\{[^ ]+ [^ ]+ ([^/\s}]+)/([^\s}]+)`)
	boundsRe   = regexp.MustCompile(`^\[(-?\d+),(-?\d+)\]\[(-?\d+),(-?\d+)\]$`)
)

// ParseSize prefers the override size (what input uses) over physical.
func ParseSize(out string) (int, int, error) {
	w, h := 0, 0
	for _, m := range sizeRe.FindAllStringSubmatch(out, -1) {
		mw, _ := strconv.Atoi(m[2])
		mh, _ := strconv.Atoi(m[3])
		if m[1] == "Override" || w == 0 {
			w, h = mw, mh
		}
	}
	if w <= 0 || h <= 0 {
		return 0, 0, fmt.Errorf("cannot parse screen size from %q", strings.TrimSpace(out))
	}
	return w, h, nil
}

// ParseDensity reads `wm density`.
func ParseDensity(out string) int {
	d := 0
	for _, m := range densityRe.FindAllStringSubmatch(out, -1) {
		v, _ := strconv.Atoi(m[2])
		if m[1] == "Override" || d == 0 {
			d = v
		}
	}
	return d
}

// ParseRotation reads the surface orientation (0-3).
func ParseRotation(out string) (int, bool) {
	if m := rotationRe.FindStringSubmatch(out); m != nil {
		r, _ := strconv.Atoi(m[1])
		return r, true
	}
	if m := displayRot.FindStringSubmatch(out); m != nil {
		r, _ := strconv.Atoi(m[1])
		return r, true
	}
	return 0, false
}

// ParseFocus reads the focused window's package and activity.
func ParseFocus(out string) Focus {
	if m := focusRe.FindStringSubmatch(out); m != nil {
		return Focus{Package: m[1], Activity: m[2]}
	}
	if m := resumedRe.FindStringSubmatch(out); m != nil {
		return Focus{Package: m[1], Activity: m[2]}
	}
	return Focus{}
}

type xmlNode struct {
	Text        string    `xml:"text,attr"`
	ResourceID  string    `xml:"resource-id,attr"`
	Class       string    `xml:"class,attr"`
	Package     string    `xml:"package,attr"`
	ContentDesc string    `xml:"content-desc,attr"`
	Clickable   string    `xml:"clickable,attr"`
	Focused     string    `xml:"focused,attr"`
	Enabled     string    `xml:"enabled,attr"`
	Password    string    `xml:"password,attr"`
	Bounds      string    `xml:"bounds,attr"`
	Nodes       []xmlNode `xml:"node"`
}

// ParseUIDump parses `uiautomator dump` XML. The trailing status line
// uiautomator prints after the XML is ignored. Password fields never carry
// their text.
func ParseUIDump(data []byte) ([]Element, error) {
	text := string(data)
	start := strings.Index(text, "<hierarchy")
	end := strings.LastIndex(text, "</hierarchy>")
	if start < 0 || end < start {
		return nil, fmt.Errorf("no UI hierarchy in uiautomator output")
	}
	var root struct {
		Nodes []xmlNode `xml:"node"`
	}
	if err := xml.Unmarshal([]byte(text[start:end+len("</hierarchy>")]), &root); err != nil {
		return nil, err
	}
	var out []Element
	var walk func([]xmlNode)
	walk = func(nodes []xmlNode) {
		for _, n := range nodes {
			m := boundsRe.FindStringSubmatch(n.Bounds)
			if m != nil {
				var b [4]int
				for i := range 4 {
					b[i], _ = strconv.Atoi(m[i+1])
				}
				e := Element{Index: len(out) + 1, Text: n.Text, ResourceID: n.ResourceID, ContentDesc: n.ContentDesc, Class: n.Class, Package: n.Package, Clickable: n.Clickable == "true", Focused: n.Focused == "true", Enabled: n.Enabled != "false", Password: n.Password == "true", Bounds: b}
				if e.Password {
					e.Text = ""
				}
				out = append(out, e)
			}
			walk(n.Nodes)
		}
	}
	walk(root.Nodes)
	return out, nil
}

// Controller adds observation tracking and safe input on top of ADB.
type Controller struct {
	ADB *ADB
	mu  sync.Mutex
	// last is the most recent observation per serial.
	last map[string]*Observation
	// locks serializes input per device.
	locks map[string]*sync.Mutex
}

// NewController wraps adb.
func NewController(a *ADB) *Controller {
	return &Controller{ADB: a, last: map[string]*Observation{}, locks: map[string]*sync.Mutex{}}
}

func (c *Controller) deviceLock(serial string) *sync.Mutex {
	c.mu.Lock()
	defer c.mu.Unlock()
	l, ok := c.locks[serial]
	if !ok {
		l = &sync.Mutex{}
		c.locks[serial] = l
	}
	return l
}

// Probe reads geometry and focus (cheap; used before every input).
func (c *Controller) Probe(ctx context.Context, serial string) (Screen, Focus, error) {
	sizeOut, err := c.ADB.Shell(ctx, serial, "wm", "size")
	if err != nil {
		return Screen{}, Focus{}, err
	}
	w, h, err := ParseSize(sizeOut)
	if err != nil {
		return Screen{}, Focus{}, err
	}
	densOut, _ := c.ADB.Shell(ctx, serial, "wm", "density")
	rot := 0
	if out, err := c.ADB.Shell(ctx, serial, "dumpsys", "input"); err == nil {
		if r, ok := ParseRotation(out); ok {
			rot = r
		}
	}
	focus := Focus{}
	if out, err := c.ADB.Shell(ctx, serial, "dumpsys", "window"); err == nil {
		focus = ParseFocus(out)
	}
	return Screen{Width: w, Height: h, Density: ParseDensity(densOut), Rotation: rot}, focus, nil
}

// Observe captures geometry, focus and optionally the UI tree and a
// screenshot. When the UI tree is enough, callers skip the image.
func (c *Controller) Observe(ctx context.Context, serial string, ui, image bool) (*Observation, error) {
	if _, err := c.ADB.RequireReady(ctx, serial); err != nil {
		return nil, err
	}
	screen, focus, err := c.Probe(ctx, serial)
	if err != nil {
		return nil, err
	}
	obs := &Observation{Serial: serial, Screen: screen, Focus: focus, CapturedAt: time.Now().UnixMilli()}
	if ui {
		data, err := c.ADB.ExecOut(ctx, serial, "uiautomator", "dump", "/dev/tty")
		if err != nil {
			return nil, err
		}
		if obs.Elements, err = ParseUIDump(data); err != nil {
			return nil, err
		}
		sum := sha256.Sum256(data)
		obs.UIHash = hex.EncodeToString(sum[:8])
	}
	if image {
		png, err := c.ADB.ExecOut(ctx, serial, "screencap", "-p")
		if err != nil {
			return nil, err
		}
		if len(png) < 8 || string(png[1:4]) != "PNG" {
			return nil, errors.New("screencap did not return a PNG (the screen may be secure or the device locked)")
		}
		obs.PNG = png
	}
	key := fmt.Sprintf("%s|%dx%d|%d|%s/%s|%s|%d", serial, screen.Width, screen.Height, screen.Rotation, focus.Package, focus.Activity, obs.UIHash, obs.CapturedAt)
	sum := sha256.Sum256([]byte(key))
	obs.ID = "aobs-" + hex.EncodeToString(sum[:8])
	c.mu.Lock()
	c.last[serial] = obs
	c.mu.Unlock()
	return obs, nil
}

// StaleError explains why an observation can no longer aim input.
type StaleError struct{ Reason string }

func (e *StaleError) Error() string { return "stale_observation: " + e.Reason }

// validate re-probes the device and rejects input aimed with an
// observation that is not the latest one or whose geometry or foreground
// app changed (rotation, resize, app switch).
func (c *Controller) validate(ctx context.Context, serial, observationID string) (*Observation, error) {
	c.mu.Lock()
	obs := c.last[serial]
	c.mu.Unlock()
	if obs == nil || obs.ID != observationID {
		return nil, &StaleError{Reason: "observation_id is not the latest observation of " + serial + "; observe again"}
	}
	if time.Since(time.UnixMilli(obs.CapturedAt)) > 2*time.Minute {
		return nil, &StaleError{Reason: "the observation is older than two minutes"}
	}
	screen, focus, err := c.Probe(ctx, serial)
	if err != nil {
		return nil, err
	}
	switch {
	case screen.Width != obs.Screen.Width || screen.Height != obs.Screen.Height:
		return nil, &StaleError{Reason: fmt.Sprintf("screen size changed from %dx%d to %dx%d", obs.Screen.Width, obs.Screen.Height, screen.Width, screen.Height)}
	case screen.Rotation != obs.Screen.Rotation:
		return nil, &StaleError{Reason: fmt.Sprintf("screen rotated from %d to %d", obs.Screen.Rotation, screen.Rotation)}
	case focus.Package != obs.Focus.Package || focus.Activity != obs.Focus.Activity:
		return nil, &StaleError{Reason: fmt.Sprintf("foreground app changed from %s/%s to %s/%s", obs.Focus.Package, obs.Focus.Activity, focus.Package, focus.Activity)}
	}
	return obs, nil
}

// consume invalidates the observation after input that may change the UI.
func (c *Controller) consume(serial, id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if o := c.last[serial]; o != nil && o.ID == id {
		delete(c.last, serial)
	}
}

// Target selects where to tap: an element index from the observation, a
// selector resolved against the observation, or raw coordinates.
type Target struct {
	Element     int    `json:"element,omitempty"`
	ResourceID  string `json:"resource_id,omitempty"`
	Text        string `json:"text,omitempty"`
	ContentDesc string `json:"content_desc,omitempty"`
	X           int    `json:"x,omitempty"`
	Y           int    `json:"y,omitempty"`
}

func (t Target) selector() bool { return t.ResourceID != "" || t.Text != "" || t.ContentDesc != "" }

// Resolve maps a target to in-bounds screen coordinates.
func Resolve(obs *Observation, t Target) (int, int, *Element, error) {
	var el *Element
	switch {
	case t.Element > 0:
		if t.Element > len(obs.Elements) {
			return 0, 0, nil, fmt.Errorf("element %d is not in the observation (it has %d elements)", t.Element, len(obs.Elements))
		}
		el = &obs.Elements[t.Element-1]
	case t.selector():
		var matches []*Element
		for i := range obs.Elements {
			e := &obs.Elements[i]
			if (t.ResourceID == "" || e.ResourceID == t.ResourceID) && (t.Text == "" || e.Text == t.Text) && (t.ContentDesc == "" || e.ContentDesc == t.ContentDesc) {
				matches = append(matches, e)
			}
		}
		if len(matches) == 0 {
			return 0, 0, nil, errors.New("no element in the observation matches the selector")
		}
		if len(matches) > 1 {
			return 0, 0, nil, fmt.Errorf("%d elements match the selector; add resource_id or use element", len(matches))
		}
		el = matches[0]
	}
	x, y := t.X, t.Y
	if el != nil {
		if el.Bounds[2] <= el.Bounds[0] || el.Bounds[3] <= el.Bounds[1] {
			return 0, 0, el, errors.New("the element has empty bounds (not on screen)")
		}
		x, y = el.Center()
	}
	if x < 0 || y < 0 || x >= obs.Screen.Width || y >= obs.Screen.Height {
		return 0, 0, el, fmt.Errorf("point (%d,%d) is outside the %dx%d screen", x, y, obs.Screen.Width, obs.Screen.Height)
	}
	return x, y, el, nil
}

// Tap validates the observation and taps the resolved target.
func (c *Controller) Tap(ctx context.Context, serial, observationID string, t Target) (int, int, error) {
	l := c.deviceLock(serial)
	l.Lock()
	defer l.Unlock()
	obs, err := c.validate(ctx, serial, observationID)
	if err != nil {
		return 0, 0, err
	}
	x, y, _, err := Resolve(obs, t)
	if err != nil {
		return 0, 0, err
	}
	if _, err := c.ADB.Shell(ctx, serial, "input", "tap", strconv.Itoa(x), strconv.Itoa(y)); err != nil {
		return x, y, err
	}
	c.consume(serial, observationID)
	return x, y, nil
}

// Swipe validates and swipes between two in-bounds points.
func (c *Controller) Swipe(ctx context.Context, serial, observationID string, x1, y1, x2, y2, durationMS int) error {
	l := c.deviceLock(serial)
	l.Lock()
	defer l.Unlock()
	obs, err := c.validate(ctx, serial, observationID)
	if err != nil {
		return err
	}
	for _, p := range [][2]int{{x1, y1}, {x2, y2}} {
		if p[0] < 0 || p[1] < 0 || p[0] >= obs.Screen.Width || p[1] >= obs.Screen.Height {
			return fmt.Errorf("swipe point (%d,%d) is outside the %dx%d screen", p[0], p[1], obs.Screen.Width, obs.Screen.Height)
		}
	}
	if durationMS <= 0 {
		durationMS = 300
	}
	if _, err := c.ADB.Shell(ctx, serial, "input", "swipe", strconv.Itoa(x1), strconv.Itoa(y1), strconv.Itoa(x2), strconv.Itoa(y2), strconv.Itoa(min(durationMS, 5000))); err != nil {
		return err
	}
	c.consume(serial, observationID)
	return nil
}

// Keycodes Atlas exposes by name.
var Keycodes = map[string]string{
	"back": "4", "home": "3", "enter": "66", "tab": "61", "delete": "67", "del": "67",
	"app_switch": "187", "menu": "82", "escape": "111", "dpad_up": "19", "dpad_down": "20",
	"dpad_left": "21", "dpad_right": "22", "space": "62", "volume_up": "24", "volume_down": "25",
}

// Key sends a named keyevent. Keys need no coordinates, so no observation.
func (c *Controller) Key(ctx context.Context, serial, key string) error {
	return c.KeyObserved(ctx, serial, key, "")
}

// KeyObserved binds a key to the latest foreground/geometry observation
// when observationID is provided, checking it under the device input lock.
func (c *Controller) KeyObserved(ctx context.Context, serial, key, observationID string) error {
	code, ok := Keycodes[strings.ToLower(key)]
	if !ok {
		return fmt.Errorf("unknown key %q", key)
	}
	if _, err := c.ADB.RequireReady(ctx, serial); err != nil {
		return err
	}
	l := c.deviceLock(serial)
	l.Lock()
	defer l.Unlock()
	if observationID != "" {
		if _, err := c.validate(ctx, serial, observationID); err != nil {
			return err
		}
	}
	_, err := c.ADB.Shell(ctx, serial, "input", "keyevent", code)
	c.mu.Lock()
	delete(c.last, serial)
	c.mu.Unlock()
	return err
}

// TextMethod reports how text will be typed.
type TextMethod string

const (
	TextInputCommand TextMethod = "input_text"
	TextADBKeyboard  TextMethod = "adb_keyboard_ime"
)

// UnicodeError is returned for text `input text` cannot deliver.
type UnicodeError struct{ Rune rune }

func (e *UnicodeError) Error() string {
	if e.Rune < unicode.MaxASCII {
		return fmt.Sprintf("text_unsupported: %q cannot be passed safely through \"adb shell input text\". Enable the ADB Keyboard IME (com.android.adbkeyboard) to type it; nothing was typed", e.Rune)
	}
	return fmt.Sprintf("unicode_unsupported: %q cannot be typed with \"adb shell input text\" (ASCII only). Install and enable the ADB Keyboard IME (com.android.adbkeyboard) for non-ASCII text such as Turkish; nothing was typed", e.Rune)
}

const adbKeyboardIME = "com.android.adbkeyboard/.AdbIME"

// inputTextSafe is what `input text` passes through the device shell
// unchanged once spaces are encoded as %s.
func inputTextSafe(r rune) bool {
	return r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune(" .,_@+-:/=", r))
}

// TypeText types text into the focused field. Non-ASCII text uses the ADB
// Keyboard IME only when it is the active input method; otherwise the call
// fails before anything is typed.
func (c *Controller) TypeText(ctx context.Context, serial, text string) (TextMethod, error) {
	return c.TypeTextObserved(ctx, serial, text, "")
}

// TypeTextObserved checks an optional foreground/geometry observation under
// the input lock before typing, then invalidates it after the attempt.
func (c *Controller) TypeTextObserved(ctx context.Context, serial, text, observationID string) (TextMethod, error) {
	if text == "" || len(text) > 4096 {
		return "", errors.New("text must be 1-4096 bytes")
	}
	if _, err := c.ADB.RequireReady(ctx, serial); err != nil {
		return "", err
	}
	l := c.deviceLock(serial)
	l.Lock()
	defer l.Unlock()
	if observationID != "" {
		if _, err := c.validate(ctx, serial, observationID); err != nil {
			return "", err
		}
		defer c.consume(serial, observationID)
	}
	var unsafe rune = -1
	for _, r := range text {
		if !inputTextSafe(r) {
			unsafe = r
			break
		}
	}
	if unsafe < 0 {
		_, err := c.ADB.Shell(ctx, serial, "input", "text", strings.ReplaceAll(text, " ", "%s"))
		return TextInputCommand, err
	}
	ime, _ := c.ADB.Shell(ctx, serial, "settings", "get", "secure", "default_input_method")
	if strings.TrimSpace(ime) != adbKeyboardIME {
		return "", &UnicodeError{Rune: unsafe}
	}
	_, err := c.ADB.Shell(ctx, serial, "am", "broadcast", "-a", "ADB_INPUT_B64", "--es", "msg", base64.StdEncoding.EncodeToString([]byte(text)))
	return TextADBKeyboard, err
}

var packageRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z][A-Za-z0-9_]*)+$`)

// Launch starts an app's launcher activity, or a component pkg/.Activity.
func (c *Controller) Launch(ctx context.Context, serial, pkg, activity string) error {
	if !packageRe.MatchString(pkg) {
		return fmt.Errorf("invalid package name %q", pkg)
	}
	if _, err := c.ADB.RequireReady(ctx, serial); err != nil {
		return err
	}
	var err error
	var out string
	if activity != "" {
		if strings.ContainsAny(activity, " ;&|$`'\"") {
			return fmt.Errorf("invalid activity %q", activity)
		}
		out, err = c.ADB.Shell(ctx, serial, "am", "start", "-W", "-n", pkg+"/"+activity)
	} else {
		out, err = c.ADB.Shell(ctx, serial, "monkey", "-p", pkg, "-c", "android.intent.category.LAUNCHER", "1")
	}
	if err == nil && (strings.Contains(out, "Error") || strings.Contains(out, "No activities found")) {
		err = fmt.Errorf("launch failed: %s", strings.TrimSpace(out))
	}
	c.mu.Lock()
	delete(c.last, serial)
	c.mu.Unlock()
	return err
}

// FindText reports whether a fresh UI dump contains an element whose text
// (or content description) contains want. It is how typed text and
// navigation results are verified instead of assumed.
func (c *Controller) FindText(ctx context.Context, serial, want string, target Target) (*Element, *Observation, error) {
	obs, err := c.Observe(ctx, serial, true, false)
	if err != nil {
		return nil, nil, err
	}
	for i := range obs.Elements {
		e := &obs.Elements[i]
		if target.ResourceID != "" && e.ResourceID != target.ResourceID {
			continue
		}
		if strings.Contains(e.Text, want) || strings.Contains(e.ContentDesc, want) {
			return e, obs, nil
		}
	}
	return nil, obs, nil
}
