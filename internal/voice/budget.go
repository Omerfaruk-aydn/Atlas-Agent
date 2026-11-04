package voice

import (
	"fmt"
	"sync"
	"time"
)

// minUsefulCall is the shortest call worth admitting.
const minUsefulCall = 15 * time.Second

// Budget enforces concurrency, per-call duration, daily minutes and the
// outbound call rate.
type Budget struct {
	cfg   Config
	store *Store
	now   func() time.Time

	mu     sync.Mutex
	active map[string]bool
}

// NewBudget builds a budget over store.
func NewBudget(cfg Config, store *Store, now func() time.Time) *Budget {
	if now == nil {
		now = time.Now
	}
	return &Budget{cfg: cfg.WithDefaults(), store: store, now: now, active: map[string]bool{}}
}

func (b *Budget) dayStart() time.Time {
	t := b.now().In(b.cfg.Location())
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// RemainingToday is the call time left today.
func (b *Budget) RemainingToday() time.Duration {
	used := b.store.SecondsSince(b.dayStart(), b.now())
	left := b.cfg.MaxDailyMinutes*60 - used
	return time.Duration(max(left, 0)) * time.Second
}

// Admit reserves a call slot. It returns how long the call may last and a
// release function that must be called when the call ends.
func (b *Budget) Admit(callID string) (time.Duration, func(), error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.active[callID] {
		return 0, nil, ErrCallExists
	}
	if len(b.active) >= b.cfg.MaxConcurrentCalls {
		return 0, nil, fmt.Errorf("%w: %d call(s) already active", ErrBudgetExceeded, len(b.active))
	}
	limit := min(time.Duration(b.cfg.MaxCallSeconds)*time.Second, b.RemainingToday())
	if limit < minUsefulCall {
		return 0, nil, fmt.Errorf("%w: daily minutes used up", ErrBudgetExceeded)
	}
	b.active[callID] = true
	var once sync.Once
	return limit, func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.active, callID)
			b.mu.Unlock()
		})
	}, nil
}

// Active is the number of admitted calls.
func (b *Budget) Active() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.active)
}

// AllowOutbound checks the outbound rate. Urgent calls skip the minimum
// interval but never the hourly cap.
func (b *Budget) AllowOutbound(priority string) error {
	now := b.now()
	recent := b.store.OutboundSince(now.Add(-time.Hour))
	if len(recent) >= b.cfg.Outbound.MaxPerHour {
		return fmt.Errorf("%w: %d outbound calls in the last hour", ErrRateLimited, len(recent))
	}
	if priority != PriorityUrgent && len(recent) > 0 {
		last := recent[len(recent)-1].CreatedAt
		if gap := time.Duration(b.cfg.Outbound.MinIntervalSeconds) * time.Second; now.Sub(last) < gap {
			return fmt.Errorf("%w: last outbound call was %s ago", ErrRateLimited, now.Sub(last).Round(time.Second))
		}
	}
	if b.RemainingToday() < minUsefulCall {
		return fmt.Errorf("%w: daily minutes used up", ErrBudgetExceeded)
	}
	return nil
}
