package activity

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"sync"
)

// The gesture library is data, not code: each gesture is a set of keyframe
// tracks on named pose channels. It is authored (including by AI) offline
// and validated by tests, so new performances never need renderer changes
// and can never jump, overshoot or leave a residual pose.
//
// Schema of mascot_gestures.json, one object per gesture:
//
//	name     unique id
//	moods    idle, thinking, working, waiting, blocked, done
//	contexts optional: hello, streak, frustrated, recovered, longThink,
//	         longWait, longType (see mascotContext)
//	only     play only while one of the contexts is active
//	onset    a reaction when the mood begins instead of a filler
//	weight   relative chance
//	seconds  nominal duration
//	tracks   channel -> [[t, value], ...] with t from 0 to 1, starting and
//	         ending at value 0
//
// Channels named arm, openSide and armOther, openOther refer to the side
// chosen per performance; yaw, roll, gazeX, arcTilt, mouthSkew and lapRoll
// mirror with it.
//
//go:embed mascot_gestures.json
var mascotGestureData []byte

type mascotChannelSpec struct {
	lo, hi float64
	mirror bool
	apply  func(o *mascotOffset, v, side float64)
}

func armChannel(own bool) func(o *mascotOffset, v, side float64) {
	return func(o *mascotOffset, v, side float64) {
		i := int(side+1) / 2
		if !own {
			i = 1 - i
		}
		o.Arm[i] += v
	}
}

func openChannel(own bool) func(o *mascotOffset, v, side float64) {
	return func(o *mascotOffset, v, side float64) {
		i := int(side+1) / 2
		if !own {
			i = 1 - i
		}
		o.Open[i] += v
	}
}

var mascotChannels = map[string]mascotChannelSpec{
	"yaw":        {-1, 1, true, func(o *mascotOffset, v, _ float64) { o.Yaw += v }},
	"pitch":      {-.3, .3, false, func(o *mascotOffset, v, _ float64) { o.Pitch += v }},
	"roll":       {-.3, .3, true, func(o *mascotOffset, v, _ float64) { o.Roll += v }},
	"squash":     {-.3, .3, false, func(o *mascotOffset, v, _ float64) { o.Squash += v }},
	"lift":       {-2, 3.5, false, func(o *mascotOffset, v, _ float64) { o.Lift += v }},
	"gazeX":      {-.5, .5, true, func(o *mascotOffset, v, _ float64) { o.GazeX += v }},
	"gazeY":      {-3.5, 3.5, false, func(o *mascotOffset, v, _ float64) { o.GazeY += v }},
	"open":       {-.95, .5, false, func(o *mascotOffset, v, _ float64) { o.Open[0] += v; o.Open[1] += v }},
	"openSide":   {-.95, .5, false, openChannel(true)},
	"openOther":  {-.95, .5, false, openChannel(false)},
	"smile":      {-1, 1, false, func(o *mascotOffset, v, _ float64) { o.Smile += v }},
	"slant":      {-.5, .5, false, func(o *mascotOffset, v, _ float64) { o.Slant += v }},
	"arcLift":    {-3.5, 3.5, false, func(o *mascotOffset, v, _ float64) { o.ArcLift += v }},
	"arcTilt":    {-.4, .4, true, func(o *mascotOffset, v, _ float64) { o.ArcTilt += v }},
	"arm":        {-2.5, 3.5, false, armChannel(true)},
	"armOther":   {-2.5, 3.5, false, armChannel(false)},
	"arms":       {-2.5, 3.5, false, func(o *mascotOffset, v, _ float64) { o.Arm[0] += v; o.Arm[1] += v }},
	"mouthOpen":  {-.5, 1, false, func(o *mascotOffset, v, _ float64) { o.MouthOpen += v }},
	"mouthCurve": {-1.5, 1.5, false, func(o *mascotOffset, v, _ float64) { o.MouthCurve += v }},
	"mouthWide":  {-1, 1, false, func(o *mascotOffset, v, _ float64) { o.MouthWide += v }},
	"mouthSkew":  {-1, 1, true, func(o *mascotOffset, v, _ float64) { o.MouthSkew += v }},
	"mouthWave":  {-1, 1, false, func(o *mascotOffset, v, _ float64) { o.MouthWave += v }},
	"blush":      {-1, 1, false, func(o *mascotOffset, v, _ float64) { o.Blush += v }},
	"lapLift":    {-2, 2, false, func(o *mascotOffset, v, _ float64) { o.LapLift += v }},
	"lapRoll":    {-.2, .2, true, func(o *mascotOffset, v, _ float64) { o.LapRoll += v }},
}

var mascotMoodNames = map[string]mascotMood{
	"idle": moodIdle, "thinking": moodThinking, "working": moodWorking,
	"waiting": moodWaiting, "blocked": moodBlocked, "done": moodDone,
}

type mascotGestureSpec struct {
	Name     string                  `json:"name"`
	Moods    []string                `json:"moods"`
	Contexts []string                `json:"contexts"`
	Only     bool                    `json:"only"`
	Onset    bool                    `json:"onset"`
	Weight   float64                 `json:"weight"`
	Seconds  float64                 `json:"seconds"`
	Tracks   map[string][][2]float64 `json:"tracks"`
}

