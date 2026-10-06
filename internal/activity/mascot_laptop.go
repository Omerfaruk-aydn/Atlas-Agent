package activity

import (
	"math"
	"time"
)

// Deck coordinates run from the character's left (0) to right (1) and from
// the hinge (0) to the edge nearest Atlas (1). The camera looks over the
// lid, so the keys sit in the half of the deck that stays visible; the
// mouse rests on the table to the right, outside the deck.
var (
	mascotRestHands = [2][2]float64{{.2, .96}, {.8, .96}}
	mascotMouse     = [2]float64{1.22, .8}
	mascotDeskGaze  = [2]float64{0, -1.7}
)

const (
	mascotDeskShift = 0.0 // Body offset while the laptop is out.
	mascotDeskRise  = 4.2 // Body lift while the laptop is out.
	mascotDeskYaw   = 0.0 // Head turn toward the screen.
	mascotKeyCols   = 12  // Keyboard columns; each hand owns one half.
	mascotKeyRows   = 3
	mascotKeyA0     = .07 // Keyboard bounds in deck coordinates.
	mascotKeyA1     = .93
	mascotKeyB0     = .46
	mascotKeyB1     = .9
)

// mascotDesk places the laptop in the slot. It is part of the scene so
// alternative compositions can be compared with the same renderer.
type mascotDesk struct {
	pos        vec3    // Hinge center.
	yaw, tilt  float64 // Turn around y, then the viewing tilt around x.
	w, d, h, t float64 // Width, deck depth, lid height and thickness.
	open       float64 // Full lid opening in radians.
}

// mascotKey is the deck position of a key.
func mascotKey(col, row int) [2]float64 {
	return [2]float64{
		mascotKeyA0 + (mascotKeyA1-mascotKeyA0)*(float64(col)+.5)/mascotKeyCols,
		mascotKeyB0 + (mascotKeyB1-mascotKeyB0)*(float64(row)+.5)/mascotKeyRows,
	}
}

func mascotKeyIndex(uv [2]float64) (int, int) {
	return int(math.Floor((uv[0] - mascotKeyA0) / (mascotKeyA1 - mascotKeyA0) * mascotKeyCols)),
		int(math.Floor((uv[1] - mascotKeyB0) / (mascotKeyB1 - mascotKeyB0) * mascotKeyRows))
}

// mascotScreen is the laptop state that the lid logo and the screen light
// express; it follows the same verified events as the character.
type mascotScreen struct {
	Load   float64 // Navigation progress, 0 to 1.
	Ripple float64 // Mouse click flash, 0 to 1.
	Scan   float64 // Inspection.
	Dots   float64 // Thinking.
	Dialog float64 // Waiting for the user.
	Typing float64 // Key light flicker.
	Time   float64
}

