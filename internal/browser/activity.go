package browser

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/activity"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/i18n"
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

// SetRespondHandler lets the native control island answer the exact
// request revision it displays. The page overlay never answers anything.
func (r browserActivityRenderer) SetRespondHandler(respond func(uint64, activity.PromptResponse) error) {
	if native, ok := r.session.activityNative.(interface {
		SetRespondHandler(func(uint64, activity.PromptResponse) error)
	}); ok {
		native.SetRespondHandler(respond)
	}
}

// SetStopHandler forwards the exact manager revision to the native overlay.
func (r browserActivityRenderer) SetStopHandler(stop func(uint64) bool) {
	if native, ok := r.session.activityNative.(interface{ SetStopHandler(func(uint64) bool) }); ok {
		native.SetStopHandler(stop)
	}
}

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
	if r.session.activityNative != nil {
		r.session.activityNative.Render(e)
		return
	}
	remaining := time.Until(e.FinishedUntil).Milliseconds()
	visible := e.Visible || remaining > 0
	_, stop := activity.BannerText(e)
	now := time.Now()
	caption := r.session.activityStatus.Text(e, now)
	canStop := e.CanStop && visible && r.session.ensureActivityStopWatcher()
	// Page scripts can reach this overlay, so a pending request is only
	// announced here and answered in Atlas, never from the page.
	waiting := e.Prompt != nil && e.Visible
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
		PointerStamp    string `json:"pointer_stamp"`
		Done            bool   `json:"done"`
		Elapsed         int64  `json:"elapsed"`
		Running         bool   `json:"running"`
		Waiting         bool   `json:"waiting"`
		WaitingHint     string `json:"waiting_hint"`
	}{
		activity.CursorSVG, activity.BannerMascotSVG, visible, e.ID, x, y, max(0, remaining), e.Point, e.Persistent, e.ReducedMotion, caption, stop, canStop, e.PointerKind, e.PointerRevision, max(0, time.Since(e.PointerAt).Milliseconds()), e.PointerAt.UTC().Format(time.RFC3339Nano), e.Phase == activity.PhaseDone && !e.Persistent,
		e.Elapsed(now).Milliseconds(), !e.RunStarted.IsZero() && e.RunEnded.IsZero(), waiting, i18n.Text(e.Language, "Answer in the terminal"),
	})

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
	r.session.activityMascot.update(e, visible, r.session.paintMascot)
}

func (r browserActivityRenderer) Close() {
	if r.session.activityNative != nil {
		r.session.activityNative.Close()
		return
	}
	r.session.activityMascot.close()
	r.session.activityEval(activityRemove, nil)
}

// BeginActivity is optional for non-chromedp and mock browser drivers.
func (s *chromedpSession) BeginActivity(ctx context.Context, sessionID, action, selector string) func() {
	return s.StartActivity(ctx, sessionID, action, selector).End
}

// StartActivity returns the visible operation so its outcome can be recorded.
func (s *chromedpSession) StartActivity(ctx context.Context, sessionID, action, selector string) activity.Operation {
	if !s.activityEnabled {
		return activity.Operation{}
	}
	s.mu.Lock()
	if s.activityClosed {
		s.mu.Unlock()
		return activity.Operation{}
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
	return manager.Start(ctx, e)
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
	// CDP captures the document, not desktop windows. The native indicator
	// cannot pollute this capture and must survive observations/navigation.
	if s.activityNative != nil {
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

// CloseActivity releases native or page surfaces before a session is retired.
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
		if s.activityNative != nil {
			return nil
		}
		return s.activityEval(activityRemove, nil)
	}
	if s.activityNative != nil {
		s.activityNative.Close()
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
