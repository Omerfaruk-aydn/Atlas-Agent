package support

import (
	"context"
	"sync"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/activity"
)

// ActivityOverlay shows the banner and agent cursor during remote work.
// An idle connection never claims the owner's cursor. Overlapping operations
// share a flow, which releases control when the last operation ends.
type ActivityOverlay struct {
	Manager  *activity.Manager
	Language string

	mu   sync.Mutex
	key  string
	stop func()
	work *overlayWork
}

type overlayWork struct {
	ctx     context.Context
	finish  func(bool)
	pending int
	closed  bool
}

var _ Overlay = (*ActivityOverlay)(nil)

// Start implements Overlay. The peer name is not drawn: the banner carries
// only Atlas-authored text; the owner's /support screen names the peer.
func (o *ActivityOverlay) Start(sessionID, _ string, stop func()) {
	o.End()
	o.mu.Lock()
	o.key, o.stop = "support-"+sessionID, stop
	o.mu.Unlock()
}

// Operation implements Overlay.
func (o *ActivityOverlay) Operation(action string, x, y int, point bool) func() {
	o.mu.Lock()
	if o.key == "" {
		o.mu.Unlock()
		return func() {}
	}
	if o.work == nil {
		ctx := activity.WithLanguage(context.Background(), o.Language)
		ctx, finish := activity.StartRun(ctx, o.key)
		o.work = &overlayWork{ctx: ctx, finish: finish}
		go o.watchStop(o.work)
	}
	w := o.work
	if w.ctx.Err() != nil {
		o.mu.Unlock()
		return func() {}
	}
	w.pending++
	op := o.manager().Start(w.ctx, activity.Event{Session: o.key, Resource: "support", Action: action, X: x, Y: y, Point: point})
	o.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			o.mu.Lock()
			w.pending--
			finished := o.work == w && w.pending == 0 && w.ctx.Err() == nil
			if finished {
				w.closed = true
				o.work = nil
			}
			o.mu.Unlock()
			op.End()
			if finished {
				w.finish(false)
			}
		})
	}
}

// watchStop distinguishes a banner stop from normal work/session teardown.
func (o *ActivityOverlay) watchStop(w *overlayWork) {
	<-w.ctx.Done()
	o.mu.Lock()
	var stop func()
	if o.work == w && !w.closed {
		w.closed = true
		stop = o.stop
	}
	o.mu.Unlock()
	if stop != nil {
		stop()
	}
}

// End implements Overlay.
func (o *ActivityOverlay) End() {
	o.mu.Lock()
	w := o.work
	if w != nil {
		w.closed = true
	}
	o.work, o.key, o.stop = nil, "", nil
	o.mu.Unlock()
	if w != nil {
		w.finish(false)
	}
}

func (o *ActivityOverlay) manager() *activity.Manager {
	if o.Manager != nil {
		return o.Manager
	}
	return activity.Default
}