// laptop drives the hands, keys and mouse. Typing is a stream of taps on
// varying keys with pauses between words; the mouse button is pressed only
// for confirmed clicks.
func (a *mascotAnimator) laptop(e Event, mood mascotMood, now time.Time, dt, wall float64, p *mascotPose) {
	s := func(i int) float64 { return a.springs[i].x }
	if mood == moodWorking {
		work := mascotWorkFor(e.Action)
		if work == workOpen && (a.work != workOpen || a.loadAt.IsZero()) {
			a.loadAt = now
		}
		a.work = work
	}
	typing := max(0, min(1, s(chType)))
	seed := a.perf.seed
	if !e.ReducedMotion {
		for i := range 2 {
			rate := 4.6 + 1.4*mascotDrift(seed+61+uint32(i)*7, wall*.5)
			a.keys[i] += dt * rate * typing
			tap := math.Floor(a.keys[i])
			f := a.keys[i] - tap
			k := uint32(int64(tap))
			key := func(n uint32) [2]float64 {
				col := int(mascotNoise(seed+n*2971+uint32(i)*131) * (mascotKeyCols / 2))
				if i == 1 {
					col += mascotKeyCols / 2
				}
				row := int(mascotNoise(seed+n*1871+uint32(i)*57) * mascotKeyRows)
				return mascotKey(min(col, mascotKeyCols-1), min(row, mascotKeyRows-1))
			}
			from, to := key(k), key(k+1)
			move := smooth(0, .4, f)
			// Word gaps: the drift gate lifts the fingers now and then.
			gate := smooth(-.35, .1, mascotDrift(seed+71+uint32(i)*13, wall*.8))
			for j := range 2 {
				p.Hand[i][j] += (from[j] + (to[j]-from[j])*move - p.Hand[i][j]) * typing
			}
			p.Press[i] = within(f, .42, .92) * gate * typing
		}
	}
	// Scrolling rolls the wheel: the finger strokes back and forth.
	pad := max(0, s(chPad))
	if !e.ReducedMotion {
		p.Hand[1][1] += .05 * pad * math.Sin(wall*2*math.Pi*1.6)
	}
	// While pointing, the hand drifts the mouse toward the target.
	onMouse := smooth(1, 1.12, p.Hand[1][0])
	if !e.ReducedMotion && onMouse > 0 {
		p.Hand[1][0] += .035 * mascotDrift(seed+91, wall*.9) * onMouse
		p.Hand[1][1] += .07 * mascotDrift(seed+97, wall*.8) * onMouse
	}
	p.MouseAt = [2]float64{
		mascotMouse[0] + (p.Hand[1][0]-mascotMouse[0])*onMouse,
		mascotMouse[1] + (p.Hand[1][1]-mascotMouse[1])*onMouse,
	}
	p.Press[1] = max(p.Press[1], max(0, min(1, s(chClick))), .35*pad)
	// Each keystroke knocks the laptop a little, from the side it lands.
	if !e.ReducedMotion {
		for i := range 2 {
			down := p.Press[i] > .55
			if down && !a.pressed[i] {
				a.springs[chLapLift].v -= .9
				a.springs[chLapRoll].v += float64(i*2-1) * .35
			}
			a.pressed[i] = down
		}
	}
	// The laptop follows Atlas a little and never sits perfectly still.
	p.LapYaw, p.LapLift, p.LapRoll = s(chLapYaw)+.12*p.Yaw, s(chLapLift), s(chLapRoll)
	if !e.ReducedMotion {
		p.LapYaw += .04 * mascotDrift(seed+103, wall*.3)
		p.LapLift += .12 * mascotDrift(seed+107, wall*.45)
		p.LapRoll += .012 * mascotDrift(seed+109, wall*.35)
	}
	p.Screen = mascotScreen{
		Scan: max(0, s(chScan)), Dots: max(0, min(1, s(chDots))), Dialog: max(0, min(1, s(chDialog))),
		Typing: typing * (p.Press[0] + p.Press[1]) / 2,
	}
	if !e.ReducedMotion {
		p.Screen.Time = wall
	}
	if !a.loadAt.IsZero() {
		if load := now.Sub(a.loadAt).Seconds() / .9; load < 1.4 {
			p.Screen.Load = min(1, load) * (1 - smooth(1.1, 1.4, load))
		}
	}
	if age := now.Sub(a.tapStamp).Seconds(); !a.tapStamp.IsZero() && age >= 0 && age < .5 {
		p.Screen.Ripple = 1 - age/.5
	}
}

type mascotPanelKind uint8

const (
	panelDeck    mascotPanelKind = iota
	panelEdge                    // Machined side walls of the base.
	panelLid                     // The screen side of the lid.
	panelLidBack                 // The outer lid with the logo.
	panelLidEdge                 // The thin outer rim of the lid.
)

// mascotPanel is a flat rounded parallelogram c + a*u + b*v, a and b in
// [0, 1].
type mascotPanel struct {
	c, u, v    vec3
	n          vec3 // Front normal.
	kind       mascotPanelKind
	round      float64 // Corner radius in logical pixels.
	det        float64
	sizeA      float64 // Logical pixel extent across a and across b.
	sizeB      float64
	minX, maxX float64
	minY, maxY float64
}

