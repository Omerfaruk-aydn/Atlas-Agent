package activity

import (
	"errors"
	"fmt"
	"math"
	"time"
)

type vec3 struct{ x, y, z float64 }

func (a vec3) add(b vec3) vec3            { return vec3{a.x + b.x, a.y + b.y, a.z + b.z} }
func (a vec3) sub(b vec3) vec3            { return vec3{a.x - b.x, a.y - b.y, a.z - b.z} }
func (a vec3) mul(k float64) vec3         { return vec3{a.x * k, a.y * k, a.z * k} }
func (a vec3) dot(b vec3) float64         { return a.x*b.x + a.y*b.y + a.z*b.z }
func (a vec3) length() float64            { return math.Sqrt(a.dot(a)) }
func (a vec3) unit() vec3                 { return a.mul(1 / max(1e-12, a.length())) }
func (a vec3) scaled(b vec3) vec3         { return vec3{a.x * b.x, a.y * b.y, a.z * b.z} }
func (a vec3) mix(b vec3, t float64) vec3 { return a.add(b.sub(a).mul(t)) }

type mat3 [3]vec3

func (m mat3) apply(v vec3) vec3 { return vec3{m[0].dot(v), m[1].dot(v), m[2].dot(v)} }
func (m mat3) transpose() mat3 {
	return mat3{{m[0].x, m[1].x, m[2].x}, {m[0].y, m[1].y, m[2].y}, {m[0].z, m[1].z, m[2].z}}
}

func (m mat3) times(n mat3) mat3 {
	t := n.transpose()
	var r mat3
	for i := range r {
		r[i] = vec3{m[i].dot(t[0]), m[i].dot(t[1]), m[i].dot(t[2])}
	}
	return r
}

// Rotation applies yaw (face turns right), then pitch (face turns up), then
// roll (counterclockwise on screen) in a y-up, camera-facing +z frame.
func mascotRotation(yaw, pitch, roll float64) mat3 {
	cy, sy, cp, sp, cr, sr := math.Cos(yaw), math.Sin(yaw), math.Cos(pitch), math.Sin(pitch), math.Cos(roll), math.Sin(roll)
	ry := mat3{{cy, 0, sy}, {0, 1, 0}, {-sy, 0, cy}}
	rx := mat3{{1, 0, 0}, {0, cp, sp}, {0, -sp, cp}}
	rz := mat3{{cr, -sr, 0}, {sr, cr, 0}, {0, 0, 1}}
	return rz.times(rx).times(ry)
}

type mascotAccessory uint8

const (
	accessoryHalo mascotAccessory = iota
	accessoryRing
	accessoryAntenna
)

// mascotScene is the character construction. The shipped design is
// atlasTitan; previews compare alternative constructions with the same
// renderer so the comparison is honest about quality at banner scale.
type mascotScene struct {
	size      vec3    // Body radii in logical pixels for the unit capsule.
	half      float64 // Half length of the capsule core in unit space.
	feet      float64 // Logical y of the resting contact point in the slot.
	eyeY      float64 // Eye height in unit space above the feet.
	eyeSpread float64 // Eye azimuth from the face center in radians.
	eyeW      float64
	eyeH      float64
	albedo    vec3 // Linear body color.
	accessory mascotAccessory
	tube      float64 // Accessory tube radius in logical pixels.
	desk      *mascotDesk
	mouthY    float64 // Mouth height in unit space; zero draws no mouth.
}

var atlasTitan = mascotScene{
	size: vec3{8.8, 6.5, 7.8}, half: .3, feet: 32.4,
	eyeY: 1.5, eyeSpread: .37, eyeW: 2.9, eyeH: 5.6,
	albedo: vec3{.80, .83, .87}, accessory: accessoryHalo, tube: .55,
	desk: &mascotDeskFront, mouthY: .84,
}

// The laptop sits in front of Atlas, seen from behind at three quarters:
// a tall, thin lid with the keyboard showing beside it.
var mascotDeskFront = mascotDesk{
	pos: vec3{-2, -1.2, 22}, yaw: -.25, tilt: .48,
	w: 15.5, d: 9, h: 8.4, t: .8, open: 1.68,
}

// errMascotUnavailable reports that the caller must show its static fallback.
var errMascotUnavailable = errors.New("mascot render unavailable")

