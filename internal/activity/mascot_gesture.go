package activity

import (
	"math"
	"time"
)

// mascotOffset is an additive pose layer. Every gesture returns zero at its
// start and end, so it can begin, finish or fade out without a jump.
type mascotOffset struct {
	Yaw, Pitch, Roll float64
	Squash, Lift     float64
	GazeX, GazeY     float64
	Open             [2]float64
	Smile, Slant     float64
	ArcLift, ArcTilt float64
	Arm              [2]float64
	MouthOpen        float64
	MouthCurve       float64
	MouthWide        float64
	MouthSkew        float64
	MouthWave        float64
	Blush            float64
	LapLift, LapRoll float64
}

func (o mascotOffset) addTo(p *mascotPose, w float64) {
	p.Yaw += o.Yaw * w
	p.Pitch += o.Pitch * w
	p.Roll += o.Roll * w * .55 // Leans stay subtle; Atlas reads upright.
	p.Squash += o.Squash * w
	p.Lift += o.Lift * w
	p.GazeX += o.GazeX * w
	p.GazeY += o.GazeY * w
	p.Smile += o.Smile * w
	p.Slant += o.Slant * w
	p.ArcLift += o.ArcLift * w
	p.ArcTilt += o.ArcTilt * w
	p.MouthOpen += o.MouthOpen * w
	p.MouthCurve += o.MouthCurve * w
	p.MouthWide += o.MouthWide * w
	p.MouthSkew += o.MouthSkew * w
	p.MouthWave += o.MouthWave * w
	p.Blush += o.Blush * w
	p.LapLift += o.LapLift * w
	p.LapRoll += o.LapRoll * w
	for i := range 2 {
		p.Open[i] += o.Open[i] * w
		p.Arm[i] += o.Arm[i] * w
	}
}

// Envelope helpers; each is zero with zero slope at t = 0 and t = 1.
func bump(t float64) float64 {
	s := math.Sin(math.Pi * max(0, min(1, t)))
	return s * s
}

func plateau(t, in, out float64) float64 { return smooth(0, in, t) * (1 - smooth(1-out, 1, t)) }

func cycle(t, n float64) float64 { return math.Sin(2 * math.Pi * n * t) }

// within maps t into a sub-window and returns its bump.
func within(t, from, to float64) float64 { return bump((t - from) / (to - from)) }

type mascotGesture struct {
	name    string
	moods   uint8 // Bit set of mascotMood values where it may play.
	weight  float64
	seconds float64
	eyes    bool // The gesture moves the lids, so blinks wait.
	// contexts favor the gesture while one of them is active; only
	// restricts it to those contexts.
	contexts uint16
	only     bool
	// play returns the offset at progress t; side is -1 or 1 and amp is a
	// per-performance intensity, so no two performances are identical.
	play func(t, side, amp float64) mascotOffset
}

// busyMask lets thinking and working share gestures: real runs flip between
// them every few hundred milliseconds, and the performance must not stutter.
func busyMask(m mascotMood) uint8 {
	if m == moodThinking || m == moodWorking {
		return 1<<moodThinking | 1<<moodWorking
	}
	return 1 << m
}

func moods(m ...mascotMood) uint8 {
	var bits uint8
	for _, v := range m {
		bits |= 1 << v
	}
	return bits
}