func newMascotPanel(kind mascotPanelKind, c, u, v, n vec3, round float64) mascotPanel {
	p := mascotPanel{c: c, u: u, v: v, n: n.unit(), kind: kind}
	p.det = u.x*v.y - u.y*v.x
	p.sizeA = math.Abs(p.det) / max(1e-9, hypot(v.x, v.y))
	p.sizeB = math.Abs(p.det) / max(1e-9, hypot(u.x, u.y))
	p.round = min(round, p.sizeA/2, p.sizeB/2)
	xs := [4]float64{c.x, c.x + u.x, c.x + v.x, c.x + u.x + v.x}
	ys := [4]float64{c.y, c.y + u.y, c.y + v.y, c.y + u.y + v.y}
	p.minX, p.maxX, p.minY, p.maxY = xs[0], xs[0], ys[0], ys[0]
	for i := 1; i < 4; i++ {
		p.minX, p.maxX = min(p.minX, xs[i]), max(p.maxX, xs[i])
		p.minY, p.maxY = min(p.minY, ys[i]), max(p.maxY, ys[i])
	}
	p.minX, p.maxX, p.minY, p.maxY = p.minX-1, p.maxX+1, p.minY-1, p.maxY+1
	return p
}

// roundRect is the signed distance to a rounded rectangle of half size
// hx, hy centered at the origin.
func roundRect(x, y, hx, hy, r float64) float64 {
	qx, qy := math.Abs(x)-(hx-r), math.Abs(y)-(hy-r)
	return hypot(max(qx, 0), max(qy, 0)) + min(max(qx, qy), 0) - r
}

// mascotLaptop is the laptop frame for one rendered pose.
type mascotLaptop struct {
	on         bool
	hinge      vec3
	ax, ay, az vec3 // Width, deck normal and hinge-to-user directions.
	w, d, t    float64
	glow       vec3 // Screen light cast on Atlas.
	glowDir    vec3
	shadow     [4]float64 // Contact shadow center and radii on screen.
	shade      float64
}

// deck returns the world point at deck coordinates a, b and height h.
func (l *mascotLaptop) deck(a, b, h float64) vec3 {
	return l.hinge.add(l.ax.mul((a - .5) * l.w)).add(l.az.mul(b * l.d)).add(l.ay.mul(h))
}

// hand returns where a hand rests for deck coordinates uv while pressing.
func (l *mascotLaptop) hand(uv [2]float64, press float64) vec3 {
	if uv[0] > 1 {
		// Above the mouse, which sits on the table beside the deck.
		return l.deck(uv[0], uv[1], 2.45-l.t-.4*press)
	}
	return l.deck(uv[0], uv[1], 1.45-.95*press)
}