type mascotSegment struct {
	a, b       vec3
	radius     float64
	skin       bool // Body material instead of light.
	glow       bool // A halo segment with a soft bloom around it.
	u0, u1     float64
	minX, maxX float64
	minY, maxY float64
}

type mascotFrame struct {
	scene         *mascotScene
	pose          mascotPose
	scale         float64
	rot, inv      mat3 // Local to world and world to local (without translation).
	normal        mat3 // Normal matrix for unit-space normals.
	origin        vec3
	rayLocal      vec3
	localPerPixel float64
	segA, segB    vec3
	tubes         []mascotSegment
	beads         []mascotBead
	panels        []mascotPanel
	lap           mascotLaptop
	mouse         mascotMouseShape
	seed          uint32
	bodyMin       [2]float64
	bodyMax       [2]float64
	key, fill     vec3
	half          vec3
}

func srgbToLinear(c byte) float64 { v := float64(c) / 255; return v * v * (.3 + .7*v) }

// Smooth step without branches beyond the clamp.
func smooth(e0, e1, x float64) float64 {
	t := max(0, min(1, (x-e0)/(e1-e0)))
	return t * t * (3 - 2*t)
}

// setup prepares a frame, reusing its part buffers between frames.
func (f *mascotFrame) setup(scene *mascotScene, pose mascotPose, scale float64) {
	*f = mascotFrame{scene: scene, pose: pose, scale: scale, tubes: f.tubes[:0], beads: f.beads[:0], panels: f.panels[:0], seed: 0x9e37}
	stretch := 1 + max(-.4, min(.4, pose.Squash))
	d := vec3{1 / math.Sqrt(stretch), stretch, 1 / math.Sqrt(stretch)}
	size := scene.size.scaled(d)
	r := mascotRotation(pose.Yaw, pose.Pitch, pose.Roll)
	diag := func(v vec3) mat3 { return mat3{{v.x, 0, 0}, {0, v.y, 0}, {0, 0, v.z}} }
	f.rot = r.times(diag(size))
	f.inv = diag(vec3{1 / size.x, 1 / size.y, 1 / size.z}).times(r.transpose())
	f.normal = r.times(diag(vec3{1 / size.x, 1 / size.y, 1 / size.z}))
	// Atlas sits up behind the laptop so the hands never cover the mouth.
	f.origin = vec3{pose.BodyX, pose.Lift + mascotDeskRise*min(1, pose.Laptop), 0}
	f.rayLocal = f.inv.apply(vec3{0, 0, -1}).unit()
	f.segA, f.segB = vec3{0, 1, 0}, vec3{0, 1 + 2*scene.half, 0}
	// Local units per world pixel, measured across the view direction.
	perp := func(v vec3) float64 { v = f.inv.apply(v); return v.sub(f.rayLocal.mul(v.dot(f.rayLocal))).length() }
	f.localPerPixel = (perp(vec3{1, 0, 0}) + perp(vec3{0, 1, 0})) / 2
	extent := max(size.x, size.y*(1+scene.half), size.z) * 1.5
	center := f.origin.add(r.apply(vec3{0, size.y * (1 + scene.half), 0}))
	f.bodyMin = [2]float64{center.x - extent, center.y - extent}
	f.bodyMax = [2]float64{center.x + extent, center.y + extent}
	f.key, f.fill = vec3{-.48, .62, .62}.unit(), vec3{.75, -.05, .66}.unit()
	f.half = f.key.add(vec3{0, 0, 1}).unit()
	f.buildLaptop()
	f.buildAccessory(size, r)
}

