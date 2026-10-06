package activity

import "math"

// mascotMouseShape is a low, rounded mouse: an ellipsoid aligned with the
// laptop, with a glossy shell, a button seam, a glowing scroll wheel and an
// RGB underglow that flares on confirmed clicks.
type mascotMouseShape struct {
	on     bool
	center vec3
	axes   mat3 // Rows are the width, up and length directions.
	radii  vec3
	press  float64
	click  float64
	minX   float64
	maxX   float64
	minY   float64
	maxY   float64
}

func (f *mascotFrame) buildMouse(k float64) {
	l := &f.lap
	m := &f.mouse
	*m = mascotMouseShape{on: true, radii: vec3{1.75, .8, 2.6}.mul(k)}
	m.axes = mat3{l.ax, l.ay, l.az}
	m.click = f.pose.Screen.Ripple
	m.press = f.pose.Press[1] * smooth(1, 1.05, f.pose.Hand[1][0])
	// A click dips the shell under the finger.
	at := f.pose.MouseAt
	if at == [2]float64{} {
		at = mascotMouse
	}
	m.center = l.deck(at[0], at[1], m.radii.y*.55-l.t-.12*m.press)
	r := max(m.radii.x, m.radii.y, m.radii.z) + 1
	m.minX, m.maxX, m.minY, m.maxY = m.center.x-r, m.center.x+r, m.center.y-r, m.center.y+r
}

func (f *mascotFrame) mouseSample(x, y float64) mascotSample {
	m := &f.mouse
	if !m.on || x < m.minX || x > m.maxX || y < m.minY || y > m.maxY {
		return mascotSample{}
	}
	// Ray along -z in the unit-sphere space of the ellipsoid.
	rel := vec3{x, y, 60}.sub(m.center)
	o := m.axes.apply(rel)
	o = vec3{o.x / m.radii.x, o.y / m.radii.y, o.z / m.radii.z}
	d := m.axes.apply(vec3{0, 0, -1})
	d = vec3{d.x / m.radii.x, d.y / m.radii.y, d.z / m.radii.z}
	dl := d.length()
	dn := d.mul(1 / dl)
	along := -o.dot(dn)
	closest := o.add(dn.mul(along))
	h := closest.length()
	// Silhouette distance in logical pixels, scaled by the visible radius.
	reach := (m.radii.x + m.radii.z) / 2
	alpha := max(0, min(1, .5-(h-1)*reach*f.scale))
	if alpha <= 0 {
		return mascotSample{}
	}
	p := closest
	if h < 1 {
		p = o.add(dn.mul(along - math.Sqrt(1-h*h)))
	} else {
		p = p.mul(1 / h)
	}
	// World normal from the ellipsoid gradient.
	g := vec3{p.x / m.radii.x, p.y / m.radii.y, p.z / m.radii.z}
	t := m.axes.transpose()
	n := t.apply(g).unit()
	if n.z < 0 {
		n = n.mul(-1)
	}
	world := m.center.add(t.apply(vec3{p.x * m.radii.x, p.y * m.radii.y, p.z * m.radii.z}))
	return mascotSample{alpha: alpha, depth: world.z, color: f.mouseColor(p, n)}
}

// mouseColor shades the shell. p is on the unit sphere: x across, y up and
// z toward Atlas, so the buttons are at negative z.
func (f *mascotFrame) mouseColor(p, n vec3) vec3 {
	m := &f.mouse
	c := f.shade(n, vec3{.012, .013, .016}, 1.6, 1)
	// A sharp studio reflection sells the glossy shell.
	c = c.add(vec3{.85, .88, .95}.mul(smooth(.86, .97, n.y*.62+n.z*.5) * .5))
	// The RGB light wraps the silhouette like the banner rim.
	rim := linearHue(math.Atan2(p.z, p.x)/(2*math.Pi) + f.pose.Flow*.35)
	c = c.add(rim.mul(fresnel(n) * (.9 + 1.2*m.click)))
	px := 1 / max(.5, f.scale)
	front := smooth(.05, -.25, p.z)
	// Button seam down the front half.
	if seam := math.Abs(p.x) * m.radii.x; seam < .12+px*.5 && p.y > 0 {
		c = c.mul(1 - .7*front*(1-smooth(.06, .12+px*.5, seam)))
	}
	// The pressed button lights from inside.
	c = c.add(vec3{.25, .45, 1}.mul(m.click * front * .45 * smooth(-.2, .6, p.y)))
	// Scroll wheel: a small glowing capsule at the front of the seam.
	wx, wz := p.x*m.radii.x, (p.z+.45)*m.radii.z
	if wheel := hypot(wx, max(0, math.Abs(wz)-.35)) - .2; p.y > .2 {
		cover := max(0, min(1, .5-wheel*f.scale))
		hue := linearHue(f.pose.Flow*.5 + wz*.2)
		c = c.mix(hue.mul(.9+.8*m.click), cover)
	}
	// RGB underglow around the base, brighter on clicks.
	if p.y < -.05 {
		c = c.add(rim.mul(smooth(-.05, -.45, p.y) * (.6 + .9*m.click)))
	}
	return c
}
