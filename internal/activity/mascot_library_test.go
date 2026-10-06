package activity

import (
	"encoding/json"
	"math"

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