// The accessory follows the body frame: rotation and squash move its
// anchors, but tubes keep a uniform thickness.
func (f *mascotFrame) buildAccessory(size vec3, r mat3) {
	s := f.scene
	world := func(p vec3) vec3 { return f.origin.add(r.apply(p)) }
	top := size.y * (2 + 2*s.half)
	addChain := func(points []vec3, radius float64, skin bool) {
		total := 0.0
		for i := 1; i < len(points); i++ {
			total += points[i].sub(points[i-1]).length()
		}
		run := 0.0
		for i := 1; i < len(points); i++ {
			a, b := world(points[i-1]), world(points[i])
			step := points[i].sub(points[i-1]).length()
			pad := radius + 1
			f.tubes = append(f.tubes, mascotSegment{
				a: a, b: b, radius: radius, skin: skin, u0: run / total, u1: (run + step) / total,
				minX: min(a.x, b.x) - pad, maxX: max(a.x, b.x) + pad, minY: min(a.y, b.y) - pad, maxY: max(a.y, b.y) + pad,
			})
			run += step
		}
	}
	switch s.accessory {
	case accessoryHalo:
		// A thin ring of light floats above Atlas. It is a real 3D ring in
		// the body frame, so turning and leaning foreshorten it; ArcLift
		// raises it, ArcTilt rocks it and its colors flow like the rim.
		lift := f.pose.ArcLift
		bob := .22 * math.Sin(f.pose.Flow*5)
		center := vec3{0, top + 2.8 + lift*.32 + bob, -.2}
		rx, rz := size.x*.66, size.z*.6
		ct, st := math.Cos(f.pose.ArcTilt), math.Sin(f.pose.ArcTilt)
		var ring [33]vec3
		for i := range ring {
			a := 2 * math.Pi * float64(i) / float64(len(ring)-1)
			x, z := rx*math.Cos(a), rz*math.Sin(a)
			// Seen a little from above: the back of the ring sits higher.
			y := -z * .36
			ring[i] = center.add(vec3{x*ct - y*st, x*st + y*ct, z})
		}
		f.addHalo(ring[:], world, s.tube)
		if f.pose.Browser {
			// A small orb circles the halo while Atlas drives the browser.
			a := f.pose.Flow*2.2 + math.Pi/2
			x, z := rx*math.Cos(a), rz*math.Sin(a)
			y := -z * .36
			orb := center.add(vec3{x*ct - y*st, x*st + y*ct, z})
			f.beads = append(f.beads, mascotBead{center: world(orb), radius: 1.3})
		}
		// Arms rest at the sides; gestures lift them, typing brings the
		// hands to the keyboard.
		restY := size.y * .9
		hands := [2]vec3{
			{-(size.x + .9), restY + 2.2*f.pose.Arm[0], .8},
			{size.x + .9, restY + 2.2*f.pose.Arm[1], .8},
		}
		radius := 1.75
		if on := f.pose.Hands * min(1, f.pose.Laptop); f.lap.on && on > 0 {
			rt := r.transpose()
			for i := range hands {
				onDeck := rt.apply(f.lap.hand(f.pose.Hand[i], f.pose.Press[i]).sub(f.origin))
				onDeck.y += f.pose.Arm[i]
				hands[i] = hands[i].mix(onDeck, on)
			}
			radius = 1.75 - .2*on
		}
		for side, hand := range hands {
			sign := float64(side*2 - 1)
			shoulder := vec3{sign * size.x * .86, size.y * (1 + 2*s.half) * (.86 - .12*f.pose.Hands), .4}
			// The elbow bows outward so the arm reads as a soft limb.
			elbow := shoulder.mix(hand, .5).add(vec3{sign * (.9 + 1.5*f.pose.Hands), -.2, .4})
			addChain([]vec3{shoulder, elbow, hand}, 1.6-.15*f.pose.Hands, true)
			f.beads = append(f.beads, mascotBead{center: world(hand), radius: radius, skin: true})
		}
	case accessoryRing:
		var ring []vec3
		for i := 0; i <= 28; i++ {
			a := 2 * math.Pi * float64(i) / 28
			ring = append(ring, vec3{size.x * 1.32 * math.Cos(a), size.y*(1+s.half) + 1.6*math.Sin(a+.4), size.z * 1.32 * math.Sin(a)})
		}
		addChain(ring, s.tube, false)
	case accessoryAntenna:
		points := []vec3{{0, top - .8, 0}, {.6, top + 2.2, 0}, {1.6, top + 4 + .4*f.pose.ArcLift, 0}}
		addChain(points, s.tube, false)
		f.beads = append(f.beads, mascotBead{center: world(points[2]), radius: 1.75})
	}
}

type mascotBead struct {
	center vec3
	radius float64
	skin   bool
	albedo vec3 // A prop material when non-zero.
	glow   vec3
}

type mascotSample struct {
	color vec3 // Linear, not premultiplied.
	alpha float64
	depth float64
}

