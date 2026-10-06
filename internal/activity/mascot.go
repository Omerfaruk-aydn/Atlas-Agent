package activity

import (
	"math"
	"time"
)

// Phase is the verified run state that the banner character expresses. The
// zero value describes an executing operation, so Begin needs no extra field.
type Phase uint8

const (
	// PhaseWorking marks a tool operation that is executing.
	PhaseWorking Phase = iota
	// PhaseThinking marks a run waiting on the model between operations.
	PhaseThinking
	// PhaseWaiting marks a run blocked on an explicit user decision.
	PhaseWaiting
	// PhaseFailed marks an operation whose tool reported a failure.
	PhaseFailed
	// PhaseDone marks a run that completed without cancellation or error.
	PhaseDone
)

const (
	// MascotWidth and MascotHeight are the character slot in logical pixels.
	MascotWidth  = 36
	MascotHeight = 36

	// A provider wait shorter than this keeps the working pose, so quick
	// tool chains do not flicker between working and thinking.
	mascotThinkingDelay = 280 * time.Millisecond
	// A failure reaction is brief; afterwards the run is thinking again.
	mascotBlockedHold = 1400 * time.Millisecond
	// The completed run keeps only the banner for its settling motion.
	mascotDoneLinger = 1300 * time.Millisecond
	// Confirmed input older than this no longer earns a tap reaction.
	mascotTapWindow = 300 * time.Millisecond
)

type mascotMood uint8

const (
	moodIdle mascotMood = iota
	moodThinking
	moodWorking
	moodWaiting
	moodBlocked
	moodDone
)

func (m mascotMood) String() string {
	return [...]string{"idle", "thinking", "working", "waiting", "blocked", "done"}[m]
}

// mascotMoodFor maps only recorded application events to an expression; it
// never inspects model text.
func mascotMoodFor(e Event, now time.Time) mascotMood {
	age := now.Sub(e.PhaseAt)
	switch e.Phase {
	case PhaseDone:
		return moodDone
	case PhaseWaiting:
		return moodWaiting
	case PhaseFailed:
		if age < mascotBlockedHold {
			return moodBlocked
		}
		return moodThinking
	case PhaseThinking:
		if age < mascotThinkingDelay {
			return moodWorking
		}
		return moodThinking
	}
	if e.Action == "wait" || e.Action == "wait_for" || e.Action == "download_wait" {
		return moodIdle
	}
	return moodWorking
}

// mascotMoodOnset is when the current mood began according to the event,
// so reactions are anchored to real event time rather than frame timing.
func mascotMoodOnset(e Event, mood mascotMood) time.Time {
	switch {
	case e.Phase == PhaseThinking && mood == moodThinking:
		return e.PhaseAt.Add(mascotThinkingDelay)
	case e.Phase == PhaseFailed && mood == moodThinking:
		return e.PhaseAt.Add(mascotBlockedHold)
	}
	return e.PhaseAt
}

type mascotWork uint8

const (
	workAct mascotWork = iota
	workPoint
	workType
	workOpen
	workScroll
	workInspect
)

func mascotWorkFor(action string) mascotWork {
	switch action {
	case "click", "double_click", "right_click", "invoke", "semantic_click", "move", "drag", "select", "focus", "upload":
		return workPoint
	case "type", "set_value", "vault_fill", "auth_code", "semantic_type", "key", "hotkey":
		return workType
	case "navigate", "back", "forward", "launch_app", "tab_new", "tab_select", "tab_close", "download_start":
		return workOpen
	case "scroll":
		return workScroll
	case "observe", "screenshot", "inspect", "find", "assert", "ocr", "capture_region", "capture_window", "windows",
		"monitors", "text", "html", "snapshot", "images", "console", "frames", "network", "url", "tabs", "health":
		return workInspect
	}
	return workAct
}

// Look points from the banner toward the verified target, each axis in
// [-1, 1]; positive Y is above the banner.
type Look struct{ X, Y float64 }

type mascotPose struct {
	Yaw, Pitch, Roll float64
	Squash, Lift     float64
	GazeX, GazeY     float64
	Open             [2]float64
	Slant, Smile     float64
	ArcLift, Glow    float64
	ArcTilt          float64
	Arm              [2]float64 // Independent hand lift in logical pixels.
	Warn, Flow       float64
	Browser          bool
	// Expression beyond the eyes.
	MouthOpen, MouthCurve float64
	MouthWide, MouthSkew  float64
	MouthWave, Blush      float64
	// The laptop Atlas works on, and the hands that use it.
	Laptop, Lid  float64 // Presence and lid opening, 0 to 1.
	Hands, BodyX float64 // Hands on the laptop instead of the sky.
	Hand         [2][2]float64
	Press        [2]float64
	Screen       mascotScreen
	// The laptop moves with Atlas: it turns, bounces and rocks.
	LapYaw, LapLift, LapRoll float64
	MouseAt                  [2]float64
}

