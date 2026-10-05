// Package activity renders bounded, non-sensitive agent activity metadata.
package activity

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// Event deliberately contains no user text, URLs or credentials.
type Event struct {
	Session            string
	Resource           string
	Action             string
	WindowID           string
	X, Y               int
	Point              bool
	ReducedMotion      bool
	Persistent         bool
	CanStop            bool
	Language           string
	Visible            bool
	ID                 uint64
	FinishedUntil      time.Time
	PointerKind        string
	PointerRevision    uint64
	PointerAt          time.Time
	PointerX, PointerY float64
	Phase              Phase
	PhaseAt            time.Time
	// State is the owning run's work state; Phase is its expression.
	State   RunState
	StateAt time.Time
	// RunStarted carries a monotonic reading from the run's start, so the
	// elapsed time survives tools, tabs and questions. RunEnded freezes it.
	RunStarted, RunEnded time.Time
	// Prompt is the displayed pending request, shared read-only.
	Prompt *Prompt
	// AfterFailure marks an operation that follows a verified failure.
	AfterFailure bool
}

// Renderer must render only metadata and tolerate an invisible event.
type Renderer interface {
	Render(Event)
	Close()
}

// Manager coalesces visual updates independently of tool execution.
type Manager struct {
	mu        sync.Mutex
	renderMu  sync.Mutex
	flow      *flow
	renderer  Renderer
	event     Event
	next      uint64
	suspended int
	closed    bool
	started   bool
	wake      chan struct{}
	stop      chan struct{}
	done      chan struct{}
	stopEvent atomic.Pointer[Event]
}

var managers sync.Map

// ClearSession removes native and browser indicators on handoff.
func ClearSession(session string) {
	managers.Range(func(key, value any) bool { key.(*Manager).Clear(session); return true })
}

// CancelSession synchronously releases control owned by the cancelled session.
// Other chats and their active flows retain their own indicators.
func CancelSession(session string) {
	managers.Range(func(key, value any) bool {
		m := key.(*Manager)
		m.mu.Lock()
		f := m.flow
		owned := m.event.Session == session
		m.mu.Unlock()
		if owned && f != nil {
			f.finish(false)
		} else if owned {
			m.Clear(session)
			m.Present()
		}
		return true
	})
}

func New(r Renderer) *Manager {
	m := &Manager{renderer: r, wake: make(chan struct{}, 1), stop: make(chan struct{}), done: make(chan struct{})}
	if aware, ok := r.(interface{ SetStopHandler(func(uint64) bool) }); ok {
		aware.SetStopHandler(m.Stop)
	}
	if aware, ok := r.(interface {
		SetRespondHandler(func(uint64, PromptResponse) error)
	}); ok {
		aware.SetRespondHandler(m.Respond)
	}
	managers.Store(m, true)
	return m
}

func (m *Manager) loop() {
	defer close(m.done)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-m.stop:
			return
		case <-ticker.C:
			if e := m.Snapshot(); e.Visible && e.Persistent {
				m.signal()
			}
		case <-m.wake:
			m.renderMu.Lock()
			m.renderer.Render(m.Snapshot())
			m.renderMu.Unlock()
		}
	}
}