// lineSegment returns the closest approach between a unit-direction line
// and a segment: the segment parameter, the line parameter and the distance.
func lineSegment(o, d, a, b vec3) (float64, float64, float64) {
	u, w := b.sub(a), o.sub(a)
	bd, c, dd, e := d.dot(u), u.dot(u), d.dot(w), u.dot(w)
	s := 0.0
	if den := c - bd*bd; den > 1e-12 {
		s = max(0, min(1, (e-dd*bd)/den))
	}
	t := s*bd - dd
	return s, t, w.add(d.mul(t)).sub(u.mul(s)).length()
}

// capsuleHit intersects a unit-radius capsule along a unit direction.
func capsuleHit(o, d, a, b vec3) (float64, bool) {
	sphere := func(c vec3) (float64, bool) {
		oc := o.sub(c)
		k := d.dot(oc)
		h := k*k - oc.dot(oc) + 1
		if h < 0 {
			return 0, false
		}
		return -k - math.Sqrt(h), true
	}
	ba, oa := b.sub(a), o.sub(a)
	baba := ba.dot(ba)
	if baba < 1e-9 {
		return sphere(a)
	}
	bard, baoa, rdoa, oaoa := ba.dot(d), ba.dot(oa), d.dot(oa), oa.dot(oa)
	qa, qb, qc := baba-bard*bard, baba*rdoa-baoa*bard, baba*oaoa-baoa*baoa-baba
	h := qb*qb - qa*qc
	if h < 0 || qa < 1e-12 {
		return 0, false
	}
	t := (-qb - math.Sqrt(h)) / qa
	if y := baoa + t*bard; y > 0 && y < baba {
		return t, true
	} else if y <= 0 {
		return sphere(a)
	}
	return sphere(b)
}

func (f *mascotFrame) shade(n vec3, albedo vec3, gloss, ao float64) vec3 {
	sky, ground := vec3{.36, .39, .47}, vec3{.10, .10, .13}
	ambient := ground.mix(sky, n.y*.5+.5).mul(ao)
	diffuse := max(0, (n.dot(f.key)+.25)/1.25)
	fill := max(0, n.dot(f.fill)) * .32
	c := albedo.scaled(ambient.add(vec3{1, .97, .93}.mul(diffuse * 1.05 * ao)).add(vec3{.55, .63, .8}.mul(fill)))
	h := max(0, n.dot(f.half))
	h2 := h * h
	h4 := h2 * h2
	h32 := h4 * h4
	h32 *= h32
	spec := h32 * h4 * gloss
	return c.add(vec3{1, 1, 1}.mul(spec))
}

// hypot skips math.Hypot's overflow handling; inputs are a few pixels.
func hypot(x, y float64) float64 { return math.Sqrt(x*x + y*y) }

// fresnel approximates (1-n.z)^2.5 without a per-pixel power call.
func fresnel(n vec3) float64 {
	t := 1 - max(0, n.z)
	return t * t * math.Sqrt(t)
}

func (f *mascotFrame) rimColor(n vec3) vec3 {
	hue := math.Atan2(n.y, n.x)/(2*math.Pi) + f.pose.Flow*.18
	c := edgeColor(hue)
	return vec3{srgbToLinear(c[0]), srgbToLinear(c[1]), srgbToLinear(c[2])}
}

func (f *mascotFrame) body(x, y float64) mascotSample {
	if x < f.bodyMin[0] || x > f.bodyMax[0] || y < f.bodyMin[1] || y > f.bodyMax[1] {
		return mascotSample{}
	}
	s := f.scene
	o := f.inv.apply(vec3{x, y, 60}.sub(f.origin))
	_, t, dist := lineSegment(o, f.rayLocal, f.segA, f.segB)
	signed := (dist - 1) / f.localPerPixel
	alpha := max(0, min(1, .5-signed*f.scale))
	if alpha <= 0 {
		return mascotSample{}
	}
	// Front surface hit, or the silhouette point for the antialiased rim.
	p := o.add(f.rayLocal.mul(t))
	if hit, ok := capsuleHit(o, f.rayLocal, f.segA, f.segB); ok && dist < 1 {
		p = o.add(f.rayLocal.mul(hit))
	}
	core := f.segA.add(f.segB.sub(f.segA).mul(max(0, min(1, (p.y-f.segA.y)/max(1e-9, f.segB.y-f.segA.y)))))
	local := p.sub(core).unit()
	p = core.add(local)
	n := f.normal.apply(local).unit()
	ao := .62 + .38*smooth(.05, 1.15, p.y)
	color := f.shade(n, s.albedo, .3, ao)
	// Thin RGB fresnel picked up from the banner rim.
	color = color.mix(f.rimColor(n).mul(1.25), fresnel(n)*.42)
	if f.lap.on {
		color = color.add(s.albedo.scaled(f.lap.glow).mul(max(0, n.dot(f.lap.glowDir))))
	}
	color = f.face(color, p, local, n)
	color = f.eyes(color, p, local, n)
	world := f.origin.add(f.rot.apply(p))
	return mascotSample{color: color, alpha: alpha, depth: world.z}
}

