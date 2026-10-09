package browser

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type sentInput struct {
	method string
	params map[string]any
}

func recordingSender(failAt int, failErr error, cancelAt int, cancel context.CancelFunc) (*[]sentInput, inputSender) {
	var sent []sentInput
	return &sent, func(ctx context.Context, method string, params map[string]any) error {
		sent = append(sent, sentInput{method, params})
		if cancelAt > 0 && len(sent) == cancelAt {
			cancel()
		}
		if failAt > 0 && len(sent) == failAt {
			return failErr
		}
		return nil
	}
}

func eventTypes(events []sentInput) []string {
	var out []string
	for _, e := range events {
		out = append(out, e.params["type"].(string))
	}
	return out
}

var testViewport = Viewport{ID: "vp-1", ImageWidth: 800, ImageHeight: 600, Scale: 2, CSSWidth: 400, CSSHeight: 300}

func TestPlanPointerClickScalesScreenshotPixelsToCSS(t *testing.T) {
	t.Parallel()
	events, err := planInput(Request{Action: InputPointerClick, ViewportID: "vp-1", X: 200, Y: 100, ClickCount: 2, Modifiers: []string{"shift"}}, testViewport)
	require.NoError(t, err)
	var types []string
	for _, e := range events {
		require.Equal(t, cdpMouseEvent, e.method)
		require.Equal(t, 100.0, e.params["x"])
		require.Equal(t, 50.0, e.params["y"])
		require.Equal(t, 8, e.params["modifiers"])
		types = append(types, e.params["type"].(string))
	}
	require.Equal(t, []string{"mouseMoved", "mousePressed", "mouseReleased", "mousePressed", "mouseReleased"}, types)
	require.Equal(t, 2, events[3].params["clickCount"], "the second press is the double click")
}

func TestPlanPointerDragIsOrderedWithButtonHeld(t *testing.T) {
	t.Parallel()
	events, err := planInput(Request{Action: InputPointerDrag, ViewportID: "vp-1", X: 0, Y: 0, ToX: 400, ToY: 200, Steps: 4}, testViewport)
	require.NoError(t, err)
	require.Len(t, events, 7)
	require.Equal(t, "mouseMoved", events[0].params["type"])
	require.Equal(t, 0, events[0].params["buttons"])
	require.Equal(t, "mousePressed", events[1].params["type"])
	for _, e := range events[2:6] {
		require.Equal(t, "mouseMoved", e.params["type"])
		require.Equal(t, 1, e.params["buttons"], "moves during a drag carry the held button")
	}
	last := events[6]
	require.Equal(t, "mouseReleased", last.params["type"])
	require.Equal(t, 200.0, last.params["x"])
	require.Equal(t, 100.0, last.params["y"])
}

func TestPlanPointerRejectsOutOfBoundsAndMissingGrant(t *testing.T) {
	t.Parallel()
	_, err := planInput(Request{Action: InputPointerClick, ViewportID: "vp-1", X: 800, Y: 10}, testViewport)
	require.ErrorContains(t, err, "outside the 800x600 screenshot")
	_, err = planInput(Request{Action: InputPointerDrag, ViewportID: "vp-1", X: 1, Y: 1, ToX: -1, ToY: 5}, testViewport)
	require.ErrorContains(t, err, "to_x/to_y")
	_, err = planInput(Request{Action: InputPointerClick, X: 1, Y: 1}, testViewport)
	require.ErrorContains(t, err, "viewport_id")
	_, err = planInput(Request{Action: InputPointerClick, ViewportID: "vp-1", X: 1, Y: 1}, Viewport{})
	require.ErrorIs(t, err, ErrStaleViewport)
	_, err = planInput(Request{Action: InputPointerClick, ViewportID: "vp-1", X: 1, Y: 1, Button: "back"}, testViewport)
	require.ErrorContains(t, err, "unsupported button")
}