// mascotTrack is one channel curve: a cubic Hermite spline through the
// keys with Catmull-Rom tangents and flat ends.
type mascotTrack struct {
	spec   mascotChannelSpec
	t, v   []float64
	slopes []float64
}

func (k *mascotTrack) at(t float64) float64 {
	n := len(k.t)
	if t <= 0 || t >= 1 || n < 2 {
		return 0
	}
	i := 0
	for i < n-2 && k.t[i+1] <= t {
		i++
	}
	h := k.t[i+1] - k.t[i]
	u := (t - k.t[i]) / h
	u2, u3 := u*u, u*u*u
	return (2*u3-3*u2+1)*k.v[i] + (u3-2*u2+u)*h*k.slopes[i] +
		(-2*u3+3*u2)*k.v[i+1] + (u3-u2)*h*k.slopes[i+1]
}

func compileMascotGesture(s mascotGestureSpec) (mascotGesture, error) {
	g := mascotGesture{name: s.Name, weight: s.Weight, seconds: s.Seconds, only: s.Only}
	if s.Name == "" || s.Weight <= 0 || s.Seconds < .4 || s.Seconds > 5 {
		return g, fmt.Errorf("gesture %q: needs a name, a positive weight and 0.4 to 5 seconds", s.Name)
	}
	for _, m := range s.Moods {
		mood, ok := mascotMoodNames[m]
		if !ok {
			return g, fmt.Errorf("gesture %q: unknown mood %q", s.Name, m)
		}
		g.moods |= 1 << mood
	}
	if g.moods == 0 {
		return g, fmt.Errorf("gesture %q: no moods", s.Name)
	}
	for _, c := range s.Contexts {
		bit, ok := mascotContextNames[c]
		if !ok {
			return g, fmt.Errorf("gesture %q: unknown context %q", s.Name, c)
		}
		g.contexts |= bit
	}
	if g.only && g.contexts == 0 {
		return g, fmt.Errorf("gesture %q: only requires contexts", s.Name)
	}
	names := make([]string, 0, len(s.Tracks))
	for name := range s.Tracks {
		names = append(names, name)
	}
	slices.Sort(names)
	tracks := make([]mascotTrack, 0, len(names))
	for _, name := range names {
		spec, ok := mascotChannels[name]
		if !ok {
			return g, fmt.Errorf("gesture %q: unknown channel %q", s.Name, name)
		}
		keys := s.Tracks[name]
		if len(keys) < 3 || keys[0] != [2]float64{0, 0} || keys[len(keys)-1] != [2]float64{1, 0} {
			return g, fmt.Errorf("gesture %q: track %q must start at [0,0], end at [1,0] and have a key between", s.Name, name)
		}
		tr := mascotTrack{spec: spec}
		for i, k := range keys {
			if i > 0 && k[0] <= keys[i-1][0] {
				return g, fmt.Errorf("gesture %q: track %q times must increase", s.Name, name)
			}
			if k[1] < spec.lo || k[1] > spec.hi {
				return g, fmt.Errorf("gesture %q: track %q value %v outside [%v, %v]", s.Name, name, k[1], spec.lo, spec.hi)
			}
			tr.t, tr.v = append(tr.t, k[0]), append(tr.v, k[1])
		}
		tr.slopes = make([]float64, len(keys))
		for i := 1; i < len(keys)-1; i++ {
			tr.slopes[i] = (tr.v[i+1] - tr.v[i-1]) / (tr.t[i+1] - tr.t[i-1])
		}
		if name == "open" || name == "openSide" || name == "openOther" {
			g.eyes = true
		}
		tracks = append(tracks, tr)
	}
	if len(tracks) == 0 {
		return g, fmt.Errorf("gesture %q: no tracks", s.Name)
	}
	g.play = func(t, side, amp float64) mascotOffset {
		var o mascotOffset
		for i := range tracks {
			tr := &tracks[i]
			v := tr.at(t) * amp
			if tr.spec.mirror {
				v *= side
			}
			tr.spec.apply(&o, v, side)
		}
		return o
	}
	return g, nil
}

// mascotLibrary parses the embedded gestures once. A malformed library is
// reported and skipped, so the built-in repertoire always remains.
var mascotLibrary = sync.OnceValues(func() (fillers, onsets []mascotGesture) {
	fillers = append(fillers, mascotRepertoire...)
	onsets = append(onsets, mascotOnsets...)
	var specs []mascotGestureSpec
	if err := json.Unmarshal(mascotGestureData, &specs); err != nil {
		slog.Warn("Mascot gesture library unreadable", "error", err)
		return fillers, onsets
	}
	for _, s := range specs {
		g, err := compileMascotGesture(s)
		if err != nil {
			slog.Warn("Mascot gesture skipped", "error", err)
			continue
		}
		if s.Onset {
			onsets = append(onsets, g)
		} else {
			fillers = append(fillers, g)
		}
	}
	return fillers, onsets
})

// mascotSpan is a channel's allowed range, used by the library tests.
func mascotSpan(name string) float64 {
	spec := mascotChannels[name]
	return math.Max(spec.hi-spec.lo, 1e-9)
}
