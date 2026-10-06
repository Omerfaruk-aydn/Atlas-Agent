package activity

import (
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