// Animated channels. Procedural amplitudes are springs too, so a rhythm can
// fade in and out instead of starting or stopping on a frame boundary.
const (
	chYaw = iota
	chPitch
	chRoll
	chSquash
	chLift
	chGazeX
	chGazeY
	chOpenL
	chOpenR
	chSlant
	chSmile
	chArcLift
	chGlow
	chWarn
	chFlowRate
	chBreath
	chBob
	chScan
	chMouthOpen
	chMouthCurve
	chMouthWide
	chMouthSkew
	chMouthWave
	chBlush
	chLaptop
	chLid
	chHands
	chBodyX
	chHandLA
	chHandLB
	chHandRA
	chHandRB
	chType
	chPad
	chDots
	chDialog
	chClick
	chLapYaw
	chLapLift
	chLapRoll
	chCount
)

// Natural frequency (rad/s) and damping ratio per channel. The body is
// slightly underdamped for organic settling; eyes and lids are critical.
var mascotSprings = [chCount][2]float64{
	chYaw: {13, .78}, chPitch: {13, .78}, chRoll: {12, .7}, chSquash: {18, .52}, chLift: {16, .5},
	chGazeX: {26, 1}, chGazeY: {26, 1}, chOpenL: {30, 1}, chOpenR: {28, 1}, chSlant: {16, 1}, chSmile: {14, 1},
	chArcLift: {11, .62}, chGlow: {6, 1}, chWarn: {7, 1}, chFlowRate: {4, 1}, chBreath: {3, 1}, chBob: {8, 1}, chScan: {8, 1},
	chMouthOpen: {20, .75}, chMouthCurve: {14, 1}, chMouthWide: {14, 1}, chMouthSkew: {10, 1}, chMouthWave: {10, 1}, chBlush: {5, 1},
	chLaptop: {9, .55}, chLid: {9, .8}, chHands: {8, .85}, chBodyX: {8, .9},
	chHandLA: {15, .9}, chHandLB: {15, .9}, chHandRA: {15, .9}, chHandRB: {15, .9},
	chType: {9, 1}, chPad: {8, 1}, chDots: {6, 1}, chDialog: {7, 1}, chClick: {26, 1},
	chLapYaw: {9, .6}, chLapLift: {17, .38}, chLapRoll: {15, .35},
}

type mascotSpring struct{ x, v float64 }

type mascotAnimator struct {
	ready    bool
	epoch    time.Time
	last     time.Time
	springs  [chCount]mascotSpring
	mood     mascotMood
	moodAt   time.Time
	tapStamp time.Time
	flow     float64
	breath   float64
	blink    mascotBlink
	perf     mascotPerformer
	ctx      mascotContext
	keys     [2]float64 // Typing phase per hand, in taps.
	pressed  [2]bool    // Whether each hand was pressing last frame.
	work     mascotWork
	loadAt   time.Time
}

type mascotBlink struct {
	next, start time.Time
	double      bool
	n           uint32
}

func (a *mascotAnimator) reset() { *a = mascotAnimator{} }

// mascotNoise is a stable pseudo-random value in [0, 1) for schedules.
func mascotNoise(n uint32) float64 {
	n = n*747796405 + 2891336453
	n = ((n >> ((n >> 28) + 4)) ^ n) * 277803737
	return float64((n>>22)^n) / float64(1<<32)
}

func clampUnit(v float64) float64 { return max(-1, min(1, v)) }

