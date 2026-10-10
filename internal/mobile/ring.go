package mobile

import (
	"encoding/json"
	"sync"
	"time"
)

// Event is one ordered state change sent to phones.
type Event struct {
	Seq       uint64 `json:"seq"`
	Type      string `json:"type"`
	SessionID string `json:"session_id,omitempty"`
	At        int64  `json:"at"`
	Data      any    `json:"data"`
}

// ring keeps the most recent events so a reconnecting phone can replay
// exactly what it missed, in order, or learn it must resync.
type ring struct {
	mu     sync.Mutex
	cond   *sync.Cond
	events []Event
	size   int
	seq    uint64
}

func newRing(size int) *ring {
	r := &ring{size: size}
	r.cond = sync.NewCond(&r.mu)
	return r
}

func (r *ring) add(typ, session string, data any) Event {
	// Snapshot data now so later mutation by the producer cannot change a
	// delivered event.
	raw, _ := json.Marshal(data)
	r.mu.Lock()
	r.seq++
	e := Event{Seq: r.seq, Type: typ, SessionID: session, At: time.Now().UnixMilli(), Data: json.RawMessage(raw)}
	r.events = append(r.events, e)
	if len(r.events) > r.size {
		r.events = r.events[len(r.events)-r.size:]
	}
	r.mu.Unlock()
	r.cond.Broadcast()
	return e
}

// since returns events after seq. gap is true when events after seq were
// already evicted, so the client must reload full state.
func (r *ring) since(seq uint64) (out []Event, gap bool, last uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	last = r.seq
	if seq > r.seq {
		// The client saw a sequence from an earlier gateway run.
		return nil, true, last
	}
	if len(r.events) > 0 && seq+1 < r.events[0].Seq {
		gap = true
	}
	for _, e := range r.events {
		if e.Seq > seq {
			out = append(out, e)
		}
	}
	return out, gap, last
}

// wait blocks until an event after seq exists or the timeout passes.
func (r *ring) wait(seq uint64, timeout time.Duration) {
	done := make(chan struct{})
	timer := time.AfterFunc(timeout, func() {
		r.mu.Lock()
		close(done)
		r.mu.Unlock()
		r.cond.Broadcast()
	})
	defer timer.Stop()
	r.mu.Lock()
	defer r.mu.Unlock()
	for r.seq <= seq {
		select {
		case <-done:
			return
		default:
		}
		r.cond.Wait()
	}
}
