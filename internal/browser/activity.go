package browser

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/activity"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

//go:embed activity.js
var activityScript string

const (
	activityRemove  = `(() => { const a=window[Symbol.for('atlas.agent.activity.v1')]; if(a) a.remove(); })()`
	activityRestore = `(() => { const a=window[Symbol.for('atlas.agent.activity.v1')]; if(a) a.restore(); })()`
)

type browserActivityRenderer struct{ session *chromedpSession }

func (s *chromedpSession) ensureActivityStopWatcher() bool {
	s.activityStopOnce.Do(func() {
		manager := s.getActivityManager()
		s.activityStopWatch = activity.WatchEscape(func() func() {
			id := manager.StopRevision()
			if id == 0 {
				return nil
			}
			return func() { s.handleActivityEscape(id) }
		})
	})
	return s.activityStopWatch != nil
}

// Native physical input is never delegated to page JavaScript or a CDP binding.
func (s *chromedpSession) handleActivityEscape(id uint64) {
	m := s.getActivityManager()
	if m == nil {
		return
	}
	e := m.Snapshot()
	if !e.Visible || !e.CanStop || e.ID != id {
		return
	}
	if focused, err := s.activityFocused(); err == nil && focused {
		m.Stop(e.ID)
	}
}

// Read focus from native DOM methods in a realm page scripts cannot modify.
func (s *chromedpSession) activityFocused() (bool, error) {
	ctx, cancel := context.WithTimeout(s.currentContext(), 300*time.Millisecond)
	defer cancel()
	var focused bool
	err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		frames, err := page.GetFrameTree().Do(ctx)
		if err != nil {
			return err
		}
		world, err := page.CreateIsolatedWorld(frames.Frame.ID).WithWorldName("AtlasControlFocus").Do(ctx)
		if err != nil {
			return err
		}
		result, exception, err := runtime.Evaluate(`Document.prototype.hasFocus.call(document)`).WithContextID(world).WithReturnByValue(true).Do(ctx)
		if err != nil {
			return err
		}
		if exception != nil {
			return fmt.Errorf("read control page focus: %s", exception.Error())
		}
		return json.Unmarshal(result.Value, &focused)
	}))
	return focused, err
}

func (r browserActivityRenderer) Render(e activity.Event) {
	remaining := time.Until(e.FinishedUntil).Milliseconds()
	visible := e.Visible || remaining > 0
	caption, stop := activity.BannerText(e)
	canStop := e.CanStop && visible && r.session.ensureActivityStopWatcher()
	x, y := float64(e.X), float64(e.Y)
	if e.PointerRevision != 0 {
		x, y = e.PointerX, e.PointerY
	}
	payload, _ := json.Marshal(struct {
		Cursor          string `json:"cursor"`
		BannerIcon      string `json:"banner_icon"`
		Visible         bool   `json:"visible"`
		ID              uint64 `json:"id"`
		X, Y            float64
		Duration        int64 `json:"duration"`
		Point           bool
		Persistent      bool   `json:"persistent"`
		Reduced         bool   `json:"reduced"`
		Caption         string `json:"caption"`
		Stop            string `json:"stop"`
		CanStop         bool   `json:"can_stop"`
		PointerKind     string `json:"pointer_kind"`
		PointerRevision uint64 `json:"pointer_revision"`
		PointerAge      int64  `json:"pointer_age"`
	}{activity.CursorSVG, activity.BannerPointerSVG, visible, e.ID, x, y, max(0, remaining), e.Point, e.Persistent, e.ReducedMotion, caption, stop, canStop, e.PointerKind, e.PointerRevision, max(0, time.Since(e.PointerAt).Milliseconds())})

	script := fmt.Sprintf("(%s)(%s)", activityScript, payload)
	if e.PointerKind == "aim" {
		ctx, cancel := context.WithTimeout(r.session.currentContext(), 500*time.Millisecond)
		defer cancel()
		err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
			_, exception, err := runtime.Evaluate(script).WithAwaitPromise(true).Do(ctx)
			if err != nil {
				return err
			}
			if exception != nil {
				return fmt.Errorf("present pointer: %s", exception.Error())
			}
			return nil
		}))
		if err != nil {
			slog.Debug("Pointer presentation unavailable", "error", err)
		}
	} else {
		r.session.activityEval(script, nil)
	}
}
func (r browserActivityRenderer) Close() { r.session.activityEval(activityRemove, nil) }

