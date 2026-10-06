package activity

import "time"

// Context flags describe what the run has been like, derived only from
// verified events: operation starts, recorded failures and how long a mood
// has lasted. They steer which gestures the performer improvises, never
// the mood itself.
const (
	ctxHello      uint16 = 1 << iota // Atlas just appeared.
	ctxStreak                        // Several operations in a row succeeded.
	ctxFrustrated                    // Repeated recent failures.
	ctxRecovered                     // Work resumed shortly after a failure.
	ctxLongThink                     // The model has been thinking a while.
	ctxLongWait                      // The user has not answered for a while.
	ctxLongType                      // A long typing stretch.
)

var mascotContextNames = map[string]uint16{
	"hello": ctxHello, "streak": ctxStreak, "frustrated": ctxFrustrated, "recovered": ctxRecovered,
	"longThink": ctxLongThink, "longWait": ctxLongWait, "longType": ctxLongType,
}

const (
	mascotHelloWindow   = 3 * time.Second
	mascotStreakWindow  = 12 * time.Second
	mascotStreakOps     = 4
	mascotFrustrateSpan = 45 * time.Second
	mascotRecoverWindow = 10 * time.Second
	mascotLongThink     = 5 * time.Second
	mascotLongWait      = 7 * time.Second
	mascotLongType      = 3 * time.Second
)

type mascotContext struct {
	opAt, failAt time.Time
	ops          [mascotStreakOps]time.Time // Most recent first.
	fails        [2]time.Time
	typeSince    time.Time
}

// observe records operation starts and failures by their event stamps, so
// a repeated snapshot of the same event is never counted twice.
func (c *mascotContext) observe(e Event, mood mascotMood, now time.Time) {
	switch {
	case e.Phase == PhaseWorking && !e.PhaseAt.IsZero() && !e.PhaseAt.Equal(c.opAt):
		c.opAt = e.PhaseAt
		copy(c.ops[1:], c.ops[:len(c.ops)-1])
		c.ops[0] = e.PhaseAt
	case e.Phase == PhaseFailed && !e.PhaseAt.IsZero() && !e.PhaseAt.Equal(c.failAt):
		c.failAt = e.PhaseAt
		copy(c.fails[1:], c.fails[:len(c.fails)-1])
		c.fails[0] = e.PhaseAt
	}
	// A typing stretch survives short provider waits between keystrokes.
	switch {
	case mood == moodWorking && mascotWorkFor(e.Action) == workType:
		if c.typeSince.IsZero() {
			c.typeSince = now
		}
	case mood == moodThinking:
	default:
		c.typeSince = time.Time{}
	}
}

// recent reports whether at lies within span before now.
func recent(now, at time.Time, span time.Duration) bool {
	return !at.IsZero() && !at.After(now) && now.Sub(at) < span
}

func (c *mascotContext) flags(mood mascotMood, epoch, moodAt, now time.Time) uint16 {
	var f uint16
	if now.Sub(epoch) < mascotHelloWindow {
		f |= ctxHello
	}
	oldest := c.ops[len(c.ops)-1]
	if recent(now, oldest, mascotStreakWindow) && !recent(now, c.failAt, mascotStreakWindow) {
		f |= ctxStreak
	}
	if recent(now, c.fails[len(c.fails)-1], mascotFrustrateSpan) {
		f |= ctxFrustrated
	}
	if recent(now, c.failAt, mascotRecoverWindow) && now.Sub(c.failAt) > mascotBlockedHold && c.opAt.After(c.failAt) {
		f |= ctxRecovered
	}
	if mood == moodThinking && now.Sub(moodAt) > mascotLongThink {
		f |= ctxLongThink
	}
	if mood == moodWaiting && now.Sub(moodAt) > mascotLongWait {
		f |= ctxLongWait
	}
	if !c.typeSince.IsZero() && now.Sub(c.typeSince) > mascotLongType {
		f |= ctxLongType
	}
	return f
}