// The repertoire. Scheduled gestures fill quiet time in a mood; onset
// gestures react once when a mood begins.
var mascotRepertoire = []mascotGesture{
	{name: "look-around", moods: moods(moodIdle), weight: 1.2, seconds: 2.6, play: func(t, side, amp float64) mascotOffset {
		g := plateau(t, .14, .14) * amp
		x := side * .34 * g * (1 - 2*smooth(.42, .6, t))
		return mascotOffset{GazeX: x, Yaw: x * .7, GazeY: .4 * g}
	}},
	{name: "curious-tilt", moods: moods(moodIdle, moodWaiting), weight: 1, seconds: 1.8, play: func(t, side, amp float64) mascotOffset {
		g := plateau(t, .25, .3) * amp
		return mascotOffset{Roll: side * .2 * g, GazeY: 1.1 * g, Pitch: .05 * g, Open: [2]float64{.1 * g, .1 * g}}
	}},
	{name: "stretch", moods: moods(moodIdle), weight: .8, seconds: 2.1, eyes: true, play: func(t, side, amp float64) mascotOffset {
		g := bump(t) * amp
		return mascotOffset{ArcLift: 3 * g, Squash: .12 * g, Open: [2]float64{-.5 * g, -.5 * g}, Smile: .45 * g, Roll: side * .04 * g}
	}},
	{name: "hop", moods: moods(moodIdle), weight: .7, seconds: .9, play: func(t, side, amp float64) mascotOffset {
		air := within(t, .22, .8)
		return mascotOffset{
			Squash: (-.14*within(t, 0, .3) + .1*within(t, .25, .65) - .08*within(t, .75, 1)) * amp,
			Lift:   2.8 * air * amp, ArcLift: 1.2 * air * amp, Smile: .3 * bump(t),
		}
	}},
	{name: "balance-sky", moods: moods(moodIdle, moodThinking), weight: 1, seconds: 2.4, play: func(t, side, amp float64) mascotOffset {
		tilt := side * .22 * plateau(t, .15, .2) * cycle(t, 1.5) * amp
		return mascotOffset{ArcTilt: tilt, Roll: -tilt * .4, Arm: [2]float64{-tilt * 7, tilt * 7}, GazeY: 1.6 * plateau(t, .15, .2), GazeX: tilt * .5}
	}},
	{name: "peek-up", moods: moods(moodIdle, moodThinking), weight: .9, seconds: 1.7, play: func(t, side, amp float64) mascotOffset {
		g := plateau(t, .2, .3) * amp
		return mascotOffset{GazeY: 3.2 * g, Pitch: .14 * g, Open: [2]float64{.08 * g, .08 * g}, GazeX: side * .06 * g}
	}},
	{name: "wave", moods: moods(moodIdle), weight: .9, seconds: 1.9, play: func(t, side, amp float64) mascotOffset {
		g := plateau(t, .18, .22) * amp
		var o mascotOffset
		o.Arm[int(side+1)/2] = (3 + 1.2*cycle(t, 3)) * g
		o.Smile, o.Roll = .45*g, -side*.06*g
		return o
	}},
	{name: "blink-hello", moods: moods(moodIdle, moodWaiting), weight: .8, seconds: 1.1, eyes: true, play: func(t, side, amp float64) mascotOffset {
		lid := -.82 * (within(t, .12, .34) + within(t, .44, .66))
		return mascotOffset{Open: [2]float64{lid, lid}, Smile: .25 * bump(t) * amp, GazeY: -.3 * bump(t)}
	}},
	{name: "doze", moods: moods(moodIdle), weight: .45, seconds: 3.4, eyes: true, play: func(t, side, amp float64) mascotOffset {
		drowse := plateau(t, .55, .1)
		wake := within(t, .8, 1)
		return mascotOffset{
			Open:  [2]float64{-.55*drowse + .22*wake, -.5*drowse + .22*wake},
			Pitch: -.09 * drowse, Squash: -.04*drowse + .08*wake, Lift: 1.4 * wake, ArcLift: -.8*drowse + 1.2*wake,
			Roll: side * .05 * drowse,
		}
	}},
	{name: "turn-around", moods: moods(moodIdle), weight: .8, seconds: 2.2, play: func(t, side, amp float64) mascotOffset {
		g := plateau(t, .22, .25) * amp
		return mascotOffset{Yaw: side * .85 * g, GazeX: side * .14 * g}
	}},
	{name: "hum-sway", moods: moods(moodIdle, moodWaiting), weight: .9, seconds: 2.4, play: func(t, side, amp float64) mascotOffset {
		g := plateau(t, .15, .15) * amp
		return mascotOffset{Roll: .09 * g * cycle(t, 2) * side, Lift: .8 * g * math.Abs(cycle(t, 4)), Smile: .3 * g, ArcTilt: -.05 * g * cycle(t, 2) * side}
	}},
	{name: "wink", moods: moods(moodIdle, moodWaiting), weight: .6, seconds: .9, eyes: true, play: func(t, side, amp float64) mascotOffset {
		var o mascotOffset
		o.Open[int(side+1)/2] = -.9 * within(t, .12, .78)
		o.Smile, o.Roll = .4*bump(t)*amp, side*.05*bump(t)
		return o
	}},
	{name: "spin", moods: moods(moodIdle), weight: .25, seconds: 1.3, play: func(t, side, amp float64) mascotOffset {
		turn := smooth(.1, .9, t)
		return mascotOffset{Yaw: side * 2 * math.Pi * turn, Lift: 1.2 * bump(t), Squash: .06 * bump(t), Smile: .4 * bump(t)}
	}},
	{name: "ponder-tilt", moods: moods(moodThinking), weight: 1, seconds: 2.2, play: func(t, side, amp float64) mascotOffset {
		g := plateau(t, .25, .25) * amp
		return mascotOffset{Roll: side * .16 * g, GazeX: side * .18 * g, GazeY: .9 * g}
	}},
	{name: "eyes-circle", moods: moods(moodThinking), weight: .9, seconds: 1.7, play: func(t, side, amp float64) mascotOffset {
		return mascotOffset{GazeX: side * .26 * cycle(t, 1) * amp, GazeY: 1.4 * (1 - math.Cos(2*math.Pi*t)) / 2 * amp, Open: [2]float64{-.08 * bump(t), -.08 * bump(t)}}
	}},
	{name: "squint-one", moods: moods(moodThinking), weight: .8, seconds: 1.8, play: func(t, side, amp float64) mascotOffset {
		g := plateau(t, .2, .25) * amp
		var o mascotOffset
		o.Open[int(side+1)/2] = -.32 * g
		o.Slant, o.Roll = -.1*g, side*.05*g
		return o
	}},
	{name: "finger-tap", moods: moods(moodThinking, moodWaiting), weight: .8, seconds: 1.9, play: func(t, side, amp float64) mascotOffset {
		g := plateau(t, .12, .15) * amp
		var o mascotOffset
		o.Arm[int(side+1)/2] = .9 * g * math.Abs(cycle(t, 5))
		o.GazeY = 1.1 * g
		return o
	}},
	{name: "hmm-nod", moods: moods(moodThinking), weight: .7, seconds: 1.4, play: func(t, side, amp float64) mascotOffset {
		return mascotOffset{Pitch: -.08 * cycle(t, 2) * bump(t) * amp, Squash: -.03 * bump(t)}
	}},
	{name: "look-away", moods: moods(moodThinking), weight: .7, seconds: 1.9, play: func(t, side, amp float64) mascotOffset {
		g := plateau(t, .2, .25) * amp
		return mascotOffset{Yaw: side * .4 * g, GazeX: side * .25 * g, GazeY: .8 * g}
	}},
	{name: "lean-in", moods: moods(moodWorking), weight: 1, seconds: 1.4, play: func(t, side, amp float64) mascotOffset {
		g := plateau(t, .2, .3) * amp
		return mascotOffset{Pitch: -.12 * g, Squash: -.05 * g, Open: [2]float64{-.08 * g, -.08 * g}}
	}},
	{name: "effort-pulse", moods: moods(moodWorking), weight: 1, seconds: 1.2, play: func(t, side, amp float64) mascotOffset {
		g := bump(t) * amp
		return mascotOffset{ArcLift: 1.5 * g * math.Abs(cycle(t, 2)), Squash: -.06 * g * math.Abs(cycle(t, 2))}
	}},
	{name: "juggle", moods: moods(moodWorking), weight: .8, seconds: 1.4, play: func(t, side, amp float64) mascotOffset {
		g := plateau(t, .15, .15) * amp * cycle(t, 2) * side
		return mascotOffset{Arm: [2]float64{1.5 * g, -1.5 * g}, Roll: -.03 * g}
	}},
	{name: "quick-glance", moods: moods(moodWorking), weight: .7, seconds: .8, play: func(t, side, amp float64) mascotOffset {
		g := plateau(t, .2, .3) * amp
		return mascotOffset{GazeX: side * .28 * g, Yaw: side * .1 * g}
	}},
	{name: "focus-squint", moods: moods(moodWorking), weight: .7, seconds: 1.2, play: func(t, side, amp float64) mascotOffset {
		g := plateau(t, .2, .3) * amp
		return mascotOffset{Open: [2]float64{-.25 * g, -.25 * g}, Slant: .12 * g}
	}},
	{name: "small-wave", moods: moods(moodWaiting), weight: .9, seconds: 1.7, play: func(t, side, amp float64) mascotOffset {
		g := plateau(t, .2, .22) * amp
		var o mascotOffset
		o.Arm[int(side+1)/2] = (2.2 + .9*cycle(t, 3)) * g
		o.Smile = .35 * g
		return o
	}},
	{name: "patient-sway", moods: moods(moodWaiting), weight: 1, seconds: 2.8, play: func(t, side, amp float64) mascotOffset {
		g := plateau(t, .2, .2) * amp
		return mascotOffset{Roll: .06 * g * cycle(t, 1) * side, Squash: .02 * g * cycle(t, 2)}
	}},
	{name: "look-at-stop", moods: moods(moodWaiting), weight: .8, seconds: 2, play: func(t, side, amp float64) mascotOffset {
		// The text and stop controls sit to the character's right.
		g := plateau(t, .2, .25) * amp
		return mascotOffset{GazeX: .32 * g, Yaw: .3 * g, GazeY: -.2 * g}
	}},
	{name: "brows-up", moods: moods(moodWaiting), weight: .7, seconds: 1, play: func(t, side, amp float64) mascotOffset {
		return mascotOffset{Open: [2]float64{.18 * bump(t), .18 * bump(t)}, Lift: .5 * bump(t) * amp, Smile: .15 * bump(t)}
	}},
	{name: "nod-yes", moods: moods(moodIdle, moodThinking, moodWorking, moodWaiting), weight: 1, seconds: 1.3, play: func(t, side, amp float64) mascotOffset {
		g := bump(t) * amp
		return mascotOffset{Pitch: -.2 * g * cycle(t, 2), Squash: -.04 * g * math.Abs(cycle(t, 2)), Smile: .35 * g}
	}},
	{name: "shake-no", moods: moods(moodIdle, moodThinking, moodWorking), weight: .8, seconds: 1.3, play: func(t, side, amp float64) mascotOffset {
		g := bump(t) * amp
		return mascotOffset{Yaw: .5 * g * cycle(t, 2.5), GazeX: .2 * g * cycle(t, 2.5), Slant: .06 * g}
	}},
	{name: "shrug", moods: moods(moodIdle, moodThinking, moodWaiting), weight: .8, seconds: 1.5, play: func(t, side, amp float64) mascotOffset {
		g := plateau(t, .2, .3) * amp
		return mascotOffset{Arm: [2]float64{1.8 * g, 1.8 * g}, Lift: .9 * g, Squash: -.05 * g, Roll: side * .05 * g, Open: [2]float64{-.15 * g, -.15 * g}}
	}},
	{name: "cheer", moods: moods(moodIdle, moodWorking), weight: .6, seconds: 1.4, play: func(t, side, amp float64) mascotOffset {
		g := plateau(t, .15, .25) * amp
		return mascotOffset{Arm: [2]float64{2.6 * g, 2.6 * g}, ArcLift: 2.2 * g, Lift: 1.2 * g * math.Abs(cycle(t, 2)), Smile: .8 * g, Squash: .05 * g}
	}},
	{name: "hand-on-chin", moods: moods(moodThinking, moodWaiting), weight: 1, seconds: 2.4, play: func(t, side, amp float64) mascotOffset {
		g := plateau(t, .25, .25) * amp
		var o mascotOffset
		o.Arm[int(side+1)/2] = -1.6 * g
		o.Roll, o.GazeY, o.Pitch, o.Slant = side*.12*g, 1.2*g, .05*g, .08*g
		return o
	}},
	{name: "surprise", moods: moods(moodIdle, moodThinking, moodWorking, moodWaiting), weight: .7, seconds: 1.1, play: func(t, side, amp float64) mascotOffset {
		jump := within(t, 0, .25)
		hold := plateau(t, .1, .4) * amp
		return mascotOffset{Lift: 2.2 * jump * amp, Open: [2]float64{.3 * hold, .3 * hold}, Arm: [2]float64{1.4 * hold, 1.4 * hold}, ArcLift: 1.6 * hold, Squash: -.1 * jump}
	}},
	{name: "proud", moods: moods(moodIdle, moodWorking), weight: .6, seconds: 1.8, play: func(t, side, amp float64) mascotOffset {
		g := plateau(t, .25, .3) * amp
		return mascotOffset{Pitch: .12 * g, Smile: .9 * g, Squash: .07 * g, ArcLift: 1.4 * g, Open: [2]float64{-.35 * g, -.35 * g}, Lift: .6 * g}
	}},
	{name: "giggle", moods: moods(moodIdle, moodWaiting), weight: .7, seconds: 1.2, play: func(t, side, amp float64) mascotOffset {
		g := bump(t) * amp
		return mascotOffset{Lift: .9 * g * math.Abs(cycle(t, 4)), Roll: .06 * g * cycle(t, 4), Smile: .8 * g, Open: [2]float64{-.45 * g, -.45 * g}}
	}},
	{name: "shiver", moods: moods(moodIdle, moodWaiting, moodThinking), weight: .5, seconds: .9, play: func(t, side, amp float64) mascotOffset {
		g := bump(t) * amp
		return mascotOffset{Roll: .08 * g * cycle(t, 9), Squash: -.04 * g, ArcLift: -.6 * g, Arm: [2]float64{-.5 * g, -.5 * g}, Open: [2]float64{.12 * g, .12 * g}}
	}},
	{name: "rub-eyes", moods: moods(moodIdle, moodWaiting), weight: .5, seconds: 1.7, eyes: true, play: func(t, side, amp float64) mascotOffset {
		g := plateau(t, .2, .25) * amp
		lid := -.9 * g * (.6 + .4*math.Abs(cycle(t, 3)))
		var o mascotOffset
		o.Arm[int(side+1)/2] = 1.5 * g
		o.Open = [2]float64{lid, lid}
		o.Roll = side * .06 * g
		return o
	}},
	{name: "peek-side", moods: moods(moodIdle, moodThinking, moodWorking, moodWaiting), weight: 1, seconds: 1.6, play: func(t, side, amp float64) mascotOffset {
		g := plateau(t, .2, .3) * amp
		return mascotOffset{Yaw: side * .6 * g, GazeX: side * .3 * g, Roll: -side * .1 * g, Open: [2]float64{.08 * g, .08 * g}, Lift: .3 * g}
	}},
	{name: "bounce-excited", moods: moods(moodIdle, moodWorking), weight: .7, seconds: 1.5, play: func(t, side, amp float64) mascotOffset {
		g := plateau(t, .1, .2) * amp
		b := math.Abs(cycle(t, 3))
		return mascotOffset{Lift: 1.8 * g * b, Squash: (.08*b - .05) * g, Smile: .6 * g, ArcLift: 1.3 * g * b, Arm: [2]float64{1.1 * g * b, 1.1 * g * b}}
	}},
	{name: "sky-wiggle", moods: moods(moodIdle, moodThinking, moodWorking, moodWaiting), weight: 1, seconds: 1.8, play: func(t, side, amp float64) mascotOffset {
		g := plateau(t, .15, .2) * amp
		return mascotOffset{ArcTilt: .3 * g * cycle(t, 2.5), ArcLift: 1.2 * g, Arm: [2]float64{1.2 * g * cycle(t, 2.5), -1.2 * g * cycle(t, 2.5)}, Roll: -.04 * g * cycle(t, 2.5)}
	}},
	{name: "slow-blink-smile", moods: moods(moodIdle, moodWaiting, moodThinking), weight: .8, seconds: 1.5, eyes: true, play: func(t, side, amp float64) mascotOffset {
		lid := -.9 * plateau(t, .3, .3)
		return mascotOffset{Open: [2]float64{lid, lid}, Smile: .9 * plateau(t, .25, .3) * amp, Roll: side * .06 * bump(t)}
	}},
	{name: "stretch-up", moods: moods(moodWorking, moodThinking), weight: .6, seconds: 1.7, play: func(t, side, amp float64) mascotOffset {
		g := plateau(t, .25, .3) * amp
		return mascotOffset{ArcLift: 2.6 * g, Arm: [2]float64{1.8 * g, 1.8 * g}, Squash: .1 * g, Pitch: .1 * g, GazeY: 2 * g}
	}},
	{name: "dizzy-roll", moods: moods(moodIdle, moodThinking), weight: .5, seconds: 2, play: func(t, side, amp float64) mascotOffset {
		g := plateau(t, .15, .2) * amp
		return mascotOffset{Roll: .14 * g * cycle(t, 1.5), GazeX: .3 * g * cycle(t, 1.5), GazeY: 1.4 * g * (1 - math.Cos(4*math.Pi*t)) / 2, Pitch: .06 * g * cycle(t, 1.5)}
	}},
	{name: "scan-sweep", moods: moods(moodWorking, moodThinking), weight: 1, seconds: 1.5, play: func(t, side, amp float64) mascotOffset {
		g := plateau(t, .1, .15) * amp
		return mascotOffset{GazeX: .36 * g * cycle(t, 1), Yaw: .45 * g * cycle(t, 1), Open: [2]float64{-.06 * g, -.06 * g}}
	}},
	{name: "dance-step", moods: moods(moodIdle, moodWaiting), weight: .6, seconds: 2.4, play: func(t, side, amp float64) mascotOffset {
		g := plateau(t, .12, .15) * amp
		return mascotOffset{Roll: .12 * g * cycle(t, 2), Yaw: .35 * g * cycle(t, 1), Lift: .9 * g * math.Abs(cycle(t, 2)), ArcTilt: -.12 * g * cycle(t, 2), Smile: .6 * g, Arm: [2]float64{.9 * g * cycle(t, 2), -.9 * g * cycle(t, 2)}}
	}},
	{name: "crouch-spring", moods: moods(moodIdle, moodWorking), weight: .6, seconds: 1.1, play: func(t, side, amp float64) mascotOffset {
		air := within(t, .35, .85)
		return mascotOffset{Squash: -.22*within(t, 0, .35)*amp + .1*air, Lift: 3.4 * air * amp, ArcLift: 1.8 * air, Pitch: .06 * air}
	}},
}