// BeginActivity is optional for non-chromedp and mock browser drivers.
func (s *chromedpSession) BeginActivity(ctx context.Context, sessionID, action, selector string) func() {
	if !s.activityEnabled {
		return func() {}
	}
	s.mu.Lock()
	if s.activityClosed {
		s.mu.Unlock()
		return func() {}
	}
	if s.activityManager == nil {
		s.activityManager = activity.New(browserActivityRenderer{s})
	}
	manager := s.activityManager
	s.mu.Unlock()
	e := activity.Event{Session: sessionID, Resource: "browser", Action: action, ReducedMotion: s.activityReducedMotion}
	s.mu.Lock()
	if s.pointerHeld {
		e.PointerKind = "press"
		if s.pointerDragged {
			e.PointerKind = "drag_move"
		}
		e.PointerRevision, e.PointerAt = 1, time.Now()
		e.PointerX, e.PointerY, e.Point = s.pointerX, s.pointerY, true
	}
	s.mu.Unlock()
	if selector != "" && len(selector) <= 4096 {
		value, _ := json.Marshal(selector)
		var point struct {
			X, Y int
			OK   bool
		}
		s.activityEval(fmt.Sprintf(`(() => {try {const es=document.querySelectorAll(%s);if(es.length!==1)return {OK:false};const r=es[0].getBoundingClientRect();return {X:Math.round(r.x+r.width/2),Y:Math.round(r.y+r.height/2),OK:r.width>0&&r.height>0&&r.x>=0&&r.y>=0&&r.bottom<=innerHeight&&r.right<=innerWidth};}catch{return {OK:false}}})()`, value), &point)
		e.X, e.Y, e.Point = point.X, point.Y, point.OK
	}
	return manager.Begin(ctx, e)
}

func (s *chromedpSession) activityEval(script string, result any) error {
	ctx, cancel := context.WithTimeout(s.currentContext(), 300*time.Millisecond)
	defer cancel()
	err := chromedp.Run(ctx, chromedp.Evaluate(script, result))
	if err != nil {
		slog.Debug("Activity overlay update unavailable", "error", err)
	}
	return err
}

func (s *chromedpSession) getActivityManager() *activity.Manager {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.activityManager
}

func (s *chromedpSession) suspendActivity(force ...bool) (func(), error) {
	manager := s.getActivityManager()
	if manager == nil {
		return func() {}, nil
	}
	if e := manager.Snapshot(); e.Persistent && e.Visible && (len(force) == 0 || !force[0]) {
		return func() {}, nil
	}
	resume := manager.Suspend()
	if err := s.activityEval(activityRemove, nil); err != nil {
		resume()
		return nil, fmt.Errorf("cannot exclude activity from observation: %w", err)
	}
	return func() { _ = s.activityEval(activityRestore, nil); resume() }, nil
}

// CloseActivity removes the page surface before a session is retired.
func (s *chromedpSession) CloseActivity() error {
	s.activityStopOnce.Do(func() {})
	if s.activityStopWatch != nil {
		s.activityStopWatch()
	}
	s.mu.Lock()
	s.activityClosed = true
	manager := s.activityManager
	s.mu.Unlock()
	if manager != nil {
		manager.Close()
		return s.activityEval(activityRemove, nil)
	}
	return nil
}

func (s *chromedpSession) updateActivityPoint(x, y int) {
	if m := s.getActivityManager(); m != nil {
		e := m.Snapshot()
		m.Pointer(e.ID, "move", x, y)
	}
}

func (s *chromedpSession) pointerOwner() (*activity.Manager, uint64) {
	m := s.getActivityManager()
	if m == nil {
		return nil, 0
	}
	return m, m.Snapshot().ID
}

func presentPointer(m *activity.Manager, id uint64, kind string, x, y float64) {
	if m != nil && m.PointerPoint(id, kind, x, y) {
		m.Present()
	}
}