// eyes are decals in the body's own surface coordinates, so turning the
// head foreshortens the far eye and gaze slides them across the surface.
func (f *mascotFrame) eyes(color, p, local, n vec3) vec3 {
	s := f.scene
	stretch := 1 + max(-.4, min(.4, f.pose.Squash))
	sx, sy := s.size.x/math.Sqrt(stretch), s.size.y*stretch
	azimuth := math.Atan2(local.x, local.z)
	if math.Abs(azimuth) > 1.5 {
		return color
	}
	view := max(.3, n.z)
	for side := range 2 {
		sign := float64(side*2 - 1)
		ex := (azimuth - (f.pose.GazeX + sign*s.eyeSpread)) * sx
		ey := (p.y-s.eyeY)*sy - f.pose.GazeY
		d, glint := mascotEye(ex, ey, s.eyeW, s.eyeH, f.pose.Open[side], f.pose.Slant, f.pose.Smile, sign)
		cover := max(0, min(1, .5-d*f.scale*view))
		if cover <= 0 {
			continue
		}
		// Glossy dark eye with a faint RGB floor reflection and a glint.
		eye := vec3{.010, .013, .02}
		floor := smooth(.1, -.9, ey/max(.6, s.eyeH*f.pose.Open[side]/2))
		// Both eyes catch the same cool floor light, so the face stays even.
		eye = eye.mix(vec3{.12, .2, .45}.mul(.5), floor*min(.5, f.scale*.2)*(1-f.pose.Smile))
		if f.scale >= 1.25 && glint < 0 && f.pose.Smile < .4 && f.pose.Open[side] > .55 {
			eye = eye.mix(vec3{.95, .97, 1}, max(0, min(1, .5-glint*f.scale))*.9)
		}
		color = color.mix(eye, cover)
	}
	return color
}

// face draws the mouth and blush as surface decals below the eyes.
func (f *mascotFrame) face(color, p, local, n vec3) vec3 {
	s := f.scene
	if s.mouthY == 0 {
		return color
	}
	azimuth := math.Atan2(local.x, local.z)
	if math.Abs(azimuth) > 1.4 {
		return color
	}
	stretch := 1 + max(-.4, min(.4, f.pose.Squash))
	sx, sy := s.size.x/math.Sqrt(stretch), s.size.y*stretch
	view := max(.3, n.z)
	po := f.pose
	if po.Blush > .01 {
		for _, sign := range [2]float64{-1, 1} {
			bx := (azimuth - (po.GazeX + sign*(s.eyeSpread+.2))) * sx
			by := (p.y-s.eyeY)*sy + s.eyeH*.62 - po.GazeY
			blob := 1 - smooth(.2, 1.6, hypot(bx*.75, by*1.3))
			color = color.mix(vec3{.95, .32, .42}, blob*po.Blush*.5)
		}
	}
	// The mouth follows the gaze a little, like the eyes.
	x := (azimuth - po.GazeX*.6) * sx
	y := (p.y-s.mouthY)*sy - po.GazeY*.35
	d, inner := mascotMouth(x, y, po.MouthOpen, po.MouthCurve, po.MouthWide, po.MouthSkew, po.MouthWave)
	cover := max(0, min(1, .5-d*f.scale*view))
	if cover <= 0 {
		return color
	}
	mouth := vec3{.02, .015, .025}
	if po.MouthOpen > .25 {
		// A warm hint inside an open mouth.
		mouth = mouth.mix(vec3{.55, .12, .16}, smooth(.2, -.6, inner)*min(1, (po.MouthOpen-.25)*2.5))
	}
	return color.mix(mouth, cover)
}

