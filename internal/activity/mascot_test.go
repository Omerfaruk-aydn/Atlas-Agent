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

func TestMascotFailureIsScopedToItsOperation(t *testing.T) {
	t.Parallel()
	m := New(&testRenderer{})
	defer m.Close()
	ctx, end := StartFlow(t.Context(), "fail")
	defer end()
	op := m.Start(ctx, Event{Session: "fail", Action: "click"})
	op.Fail()
	op.End()
	e := m.Snapshot()
	require.Equal(t, PhaseFailed, e.Phase, "Ending must not erase a recorded failure")
	require.Equal(t, moodBlocked, mascotMoodFor(e, time.Now()))
	require.Equal(t, moodThinking, mascotMoodFor(e, time.Now().Add(2*time.Second)), "The alert is brief")
	retry := m.Start(ctx, Event{Session: "fail", Action: "click"})
	require.Equal(t, PhaseWorking, m.Snapshot().Phase, "A retry returns to work")
	op.Fail()
	require.Equal(t, PhaseWorking, m.Snapshot().Phase, "A stale failure cannot touch the retry")
	retry.End()
	require.Equal(t, PhaseThinking, m.Snapshot().Phase)
	Operation{}.Fail()
	Operation{}.End()
}

func TestMascotCompletionAndCancellation(t *testing.T) {
	t.Parallel()
	m := New(&testRenderer{})
	defer m.Close()
	ctx, finish := StartRun(t.Context(), "done")
	m.Begin(ctx, Event{Session: "done", Action: "click"})()
	id := m.Snapshot().ID
	nested, finishNested := StartRun(ctx, "done")
	require.Equal(t, ctx, nested)
	finishNested(true)
	require.Equal(t, PhaseThinking, m.Snapshot().Phase, "A nested worker cannot conclude its parent")
	finish(true)
	e := m.Snapshot()
	require.Equal(t, PhaseDone, e.Phase)
	require.False(t, e.Visible)
	require.False(t, e.Persistent, "Control and the system cursor are released at once")
	require.False(t, e.CanStop)
	require.WithinDuration(t, time.Now().Add(mascotDoneLinger), e.FinishedUntil, 100*time.Millisecond)
	require.False(t, m.Stop(id), "A completed run has nothing to stop")
	require.Zero(t, m.StopRevision())

	parent, cancel := context.WithCancel(t.Context())
	ctx, finish = StartRun(parent, "cancel")
	m.Begin(ctx, Event{Session: "cancel", Action: "click"})()
	cancel()
	require.Eventually(t, func() bool { return !m.Snapshot().Visible }, time.Second, time.Millisecond)
	finish(true)
	e = m.Snapshot()
	require.NotEqual(t, PhaseDone, e.Phase, "A cancelled run never shows completion")
	require.True(t, e.FinishedUntil.IsZero(), "Cancellation keeps the immediate teardown")

	ctx, finish = StartRun(t.Context(), "error")
	m.Begin(ctx, Event{Session: "error"})()
	finish(false)
	require.True(t, m.Snapshot().FinishedUntil.IsZero(), "A failed run ends without a success pose")
}

func TestMascotFollowsResourceHandoff(t *testing.T) {
	t.Parallel()
	desktop, browser := New(&testRenderer{}), New(&testRenderer{})
	defer desktop.Close()
	defer browser.Close()
	ctx, end := StartFlow(t.Context(), "handoff")
	defer end()
	desktop.Begin(ctx, Event{Session: "handoff", Resource: "desktop", Action: "click"})()
	browser.Begin(ctx, Event{Session: "handoff", Resource: "browser", Action: "navigate"})()
	require.False(t, desktop.Snapshot().Visible, "One owner at a time")
	require.True(t, browser.Snapshot().Visible)
	var a mascotAnimator
	pose := a.step(browser.Snapshot(), time.Now(), Look{})
	require.True(t, pose.Browser, "The browser variant keeps the same character")
	pose = a.step(Event{Visible: true, Resource: "desktop"}, time.Now(), Look{})
	require.False(t, pose.Browser)
}

