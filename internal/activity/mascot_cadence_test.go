package activity

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMascotStaysAliveThroughRealisticRun(t *testing.T) {
	start := time.Now()
	var a mascotAnimator
	a.perf.seed = 7
	seen := map[string]int{}
	started := 0
	var e Event
	next := time.Duration(0)
	working := false
	for d := time.Duration(0); d < 60*time.Second; d += time.Second / 60 {
		now := start.Add(d)
		if d >= next {
			working = !working
			if working {
				e = Event{Visible: true, Action: []string{"click", "type", "navigate", "observe", "scroll"}[int(d/time.Second)%5], PhaseAt: now}
				next = d + time.Duration(300+int(d/time.Millisecond)%900)*time.Millisecond
			} else {
				e = Event{Visible: true, Phase: PhaseThinking, PhaseAt: now}
				next = d + time.Duration(1200+int(d/time.Millisecond)%2600)*time.Millisecond
			}
		}
		a.step(e, now, Look{})
		if g := a.perf.take.gesture; g != nil && a.perf.take.start.Equal(now) {
			seen[g.name]++
			started++
		}
	}
	// Real runs flip between working and thinking constantly; the
	// performance must keep going regardless.
	require.GreaterOrEqual(t, started, 18, "%v", seen)
	require.GreaterOrEqual(t, len(seen), 14, "%v", seen)
}