func (f *mascotFrame) buildLaptop() {
	f.panels = f.panels[:0]
	f.lap = mascotLaptop{}
	f.mouse = mascotMouseShape{}
	desk := f.scene.desk
	presence := f.pose.Laptop
	if desk == nil || presence < .03 {
		return
	}
	k := min(1.12, presence)
	cx, sx := math.Cos(desk.tilt), math.Sin(desk.tilt)
	tilt := mat3{{1, 0, 0}, {0, cx, -sx}, {0, sx, cx}}
	r := tilt.times(mascotRotation(desk.yaw+f.pose.LapYaw, 0, f.pose.LapRoll))
	l := &f.lap
	l.on = true
	l.ax, l.ay, l.az = r.apply(vec3{1, 0, 0}), r.apply(vec3{0, 1, 0}), r.apply(vec3{0, 0, -1})
	// The laptop grows out of its own base so it never pops in.
	l.hinge = desk.pos.add(vec3{0, f.pose.LapLift - 2.5*(1-min(1, presence)), 0})
	l.w, l.d, l.t = desk.w*k, desk.d*k, desk.t*k
	lidH, lidT := desk.h*k, desk.t*k*.42
	angle := desk.open * f.pose.Lid
	lid := l.az.mul(math.Cos(angle)).add(l.ay.mul(math.Sin(angle)))
	screenN := l.az.mul(math.Sin(angle)).sub(l.ay.mul(math.Cos(angle)))
	left := l.hinge.sub(l.ax.mul(l.w / 2))
	width, depth, down := l.ax.mul(l.w), l.az.mul(l.d), l.ay.mul(-l.t)
	// The lid shell sits on the hinge, its outer face a lid thickness
	// behind the screen.
	lidBase := left.add(l.ay.mul(.08))
	back := screenN.mul(-lidT)
	const round = 1.1
	f.panels = append(f.panels,
		newMascotPanel(panelDeck, left, width, depth, l.ay, round),
		newMascotPanel(panelEdge, left.add(depth), width, down, l.az, .45),
		newMascotPanel(panelEdge, left, width, down, l.az.mul(-1), .45),
		newMascotPanel(panelEdge, left, depth, down, l.ax.mul(-1), .45),
		newMascotPanel(panelEdge, left.add(width), depth, down, l.ax, .45),
		newMascotPanel(panelLid, lidBase, width, lid.mul(lidH), screenN, round),
		newMascotPanel(panelLidBack, lidBase.add(back), width, lid.mul(lidH), screenN.mul(-1), round),
		newMascotPanel(panelLidEdge, lidBase.add(lid.mul(lidH)), width, back, lid, lidT/2),
		newMascotPanel(panelLidEdge, lidBase, lid.mul(lidH), back, l.ax.mul(-1), lidT/2),
		newMascotPanel(panelLidEdge, lidBase.add(width), lid.mul(lidH), back, l.ax, lidT/2),
	)
	f.buildMouse(k)
	// The soft contact shadow grounds the laptop on the banner.
	base := l.deck(.5, .5, -l.t)
	l.shadow = [4]float64{base.x + .6, base.y - .9, l.w * .62, 1.7}
	l.shade = .5 * min(1, presence)
	// The hidden screen lights the side of Atlas that faces it; its color
	// follows what the laptop is doing.
	sc := f.pose.Screen
	open := smooth(.15, .7, f.pose.Lid) * min(1, presence)
	glow := vec3{.10, .16, .30}.mul(1 + .5*sc.Typing + 1.4*sc.Load + .6*sc.Scan)
	glow = glow.mix(vec3{.18, .24, .42}, sc.Dialog*.6)
	glow = glow.mix(vec3{.38, .17, .03}, f.pose.Warn*.8)
	l.glow = glow.mul(open)
	center := l.hinge.add(lid.mul(lidH / 2))
	l.glowDir = center.sub(f.origin.add(vec3{0, 9, 0})).unit()
}

func (f *mascotFrame) panelSample(x, y float64) mascotSample {
	var best mascotSample
	for i := range f.panels {
		p := &f.panels[i]
		if x < p.minX || x > p.maxX || y < p.minY || y > p.maxY || math.Abs(p.det) < 1e-6 {
			continue
		}
		dx, dy := x-p.c.x, y-p.c.y
		a := (dx*p.v.y - dy*p.v.x) / p.det
		b := (p.u.x*dy - p.u.y*dx) / p.det
		edge := roundRect((a-.5)*p.sizeA, (b-.5)*p.sizeB, p.sizeA/2, p.sizeB/2, p.round)
		alpha := max(0, min(1, .5-edge*f.scale))
		if alpha <= 0 {
			continue
		}
		depth := p.c.z + a*p.u.z + b*p.v.z
		if best.alpha > 0 && depth <= best.depth {
			continue
		}
		a, b = max(0, min(1, a)), max(0, min(1, b))
		n := p.n
		front := n.z >= 0
		if !front {
			n = n.mul(-1)
		}
		best = mascotSample{alpha: alpha, depth: depth, color: f.panelColor(p, a, b, edge, n, front)}
	}
	if m := f.mouseSample(x, y); m.alpha > 0 && (best.alpha == 0 || m.depth > best.depth) {
		return m
	}
	return best
}

