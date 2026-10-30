package webverify

import (
	"context"
	"errors"
	"time"
)

// ErrUnavailable means the browser cannot be used at all: the user denied
// the permission, or the tool is missing. A run that meets it stops, and the
// gate treats it as "not verified" instead of as a defect in the product.
var ErrUnavailable = errors.New("browser unavailable")

// Driver is the browser the verifier drives. The production implementation
// calls Atlas's own browser tool; tests use a fake. Every method acts on the
// one tab the verifier owns.
type Driver interface {
	// Navigate loads the address and returns when the page has loaded.
	Navigate(ctx context.Context, url string) error
	// SetViewport emulates a screen.
	SetViewport(ctx context.Context, v Viewport) error
	// Eval evaluates a JavaScript expression (awaiting a promise) and
	// returns its JSON-encoded value.
	Eval(ctx context.Context, script string, timeout time.Duration) (string, error)
	// Screenshot captures the visible viewport as an image.
	Screenshot(ctx context.Context) (Image, error)
	// ConsoleSince returns console messages recorded since the previous
	// call.
	ConsoleSince(ctx context.Context) ([]ConsoleMessage, error)
	// NetworkSince returns requests recorded since the previous call.
	NetworkSince(ctx context.Context) ([]NetworkEntry, error)
	// Click presses the element matching the CSS selector.
	Click(ctx context.Context, selector string) error
	// Type replaces the content of the field matching the selector.
	Type(ctx context.Context, selector, text string) error
	// Press sends a named key to the focused element.
	Press(ctx context.Context, key string) error
	// Assert checks a condition with the browser tool's assert action. A
	// false ok with a nil error means the assertion did not hold.
	Assert(ctx context.Context, a Assertion) (ok bool, detail string, err error)
}

// Image is a captured screenshot.
type Image struct {
	Data      []byte
	MediaType string
}

// Extension returns the file extension for the image type.
func (i Image) Extension() string {
	switch i.MediaType {
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	}
	return ".png"
}

// Assertion is a condition on the page, mapped onto the browser tool's
// assert action.
type Assertion struct {
	// Condition is one of visible, hidden, text, text_contains, value,
	// enabled, url, title, checked, count.
	Condition string `json:"condition"`
	Selector  string `json:"selector,omitempty"`
	Role      string `json:"role,omitempty"`
	Name      string `json:"name,omitempty"`
	Label     string `json:"label,omitempty"`
	Expected  string `json:"expected,omitempty"`
	TimeoutMS int    `json:"timeout_ms,omitempty"`
}

// assertConditions lists the conditions a flow step may use.
var assertConditions = map[string]bool{
	"visible": true, "hidden": true, "text": true, "text_contains": true,
	"value": true, "enabled": true, "url": true, "title": true,
	"checked": true, "count": true,
}