// Onset reactions, chosen as varied takes when a mood begins.
var mascotOnsets = []mascotGesture{
	{name: "startle-shake", moods: moods(moodBlocked), weight: 1, seconds: 1, play: func(t, side, amp float64) mascotOffset {
		decay := math.Exp(-t * 4)
		return mascotOffset{Roll: .14 * decay * cycle(t, 5) * amp, Squash: -.12 * within(t, 0, .25), Lift: -.6 * within(t, 0, .3)}
	}},
	{name: "droop", moods: moods(moodBlocked), weight: 1, seconds: 1.3, play: func(t, side, amp float64) mascotOffset {
		g := plateau(t, .15, .3) * amp
		return mascotOffset{Pitch: -.14 * g, GazeY: -2.2 * g, ArcLift: -1.4 * g, Arm: [2]float64{-1.2 * g, -1.2 * g}, Squash: -.06 * g}
	}},
	{name: "scratch", moods: moods(moodBlocked), weight: .8, seconds: 1.2, play: func(t, side, amp float64) mascotOffset {
		g := plateau(t, .15, .2) * amp
		var o mascotOffset
		o.Arm[int(side+1)/2] = (2.6 + .6*cycle(t, 5)) * g
		o.Roll, o.GazeY, o.Slant = side*.08*g, .8*g, .1*g
		return o
	}},
	{name: "joy-hop", moods: moods(moodDone), weight: 1, seconds: 1, play: func(t, side, amp float64) mascotOffset {
		air := within(t, .15, .75)
		return mascotOffset{Squash: -.16*within(t, 0, .22) + .12*within(t, .18, .55) - .1*within(t, .7, .95), Lift: 3 * air * amp, ArcLift: 1.6 * air}
	}},
	{name: "joy-spin", moods: moods(moodDone), weight: .9, seconds: 1.1, play: func(t, side, amp float64) mascotOffset {
		return mascotOffset{Yaw: side * 2 * math.Pi * smooth(.05, .85, t), Lift: 1.8 * bump(t) * amp, ArcLift: 1.4 * bump(t)}
	}},
	{name: "double-hop", moods: moods(moodDone), weight: .8, seconds: 1.1, play: func(t, side, amp float64) mascotOffset {
		air := within(t, .05, .45) + .7*within(t, .5, .85)
		return mascotOffset{Lift: 2.4 * air * amp, Squash: .08*air - .1*within(t, .42, .55), ArcLift: air}
	}},
	{name: "raise-sky", moods: moods(moodDone), weight: .9, seconds: 1.1, play: func(t, side, amp float64) mascotOffset {
		g := plateau(t, .2, .25) * amp
		return mascotOffset{ArcLift: 2.8 * g, Arm: [2]float64{.8 * g * cycle(t, 3), -.8 * g * cycle(t, 3)}, Squash: .08 * g, Lift: .8 * g}
	}},
	{name: "turn-to-user", moods: moods(moodWaiting), weight: 1, seconds: .9, play: func(t, side, amp float64) mascotOffset {
		return mascotOffset{Open: [2]float64{.14 * bump(t), .14 * bump(t)}, Lift: .6 * bump(t) * amp, Pitch: -.05 * bump(t)}
	}},
	{name: "hmm", moods: moods(moodThinking), weight: 1, seconds: .8, play: func(t, side, amp float64) mascotOffset {
		return mascotOffset{Pitch: .07 * bump(t) * amp, Roll: side * .06 * bump(t), GazeY: .8 * bump(t)}
	}},
}