func (a *mascotAnimator) targets(e Event, mood mascotMood, look Look, now time.Time) [chCount]float64 {
	var t [chCount]float64
	t[chOpenL], t[chOpenR], t[chGlow], t[chFlowRate], t[chBreath] = 1, 1, .55, .05, 1
	t[chMouthCurve], t[chLaptop], t[chLid], t[chBodyX] = .3, 1, 1, mascotDeskShift
	rest := mascotRestHands
	t[chHandLA], t[chHandLB], t[chHandRA], t[chHandRB] = rest[0][0], rest[0][1], rest[1][0], rest[1][1]
	seconds := now.Sub(a.moodAt).Seconds()
	if e.ReducedMotion {
		// Expressions still change with state, but nothing drifts over time.
		seconds = 0
	}
	switch mood {
	case moodIdle:
		// A calm, slightly curious rest; the performer adds the gestures.
		t[chSmile], t[chGazeY] = .06, .3
		t[chMouthCurve], t[chMouthWide] = .4, .1
	case moodThinking:
		// Upright and symmetric; only the eyes drift while thinking.
		t[chPitch] = .06
		t[chGazeX] = .08 * math.Sin(seconds*1.6)
		t[chGazeY] = 1.5
		t[chOpenL], t[chOpenR] = .8, .8
		t[chSlant] = -.06
		t[chArcLift], t[chGlow], t[chFlowRate] = .5, .72, .12
		// Hands rest on the laptop while the model answers.
		t[chHands], t[chDots] = 1, 1
		t[chMouthCurve], t[chMouthWide], t[chMouthSkew] = -.05, -.45, .6
	case moodWorking:
		x, y := clampUnit(look.X), clampUnit(look.Y)
		t[chGazeX], t[chGazeY] = x*.38, y*2.2
		t[chYaw], t[chPitch] = x*.2, y*.08
		t[chOpenL], t[chOpenR] = .92, .92
		t[chSlant] = .06
		t[chArcLift], t[chGlow], t[chFlowRate], t[chBreath] = 1, .92, .3, .4
		// Working happens on the laptop: the target only steers the gaze.
		t[chHands], t[chType] = 1, .35
		t[chYaw], t[chPitch] = x*.04+mascotDeskYaw, y*.03-.06
		t[chGazeX], t[chGazeY] = x*.14+mascotDeskGaze[0], y*.6+mascotDeskGaze[1]
		t[chMouthCurve], t[chMouthWide] = .12, -.25
		mouse := [2]float64{mascotMouse[0] + .03*x, mascotMouse[1] - .05*y}
		switch mascotWorkFor(e.Action) {
		case workType:
			t[chGazeY] -= .5
			t[chBob], t[chType] = .6, 1
			t[chMouthOpen], t[chMouthCurve] = .12, .05
		case workPoint:
			t[chType] = 0
			t[chHandRA], t[chHandRB] = mouse[0], mouse[1]
		case workOpen:
			t[chPitch] -= .05
			t[chArcLift], t[chType] = 1.6, .6
		case workScroll:
			t[chType], t[chPad] = 0, 1
			t[chHandRA], t[chHandRB] = mouse[0], mouse[1]
			t[chGazeY] += .6 * math.Sin(seconds*4.2)
		case workInspect:
			t[chScan], t[chType] = 1, 0
			t[chOpenL], t[chOpenR] = .86, .86
			t[chYaw], t[chPitch], t[chBodyX] = mascotDeskYaw*1.4, -.12, mascotDeskShift*.7
			t[chMouthCurve], t[chMouthOpen], t[chMouthWide] = 0, .2, -.6
		}
	case moodWaiting:
		// Facing the user: centered, attentive and visibly not busy.
		t[chPitch], t[chGazeY] = -.03, -.3
		t[chOpenL], t[chOpenR] = 1.06, 1.06
		t[chSmile], t[chGlow], t[chFlowRate], t[chBreath] = .2, .45, .03, .7
		t[chMouthCurve], t[chMouthOpen], t[chDialog], t[chBodyX] = .55, .08, 1, mascotDeskShift*.6
	case moodBlocked:
		t[chOpenL], t[chOpenR] = .98, .98
		if seconds < .25 {
			t[chOpenL], t[chOpenR] = 1.16, 1.16
		}
		t[chSlant], t[chWarn], t[chGazeY] = .45, 1, -.8
		t[chArcLift], t[chGlow], t[chFlowRate], t[chBreath] = -.9, .38, .02, .3
		t[chMouthCurve], t[chMouthWave], t[chMouthWide] = -.6, .8, .1
		if seconds < .35 {
			t[chMouthOpen], t[chMouthWave] = .55, 0
		}
	case moodDone:
		t[chSmile], t[chArcLift], t[chGlow], t[chFlowRate] = 1, 2.1, 1, .25
		t[chOpenL], t[chOpenR] = 1.08, 1.08
		// The work is finished: close the lid and put the laptop away.
		t[chMouthCurve], t[chMouthOpen], t[chMouthWide], t[chBlush], t[chLid] = 1, .7, .5, 1, 0
		t[chBodyX] = 0
		if seconds > .45 {
			t[chLaptop] = 0
		}
	}
	return t
}