// mascotMouth is a signed distance in logical pixels for the mouth. curve
// bends it into a smile or a frown, open drops the lower lip, skew lifts
// one corner and wave makes it wobble; at rest it is a closed line.
func mascotMouth(x, y, open, curve, wide, skew, wave float64) (float64, float64) {
	hw := 1.5 + .7*max(-.6, min(1, wide)) + .4*open
	u := max(-1, min(1, x/hw))
	center := curve*.9*(u*u-.4) + skew*.45*u + wave*.32*math.Sin(u*math.Pi*2.5)
	const thick = .36
	upper := center + thick
	lower := center - thick - open*2.1*(1-u*u)*(.7+.3*max(0, curve))
	dy := max(y-upper, lower-y)
	dx := math.Abs(x) - hw
	d := hypot(max(dx, 0), max(dy, 0)) + min(max(dx, dy), 0)
	// Inner is negative toward the lower lip, for the open mouth tint.
	inner := (y - lower) - (upper-lower)*.45
	return d, inner
}

// mascotEye is a 2D signed distance (logical pixels, y up) for one eye and
// for its specular glint. Lids, slant and smile shape the eye but always
// leave a visible line, so an eye never disappears mid-transition.
func mascotEye(x, y, w, h, open, slant, smile, side float64) (float64, float64) {
	hw := w / 2
	hh := max(.6, h*open/2)
	r := min(hw, hh)
	qx, qy := math.Abs(x)-(hw-r), math.Abs(y)-(hh-r)
	d := hypot(max(qx, 0), max(qy, 0)) + min(max(qx, qy), 0) - r
	if slant != 0 {
		inner := -x * side / hw
		lid := hh*1.08 - math.Abs(slant)*hw*(1-math.Copysign(1, slant)*inner)*1.15
		d = max(d, (y-lid)/math.Sqrt(1+slant*slant))
	}
	if smile > 0 {
		radius := hw*1.5 + hh
		cy := -hh - radius + smile*1.3*hh
		d = max(d, radius-hypot(x, y-cy))
	}
	glint := hypot(x+hw*.28*side, y-hh*.45) - max(.35, w*.17)
	return d, glint
}

func (f *mascotFrame) tube(x, y float64) mascotSample {
	if len(f.tubes) == 0 {
		return mascotSample{}
	}
	s := f.scene
	best, bestS := math.Inf(1), 0.0
	var seg *mascotSegment
	// Pick the segment whose surface is nearest, then its front depth.
	for i := range f.tubes {
		t := &f.tubes[i]
		if x < t.minX || x > t.maxX || y < t.minY || y > t.maxY {
			continue
		}
		ax, ay, bx, by := t.a.x, t.a.y, t.b.x, t.b.y
		dx, dy := bx-ax, by-ay
		k := max(0, min(1, ((x-ax)*dx+(y-ay)*dy)/max(1e-12, dx*dx+dy*dy)))
		if d := hypot(x-ax-k*dx, y-ay-k*dy) - t.radius; d < best {
			best, bestS, seg = d, k, t
		}
	}
	if seg == nil {
		return mascotSample{}
	}
	alpha := max(0, min(1, .5-best*f.scale))
	if alpha <= 0 {
		if !seg.glow || best > mascotBloom {
			return mascotSample{}
		}
		// Soft bloom: emissive light fading out around the halo.
		u := seg.u0 + (seg.u1-seg.u0)*bestS
		fall := 1 - best/mascotBloom
		light := f.haloColor(u)
		depth := seg.a.z + (seg.b.z-seg.a.z)*bestS - .01
		return mascotSample{color: light.mul(1.25), alpha: .42 * fall * fall * (.45 + .55*f.pose.Glow), depth: depth}
	}
	cx := seg.a.x + (seg.b.x-seg.a.x)*bestS
	cy := seg.a.y + (seg.b.y-seg.a.y)*bestS
	r := min(best+seg.radius, seg.radius)
	nz := math.Sqrt(max(0, seg.radius*seg.radius-r*r))
	n := vec3{x - cx, y - cy, nz}.unit()
	depth := seg.a.z + (seg.b.z-seg.a.z)*bestS + nz
	if seg.skin {
		color := f.shade(n, s.albedo, .3, .92)
		color = color.mix(f.rimColor(n).mul(1.25), fresnel(n)*.42)
		return mascotSample{color: color, alpha: alpha, depth: depth}
	}
	u := seg.u0 + (seg.u1-seg.u0)*bestS
	albedo := f.haloColor(u)
	glow := .35 + .65*f.pose.Glow
	// A white-hot core makes the thin ring read as light, not plastic.
	core := 1 - smooth(0, seg.radius, r)
	color := f.shade(n, albedo, .55, 1).mul(.4).add(albedo.mul(glow * .9))
	color = color.mix(vec3{1, 1, 1}.mul(1.1), core*.45*glow)
	return mascotSample{color: color, alpha: alpha, depth: depth}
}

