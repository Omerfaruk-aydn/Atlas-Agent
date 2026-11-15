package design

import (
	"encoding/json"
	"fmt"
	"math"
)

// Pad is a layout's padding: top, right, bottom, left. In JSON it is one
// number, two (vertical, horizontal) or four.
type Pad [4]float64

// MarshalJSON writes the four sides.
func (p Pad) MarshalJSON() ([]byte, error) { return json.Marshal([4]float64(p)) }

// UnmarshalJSON reads one, two or four numbers.
func (p *Pad) UnmarshalJSON(data []byte) error {
	var one float64
	if err := json.Unmarshal(data, &one); err == nil {
		*p = Pad{one, one, one, one}
		return nil
	}
	var many []float64
	if err := json.Unmarshal(data, &many); err != nil {
		return fmt.Errorf("pad must be a number or a list of 2 or 4 numbers")
	}
	switch len(many) {
	case 1:
		*p = Pad{many[0], many[0], many[0], many[0]}
	case 2:
		*p = Pad{many[0], many[1], many[0], many[1]}
	case 4:
		*p = Pad{many[0], many[1], many[2], many[3]}
	default:
		return fmt.Errorf("pad must be a number or a list of 2 or 4 numbers")
	}
	return nil
}

// Layout stacks a frame's children in a row or a column.
type Layout struct {
	Dir   string  `json:"dir"`
	Gap   float64 `json:"gap"`
	Pad   Pad     `json:"pad"`
	Main  string  `json:"main"`
	Cross string  `json:"cross"`
}

// Constraints say how a child follows its parent's resizing.
type Constraints struct {
	X string `json:"x"`
	Y string `json:"y"`
}

func oneOf(v string, fallback string, allowed ...string) string {
	for _, a := range allowed {
		if v == a {
			return v
		}
	}
	return fallback
}

func sanitizeLayout(l *Layout) *Layout {
	if l == nil {
		return nil
	}
	l.Dir = oneOf(l.Dir, "column", "row", "column")
	l.Gap = clamp(l.Gap, 0, limit)
	for i := range l.Pad {
		l.Pad[i] = clamp(l.Pad[i], 0, limit)
	}
	l.Main = oneOf(l.Main, "start", "start", "center", "end", "between")
	l.Cross = oneOf(l.Cross, "start", "start", "center", "end", "stretch")
	return l
}

func sanitizeConstraints(c *Constraints) *Constraints {
	if c == nil {
		return nil
	}
	c.X = oneOf(c.X, "left", "left", "right", "center", "stretch", "scale")
	c.Y = oneOf(c.Y, "top", "top", "bottom", "center", "stretch", "scale")
	if c.X == "left" && c.Y == "top" {
		return nil
	}
	return c
}

const eps = 1e-6

func differs(a, b float64) bool { return math.Abs(a-b) > eps }

func inFlow(n *Node) bool { return n.IsVisible() && !n.Absolute }

// sizeOf is a node's box as layout sees it: auto-sized text by estimate,
// everything else as stored.
func sizeOf(n *Node) (float64, float64) {
	if n.Type == TypeText && n.AutoResize != "none" {
		c := *n
		Measure(&c)
		return c.Width, c.Height
	}
	return n.Width, n.Height
}

type box struct{ x, y, w, h float64 }

func indexBoxes(nodes []*Node, into map[string]box) {
	for _, n := range nodes {
		into[n.ID] = box{n.X, n.Y, n.Width, n.Height}
		indexBoxes(n.Children, into)
	}
}

func constrainChild(c *Node, from, to box) {
	k := c.Constraints
	dw, dh := to.w-from.w, to.h-from.h
	switch k.X {
	case "right":
		c.X += dw
	case "center":
		c.X += dw / 2
	case "stretch":
		c.Width += dw
	case "scale":
		if from.w > 0 {
			c.X = c.X * to.w / from.w
			c.Width = c.Width * to.w / from.w
		}
	}
	switch k.Y {
	case "bottom":
		c.Y += dh
	case "center":
		c.Y += dh / 2
	case "stretch":
		c.Height += dh
	case "scale":
		if from.h > 0 {
			c.Y = c.Y * to.h / from.h
			c.Height = c.Height * to.h / from.h
		}
	}
	c.Width = math.Max(0, c.Width)
	c.Height = math.Max(0, c.Height)
}