type mascotTake struct {
	gesture *mascotGesture
	start   time.Time
	seconds float64
	side    float64
	amp     float64
	fadeAt  time.Time // Non-zero once the take is fading out.
}

const mascotFade = .2 // Seconds to fade an interrupted gesture.

func (k *mascotTake) progress(now time.Time) float64 {
	return now.Sub(k.start).Seconds() / k.seconds
}

func (k *mascotTake) weight(now time.Time) float64 {
	if k.fadeAt.IsZero() {
		return 1
	}
	return 1 - smooth(0, mascotFade, now.Sub(k.fadeAt).Seconds())
}

func (k *mascotTake) done(now time.Time) bool {
	return k.gesture == nil || k.progress(now) >= 1 || (!k.fadeAt.IsZero() && now.Sub(k.fadeAt).Seconds() >= mascotFade)
}

// mascotPerformer improvises: it never repeats a recent gesture, varies
// timing, side and intensity per take, and blends interrupted takes out.
type mascotPerformer struct {
	seed   uint32
	n      uint32
	take   mascotTake
	fading mascotTake
	next   time.Time
	recent [4]string
	mood   mascotMood
	// context is the current run context; a newly active context brings
	// the next gesture forward so Atlas reacts to it.
	context uint16
	// react holds contexts that just became active; the next pick answers
	// one of them before anything else.
	react uint16
}