// mascotBloom is how far the halo glow reaches, in logical pixels.
const mascotBloom = 2.4

// haloColor is the flowing RGB light at position u around the halo; a
// blocked run cools it to amber, a tint rather than a flash.
func (f *mascotFrame) haloColor(u float64) vec3 {
	return linearHue(u-f.pose.Flow*.35).mix(vec3{.95, .45, .08}, f.pose.Warn*.75)
}

// addHalo adds a closed glowing ring through the given body-frame points.
func (f *mascotFrame) addHalo(points []vec3, world func(vec3) vec3, radius float64) {
	pad := radius + mascotBloom + 1
	for i := 1; i < len(points); i++ {
		a, b := world(points[i-1]), world(points[i])
		f.tubes = append(f.tubes, mascotSegment{
			a: a, b: b, radius: radius, glow: true,
			u0: float64(i-1) / float64(len(points)-1), u1: float64(i) / float64(len(points)-1),
			minX: min(a.x, b.x) - pad, maxX: max(a.x, b.x) + pad, minY: min(a.y, b.y) - pad, maxY: max(a.y, b.y) + pad,
		})
	}
}

func (f *mascotFrame) beadSample(x, y float64) mascotSample {
	var best mascotSample
	for _, b := range f.beads {
		d := hypot(x-b.center.x, y-b.center.y)
		alpha := max(0, min(1, .5-(d-b.radius)*f.scale))
		if alpha <= 0 {
			continue
		}
		r := min(d, b.radius)
		n := vec3{x - b.center.x, y - b.center.y, math.Sqrt(max(0, b.radius*b.radius-r*r))}.unit()
		sample := mascotSample{alpha: alpha, depth: b.center.z + n.z*b.radius}
		switch {
		case b.skin:
			sample.color = f.shade(n, f.scene.albedo, .3, .95)
			sample.color = sample.color.mix(f.rimColor(n).mul(1.25), fresnel(n)*.42)
		case b.albedo != vec3{}:
			sample.color = f.shade(n, b.albedo, .8, 1).add(b.glow.mul(fresnel(n)*2 + .3))
		default:
			sample.color = f.shade(n, vec3{.55, .82, .96}, .8, 1)
		}
		if best.alpha == 0 || sample.depth > best.depth {
			best = sample
		}
	}
	return best
}