// onMood applies one-shot impulses so reactions add momentum to the current
// pose instead of restarting it.
func (a *mascotAnimator) onMood(from, to mascotMood, now time.Time) {
	switch to {
	case moodBlocked:
		// The failure jolts the laptop as well as Atlas.
		a.springs[chLapRoll].v += 2.6
		a.springs[chLapLift].v -= 7
	case moodDone:
		// A small hop as the lid snaps shut.
		a.springs[chLapLift].v += 12
	case moodWaiting:
		a.springs[chLapYaw].v += 1.5
	}
	if to == moodWorking && (from == moodIdle || from == moodWaiting) {
		a.springs[chPitch].v -= 2.2 // Acknowledge the task.
	}
	a.perf.moodChanged(to, now)
}

// blinkStart schedules blinks independently of gaze and body motion and
// returns the start of the active blink, or zero between blinks.
func (a *mascotAnimator) blinkStart(mood mascotMood, smile float64, now time.Time) time.Time {
	b := &a.blink
	if b.next.IsZero() {
		b.next = now.Add(time.Duration((1.2 + 1.8*mascotNoise(b.n)) * float64(time.Second)))
	}
	if b.start.IsZero() && !now.Before(b.next) && smile < .5 && mood != moodBlocked && !a.perf.eyesBusy(now) {
		b.start, b.double = now, mascotNoise(b.n*7+5) < .16
	}
	if b.start.IsZero() {
		return time.Time{}
	}
	length := .18
	if b.double {
		length = .4
	}
	if now.Sub(b.start).Seconds() >= length {
		b.n++
		mean := 3.6
		switch mood {
		case moodIdle:
			mean = 3.4
		case moodThinking:
			mean = 2.6
		case moodWorking:
			mean = 4.4
		case moodWaiting:
			mean = 4.2
		}
		b.next = now.Add(time.Duration(mean * (.6 + .8*mascotNoise(b.n*3+11)) * float64(time.Second)))
		b.start = time.Time{}
	}
	return b.start
}

// blinkCurve closes fast and opens slower; it never fully hides an eye.
func blinkCurve(age float64, double bool) float64 {
	if double && age >= .22 {
		age -= .22
	}
	switch {
	case age < 0 || age >= .16:
		return 1
	case age < .055:
		u := age / .055
		return 1 - .9*u*u
	default:
		u := (age - .055) / .105
		return .1 + .9*(1-(1-u)*(1-u))
	}
}

