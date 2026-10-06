package activity

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// offsetChannels flattens an offset with each channel's full range, so
// frame-to-frame changes can be compared fairly across channels.
func offsetChannels(o mascotOffset) [23][2]float64 {
	return [23][2]float64{
		{o.Yaw, 2},
		{o.Pitch, .6},
		{o.Roll, .6},
		{o.Squash, .6},
		{o.Lift, 5.5},
		{o.GazeX, 1},
		{o.GazeY, 7},
		{o.Open[0], 1.45},
		{o.Open[1], 1.45},
		{o.Smile, 2},
		{o.Slant, 1},
		{o.ArcLift, 7},
		{o.ArcTilt, .8},
		{o.Arm[0], 6},
		{o.Arm[1], 6},
		{o.MouthOpen, 1.5},
		{o.MouthCurve, 3},
		{o.MouthWide, 2},
		{o.MouthSkew, 2},
		{o.MouthWave, 2},
		{o.Blush, 2},
		{o.LapLift, 4},
		{o.LapRoll, .4},
	}
}

func TestMascotGestureLibraryIsValid(t *testing.T) {
	t.Parallel()
	var specs []mascotGestureSpec
	require.NoError(t, json.Unmarshal(mascotGestureData, &specs))
	require.GreaterOrEqual(t, len(specs), 60, "The library must stay large enough to feel improvised")
	names := map[string]bool{}
	for _, g := range mascotRepertoire {
		names[g.name] = true
	}
	for _, g := range mascotOnsets {
		names[g.name] = true
	}
	contexts := uint16(0)
	for _, s := range specs {
		g, err := compileMascotGesture(s)
		require.NoError(t, err)
		require.False(t, names[s.Name], "Duplicate gesture %q", s.Name)
		names[s.Name] = true
		contexts |= g.contexts
		for _, side := range []float64{-1, 1} {
			for _, v := range offsetChannels(g.play(0, side, 1.2)) {
				require.Zero(t, v[0], "%s must start from the base pose", s.Name)
			}
			for _, v := range offsetChannels(g.play(1, side, 1.2)) {
				require.Zero(t, v[0], "%s must end on the base pose", s.Name)
			}
			// The fastest possible take at 60 frames per second.
			frames := int(s.Seconds * .85 * 60)
			previous := offsetChannels(g.play(0, side, 1.2))
			for f := 1; f <= frames; f++ {
				current := offsetChannels(g.play(float64(f)/float64(frames), side, 1.2))
				for c := range current {
					require.LessOrEqual(t, math.Abs(current[c][0]), current[c][1],
						"%s overshoots channel %d", s.Name, c)
					require.LessOrEqual(t, math.Abs(current[c][0]-previous[c][0])/current[c][1], .16,
						"%s jumps on channel %d at frame %d", s.Name, c, f)
				}
				previous = current
			}
		}
	}
	for name, bit := range mascotContextNames {
		require.NotZero(t, contexts&bit, "Context %q needs at least one gesture", name)
	}
	fillers, onsets := mascotLibrary()
	require.Len(t, fillers, len(mascotRepertoire)+countSpecs(specs, false))
	require.Len(t, onsets, len(mascotOnsets)+countSpecs(specs, true))
}

func countSpecs(specs []mascotGestureSpec, onset bool) int {
	n := 0
	for _, s := range specs {
		if s.Onset == onset {
			n++
		}
	}
	return n
}

func TestMascotGestureLibraryRejectsUnsafeCurves(t *testing.T) {
	t.Parallel()
	base := mascotGestureSpec{Name: "x", Moods: []string{"idle"}, Weight: 1, Seconds: 1}
	cases := map[string]map[string][][2]float64{
		"residual pose":   {"yaw": {{0, 0}, {.5, .3}, {1, .1}}},
		"jump at start":   {"yaw": {{0, .2}, {.5, .3}, {1, 0}}},
		"out of range":    {"roll": {{0, 0}, {.5, 2}, {1, 0}}},
		"unknown channel": {"teleport": {{0, 0}, {.5, 1}, {1, 0}}},
		"time reversed":   {"yaw": {{0, 0}, {.6, .1}, {.4, .1}, {1, 0}}},
	}
	for name, tracks := range cases {
		s := base
		s.Tracks = tracks
		_, err := compileMascotGesture(s)
		require.Error(t, err, name)
	}
	s := base
	s.Tracks = map[string][][2]float64{"yaw": {{0, 0}, {.5, .3}, {1, 0}}}
	s.Only = true
	_, err := compileMascotGesture(s)
	require.Error(t, err, "Only without contexts would never play")
}