// render writes premultiplied RGBA for a width x height device-pixel slot
// whose logical size is MascotWidth x MascotHeight.
func (f *mascotFrame) render(pix []byte, width, height int) {
	feetX, feetY := float64(MascotWidth)/2, f.scene.feet
	sx, sy := float64(MascotWidth)/float64(width), float64(MascotHeight)/float64(height)
	// Union of every part's bounds; pixels outside it are transparent.
	lo, hi := f.bodyMin, f.bodyMax
	for _, t := range f.tubes {
		lo, hi = [2]float64{min(lo[0], t.minX), min(lo[1], t.minY)}, [2]float64{max(hi[0], t.maxX), max(hi[1], t.maxY)}
	}
	for _, b := range f.beads {
		r := b.radius + 1
		lo, hi = [2]float64{min(lo[0], b.center.x-r), min(lo[1], b.center.y-r)}, [2]float64{max(hi[0], b.center.x+r), max(hi[1], b.center.y+r)}
	}
	for _, p := range f.panels {
		lo, hi = [2]float64{min(lo[0], p.minX), min(lo[1], p.minY)}, [2]float64{max(hi[0], p.maxX), max(hi[1], p.maxY)}
	}
	if m := f.mouse; m.on {
		lo, hi = [2]float64{min(lo[0], m.minX), min(lo[1], m.minY)}, [2]float64{max(hi[0], m.maxX), max(hi[1], m.maxY)}
	}
	if f.lap.on {
		s := f.lap.shadow
		lo, hi = [2]float64{min(lo[0], s[0]-s[2]), min(lo[1], s[1]-s[3])}, [2]float64{max(hi[0], s[0]+s[2]), max(hi[1], s[1]+s[3])}
	}
	var layers [5]mascotSample
	for j := range height {
		wy := feetY - (float64(j)+.5)*sy
		for i := range width {
			wx := (float64(i)+.5)*sx - feetX
			if wx < lo[0] || wx > hi[0] || wy < lo[1] || wy > hi[1] {
				k := (j*width + i) * 4
				pix[k], pix[k+1], pix[k+2], pix[k+3] = 0, 0, 0, 0
				continue
			}
			layers = [5]mascotSample{f.shadowSample(wx, wy), f.body(wx, wy), f.tube(wx, wy), f.beadSample(wx, wy), f.panelSample(wx, wy)}
			// Composite back to front by depth (insertion sort).
			for m := 1; m < len(layers); m++ {
				for n := m; n > 0 && layers[n-1].depth > layers[n].depth; n-- {
					layers[n-1], layers[n] = layers[n], layers[n-1]
				}
			}
			var c vec3
			a := 0.0
			for _, l := range layers {
				if l.alpha <= 0 {
					continue
				}
				c = c.mul(1 - l.alpha).add(l.color.mul(l.alpha))
				a = a*(1-l.alpha) + l.alpha
			}
			k := (j*width + i) * 4
			if a <= 0 {
				pix[k], pix[k+1], pix[k+2], pix[k+3] = 0, 0, 0, 0
				continue
			}
			pix[k], pix[k+1], pix[k+2], pix[k+3] = mascotEncode(c.x, a), mascotEncode(c.y, a), mascotEncode(c.z, a), byte(math.Round(255*a))
		}
	}
}

// The display curve is tabulated once; the frame loop only indexes it.
var mascotGamma = func() (t [4097]float64) {
	for i := range t {
		v := float64(i) / 4096
		t[i] = 255 * math.Pow(v, 1/2.2)
	}
	return t
}()

// mascotEncode tone maps premultiplied linear light into premultiplied sRGB.
func mascotEncode(v, alpha float64) byte {
	v /= alpha
	v /= 1 + .18*v
	x := max(0, min(1, v*1.12)) * 4095
	i := int(x)
	return byte((mascotGamma[i]+(mascotGamma[i+1]-mascotGamma[i])*(x-float64(i)))*alpha + .5)
}

// Mascot is the single banner character shared by desktop and browser
// control. One animator turns verified events into a pose; one renderer
// draws that pose at any device scale.
type Mascot struct {
	anim  mascotAnimator
	pose  mascotPose
	scene *mascotScene
	frame mascotFrame
	// draw is replaceable so tests can prove the static fallback.
	draw func(*mascotFrame, []byte, int, int)
	// Frames records the number of successfully rendered frames.
	Frames uint64
}

// NewMascot returns the Atlas character in its resting pose.
func NewMascot() *Mascot { return &Mascot{scene: &atlasTitan} }

// Reset forgets motion state; the next frame starts from a complete pose.
func (m *Mascot) Reset() {
	if m != nil {
		m.anim.reset()
	}
}

// Advance moves the animation to now for the latest event snapshot.
func (m *Mascot) Advance(e Event, now time.Time, look Look) {
	if m != nil {
		m.pose = m.anim.step(e, now, look)
	}
}

// Render draws the current pose as premultiplied RGBA. A render failure is
// reported, never propagated, so control surfaces keep working.
func (m *Mascot) Render(pix []byte, width, height int, scale float64) (err error) {
	if m == nil || m.scene == nil || width <= 0 || height <= 0 || len(pix) < width*height*4 || !(scale > 0) || math.IsInf(scale, 0) {
		return errMascotUnavailable
	}
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("%w: %v", errMascotUnavailable, v)
		}
	}()
	m.frame.setup(m.scene, m.pose, scale)
	if m.draw != nil {
		m.draw(&m.frame, pix, width, height)
	} else {
		m.frame.render(pix, width, height)
	}
	m.Frames++
	return nil
}