// constrain applies a frame's resize from `from` to its children, and on
// into children that were stretched by it.
func constrain(n *Node, from box, prev map[string]box, done map[string]bool) {
	done[n.ID] = true
	if len(n.Children) == 0 || n.Layout != nil || (!differs(from.w, n.Width) && !differs(from.h, n.Height)) {
		return
	}
	to := box{w: n.Width, h: n.Height}
	for _, c := range n.Children {
		p, ok := prev[c.ID]
		// A child the edit also touched keeps what the edit says.
		if !ok || c.Constraints == nil || differs(p.x, c.X) || differs(p.y, c.Y) || differs(p.w, c.Width) || differs(p.h, c.Height) {
			continue
		}
		constrainChild(c, from, to)
		if len(c.Children) > 0 && (differs(p.w, c.Width) || differs(p.h, c.Height)) {
			constrain(c, p, prev, done)
		}
	}
}

func constrainTree(nodes []*Node, prev map[string]box, done map[string]bool) {
	for _, n := range nodes {
		if p, ok := prev[n.ID]; ok && !done[n.ID] && len(n.Children) > 0 && n.Layout == nil {
			constrain(n, p, prev, done)
		}
		constrainTree(n.Children, prev, done)
	}
}

// given forces a size on a node: a fill child takes its share from the
// parent's layout.
type given struct {
	w, h   float64
	hasW   bool
	hasH   bool
	forced bool
}

func force(n *Node, g given) {
	if g.hasW {
		n.Width = g.w
		if n.Type == TypeText && n.AutoResize == "width" || (n.Type == TypeText && n.AutoResize == "") {
			n.AutoResize = "height"
		}
	}
	if g.hasH {
		n.Height = g.h
		if n.Type == TypeText && n.AutoResize != "none" {
			n.AutoResize = "none"
		}
	}
}

func layoutNode(n *Node, g given) {
	force(n, g)
	l := n.Layout
	if l == nil || len(n.Children) == 0 {
		for _, c := range n.Children {
			layoutNode(c, given{})
		}
		return
	}
	row := l.Dir == "row"
	padMain, padCross := l.Pad[0]+l.Pad[2], l.Pad[1]+l.Pad[3]
	if row {
		padMain, padCross = l.Pad[3]+l.Pad[1], l.Pad[0]+l.Pad[2]
	}
	// When the cross size is already known, children that fill it get it in
	// the first pass: text that wraps at the frame's width is as tall as it
	// will be before the frame hugs it. layout.ts does the same.
	// A fill frame's own size is not known until its parent gives it.
	crossKnown := g.hasW || n.SizingX == ""
	knownCross := math.Max(0, n.Width-padCross)
	if row {
		crossKnown = g.hasH || n.SizingY == ""
		knownCross = math.Max(0, n.Height-padCross)
	}
	for _, c := range n.Children {
		stretch := l.Cross == "stretch" || (row && c.SizingY == "fill") || (!row && c.SizingX == "fill")
		switch {
		case crossKnown && inFlow(c) && stretch && row:
			layoutNode(c, given{h: knownCross, hasH: true})
		case crossKnown && inFlow(c) && stretch:
			layoutNode(c, given{w: knownCross, hasW: true})
		default:
			layoutNode(c, given{})
		}
	}
	mainOf := func(c *Node) float64 {
		w, h := sizeOf(c)
		if row {
			return w
		}
		return h
	}
	crossOf := func(c *Node) float64 {
		w, h := sizeOf(c)
		if row {
			return h
		}
		return w
	}
	fillMain := func(c *Node) bool {
		if row {
			return c.SizingX == "fill"
		}
		return c.SizingY == "fill"
	}
	fillCross := func(c *Node) bool {
		if l.Cross == "stretch" {
			return true
		}
		if row {
			return c.SizingY == "fill"
		}
		return c.SizingX == "fill"
	}
	var flow []*Node
	for _, c := range n.Children {
		if inFlow(c) {
			flow = append(flow, c)
		}
	}
	gaps := float64(max(0, len(flow)-1)) * l.Gap
	natural, crossMax := 0.0, 0.0
	for _, c := range flow {
		natural += mainOf(c)
		crossMax = math.Max(crossMax, crossOf(c))
	}
	hugX := !g.hasW && n.SizingX == "hug"
	hugY := !g.hasH && n.SizingY == "hug"
	if hugX {
		if row {
			n.Width = natural + gaps + padMain
		} else {
			n.Width = crossMax + padCross
		}
	}
	if hugY {
		if row {
			n.Height = crossMax + padCross
		} else {
			n.Height = natural + gaps + padMain
		}
	}
	mainSize, crossSize := n.Height, n.Width
	if row {
		mainSize, crossSize = n.Width, n.Height
	}
	innerMain := math.Max(0, mainSize-padMain)
	innerCross := math.Max(0, crossSize-padCross)
	fills, taken := 0, 0.0
	for _, c := range flow {
		if fillMain(c) {
			fills++
		} else {
			taken += mainOf(c)
		}
	}
	share := 0.0
	if fills > 0 {
		share = math.Max(0, innerMain-taken-gaps) / float64(fills)
	}
	for _, c := range flow {
		var f given
		if fillMain(c) {
			if row {
				f.w, f.hasW = share, true
			} else {
				f.h, f.hasH = share, true
			}
		}
		if fillCross(c) {
			if row {
				f.h, f.hasH = innerCross, true
			} else {
				f.w, f.hasW = innerCross, true
			}
		}
		if f.hasW || f.hasH {
			layoutNode(c, f)
		}
	}
	used := gaps
	for _, c := range flow {
		used += mainOf(c)
	}
	free := math.Max(0, innerMain-used)
	cursor := l.Pad[0]
	if row {
		cursor = l.Pad[3]
	}
	switch l.Main {
	case "center":
		cursor += free / 2
	case "end":
		cursor += free
	}
	spread := 0.0
	if l.Main == "between" && len(flow) > 1 {
		spread = free / float64(len(flow)-1)
	}
	crossStart := l.Pad[3]
	if row {
		crossStart = l.Pad[0]
	}
	for _, c := range flow {
		w, h := sizeOf(c)
		main, cross := h, w
		if row {
			main, cross = w, h
		}
		offset := 0.0
		switch l.Cross {
		case "center":
			offset = (innerCross - cross) / 2
		case "end":
			offset = innerCross - cross
		}
		if row {
			c.X, c.Y = cursor, crossStart+offset
		} else {
			c.X, c.Y = crossStart+offset, cursor
		}
		cursor += main + l.Gap + spread
	}
}