func (m *Manager) signal() {
	m.mu.Lock()
	e := m.event
	if !m.closed && m.suspended == 0 && e.Visible && e.CanStop {
		m.stopEvent.Store(&e)
	} else {
		m.stopEvent.Store(nil)
	}
	m.mu.Unlock()
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

// StopRevision captures the visible owner without blocking a native key hook.
func (m *Manager) StopRevision() uint64 {
	if e := m.stopEvent.Load(); e != nil {
		return e.ID
	}
	return 0
}

// Operation is one visible tool action. Its recorded outcome, not model
// text, drives the banner character.
type Operation struct {
	m   *Manager
	id  uint64
	end func()
}

// End is idempotent and safe on a zero Operation.
func (o Operation) End() {
	if o.end != nil {
		o.end()
	}
}

// Fail records a verified tool failure before the operation ends. It never
// affects a newer operation or a cleared indicator.
func (o Operation) Fail() {
	if o.m == nil || o.id == 0 {
		return
	}
	o.m.mu.Lock()
	changed := o.m.event.ID == o.id && o.m.event.Visible
	if changed {
		changed = o.m.enterLocked(StateFailed)
	}
	o.m.mu.Unlock()
	if changed {
		o.m.signal()
	}
}

// Begin returns an idempotent finish function scoped to this operation.
func (m *Manager) Begin(ctx context.Context, e Event) func() {
	return m.Start(ctx, e).End
}

// Start shows an operation and returns its handle.
func (m *Manager) Start(ctx context.Context, e Event) Operation {
	none := Operation{end: func() {}}
	if ctx.Err() != nil {
		return none
	}
	f, _ := ctx.Value(flowKey{}).(*flow)
	if f != nil && f.session == e.Session {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.closed || f.paused > 0 {
			return none
		}
		e.Persistent = true
		e.RunStarted = f.started
		f.managers[m] = struct{}{}
		for other := range f.managers {
			if other == m {
				continue
			}
			other.mu.Lock()
			if other.flow == f {
				other.clearLocked()
			}
			other.mu.Unlock()
			other.signal()
		}
	} else {
		f = nil
	}
	e.CanStop = f != nil
	if e.Language == "" {
		e.Language, _ = ctx.Value(languageKey{}).(string)
	}
	m.mu.Lock()
	if m.closed || ctx.Err() != nil {
		m.mu.Unlock()
		return none
	}
	if !m.started {
		m.started = true
		go m.loop()
	}
	m.next++
	if f != nil && m.flow == f && m.event.PointerKind == "click" && time.Since(m.event.PointerAt) < pointerClickDuration && e.PointerKind == "" && !e.Point {
		e.PointerKind, e.PointerAt, e.PointerRevision = m.event.PointerKind, m.event.PointerAt, m.event.PointerRevision
		e.PointerX, e.PointerY = m.event.PointerX, m.event.PointerY
	}
	e.ID = m.next
	e.Visible = true
	if e.RunStarted.IsZero() {
		e.RunStarted = time.Now()
	}
	previous := m.event.State
	if m.flow != f || m.event.Session != e.Session {
		previous = StateIdle
	}
	e.AfterFailure = previous == StateFailed && f != nil
	e.State, e.StateAt, e.Phase, e.PhaseAt = previous, m.event.StateAt, m.event.Phase, m.event.PhaseAt
	m.event = e
	m.flow = f
	if !m.enterLocked(StateTool) {
		// An operation always runs as a tool, whatever came before it.
		m.event.State = StateIdle
		m.enterLocked(StateTool)
	}
	if f != nil {
		// Start holds f.mu for its whole body.
		if head, ok := f.headLocked(); ok {
			m.showPromptLocked(head, true, 0)
		}
	}
	m.mu.Unlock()
	m.signal()
	var once sync.Once
	finished := make(chan struct{})
	end := func() {
		once.Do(func() {
			close(finished)
			m.mu.Lock()
			if m.event.ID == e.ID && m.event.Persistent {
				m.event.Action = "wait"
				// A recorded failure stays visible while the model reacts,
				// and a pending request keeps the run waiting.
				if m.event.State == StateTool {
					m.enterLocked(StateThinking)
				}
				if m.event.PointerKind != "drag" && m.event.PointerKind != "drag_move" && m.event.PointerKind != "press" {
					m.event.Point = false
				}
			} else if m.event.ID == e.ID {
				m.event.Visible = false
				if ctx.Err() == nil {
					m.event.FinishedUntil = time.Now().Add(550 * time.Millisecond)
				}
			}
			m.mu.Unlock()
			m.signal()
		})
	}
	if ctx.Done() != nil {
		go func() {
			select {
			case <-ctx.Done():
				end()
			case <-finished:
			}
		}()
	}
	return Operation{m: m, id: e.ID, end: end}
}

// Stop cancels only the run owning this exact visible banner revision.
func (m *Manager) Stop(id uint64) bool {
	m.mu.Lock()
	f := m.flow
	valid := !m.closed && m.suspended == 0 && m.event.Visible && m.event.CanStop && m.event.ID == id && f != nil
	m.mu.Unlock()
	if !valid {
		return false
	}
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return false
	}
	f.mu.Unlock()
	f.finish(false)
	return true
}

// Snapshot is the current visible state, including capture suspension.
func (m *Manager) Snapshot() Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.event
	if m.suspended > 0 || m.closed {
		e.Visible = false
		e.FinishedUntil = time.Time{}
	}
	return e
}

// Suspend synchronously removes visuals until the returned restoration runs.
func (m *Manager) Suspend() func() {
	m.renderMu.Lock()
	m.mu.Lock()
	m.suspended++
	m.stopEvent.Store(nil)
	m.mu.Unlock()
	m.renderer.Render(m.Snapshot())
	m.renderMu.Unlock()
	var once sync.Once
	return func() { once.Do(func() { m.mu.Lock(); m.suspended--; m.mu.Unlock(); m.signal() }) }
}

// SuspendForObservation keeps a run-owned cursor on screen. Standalone
// operations retain the clean-capture suspension behavior.
func (m *Manager) SuspendForObservation() func() {
	if e := m.Snapshot(); e.Persistent && e.Visible {
		return func() {}
	}
	return m.Suspend()
}

// Clear hides activity only when it belongs to this session.
func (m *Manager) Clear(session string) {
	m.mu.Lock()
	if m.event.Session == session {
		m.clearLocked()
	}
	m.mu.Unlock()
	m.signal()
}

// Close removes visuals and releases renderer resources.
func (m *Manager) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	started := m.started
	m.stopEvent.Store(nil)
	managers.Delete(m)
	close(m.stop)
	m.mu.Unlock()
	if started {
		<-m.done
	} else {
		close(m.done)
	}
	m.renderMu.Lock()
	defer m.renderMu.Unlock()
	m.renderer.Render(Event{})
	m.renderer.Close()
}

// Label maps tool operations to generic user-facing activity.
func Label(action string) string {
	switch action {
	case "click", "double_click", "right_click", "invoke", "semantic_click":
		return "Tıklıyor"
	case "type", "set_value", "vault_fill", "auth_code", "semantic_type":
		return "Yazıyor"
	case "navigate", "back", "forward", "launch_app":
		return "Açıyor"
	case "wait", "wait_for":
		return "Bekliyor"
	case "scroll", "drag", "move", "key", "hotkey":
		return "Kontrol ediyor"
	default:
		return "İnceliyor"
	}
}

var Default = New(newPlatformRenderer())

// Target updates only the active operation after its target has been verified.
func (m *Manager) Target(session, window string, x, y int) {
	m.mu.Lock()
	if m.event.Session == session && m.event.Visible {
		m.event.WindowID = window
		m.event.X = x
		m.event.Y = y
		m.event.Point = true
		m.event.PointerKind = "move"
		m.event.PointerAt = time.Now()
		m.event.PointerRevision++
		m.event.PointerX, m.event.PointerY = float64(x), float64(y)
	}
	m.mu.Unlock()
	m.signal()
}
