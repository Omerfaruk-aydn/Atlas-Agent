package activity

import (
	"context"
	"sync"
	"time"
)

type flowKey struct{}

type flow struct {
	mu        sync.Mutex
	session   string
	closed    bool
	paused    int
	managers  map[*Manager]struct{}
	done      chan struct{}
	finalized chan struct{}
	cancel    context.CancelFunc
	finish    func(bool)
	started   time.Time
	prompts   []*pendingPrompt
}

// PauseFlow removes control indicators while a run waits for user input.
// Resuming permits new activity without restoring the previous indicator.
func PauseFlow(ctx context.Context, session string) func() {
	f, _ := ctx.Value(flowKey{}).(*flow)
	if f == nil || f.session != session {
		ClearSession(session)
		return func() {}
	}
	f.mu.Lock()
	f.paused++
	owned := make([]*Manager, 0, len(f.managers))
	for m := range f.managers {
		m.mu.Lock()
		if m.flow == f {
			m.clearLocked()
			owned = append(owned, m)
		}
		m.mu.Unlock()
	}
	f.mu.Unlock()
	for _, m := range owned {
		m.Present()
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			f.mu.Lock()
			f.paused--
			f.mu.Unlock()
		})
	}
}

// AwaitUser shows the waiting expression while a run is blocked on an
// explicit user decision, such as a permission prompt. The returned
// function restores the thinking state unless newer activity replaced it.
func AwaitUser(ctx context.Context) func() {
	f, _ := ctx.Value(flowKey{}).(*flow)
	if f == nil {
		return func() {}
	}
	type mark struct {
		m  *Manager
		id uint64
	}
	var marks []mark
	f.mu.Lock()
	for m := range f.managers {
		m.mu.Lock()
		if m.flow == f && m.event.Visible && m.event.Persistent && m.enterLocked(StateAwaitPermission) {
			marks = append(marks, mark{m, m.event.ID})
		}
		m.mu.Unlock()
	}
	f.mu.Unlock()
	for _, k := range marks {
		k.m.signal()
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			for _, k := range marks {
				k.m.mu.Lock()
				restored := k.m.event.ID == k.id && k.m.event.State == StateAwaitPermission && k.m.event.Prompt == nil
				if restored {
					k.m.enterLocked(StateThinking)
				}
				k.m.mu.Unlock()
				if restored {
					k.m.signal()
				}
			}
		})
	}
}

// StartFlow binds visual ownership to the complete agent run, including
// provider waits between tools. Nested workers reuse their parent's flow.
func StartFlow(ctx context.Context, session string) (context.Context, func()) {
	ctx, finish := StartRun(ctx, session)
	return ctx, func() { finish(false) }
}

// StartRun is StartFlow with an outcome: finish(true) for a run that
// completed normally leaves a brief, banner-only completion pose. Nested
// workers reuse their parent's flow and cannot conclude it.
func StartRun(ctx context.Context, session string) (context.Context, func(completed bool)) {
	if existing, ok := ctx.Value(flowKey{}).(*flow); ok && existing.session == session {
		return ctx, func(bool) {}
	}
	ctx, cancel := context.WithCancel(ctx)
	f := &flow{session: session, managers: make(map[*Manager]struct{}), done: make(chan struct{}), finalized: make(chan struct{}), cancel: cancel, started: time.Now()}
	end := func(completed bool) {
		f.mu.Lock()
		if f.closed {
			f.mu.Unlock()
			<-f.finalized
			return
		}
		// A stopped run is cancelled, never completed.
		completed = completed && ctx.Err() == nil
		f.closed = true
		f.cancel()
		close(f.done)
		// Withdrawn requests can never be answered from a stale surface.
		f.dropPromptsLocked()
		managers := make([]*Manager, 0, len(f.managers))
		for m := range f.managers {
			managers = append(managers, m)
		}
		f.mu.Unlock()
		for _, m := range managers {
			m.mu.Lock()
			if m.flow == f {
				if completed && m.event.Visible && m.suspended == 0 {
					m.concludeLocked()
				} else {
					m.clearLocked()
				}
			}
			m.mu.Unlock()
			// Flush teardown before reporting the run as stopped. The renderer
			// restores the system cursor and hides its native/page surfaces.
			m.Present()
		}
		close(f.finalized)
	}
	f.finish = end
	if ctx.Done() != nil {
		go func() {
			select {
			case <-ctx.Done():
				end(false)
			case <-f.done:
			}
		}()
	}
	return context.WithValue(ctx, flowKey{}, f), end
}

// concludeLocked releases control immediately but keeps the banner alone on
// screen for the completion motion. Stop and the agent cursor are gone.
func (m *Manager) concludeLocked() {
	m.stopEvent.Store(nil)
	m.next++
	m.event.ID = m.next
	m.event.Visible = false
	m.event.Persistent = false
	m.event.CanStop = false
	m.event.Point = false
	m.event.Prompt = nil
	if !m.enterLocked(StateDone) {
		// Completion is final whatever the surface last displayed.
		m.event.State = StateIdle
		m.enterLocked(StateActive)
		m.enterLocked(StateDone)
	}
	m.event.RunEnded = m.event.PhaseAt
	m.event.FinishedUntil = m.event.PhaseAt.Add(mascotDoneLinger)
	m.flow = nil
}

func (m *Manager) clearLocked() {
	m.stopEvent.Store(nil)
	m.next++
	m.event.ID = m.next
	m.event.Visible = false
	m.event.Persistent = false
	m.event.CanStop = false
	m.event.Point = false
	m.event.PointerKind = ""
	m.event.FinishedUntil = time.Time{}
	m.event.Prompt = nil
	m.enterLocked(StateCancelled)
	m.flow = nil
}
