package browser

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"math"
	"time"

	"github.com/chromedp/chromedp"
)

// viewportScript reports everything that changes where a CSS pixel lands
// in a viewport screenshot: the document instance, scroll offsets, layout
// size, device scale and pinch zoom.
const viewportScript = `(() => { const v = window.visualViewport; return JSON.stringify([performance.timeOrigin, scrollX, scrollY, innerWidth, innerHeight, devicePixelRatio, v ? v.scale : 1, v ? v.offsetLeft : 0, v ? v.offsetTop : 0]); })()`

type viewportState struct {
	id        string
	tabID     string
	cssWidth  float64
	cssHeight float64
	zoom      float64
}

func (s *chromedpSession) viewportState(ctx context.Context) (viewportState, error) {
	var raw string
	var tab string
	err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		c := chromedp.FromContext(ctx)
		if c == nil || c.Target == nil {
			return errors.New("no active browser target")
		}
		tab = string(c.Target.TargetID)
		return chromedp.Evaluate(viewportScript, &raw).Do(ctx)
	}))
	if err != nil {
		return viewportState{}, err
	}
	var fields []float64
	if err := json.Unmarshal([]byte(raw), &fields); err != nil || len(fields) != 9 {
		return viewportState{}, fmt.Errorf("reading viewport state failed")
	}
	sum := sha256.Sum256([]byte(tab + "|" + raw))
	return viewportState{id: "vp-" + hex.EncodeToString(sum[:8]), tabID: tab, cssWidth: fields[3], cssHeight: fields[4], zoom: fields[6]}, nil
}

// inputContext bounds one input call by the action timeout and the
// caller's cancellation, like Advanced.
func (s *chromedpSession) inputContext(parent context.Context, extra time.Duration) (context.Context, context.CancelFunc) {
	timeout := s.actionTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(s.currentContext(), timeout+extra)
	stop := context.AfterFunc(parent, cancel)
	return ctx, func() { stop(); cancel() }
}

// ViewportScreenshot captures the annotated viewport and issues a pointer
// grant when the viewport did not change during capture.
func (s *chromedpSession) ViewportScreenshot(parent context.Context) ([]byte, Viewport, error) {
	return s.captureViewportScreenshot(parent, func() ([]byte, error) { return s.AnnotatedScreenshot(false) })
}

func (s *chromedpSession) captureViewportScreenshot(parent context.Context, capture func() ([]byte, error)) ([]byte, Viewport, error) {
	ctx, cancel := s.inputContext(parent, 0)
	defer cancel()
	before, err := s.viewportState(ctx)
	if err != nil {
		return nil, Viewport{}, s.explainFailure(err)
	}
	data, err := capture()
	if err != nil {
		return nil, Viewport{}, err
	}
	after, err := s.viewportState(ctx)
	if err != nil || after.id != before.id || math.Abs(after.zoom-1) > 0.01 || after.cssWidth <= 0 || after.cssHeight <= 0 {
		// The screenshot is still useful to look at, but its pixels
		// cannot be trusted as pointer coordinates.
		return data, Viewport{}, nil
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return data, Viewport{}, nil
	}
	vp := Viewport{
		ID: after.id, TabID: after.tabID, CSSWidth: after.cssWidth, CSSHeight: after.cssHeight,
		ImageWidth: cfg.Width, ImageHeight: cfg.Height, Scale: float64(cfg.Width) / after.cssWidth, Captured: time.Now(),
	}
	s.mu.Lock()
	s.viewports = append(s.viewports, vp)
	if len(s.viewports) > maxViewportGrants {
		s.viewports = s.viewports[len(s.viewports)-maxViewportGrants:]
	}
	s.mu.Unlock()
	return data, vp, nil
}

// Input runs one typed input action. Pointer actions must name a viewport
// grant that still matches the live page; nothing is sent otherwise.
func (s *chromedpSession) Input(parent context.Context, p Request) (InputResult, error) {
	if err := parent.Err(); err != nil {
		return InputResult{}, err
	}
	if err := ValidateInputRequest(p); err != nil {
		return InputResult{}, err
	}
	extra := time.Duration(p.HoldMS)*time.Millisecond + time.Duration(max(p.Steps, defaultDragSeg))*16*time.Millisecond
	ctx, cancel := s.inputContext(parent, extra)
	defer cancel()
	result := InputResult{Action: p.Action}
	if err := s.recoverInput(ctx); err != nil {
		return inputFailureResult(result, err), s.explainFailure(err)
	}
	var vp Viewport
	if IsPointerAction(p.Action) {
		s.mu.Lock()
		grant, ok := grantFor(s.viewports, p.ViewportID, time.Now())
		s.mu.Unlock()
		if !ok {
			return result, ErrStaleViewport
		}
		current, err := s.viewportState(ctx)
		if err != nil {
			return result, s.explainFailure(err)
		}
		if current.id != grant.ID {
			s.dropViewport(grant.ID)
			return result, ErrStaleViewport
		}
		vp = grant
	}
	events, err := planInput(p, vp)
	if err != nil {
		return result, err
	}
	m, pointerID := s.pointerOwner()
	send := func(ctx context.Context, method string, params map[string]any) error {
		if method == cdpMouseEvent && params["type"] == "mousePressed" {
			s.presentMouseAim(m, pointerID, params)
		}
		err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
			c := chromedp.FromContext(ctx)
			if c == nil || c.Target == nil {
				return errors.New("no active browser target")
			}
			return c.Target.Execute(ctx, method, params, nil)
		}))
		if err == nil && method == cdpMouseEvent {
			s.presentMouseEvent(m, pointerID, params)
		}
		return err
	}
	if IsPointerAction(p.Action) {
		// A raw CDP press left without its release would turn this
		// action into an accidental drag; release it first.
		if err := s.releaseHeldPointer(ctx, send); err != nil {
			return inputFailureResult(result, err), s.explainFailure(err)
		}
	}
	result.EventsSent, err = s.dispatchSessionInput(ctx, send, events)
	if err != nil {
		return inputFailureResult(result, err), s.explainFailure(err)
	}
	result.Next = "Input dispatched; this does not prove the intended change. Verify the result with a screenshot or DOM check."
	if IsPointerAction(p.Action) {
		after, err := s.viewportState(ctx)
		if err == nil && after.id == vp.ID {
			result.ViewportID = vp.ID
		} else {
			s.dropViewport(vp.ID)
			result.ViewportChanged = true
			result.Next = "Input dispatched and the viewport changed; take a new screenshot before further pointer input."
		}
	}
	return result, nil
}

func (s *chromedpSession) dropViewport(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.viewports[:0]
	for _, g := range s.viewports {
		if g.ID != id {
			kept = append(kept, g)
		}
	}
	s.viewports = kept
}

func (s *chromedpSession) releaseHeldPointer(ctx context.Context, send inputSender) error {
	s.inputMu.Lock()
	defer s.inputMu.Unlock()
	if err := s.recoverInputLocked(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	held, button, x, y := s.pointerHeld, s.pointerButton, s.pointerX, s.pointerY
	s.mu.Unlock()
	if !held {
		return nil
	}
	if button == "" {
		button = "left"
	}
	events := []inputEvent{{method: cdpMouseEvent, params: map[string]any{"type": "mouseReleased", "x": x, "y": y, "button": button, "buttons": 0, "clickCount": 1, "modifiers": 0}}}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	pending, err := releaseInput(cleanup, send, events)
	if err != nil {
		s.inputRecovery = &inputRecovery{ctx: context.WithoutCancel(ctx), send: send, releases: pending}
	}
	return err
}