// shadowSample is the contact shadow, always behind every part.
func (f *mascotFrame) shadowSample(x, y float64) mascotSample {
	l := &f.lap
	if !l.on {
		return mascotSample{}
	}
	s := l.shadow
	d := hypot((x-s[0])/s[2], (y-s[1])/s[3])
	if d >= 1 {
		return mascotSample{}
	}
	return mascotSample{alpha: l.shade * (1 - smooth(.15, 1, d)), depth: -1e3}
}

var (
	mascotAluminum = vec3{.085, .09, .105} // Space gray.
	mascotLidGray  = vec3{.105, .11, .122}
	mascotKeyCap   = vec3{.018, .02, .025}
)

func linearHue(h float64) vec3 {
	c := edgeColor(h)
	return vec3{srgbToLinear(c[0]), srgbToLinear(c[1]), srgbToLinear(c[2])}
}

// metal is anodized aluminum: a tight highlight, a soft studio reflection
// of the sky above and floor below, the banner's RGB rim light, and a
// polished chamfer that catches light along every edge.
func (f *mascotFrame) metal(n, albedo vec3, edge float64) vec3 {
	c := f.shade(n, albedo, 1.2, .95)
	ry := 2 * n.z * n.y // Reflected view direction, y component.
	env := vec3{.025, .025, .03}.mix(vec3{.62, .66, .76}, smooth(-.3, .9, ry))
	c = c.add(env.mul(.13))
	c = c.mix(f.rimColor(n).mul(1.1), fresnel(n)*.3)
	chamfer := smooth(-.6, -.12, edge) * (1 - smooth(-.12, .2, edge))
	return c.add(vec3{.75, .78, .86}.mul(chamfer * (.3 + .35*max(0, n.y))))
}

func (f *mascotFrame) panelColor(p *mascotPanel, a, b, edge float64, n vec3, front bool) vec3 {
	switch p.kind {
	case panelEdge:
		return f.metal(n, mascotAluminum.mul(.75), edge)
	case panelLidEdge:
		return f.metal(n, mascotAluminum, edge).mul(1.15)
	case panelDeck:
		return f.deckColor(p, a, b, edge, n)
	case panelLidBack:
		return f.lidColor(p, a, b, edge, n)
	}
	// The screen side is only seen while the lid closes: black glass with
	// a thin bezel and its own light.
	glass := vec3{.008, .01, .02}.add(f.lap.glow.mul(.7))
	return glass.add(vec3{1, 1, 1}.mul(f.shade(n, vec3{}, 1.4, 1).x))
}