// mascotContextBoost is how much a matching context favors a gesture.
const mascotContextBoost = 6

func (p *mascotPerformer) weight(g *mascotGesture, mood mascotMood, require uint16) float64 {
	if g.moods&busyMask(mood) == 0 || p.isRecent(g.name) || (require != 0 && g.contexts&require == 0) {
		return 0
	}
	switch {
	case g.contexts&p.context != 0:
		return g.weight * mascotContextBoost
	case g.only:
		return 0
	}
	return g.weight
}

// setContext updates the run context and reacts to newly active contexts.
func (p *mascotPerformer) setContext(c uint16, now time.Time) {
	if fresh := c &^ p.context; fresh != 0 {
		p.react |= fresh
		if !p.next.IsZero() && p.next.After(now.Add(250*time.Millisecond)) {
			p.next = now.Add(250 * time.Millisecond)
		}
	}
	p.context = c
	p.react &= c
}

func (p *mascotPerformer) random() float64 {
	p.n++
	return mascotNoise(p.seed ^ (p.n * 2654435761))
}

func (p *mascotPerformer) gap(mood mascotMood) time.Duration {
	lo, hi := .7, 2.2
	switch mood {
	case moodThinking, moodWorking:
		lo, hi = .35, 1.3
	case moodWaiting:
		lo, hi = .8, 2.2
	}
	return time.Duration((lo + (hi-lo)*p.random()) * float64(time.Second))
}