func TestMascotContextFlagsFollowVerifiedEvents(t *testing.T) {
	t.Parallel()
	start := time.Now()
	at := func(d time.Duration) time.Time { return start.Add(d) }
	var c mascotContext
	require.NotZero(t, c.flags(moodWorking, start, start, at(time.Second))&ctxHello)
	require.Zero(t, c.flags(moodWorking, start, start, at(4*time.Second))&ctxHello)

	// Four quick successful operations make a streak; a failure ends it.
	for i := range 4 {
		d := time.Duration(i) * 2 * time.Second
		c.observe(Event{PhaseAt: at(d), Action: "click"}, moodWorking, at(d))
		c.observe(Event{PhaseAt: at(d), Action: "click"}, moodWorking, at(d+time.Second/2))
	}
	now := at(7 * time.Second)
	require.NotZero(t, c.flags(moodWorking, start, start, now)&ctxStreak)
	c.observe(Event{Phase: PhaseFailed, PhaseAt: at(8 * time.Second)}, moodBlocked, at(8*time.Second))
	require.Zero(t, c.flags(moodBlocked, start, start, at(8500*time.Millisecond))&ctxStreak)
	require.Zero(t, c.flags(moodBlocked, start, start, at(8500*time.Millisecond))&ctxFrustrated, "One failure is not frustration")

	// Work resuming after the failure is a recovery.
	c.observe(Event{PhaseAt: at(10 * time.Second), Action: "type"}, moodWorking, at(10*time.Second))
	require.NotZero(t, c.flags(moodWorking, start, start, at(11*time.Second))&ctxRecovered)
	require.Zero(t, c.flags(moodWorking, start, start, at(30*time.Second))&ctxRecovered)

	// A second failure, observed repeatedly as the same event, frustrates.
	second := Event{Phase: PhaseFailed, PhaseAt: at(20 * time.Second)}
	for range 5 {
		c.observe(second, moodBlocked, at(20*time.Second))
	}
	require.NotZero(t, c.flags(moodBlocked, start, start, at(21*time.Second))&ctxFrustrated)
	require.Zero(t, c.flags(moodWorking, start, start, at(70*time.Second))&ctxFrustrated, "Frustration fades")

	// Duration-based contexts.
	require.NotZero(t, c.flags(moodThinking, start, at(time.Minute), at(time.Minute+6*time.Second))&ctxLongThink)
	require.Zero(t, c.flags(moodThinking, start, at(time.Minute), at(time.Minute+2*time.Second))&ctxLongThink)
	require.NotZero(t, c.flags(moodWaiting, start, at(time.Minute), at(time.Minute+8*time.Second))&ctxLongWait)

	// Typing survives provider waits but not other work.
	var typing mascotContext
	typing.observe(Event{Action: "type"}, moodWorking, at(0))
	typing.observe(Event{Phase: PhaseThinking}, moodThinking, at(time.Second))
	typing.observe(Event{Action: "type"}, moodWorking, at(4*time.Second))
	require.NotZero(t, typing.flags(moodWorking, start, start, at(4*time.Second))&ctxLongType)
	typing.observe(Event{Action: "click"}, moodWorking, at(5*time.Second))
	require.Zero(t, typing.flags(moodWorking, start, start, at(5*time.Second))&ctxLongType)
}

// firstGestureWith runs the animator and returns the first gesture begun
// after from whose contexts match. Onsets may be anchored slightly before
// the frame that starts them, so a new take is detected by its identity.
func firstGestureWith(a *mascotAnimator, events func(time.Duration) Event, start time.Time, from, until time.Duration, ctx uint16) string {
	var seen mascotTake
	for d := time.Duration(0); d <= until; d += time.Second / 60 {
		a.step(events(d), start.Add(d), Look{})
		k := a.perf.take
		if k.gesture != nil && (k.gesture != seen.gesture || !k.start.Equal(seen.start)) {
			seen = k
			if d >= from && k.gesture.contexts&ctx != 0 {
				return k.gesture.name
			}
		}
	}
	return ""
}

func TestMascotContextSteersThePerformance(t *testing.T) {
	t.Parallel()
	start := time.Now()

	// Appearing, Atlas greets first.
	var hello mascotAnimator
	hello.perf.seed = 11
	work := func(time.Duration) Event { return Event{Visible: true, Action: "type", PhaseAt: start} }
	name := firstGestureWith(&hello, work, start, 0, 3*time.Second, ctxHello)
	require.True(t, strings.HasPrefix(name, "hello-"), "Got %q", name)

	// Two failures in a row: frustration shows within seconds.
	for _, seed := range []uint32{3, 17, 29} {
		var a mascotAnimator
		a.perf.seed = seed
		failing := func(d time.Duration) Event {
			switch {
			case d < 2*time.Second:
				return Event{Visible: true, Action: "click", PhaseAt: start}
			case d < 4*time.Second:
				return Event{Visible: true, Phase: PhaseFailed, PhaseAt: start.Add(2 * time.Second)}
			case d < 5*time.Second:
				return Event{Visible: true, Action: "click", PhaseAt: start.Add(4 * time.Second)}
			}
			return Event{Visible: true, Phase: PhaseFailed, PhaseAt: start.Add(5 * time.Second)}
		}
		require.NotEmpty(t, firstGestureWith(&a, failing, start, 5*time.Second, 12*time.Second, ctxFrustrated), "Seed %d", seed)
	}

	// A long wait for the user brings attention-seeking gestures.
	var waiting mascotAnimator
	waiting.perf.seed = 5
	ask := func(time.Duration) Event { return Event{Visible: true, Phase: PhaseWaiting, PhaseAt: start} }
	require.NotEmpty(t, firstGestureWith(&waiting, ask, start, 7*time.Second, 14*time.Second, ctxLongWait))

	// Context-only gestures never play without their context.
	var calm mascotAnimator
	calm.perf.seed = 23
	idle := func(time.Duration) Event { return Event{Visible: true, Action: "wait", PhaseAt: start} }
	for d := time.Duration(0); d <= 60*time.Second; d += time.Second / 60 {
		now := start.Add(d)
		calm.step(idle(d), now, Look{})
		if g := calm.perf.take.gesture; g != nil && g.only && d > mascotHelloWindow+5*time.Second {
			require.Fail(t, "Context-only gesture without its context", g.name)
		}
	}
}