// step advances every layer with monotonic time and returns the pose.
// Rendering cost and frame rate never change the motion speed.
func (a *mascotAnimator) step(e Event, now time.Time, look Look) mascotPose {
	mood := mascotMoodFor(e, now)
	fresh := !a.ready
	if fresh {
		a.mood, a.moodAt, a.epoch = mood, now, now
		if a.perf.seed == 0 {
			// Each appearance improvises differently; tests may preset it.
			a.perf.seed = uint32(now.UnixNano()) | 1
		}
		a.perf.next = now.Add(time.Duration((.6 + .9*a.perf.random()) * float64(time.Second)))
	} else if mood != a.mood {
		previous := a.mood
		// Never earlier than the last frame, so nothing starts mid-motion.
		onset := mascotMoodOnset(e, mood)
		if onset.Before(a.last) || onset.IsZero() {
			onset = a.last
		}
		if onset.After(now) {
			onset = now
		}
		a.mood, a.moodAt = mood, onset
		if !e.ReducedMotion {
			a.onMood(previous, mood, onset)
		}
	}
	a.ctx.observe(e, mood, now)
	a.perf.setContext(a.ctx.flags(mood, a.epoch, a.moodAt, now), now)
	target := a.targets(e, mood, look, now)
	if fresh || e.ReducedMotion {
		// The first frame is already a complete pose: no empty or popping frame.
		for i := range a.springs {
			a.springs[i] = mascotSpring{x: target[i]}
		}
		a.ready, a.last = true, now
		if fresh && !e.ReducedMotion {
			// The laptop grows in and its lid swings open.
			a.springs[chLaptop].x, a.springs[chLid].x = 0, 0
		}
		if fresh && mood == moodWorking && !e.ReducedMotion {
			a.springs[chPitch].v = -2.2
		}
	}
	// A confirmed click earns one tap, keyed by its input timestamp.
	if e.PointerKind == "click" && !e.PointerAt.Equal(a.tapStamp) && now.Sub(e.PointerAt) < mascotTapWindow && !e.ReducedMotion {
		a.tapStamp = e.PointerAt
		a.springs[chSquash].v -= 1.5
		a.springs[chLift].v -= 9
		a.springs[chOpenL].v -= 7
		a.springs[chOpenR].v -= 7
		a.springs[chClick].v += 60
		a.springs[chLapLift].v -= 2.5
	}
	dt := min(.1, max(0, now.Sub(a.last).Seconds()))
	a.last = now
	for remaining := dt; remaining > 1e-9; {
		h := min(remaining, .004)
		remaining -= h
		for i := range a.springs {
			s, w, z := &a.springs[i], mascotSprings[i][0], mascotSprings[i][1]
			s.v += (w*w*(target[i]-s.x) - 2*z*w*s.v) * h
			s.x += s.v * h
		}
	}
	s := func(i int) float64 { return a.springs[i].x }
	if !e.ReducedMotion {
		a.flow += s(chFlowRate) * dt
	}
	wall := now.Sub(a.epoch).Seconds()
	p := mascotPose{
		Yaw: s(chYaw), Pitch: s(chPitch), Roll: s(chRoll),
		Squash: s(chSquash), Lift: max(-1.2, min(2.4, s(chLift))),
		GazeX: s(chGazeX), GazeY: s(chGazeY),
		Slant: s(chSlant), Smile: max(0, min(1, s(chSmile))),
		ArcLift: s(chArcLift), Glow: max(0, min(1.2, s(chGlow))), Warn: max(0, min(1, s(chWarn))),
		Flow: a.flow, Browser: e.Resource == "browser",
		MouthOpen: max(0, s(chMouthOpen)), MouthCurve: s(chMouthCurve), MouthWide: s(chMouthWide),
		MouthSkew: s(chMouthSkew), MouthWave: max(0, s(chMouthWave)), Blush: max(0, s(chBlush)),
		Laptop: max(0, s(chLaptop)), Lid: max(0, min(1, s(chLid))), Hands: max(0, min(1, s(chHands))), BodyX: s(chBodyX),
		Hand: [2][2]float64{{s(chHandLA), s(chHandLB)}, {s(chHandRA), s(chHandRB)}},
	}
	a.laptop(e, mood, now, dt, wall, &p)
	if !e.ReducedMotion {
		// Additive rhythm layers; each fades with its own envelope spring.
		// Breathing drifts in tempo and slow noise keeps rest from looping.
		seed := a.perf.seed
		a.breath += dt * 2 * math.Pi / (3.3 + .5*(mascotDrift(seed+5, wall*.2)+1))
		p.Squash += .014 * s(chBreath) * math.Sin(a.breath)
		p.Lift += .55 * s(chBob) * math.Abs(math.Sin(wall*2*math.Pi*2.6))
		if scan := s(chScan); scan > .01 {
			step := uint32(wall / (.36 + .12*mascotNoise(seed+uint32(wall))))
			p.GazeX += scan * (mascotNoise(seed+step) - .5) * .62
		}
		p.GazeX += .05 * mascotDrift(seed+11, wall*.35)
		p.GazeY += .35 * mascotDrift(seed+23, wall*.3)
		p.Roll += .008 * mascotDrift(seed+37, wall*.25)
		p.ArcTilt += .015 * mascotDrift(seed+41, wall*.22)
		a.perf.apply(mood, now, &p)
		p.Roll = max(-.12, min(.12, p.Roll))
		p.Lift = max(-1.5, min(3, p.Lift))
		p.ArcLift = min(3.2, p.ArcLift)
		p.Smile = max(0, min(1, p.Smile))
		p.MouthOpen, p.MouthWave = max(0, min(1.2, p.MouthOpen)), max(0, min(1, p.MouthWave))
		p.MouthCurve, p.Blush = max(-1.2, min(1.4, p.MouthCurve)), max(0, min(1, p.Blush))
		for i := range 2 {
			p.Arm[i] = max(-2, min(3.4, p.Arm[i]))
		}
	}
	left, right := 1.0, 1.0
	if !e.ReducedMotion {
		if start := a.blinkStart(mood, p.Smile, now); !start.IsZero() {
			age := now.Sub(start).Seconds()
			// The trailing lid lags slightly, reading as two separate eyes.
			left, right = blinkCurve(age, a.blink.double), blinkCurve(age-.012, a.blink.double)
		}
	}
	p.Open = [2]float64{max(.12, (s(chOpenL)+p.Open[0])*left), max(.12, (s(chOpenR)+p.Open[1])*right)}
	return p
}
