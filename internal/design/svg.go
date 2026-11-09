package design

import (
	"fmt"
	"html"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// SVG draws nodes of a page as a standalone SVG document. With no ids it
// draws the whole page on its background; otherwise just those nodes, cut
// to their bounds. Text is laid out by estimate (the app measures real
// fonts), so the agent can look at its work without the app open.
func SVG(d *Doc, page *Page, ids []string) (string, error) {
	// Draw the resolved document: variables, styles, overrides and variants applied.
	at := -1
	for i, p := range d.Pages {
		if p == page {
			at = i
		}
	}
	d = Resolved(d)
	if at >= 0 {
		page = d.Pages[at]
	}
	type item struct {
		node   *Node
		parent Matrix
	}
	var items []item
	background := ""
	if len(ids) == 0 {
		background = page.Background
		for _, n := range page.Children {
			if n.IsVisible() {
				items = append(items, item{n, Identity})
			}
		}
	} else {
		for _, id := range ids {
			at := d.Find(id)
			if at == nil || at.Page != page {
				return "", fmt.Errorf("no node %q on page %q", id, page.Name)
			}
			items = append(items, item{at.Node, World(at.Path[:len(at.Path)-1])})
		}
	}
	x0, y0 := math.Inf(1), math.Inf(1)
	x1, y1 := math.Inf(-1), math.Inf(-1)
	for _, it := range items {
		a, b, c, e := Bounds(it.parent.Mul(Local(it.node)), it.node)
		x0, y0, x1, y1 = math.Min(x0, a), math.Min(y0, b), math.Max(x1, c), math.Max(y1, e)
	}
	if len(items) == 0 {
		x0, y0, x1, y1 = 0, 0, 100, 100
	}
	w, h := math.Max(1, x1-x0), math.Max(1, y1-y0)
	r := &svgWriter{doc: d, masters: map[string]*Node{}}
	for _, p := range d.Pages {
		Walk(p.Children, func(n *Node, _ *Node, _ int) bool {
			if n.Type == TypeComponent {
				r.masters[n.ID] = n
				r.list = append(r.list, n)
			}
			return true
		})
	}
	var body strings.Builder
	for _, it := range items {
		m := Matrix{1, 0, 0, 1, -x0, -y0}.Mul(it.parent)
		fmt.Fprintf(&body, `<g transform="matrix(%s)">`, joinNums(m[:]))
		r.node(&body, it.node)
		body.WriteString("</g>")
	}
	var out strings.Builder
	fmt.Fprintf(&out, `<?xml version="1.0" encoding="UTF-8"?>`+"\n"+`<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" width="%s" height="%s" viewBox="0 0 %s %s">`, num(w), num(h), num(w), num(h))
	if background != "" {
		fmt.Fprintf(&out, `<rect width="%s" height="%s" fill="%s"/>`, num(w), num(h), background)
	}
	out.WriteString(body.String())
	out.WriteString("</svg>\n")
	return out.String(), nil
}

type svgWriter struct {
	doc     *Doc
	masters map[string]*Node
	list    []*Node
	// w and h are the size of the node being drawn, for image paints.
	w, h float64
	seq  int
}

func joinNums(v []float64) string {
	parts := make([]string, len(v))
	for i, x := range v {
		parts[i] = strconv.FormatFloat(math.Round(x*10000)/10000, 'f', -1, 64)
	}
	return strings.Join(parts, " ")
}

func (r *svgWriter) id(kind string) string {
	r.seq++
	return fmt.Sprintf("%s%d", kind, r.seq)
}

func splitColor(hex string) (string, float64) {
	h := strings.TrimPrefix(hex, "#")
	if len(h) == 3 || len(h) == 4 {
		var b strings.Builder
		for _, c := range h {
			b.WriteRune(c)
			b.WriteRune(c)
		}
		h = b.String()
	}
	if len(h) == 8 {
		a, _ := strconv.ParseUint(h[6:], 16, 8)
		return "#" + strings.ToUpper(h[:6]), float64(a) / 255
	}
	if len(h) != 6 {
		return "#000000", 1
	}
	return "#" + strings.ToUpper(h), 1
}

func esc(s string) string { return html.EscapeString(s) }

func radii(n *Node) [4]float64 {
	if n.Radius == nil {
		return [4]float64{}
	}
	lim := math.Min(n.Width, n.Height) / 2
	var out [4]float64
	for i, v := range n.Radius.Corners {
		out[i] = math.Max(0, math.Min(lim, v))
	}
	return out
}

func roundedRect(x, y, w, h float64, r [4]float64) string {
	tl, tr, br, bl := r[0], r[1], r[2], r[3]
	var b strings.Builder
	fmt.Fprintf(&b, "M%s %sH%s", num(x+tl), num(y), num(x+w-tr))
	if tr > 0 {
		fmt.Fprintf(&b, "A%s %s 0 0 1 %s %s", num(tr), num(tr), num(x+w), num(y+tr))
	}
	fmt.Fprintf(&b, "V%s", num(y+h-br))
	if br > 0 {
		fmt.Fprintf(&b, "A%s %s 0 0 1 %s %s", num(br), num(br), num(x+w-br), num(y+h))
	}
	fmt.Fprintf(&b, "H%s", num(x+bl))
	if bl > 0 {
		fmt.Fprintf(&b, "A%s %s 0 0 1 %s %s", num(bl), num(bl), num(x), num(y+h-bl))
	}
	fmt.Fprintf(&b, "V%s", num(y+tl))
	if tl > 0 {
		fmt.Fprintf(&b, "A%s %s 0 0 1 %s %s", num(tl), num(tl), num(x+tl), num(y))
	}
	b.WriteString("Z")
	return b.String()
}

func polygonPoints(sides int, w, h float64, inner float64) string {
	count := sides
	if inner > 0 {
		count = sides * 2
	}
	pts := make([]string, count)
	for i := range count {
		a := -math.Pi/2 + float64(i)*2*math.Pi/float64(count)
		k := 1.0
		if inner > 0 && i%2 == 1 {
			k = inner
		}
		pts[i] = num(w/2+math.Cos(a)*w/2*k) + "," + num(h/2+math.Sin(a)*h/2*k)
	}
	return strings.Join(pts, " ")
}

var pathToken = regexp.MustCompile(`[a-zA-Z]|-?(?:\d+\.?\d*|\.\d+)(?:[eE][-+]?\d+)?`)

// ScalePath scales SVG path data, so a path can be drawn at any box size
// with its strokes at their own width.
func ScalePath(d string, sx, sy float64) string {
	if sx == 1 && sy == 1 {
		return d
	}
	args := map[byte]int{'m': 2, 'l': 2, 'h': 1, 'v': 1, 'c': 6, 's': 4, 'q': 4, 't': 2, 'a': 7, 'z': 0}
	toks := pathToken.FindAllString(d, -1)
	var out []string
	cmd := byte(0)
	for i := 0; i < len(toks); {
		t := toks[i]
		if c := t[0]; (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			cmd = c
			out = append(out, t)
			i++
			if cmd == 'z' || cmd == 'Z' {
				continue
			}
		}
		lower := cmd | 0x20
		n := args[lower]
		if n == 0 || i+n > len(toks) {
			break
		}
		vals := make([]float64, n)
		for k := range n {
			v, err := strconv.ParseFloat(toks[i+k], 64)
			if err != nil {
				return d
			}
			vals[k] = v
		}
		i += n
		switch lower {
		case 'h':
			vals[0] *= sx
		case 'v':
			vals[0] *= sy
		case 'a':
			vals[0] *= sx
			vals[1] *= sy
			vals[5] *= sx
			vals[6] *= sy
		default:
			for k := range vals {
				if k%2 == 0 {
					vals[k] *= sx
				} else {
					vals[k] *= sy
				}
			}
		}
		out = append(out, joinNums(vals))
	}
	return strings.Join(out, " ")
}

// shape writes the outline of a node grown by `by` with the given attributes.
func shape(b *strings.Builder, n *Node, by float64, attrs string) bool {
	w, h := n.Width, n.Height
	switch n.Type {
	case TypeRect, TypeFrame, TypeComponent, TypeInstance, TypeImage:
		r := radii(n)
		for i := range r {
			if r[i] > 0 {
				r[i] = math.Max(0, r[i]+by)
			}
		}
		x, y, ww, hh := -by, -by, math.Max(0, w+2*by), math.Max(0, h+2*by)
		if r[0] == r[1] && r[1] == r[2] && r[2] == r[3] {
			rx := ""
			if r[0] > 0 {
				rx = fmt.Sprintf(` rx="%s"`, num(r[0]))
			}
			fmt.Fprintf(b, `<rect x="%s" y="%s" width="%s" height="%s"%s%s/>`, num(x), num(y), num(ww), num(hh), rx, attrs)
		} else {
			fmt.Fprintf(b, `<path d="%s"%s/>`, roundedRect(x, y, ww, hh, r), attrs)
		}
	case TypeEllipse:
		fmt.Fprintf(b, `<ellipse cx="%s" cy="%s" rx="%s" ry="%s"%s/>`, num(w/2), num(h/2), num(math.Max(0, w/2+by)), num(math.Max(0, h/2+by)), attrs)
	case TypeLine:
		fmt.Fprintf(b, `<line x1="0" y1="0" x2="%s" y2="0"%s/>`, num(w), attrs)
	case TypePolygon:
		fmt.Fprintf(b, `<polygon points="%s"%s/>`, polygonPoints(*n.Sides, w, h, 0), attrs)
	case TypeStar:
		fmt.Fprintf(b, `<polygon points="%s"%s/>`, polygonPoints(*n.Sides, w, h, *n.Inner), attrs)
	case TypePath:
		vb := [2]float64{1, 1}
		if n.ViewBox != nil {
			vb = *n.ViewBox
		}
		fmt.Fprintf(b, `<path d="%s"%s/>`, esc(ScalePath(n.D, w/vb[0], h/vb[1])), attrs)
	default:
		return false
	}
	return true
}

func (r *svgWriter) paint(defs *strings.Builder, p Paint) string {
	opacity := 1.0
	if p.Opacity != nil {
		opacity = *p.Opacity
	}
	if p.Type == "image" {
		id := r.id("g")
		ratio := "xMidYMid slice"
		switch p.Fit {
		case "contain":
			ratio = "xMidYMid meet"
		case "stretch":
			ratio = "none"
		}
		fmt.Fprintf(defs, `<pattern id="%s" patternUnits="userSpaceOnUse" x="0" y="0" width="%s" height="%s"><image href="%s" width="%s" height="%s" preserveAspectRatio="%s"/></pattern>`, id, num(r.w), num(r.h), esc(p.Src), num(r.w), num(r.h), ratio)
		out := fmt.Sprintf(` fill="url(#%s)"`, id)
		if opacity < 1 {
			out += fmt.Sprintf(` fill-opacity="%s"`, num(opacity))
		}
		return out
	}
	if p.Type == "solid" {
		rgb, a := splitColor(p.Color)
		if o := a * opacity; o < 1 {
			return fmt.Sprintf(` fill="%s" fill-opacity="%s"`, rgb, num(o))
		}
		return fmt.Sprintf(` fill="%s"`, rgb)
	}
	id := r.id("g")
	var stops strings.Builder
	for _, s := range p.Stops {
		rgb, a := splitColor(s.Color)
		op := ""
		if a < 1 {
			op = fmt.Sprintf(` stop-opacity="%s"`, num(a))
		}
		fmt.Fprintf(&stops, `<stop offset="%s" stop-color="%s"%s/>`, num(s.Offset), rgb, op)
	}
	if p.Type == "radial" {
		fmt.Fprintf(defs, `<radialGradient id="%s" cx="0.5" cy="0.5" r="0.5">%s</radialGradient>`, id, stops.String())
	} else {
		angle := 90.0
		if p.Angle != nil {
			angle = *p.Angle
		}
		a := angle * math.Pi / 180
		dx, dy := math.Cos(a)/2, math.Sin(a)/2
		fmt.Fprintf(defs, `<linearGradient id="%s" x1="%s" y1="%s" x2="%s" y2="%s">%s</linearGradient>`, id, num(0.5-dx), num(0.5-dy), num(0.5+dx), num(0.5+dy), stops.String())
	}
	out := fmt.Sprintf(` fill="url(#%s)"`, id)
	if opacity < 1 {
		out += fmt.Sprintf(` fill-opacity="%s"`, num(opacity))
	}
	return out
}

func (r *svgWriter) filter(defs *strings.Builder, effects []Effect) string {
	var parts, merge, inner []string
	blur := 0.0
	id := r.id("fx")
	for i, e := range effects {
		if e.Visible != nil && !*e.Visible {
			continue
		}
		if e.Type == "blur" {
			blur = math.Max(blur, e.Blur)
			continue
		}
		rgb, a := splitColor(e.Color)
		k := fmt.Sprintf("%s-%d", id, i)
		if e.Type == "drop-shadow" {
			src := "SourceAlpha"
			if s := deref(e.Spread); s != 0 {
				op, rad := "dilate", s
				if s < 0 {
					op, rad = "erode", -s
				}
				parts = append(parts, fmt.Sprintf(`<feMorphology in="SourceAlpha" operator="%s" radius="%s" result="%sa"/>`, op, num(rad), k))
				src = k + "a"
			}
			parts = append(parts, fmt.Sprintf(`<feGaussianBlur in="%s" stdDeviation="%s"/><feOffset dx="%s" dy="%s" result="%so"/><feFlood flood-color="%s" flood-opacity="%s"/><feComposite in2="%so" operator="in" result="%s"/>`, src, num(e.Blur/2), num(deref(e.X)), num(deref(e.Y)), k, rgb, num(a), k, k))
			merge = append(merge, k)
		} else {
			parts = append(parts, fmt.Sprintf(`<feComponentTransfer in="SourceAlpha" result="%si"><feFuncA type="table" tableValues="1 0"/></feComponentTransfer><feGaussianBlur in="%si" stdDeviation="%s"/><feOffset dx="%s" dy="%s" result="%so"/><feFlood flood-color="%s" flood-opacity="%s"/><feComposite in2="%so" operator="in" result="%sc"/><feComposite in="%sc" in2="SourceAlpha" operator="in" result="%s"/>`, k, k, num(e.Blur/2), num(deref(e.X)), num(deref(e.Y)), k, rgb, num(a), k, k, k, k))
			inner = append(inner, k)
		}
	}
	if len(parts) == 0 && blur == 0 {
		return ""
	}
	source := "SourceGraphic"
	if blur > 0 {
		parts = append(parts, fmt.Sprintf(`<feGaussianBlur in="SourceGraphic" stdDeviation="%s" result="%s-b"/>`, num(blur/2), id))
		source = id + "-b"
	}
	var m strings.Builder
	for _, k := range merge {
		fmt.Fprintf(&m, `<feMergeNode in="%s"/>`, k)
	}
	fmt.Fprintf(&m, `<feMergeNode in="%s"/>`, source)
	for _, k := range inner {
		fmt.Fprintf(&m, `<feMergeNode in="%s"/>`, k)
	}
	fmt.Fprintf(defs, `<filter id="%s" x="-50%%" y="-50%%" width="200%%" height="200%%" color-interpolation-filters="sRGB">%s<feMerge>%s</feMerge></filter>`, id, strings.Join(parts, ""), m.String())
	return id
}

func (r *svgWriter) text(b *strings.Builder, n *Node, fill string) {
	f := n.Font
	lines := Lines(n)
	lh := LineHeight(n)
	anchor, x := "start", 0.0
	switch n.Align {
	case "center":
		anchor, x = "middle", n.Width/2
	case "right":
		anchor, x = "end", n.Width
	}
	total := float64(len(lines)) * lh
	top := 0.0
	switch n.VAlign {
	case "middle":
		top = (n.Height - total) / 2
	case "bottom":
		top = n.Height - total
	}
	ascent := (lh-f.Size)/2 + f.Size*0.8
	family := esc(f.Family)
	if f.Family == "Inter" {
		family = "Inter Variable, Inter, sans-serif"
	} else {
		family += ", sans-serif"
	}
	extra := ""
	if f.Style == "italic" {
		extra += ` font-style="italic"`
	}
	if n.LetterSpacing != nil && *n.LetterSpacing != 0 {
		extra += fmt.Sprintf(` letter-spacing="%s"`, num(*n.LetterSpacing))
	}
	switch n.Decoration {
	case "underline":
		extra += ` text-decoration="underline"`
	case "strike":
		extra += ` text-decoration="line-through"`
	}
	fmt.Fprintf(b, `<text x="%s" text-anchor="%s" font-family="%s" font-size="%s" font-weight="%d" style="white-space:pre"%s%s>`, num(x), anchor, family, num(f.Size), f.Weight, extra, fill)
	for i, l := range lines {
		switch n.Transform {
		case "upper":
			l = strings.ToUpper(l)
		case "lower":
			l = strings.ToLower(l)
		}
		if l == "" {
			l = " "
		}
		fmt.Fprintf(b, `<tspan x="%s" y="%s">%s</tspan>`, num(x), num(top+ascent+float64(i)*lh), esc(l))
	}
	b.WriteString("</text>")
}

func (r *svgWriter) node(b *strings.Builder, n *Node) {
	if !n.IsVisible() {
		return
	}
	t := fmt.Sprintf("translate(%s %s)", num(n.X), num(n.Y))
	if n.Rotation != 0 {
		t += fmt.Sprintf(" rotate(%s %s %s)", num(n.Rotation), num(n.Width/2), num(n.Height/2))
	}
	attrs := ""
	if n.Opacity != nil && *n.Opacity < 1 {
		attrs += fmt.Sprintf(` opacity="%s"`, num(*n.Opacity))
	}
	if n.Blend != "" {
		attrs += fmt.Sprintf(` style="mix-blend-mode:%s"`, esc(n.Blend))
	}
	fmt.Fprintf(b, `<g transform="%s"%s>`, t, attrs)
	var defs, body strings.Builder
	if fx := r.filter(&defs, n.Effects); fx != "" {
		fmt.Fprintf(&body, `<g filter="url(#%s)">`, fx)
		r.content(&defs, &body, n)
		body.WriteString("</g>")
	} else {
		r.content(&defs, &body, n)
	}
	if defs.Len() > 0 {
		b.WriteString("<defs>" + defs.String() + "</defs>")
	}
	b.WriteString(body.String())
	b.WriteString("</g>")
}

func (r *svgWriter) content(defs, b *strings.Builder, n *Node) {
	r.w, r.h = n.Width, n.Height
	var fills []Paint
	for _, p := range n.Fills {
		if p.Visible == nil || *p.Visible {
			fills = append(fills, p)
		}
	}
	switch n.Type {
	case TypeText:
		for _, p := range fills {
			r.text(b, n, r.paint(defs, p))
		}
		return
	case TypeImage:
		for _, p := range fills {
			shape(b, n, 0, r.paint(defs, p))
		}
		if n.Src != "" {
			clip := r.id("ic")
			var c strings.Builder
			shape(&c, n, 0, "")
			fmt.Fprintf(defs, `<clipPath id="%s">%s</clipPath>`, clip, c.String())
			ratio := "xMidYMid slice"
			switch n.Fit {
			case "contain":
				ratio = "xMidYMid meet"
			case "stretch":
				ratio = "none"
			}
			fmt.Fprintf(b, `<image href="%s" width="%s" height="%s" preserveAspectRatio="%s" clip-path="url(#%s)"/>`, esc(n.Src), num(n.Width), num(n.Height), ratio, clip)
		}
	case TypeGroup:
		if n.BoolOp != "" && len(n.Children) > 0 {
			r.boolean(defs, b, n, fills)
		}
	default:
		for _, p := range fills {
			shape(b, n, 0, r.paint(defs, p))
		}
	}
	kids := n.Children
	if n.Type == TypeGroup && n.BoolOp != "" {
		kids = nil
	}
	sx, sy := 1.0, 1.0
	clipKids := n.Clip != nil && *n.Clip && n.Type != TypeGroup
	if n.Type == TypeInstance {
		if m := (&resolver{masters: r.masters, list: r.list}).master(n); m != nil {
			if len(n.Children) == 0 {
				kids = m.Children
			}
			if m.Width > 0 {
				sx = n.Width / m.Width
			}
			if m.Height > 0 {
				sy = n.Height / m.Height
			}
			clipKids = m.Clip == nil || *m.Clip
		}
	}
	if len(kids) > 0 {
		open := "<g"
		if clipKids {
			clip := r.id("cl")
			var c strings.Builder
			shape(&c, n, 0, "")
			fmt.Fprintf(defs, `<clipPath id="%s">%s</clipPath>`, clip, c.String())
			open += fmt.Sprintf(` clip-path="url(#%s)"`, clip)
		}
		if sx != 1 || sy != 1 {
			open += fmt.Sprintf(` transform="scale(%s %s)"`, num(sx), num(sy))
		}
		b.WriteString(open + ">")
		r.maskedKids(defs, b, kids)
		b.WriteString("</g>")
	}
	if n.Type == TypeGroup {
		return
	}
	for _, s := range n.Strokes {
		if (s.Visible != nil && !*s.Visible) || s.Width <= 0 {
			continue
		}
		rgb, a := splitColor(s.Color)
		if s.Opacity != nil {
			a *= *s.Opacity
		}
		attrs := fmt.Sprintf(` fill="none" stroke="%s" stroke-width="%s"`, rgb, num(s.Width))
		if a < 1 {
			attrs += fmt.Sprintf(` stroke-opacity="%s"`, num(a))
		}
		if len(s.Dash) > 0 {
			attrs += fmt.Sprintf(` stroke-dasharray="%s"`, joinNums(s.Dash))
		}
		by := 0.0
		closed := n.Type != TypeLine && n.Type != TypePath && n.Type != TypePolygon && n.Type != TypeStar
		if closed && s.Align == "inside" {
			by = -s.Width / 2
		}
		if closed && s.Align == "outside" {
			by = s.Width / 2
		}
		shape(b, n, by, attrs)
	}
}

type flatShape struct {
	node   *Node
	matrix Matrix
}

// flatShapes lists the outlines of every shape in nodes, nested groups and
// frames included, each with its matrix into the list's space.
func flatShapes(nodes []*Node, parent Matrix) []flatShape {
	var out []flatShape
	for _, c := range nodes {
		if !c.IsVisible() {
			continue
		}
		m := parent.Mul(Local(c))
		var probe strings.Builder
		if c.Type != TypeInstance && shape(&probe, c, 0, "") {
			out = append(out, flatShape{c, m})
		} else {
			out = append(out, flatShapes(c.Children, m)...)
		}
	}
	return out
}

func matrixAttr(m Matrix) string { return fmt.Sprintf(` transform="matrix(%s)"`, joinNums(m[:])) }

func shapesWith(parts []flatShape, fill string, only func(i int) bool, extra string) string {
	var b strings.Builder
	for i, p := range parts {
		if only != nil && !only(i) {
			continue
		}
		shape(&b, p.node, 0, fmt.Sprintf(` fill="%s"%s%s`, fill, extra, matrixAttr(p.matrix)))
	}
	return b.String()
}

// boolean draws a group whose shapes are combined into one: the group's
// fills paint the result, cut out with masks as the app does.
func (r *svgWriter) boolean(defs, b *strings.Builder, n *Node, fills []Paint) {
	parts := flatShapes(n.Children, Identity)
	if len(parts) == 0 {
		return
	}
	w, h := n.Width, n.Height
	region := fmt.Sprintf(`maskUnits="userSpaceOnUse" x="-1" y="-1" width="%s" height="%s"`, num(w+2), num(h+2))
	mask := func(content string) string {
		id := r.id("bm")
		fmt.Fprintf(defs, `<mask id="%s" %s>%s</mask>`, id, region, content)
		return id
	}
	var ids []string
	switch n.BoolOp {
	case "union":
		ids = []string{mask("<g>" + shapesWith(parts, "#fff", nil, "") + "</g>")}
	case "subtract":
		ids = []string{mask("<g>" + shapesWith(parts, "#fff", func(i int) bool { return i == 0 }, "") + shapesWith(parts, "#000", func(i int) bool { return i > 0 }, "") + "</g>")}
	case "exclude":
		ids = []string{mask(fmt.Sprintf(`<rect x="-1" y="-1" width="%s" height="%s" fill="#000"/><g style="isolation:isolate">%s</g>`, num(w+2), num(h+2), shapesWith(parts, "#fff", nil, ` style="mix-blend-mode:difference"`)))}
	default:
		for _, p := range parts {
			ids = append(ids, mask("<g>"+shapesWith([]flatShape{p}, "#fff", nil, "")+"</g>"))
		}
	}
	for _, f := range fills {
		open, closeTags := "", ""
		for _, id := range ids {
			open += fmt.Sprintf(`<g mask="url(#%s)">`, id)
			closeTags += "</g>"
		}
		fmt.Fprintf(b, `%s<rect width="%s" height="%s"%s/>%s`, open, num(w), num(h), r.paint(defs, f), closeTags)
	}
}

// maskedKids draws children in order; a shape marked as a mask clips the
// siblings above it and is not drawn itself.
func (r *svgWriter) maskedKids(defs, b *strings.Builder, kids []*Node) {
	at := -1
	for i, c := range kids {
		if c.Mask && c.IsVisible() {
			at = i
			break
		}
	}
	if at < 0 {
		for _, c := range kids {
			r.node(b, c)
		}
		return
	}
	for _, c := range kids[:at] {
		r.node(b, c)
	}
	clip := r.id("mk")
	probe := *kids[at]
	probe.Mask = false
	var c strings.Builder
	for _, p := range flatShapes([]*Node{&probe}, Identity) {
		shape(&c, p.node, 0, matrixAttr(p.matrix))
	}
	fmt.Fprintf(defs, `<clipPath id="%s">%s</clipPath>`, clip, c.String())
	fmt.Fprintf(b, `<g clip-path="url(#%s)">`, clip)
	r.maskedKids(defs, b, kids[at+1:])
	b.WriteString("</g>")
}
