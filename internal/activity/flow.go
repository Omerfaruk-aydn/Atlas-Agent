package activity

import (
	"context"
	"sync"
	"time"
)

type flowKey struct{}

type flow struct {
	mu       sync.Mutex
	session  string
	closed   bool
	managers map[*Manager]struct{}
	done     chan struct{}
	cancel   context.CancelFunc
}

// StartFlow binds visual ownership to the complete agent run, including
// provider waits between tools. Nested workers reuse their parent's flow.
func StartFlow(ctx context.Context, session string) (context.Context, func()) {
	if existing, ok := ctx.Value(flowKey{}).(*flow); ok && existing.session == session {
		return ctx, func() {}
	}
	ctx, cancel := context.WithCancel(ctx)
	f := &flow{session: session, managers: make(map[*Manager]struct{}), done: make(chan struct{}), cancel: cancel}
	end := func() {
		f.mu.Lock()
		if f.closed {
			f.mu.Unlock()
			return
		}
		f.closed = true
		f.cancel()
		close(f.done)
		managers := make([]*Manager, 0, len(f.managers))
		for m := range f.managers {
			managers = append(managers, m)
		}
		f.mu.Unlock()
		for _, m := range managers {
			m.mu.Lock()
			if m.flow == f {
				m.clearLocked()
			}
			m.mu.Unlock()
			m.signal()
		}
	}
	if ctx.Done() != nil {
		go func() {
			select {
			case <-ctx.Done():
				end()
			case <-f.done:
			}
		}()
	}
	return context.WithValue(ctx, flowKey{}, f), end
}

func (m *Manager) clearLocked() {
	m.stopEvent.Store(nil)
	m.next++
	m.event.ID = m.next
	m.event.Visible = false
	m.event.Persistent = false
	m.event.FinishedUntil = time.Time{}
	m.flow = nil
}