func TestMascotTapOnlyForConfirmedInput(t *testing.T) {
	t.Parallel()
	now := time.Now()
	var a mascotAnimator
	base := Event{Visible: true, Action: "click", PhaseAt: now}
	a.step(base, now, Look{})
	a.blink.next = now.Add(time.Hour)
	settled := now
	for range 60 {
		settled = settled.Add(time.Second / 60)
		a.step(base, settled, Look{})
	}
	quiet := a.springs[chSquash].v
	clicked := base
	clicked.PointerKind, clicked.PointerAt = "click", settled
	a.step(clicked, settled.Add(time.Millisecond), Look{})
	require.Less(t, a.springs[chSquash].v, quiet-1, "Confirmed input earns one tap")
	tapVelocity := a.springs[chSquash].v
	a.step(clicked, settled.Add(2*time.Millisecond), Look{})
	require.Greater(t, a.springs[chSquash].v, tapVelocity-.5, "The same input never taps twice")
	aim := base
	aim.PointerKind, aim.PointerAt = "aim", settled.Add(3*time.Millisecond)
	before := a.springs[chSquash].v
	a.step(aim, settled.Add(4*time.Millisecond), Look{})
	require.Greater(t, a.springs[chSquash].v, before-.5, "Aiming is not a click")
}

func TestMascotBlinksIndependentlyAndReducedMotionIsStill(t *testing.T) {
	t.Parallel()
	start := time.Now()
	var a mascotAnimator
	e := Event{Visible: true, Action: "wait", PhaseAt: start}
	blinked, asymmetric := false, false
	for d := time.Duration(0); d < 12*time.Second; d += time.Second / 60 {
		p := a.step(e, start.Add(d), Look{})
		if p.Open[0] < .5 {
			blinked = true
		}
		if math.Abs(p.Open[0]-p.Open[1]) > .02 {
			asymmetric = true
		}
	}
	require.True(t, blinked, "Idle blinks at natural intervals")
	require.True(t, asymmetric, "Each eye has its own lid")
	var reduced mascotAnimator
	e.ReducedMotion = true
	first := reduced.step(e, start, Look{})
	for d := time.Duration(0); d < 6*time.Second; d += time.Second / 30 {
		require.Equal(t, first, reduced.step(e, start.Add(d), Look{}), "Reduced motion holds a still pose")
	}
}

// Gestures must vary, never repeat back to back, and stay continuous over a
// long idle stretch and across interruptions.
func TestMascotImprovisesWithoutRepeating(t *testing.T) {
	t.Parallel()
	start := time.Now()
	var a mascotAnimator
	a.perf.seed = 12345
	e := Event{Visible: true, Action: "wait", PhaseAt: start}
	seen := map[string]int{}
	last := ""
	var previous mascotPose
	for d := time.Duration(0); d < 90*time.Second; d += time.Second / 60 {
		now := start.Add(d)
		switch {
		case d >= 60*time.Second && e.Action != "wait":
			e = Event{Visible: true, Action: "wait", PhaseAt: now}
		case d >= 46*time.Second && d < 60*time.Second && e.Phase != PhaseThinking:
			e = Event{Visible: true, Phase: PhaseThinking, PhaseAt: now.Add(-time.Second)}
		case d >= 40*time.Second && d < 46*time.Second && e.Action != "click":
			// An interruption mid-gesture blends out instead of cutting.
			e = Event{Visible: true, Action: "click", PhaseAt: now}
		}
		pose := a.step(e, now, Look{})
		if d > 0 {
			require.Less(t, poseDelta(previous, pose), .35, "Frame jump at %s during %q", d, a.perf.take.gesture)
		}
		previous = pose
		if g := a.perf.take.gesture; g != nil && g.name != last {
			seen[g.name]++
			last = g.name
		}
	}
	require.GreaterOrEqual(t, len(seen), 12, "A long session shows a varied repertoire: %v", seen)
	// Back-to-back repeats are excluded by the recent list.
	var b mascotAnimator
	b.perf.seed = 99
	order := []string{}
	for d := time.Duration(0); d < 60*time.Second; d += time.Second / 30 {
		b.step(Event{Visible: true, Action: "wait", PhaseAt: start}, start.Add(d), Look{})
		if g := b.perf.take.gesture; g != nil && b.perf.take.start.Equal(start.Add(d)) {
			order = append(order, g.name)
		}
	}
	require.Greater(t, len(order), 10)
	for i := 1; i < len(order); i++ {
		require.NotEqual(t, order[i-1], order[i], "No gesture repeats immediately: %v", order)
		if i > 1 {
			require.NotEqual(t, order[i-2], order[i], "No gesture returns within three takes: %v", order)
		}
	}
	// Different appearances improvise differently.
	var c mascotAnimator
	c.perf.seed = 100
	other := []string{}
	for d := time.Duration(0); d < 60*time.Second; d += time.Second / 30 {
		c.step(Event{Visible: true, Action: "wait", PhaseAt: start}, start.Add(d), Look{})
		if g := c.perf.take.gesture; g != nil && c.perf.take.start.Equal(start.Add(d)) {
			other = append(other, g.name)
		}
	}
	require.NotEqual(t, order, other)
}