func (p *mascotPerformer) pick(pool []mascotGesture, mood mascotMood) *mascotGesture {
	require := uint16(0)
	for i := range pool {
		if p.react != 0 && p.weight(&pool[i], mood, p.react) > 0 {
			require = p.react
			break
		}
	}
	total := 0.0
	for i := range pool {
		total += p.weight(&pool[i], mood, require)
	}
	if total == 0 {
		return nil
	}
	r := p.random() * total
	for i := range pool {
		w := p.weight(&pool[i], mood, require)
		if w == 0 {
			continue
		}
		if r -= w; r <= 0 {
			p.react &^= pool[i].contexts
			return &pool[i]
		}
	}
	return nil
}

func (p *mascotPerformer) isRecent(name string) bool {
	for _, r := range p.recent {
		if r == name {
			return true
		}
	}
	return false
}

func (p *mascotPerformer) begin(g *mascotGesture, now time.Time) {
	if g == nil {
		return
	}
	if p.take.gesture != nil && !p.take.done(now) {
		p.fading = p.take
		if p.fading.fadeAt.IsZero() {
			p.fading.fadeAt = now
		}
	}
	side := 1.0
	if p.random() < .5 {
		side = -1
	}
	p.take = mascotTake{gesture: g, start: now, seconds: g.seconds * (.85 + .35*p.random()), side: side, amp: .8 + .4*p.random()}
	copy(p.recent[1:], p.recent[:3])
	p.recent[0] = g.name
}