// ResolveLayout applies constraints (against prev, the document before the
// edit; nil skips them) and then auto layout to every page of d. It mirrors
// the app's layout.ts so both editors agree on where things go.
func ResolveLayout(d, prev *Doc) {
	if prev != nil {
		before := map[string]box{}
		for _, p := range prev.Pages {
			indexBoxes(p.Children, before)
		}
		done := map[string]bool{}
		for _, p := range d.Pages {
			constrainTree(p.Children, before, done)
		}
	}
	for _, p := range d.Pages {
		for _, n := range p.Children {
			layoutNode(n, given{})
		}
		bakeText(p.Children)
	}
}

// layoutInstance places an instance's layers again at the instance's size,
// the way its main's auto layout would: a longer label widens nothing it
// should not and a wider instance spreads its content, with no scaling. The
// layers are the instance's own copies and are changed in place.
func layoutInstance(m *Node, kids []*Node, w, h float64) []*Node {
	bakeText(kids)
	tmp := *m
	tmp.Children = kids
	layoutNode(&tmp, given{w: w, h: h, hasW: true, hasH: true})
	return tmp.Children
}

// InstanceSize is the size an instance takes when it hugs its content the
// way its main does: the main laid out again with the instance's overrides
// (a longer label makes a wider button). Mains without auto layout keep
// their size.
func InstanceSize(d *Doc, inst *Node) (float64, float64, bool) {
	r := &resolver{doc: d, masters: map[string]*Node{}}
	for _, p := range d.Pages {
		Walk(p.Children, func(n *Node, _ *Node, _ int) bool {
			if n.Type == TypeComponent {
				r.masters[n.ID] = n
				r.list = append(r.list, n)
			}
			return true
		})
	}
	m := r.master(inst)
	if m == nil {
		return 0, 0, false
	}
	if m.Layout == nil {
		return m.Width, m.Height, true
	}
	copyOf, err := clone(&Doc{Pages: []*Page{{Children: []*Node{m}}}})
	if err != nil {
		return 0, 0, false
	}
	tmp := copyOf.Pages[0].Children[0]
	Walk(tmp.Children, func(n *Node, _ *Node, _ int) bool {
		if o := inst.Overrides[n.ID]; o != nil {
			applyOverride(n, o)
		}
		return true
	})
	bakeText(tmp.Children)
	layoutNode(tmp, given{})
	return tmp.Width, tmp.Height, true
}

// bakeText stores the measured box of every auto-sized text node.
func bakeText(nodes []*Node) {
	for _, n := range nodes {
		Measure(n)
		bakeText(n.Children)
	}
}