func TestPlanKeyComboWrapsModifiers(t *testing.T) {
	t.Parallel()
	events, err := planInput(Request{Action: InputKeyCombo, Key: "z", Modifiers: []string{"ctrl", "shift"}}, Viewport{})
	require.NoError(t, err)
	var seq []string
	for _, e := range events {
		seq = append(seq, e.params["type"].(string)+":"+e.params["code"].(string))
	}
	require.Equal(t, []string{"rawKeyDown:ControlLeft", "rawKeyDown:ShiftLeft", "rawKeyDown:KeyZ", "keyUp:KeyZ", "keyUp:ShiftLeft", "keyUp:ControlLeft"}, seq)
	require.NotContains(t, events[2].params, "text", "command shortcuts must not insert text")

	plain, err := planInput(Request{Action: InputKeyCombo, Key: "r"}, Viewport{})
	require.NoError(t, err)
	require.Equal(t, "keyDown", plain[0].params["type"])
	require.Equal(t, "r", plain[0].params["text"])

	_, err = planInput(Request{Action: InputKeyCombo, Key: "hyper"}, Viewport{})
	require.Error(t, err)
	_, err = planInput(Request{Action: InputKeyCombo, Key: "a", Modifiers: []string{"super"}}, Viewport{})
	require.ErrorContains(t, err, "unsupported modifier")
}

func TestDispatchInputSendsInOrder(t *testing.T) {
	t.Parallel()
	events, err := planInput(Request{Action: InputPointerDrag, ViewportID: "vp-1", X: 10, Y: 10, ToX: 20, ToY: 20, Steps: 2}, testViewport)
	require.NoError(t, err)
	sent, send := recordingSender(0, nil, 0, nil)
	n, err := dispatchInput(t.Context(), send, events)
	require.NoError(t, err)
	require.Equal(t, len(events), n)
	require.Equal(t, []string{"mouseMoved", "mousePressed", "mouseMoved", "mouseMoved", "mouseReleased"}, eventTypes(*sent))
}

func TestDispatchInputReleasesButtonAfterMidDragFailure(t *testing.T) {
	t.Parallel()
	events, err := planInput(Request{Action: InputPointerDrag, ViewportID: "vp-1", X: 10, Y: 10, ToX: 30, ToY: 30, Steps: 4}, testViewport)
	require.NoError(t, err)
	sent, send := recordingSender(4, errors.New("target closed"), 0, nil)
	_, err = dispatchInput(t.Context(), send, events)
	require.ErrorContains(t, err, "target closed")
	types := eventTypes(*sent)
	require.Equal(t, "mouseReleased", types[len(types)-1], "a failed drag must not leave the button down")
	require.Len(t, types, 5)
	release := (*sent)[len(*sent)-1].params
	require.Equal(t, (*sent)[3].params["x"], release["x"], "release where the pointer last was")
}

func TestDispatchInputReleasesHeldInputOnCancel(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	events, err := planInput(Request{Action: InputPointerDrag, ViewportID: "vp-1", X: 10, Y: 10, ToX: 30, ToY: 30, Steps: 4, HoldMS: 50}, testViewport)
	require.NoError(t, err)
	sent, send := recordingSender(0, nil, 2, cancel)
	_, err = dispatchInput(ctx, send, events)
	require.ErrorIs(t, err, context.Canceled)
	types := eventTypes(*sent)
	require.Equal(t, []string{"mouseMoved", "mousePressed", "mouseReleased"}, types)

	keyCtx, keyCancel := context.WithCancel(t.Context())
	keys, err := planInput(Request{Action: InputKeyCombo, Key: "a", Modifiers: []string{"ctrl"}}, Viewport{})
	require.NoError(t, err)
	keySent, keySend := recordingSender(0, nil, 1, keyCancel)
	_, err = dispatchInput(keyCtx, keySend, keys)
	require.ErrorIs(t, err, context.Canceled)
	last := (*keySent)[len(*keySent)-1].params
	require.Equal(t, "keyUp", last["type"])
	require.Equal(t, "ControlLeft", last["code"], "a canceled shortcut must release its modifier")
}

func TestViewportGrantExpires(t *testing.T) {
	t.Parallel()
	now := time.Now()
	grants := []Viewport{{ID: "old", Captured: now.Add(-viewportGrantTTL - time.Second)}, {ID: "new", Captured: now}}
	_, ok := grantFor(grants, "old", now)
	require.False(t, ok)
	_, ok = grantFor(grants, "new", now)
	require.True(t, ok)
	_, ok = grantFor(grants, "missing", now)
	require.False(t, ok)
}
