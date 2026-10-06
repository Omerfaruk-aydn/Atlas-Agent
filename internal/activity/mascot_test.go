package activity

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMascotMoodFollowsRecordedPhases(t *testing.T) {
	t.Parallel()
	now := time.Now()
	cases := []struct {
		event Event
		mood  mascotMood
	}{
		{Event{Action: "click", PhaseAt: now}, moodWorking},
		{Event{Action: "wait", PhaseAt: now}, moodIdle},
		{Event{Phase: PhaseThinking, PhaseAt: now.Add(-100 * time.Millisecond)}, moodWorking},
		{Event{Phase: PhaseThinking, PhaseAt: now.Add(-time.Second)}, moodThinking},
		{Event{Phase: PhaseWaiting, PhaseAt: now}, moodWaiting},
		{Event{Phase: PhaseFailed, PhaseAt: now.Add(-200 * time.Millisecond)}, moodBlocked},
		{Event{Phase: PhaseFailed, PhaseAt: now.Add(-3 * time.Second)}, moodThinking},
		{Event{Phase: PhaseDone, PhaseAt: now}, moodDone},
	}
	for _, c := range cases {
		require.Equal(t, c.mood, mascotMoodFor(c.event, now), "%+v", c.event)
	}
}

// poseDelta is the largest per-channel change between two poses, used to
// detect discontinuous jumps.
func poseDelta(a, b mascotPose) float64 {
	// Orientation is periodic: a completed spin is no jump.
	yaw := math.Abs(math.Remainder(a.Yaw-b.Yaw, 2*math.Pi))
	return max(yaw, math.Abs(a.Pitch-b.Pitch), math.Abs(a.Roll-b.Roll),
		math.Abs(a.Squash-b.Squash)*4, math.Abs(a.Lift-b.Lift)/4, math.Abs(a.GazeX-b.GazeX),
		math.Abs(a.GazeY-b.GazeY)/4, math.Abs(a.Slant-b.Slant), math.Abs(a.Smile-b.Smile), math.Abs(a.ArcLift-b.ArcLift)/4,
		math.Abs(a.ArcTilt-b.ArcTilt), math.Abs(a.Arm[0]-b.Arm[0])/4, math.Abs(a.Arm[1]-b.Arm[1])/4)
}

func TestMascotTransitionsStayContinuous(t *testing.T) {
	t.Parallel()
	start := time.Now()
	var a mascotAnimator
	frame := time.Second / 60
	sequence := []Event{
		{Visible: true, Action: "click", PhaseAt: start},
		{Visible: true, Phase: PhaseThinking, PhaseAt: start},
		{Visible: true, Phase: PhaseWaiting, PhaseAt: start},
		{Visible: true, Action: "type", PhaseAt: start},
		{Visible: true, Phase: PhaseFailed, PhaseAt: start},
		{Visible: true, Action: "navigate", PhaseAt: start},
		{Visible: true, Phase: PhaseDone, PhaseAt: start},
	}
	previous := a.step(sequence[0], start, Look{})
	now := start
	for i, e := range sequence {
		e.PhaseAt = now
		for range 40 {
			now = now.Add(frame)
			pose := a.step(e, now, Look{X: .4, Y: -.5})
			require.Less(t, poseDelta(previous, pose), .35, "Frame jump entering %d", i)
			require.GreaterOrEqual(t, pose.Open[0], .12, "Eyes never disappear")
			require.GreaterOrEqual(t, pose.Open[1], .12, "Eyes never disappear")
			previous = pose
		}
	}
}

func TestMascotMotionIsFrameRateIndependent(t *testing.T) {
	t.Parallel()
	start := time.Now()
	run := func(step time.Duration) mascotPose {
		var a mascotAnimator
		a.step(Event{Visible: true, Action: "wait", PhaseAt: start}, start, Look{})
		a.blink.next = start.Add(time.Hour)
		e := Event{Visible: true, Phase: PhaseDone, PhaseAt: start}
		var p mascotPose
		for d := step; d <= 700*time.Millisecond; d += step {
			p = a.step(e, start.Add(d), Look{})
		}
		return p
	}
	fast, slow := run(time.Second/120), run(time.Second/30)
	require.Less(t, poseDelta(fast, slow), .05, "Speed must depend on monotonic time, not frame count")
}

func TestMascotRepeatedWorkDoesNotRestart(t *testing.T) {
	t.Parallel()
	m := New(&testRenderer{})
	defer m.Close()
	ctx, end := StartFlow(t.Context(), "repeat")
	defer end()
	var a mascotAnimator
	now := time.Now()
	var previous mascotPose
	for i := range 30 {
		// Back-to-back tools: begin, finish, next begin a few ms later.
		finish := m.Begin(ctx, Event{Session: "repeat", Action: "click"})
		for range 3 {
			now = now.Add(time.Second / 60)
			pose := a.step(m.Snapshot(), now, Look{})
			if i > 0 {
				require.Less(t, poseDelta(previous, pose), .35)
			}
			previous = pose
		}
		finish()
		require.Equal(t, moodWorking, mascotMoodFor(m.Snapshot(), time.Now()), "Short gaps must not flicker into thinking")
	}
	require.Equal(t, moodWorking, a.mood)
	require.True(t, a.ready, "New events never reset the animator")
}

func TestMascotWaitingForUserRestoresThinking(t *testing.T) {
	t.Parallel()
	m := New(&testRenderer{})
	defer m.Close()
	ctx, end := StartFlow(t.Context(), "await")
	defer end()
	m.Begin(ctx, Event{Session: "await", Action: "click"})()
	resume := AwaitUser(ctx)
	require.Equal(t, PhaseWaiting, m.Snapshot().Phase)
	require.True(t, m.Snapshot().Visible, "Waiting keeps the banner instead of hiding it")
	resume()
	require.Equal(t, PhaseThinking, m.Snapshot().Phase)
	finish := m.Begin(ctx, Event{Session: "await", Action: "type"})
	require.Equal(t, PhaseWorking, m.Snapshot().Phase)
	// A late restore from an older prompt cannot overwrite newer work.
	stale := AwaitUser(ctx)
	finish()
	m.Begin(ctx, Event{Session: "await", Action: "scroll"})
	stale()
	require.Equal(t, PhaseWorking, m.Snapshot().Phase)
	// Without a flow there is nothing to express.
	AwaitUser(context.Background())()
}