// lidColor is the outer lid. Its Atlas sky logo tells the laptop state: a
// sweep while a page loads, a breathing pulse while thinking, a calm blue
// while waiting for the user and amber after a failure.
func (f *mascotFrame) lidColor(p *mascotPanel, a, b, edge float64, n vec3) vec3 {
	sc := f.pose.Screen
	// Bead-blasted space gray: a broad soft falloff from the lit upper
	// corner and a faint diagonal sheen, never a mirror.
	c := f.metal(n, mascotLidGray, edge)
	falloff := .62 + .55*smooth(-.2, 1.2, .55*b+.45*(1-a))
	c = c.mul(falloff)
	sheen := 1 - smooth(0, .22, math.Abs((1-a)*.7+b*.5-.78))
	c = c.add(vec3{.05, .052, .058}.mul(sheen))
	// The logo is a dark, glossy Atlas halo ring; a state lights it from
	// within: a sweep while a page loads, a pulse while thinking, calm blue
	// while waiting for the user and amber after a failure.
	lx, ly := (a-.5)*p.sizeA, (b-.52)*p.sizeB
	radius := min(2.3, p.sizeB*.2, p.sizeA*.16)
	ring := math.Abs(hypot(lx, ly)-radius) - radius*.2
	arc := ring
	cover := max(0, min(1, .5-arc*f.scale))
	if cover <= 0 {
		return c
	}
	u := math.Atan2(ly, lx)/(2*math.Pi) + .5 // Around the ring.
	logo := vec3{.012, .013, .016}
	// Glossy inlay: a highlight along its upper edge.
	logo = logo.add(vec3{.4, .42, .48}.mul((1 - smooth(0, radius*.25, math.Abs(hypot(lx, ly)-radius*1.12))) * .35))
	light := .18*(1+math.Sin(sc.Time*4.5))*sc.Dots + .5*sc.Dialog + f.pose.Warn
	if sc.Load > 0 {
		light += 1.4 * max(0, 1-math.Abs((1-u)-sc.Load*1.2+.1)*5)
	}
	if sc.Scan > 0 {
		light += .8 * sc.Scan * max(0, 1-math.Abs((1-u)-math.Mod(sc.Time*.9, 1))*4)
	}
	hue := linearHue(u - f.pose.Flow*.35)
	hue = hue.mix(vec3{.35, .55, 1}, sc.Dialog*.7)
	hue = hue.mix(vec3{.95, .45, .08}, f.pose.Warn)
	// A whisper of the sky colors even at rest.
	logo = logo.add(hue.mul(.06 + .9*light))
	return c.mix(logo, cover)
}

func (f *mascotFrame) deckColor(p *mascotPanel, a, b, edge float64, n vec3) vec3 {
	c := f.metal(n, mascotAluminum, edge)
	x, y := a*p.sizeA, b*p.sizeB
	// The keyboard sits in a slightly darker well.
	w0, w1 := mascotKeyA0*p.sizeA, mascotKeyA1*p.sizeA
	h0, h1 := mascotKeyB0*p.sizeB, mascotKeyB1*p.sizeB
	well := roundRect(x-(w0+w1)/2, y-(h0+h1)/2, (w1-w0)/2+.35, (h1-h0)/2+.35, .6)
	if well > 0 {
		return c
	}
	c = c.mul(.72)
	cellW, cellH := (w1-w0)/mascotKeyCols, (h1-h0)/mascotKeyRows
	col, row := math.Floor((x-w0)/cellW), math.Floor((y-h0)/cellH)
	if col < 0 || col >= mascotKeyCols || row < 0 || row >= mascotKeyRows {
		return c
	}
	kx, ky := x-w0-(col+.5)*cellW, y-h0-(row+.5)*cellH
	gap := min(.16, cellW*.1, cellH*.12)
	key := roundRect(kx, ky, cellW/2-gap, cellH/2-gap, min(.22, cellW*.15, cellH*.3))
	lit := 0.0
	for h := range 2 {
		if hc, hr := mascotKeyIndex(f.pose.Hand[h]); hc == int(col) && hr == int(row) {
			lit = max(lit, f.pose.Press[h])
		}
	}
	color := linearHue(col/mascotKeyCols - f.pose.Flow*.3)
	// Backlight leaks through the gaps; a pressed key flares around itself.
	backlight := vec3{.035, .05, .09}.add(color.mul(.5 * lit))
	c = c.add(backlight.mul(1 - smooth(-.1, .35, -key)))
	cover := max(0, min(1, .5-key*f.scale))
	if cover <= 0 {
		return c
	}
	// Keycaps: matte black with a soft top highlight and their legend glow.
	cap := f.shade(n, mascotKeyCap, .15, .9).add(vec3{.025, .028, .034}.mul(smooth(-cellH/2, cellH/2, ky)))
	cap = cap.mix(color.mul(1.7), lit)
	return c.mix(cap, cover)
}