// moodChanged fades takes the new mood does not allow and plays an onset.
func (p *mascotPerformer) moodChanged(mood mascotMood, now time.Time) {
	prev := p.mood
	p.mood = mood
	if g := p.take.gesture; g != nil && g.moods&busyMask(mood) == 0 && p.take.fadeAt.IsZero() {
		p.take.fadeAt = now
	}
	// Flipping between thinking and working keeps the running schedule.
	if busyMask(prev) == busyMask(mood) && prev != mood && prev != moodIdle {
		return
	}
	_, onsets := mascotLibrary()
	if onset := p.pick(onsets, mood); onset != nil {
		p.begin(onset, now)
	}
	p.next = now.Add(p.gap(mood))
}

func (p *mascotPerformer) eyesBusy(now time.Time) bool {
	return p.take.gesture != nil && p.take.gesture.eyes && !p.take.done(now)
}

// apply advances the schedule and adds the blended gesture layer to p.
// A spin's full turn is the same orientation, so its end needs no unwind.
func (p *mascotPerformer) apply(mood mascotMood, now time.Time, pose *mascotPose) {
	if p.take.gesture != nil && p.take.done(now) {
		p.take = mascotTake{}
		p.next = now.Add(p.gap(mood))
	}
	if p.take.gesture == nil && !now.Before(p.next) && mood != moodBlocked && mood != moodDone {
		fillers, _ := mascotLibrary()
		p.begin(p.pick(fillers, mood), now)
	}
	for _, k := range []*mascotTake{&p.fading, &p.take} {
		if k.gesture != nil && !k.done(now) {
			k.gesture.play(k.progress(now), k.side, k.amp).addTo(pose, k.weight(now))
		}
	}
}

// mascotDrift is smooth value noise in [-1, 1] for slow, unlooped life.
func mascotDrift(seed uint32, x float64) float64 {
	i := math.Floor(x)
	f := x - i
	a := mascotNoise(seed+uint32(int64(i))*977) * 2
	b := mascotNoise(seed+uint32(int64(i)+1)*977) * 2
	u := f * f * (3 - 2*f)
	return a + (b-a)*u - 1
}
