package design

import (
	"fmt"
	"html"
	"regexp"
	"strings"
)

// Code is generated markup for a node: HTML with a stylesheet, or a React
// component with the same stylesheet.
type Code struct {
	Markup string
	CSS    string
}

type cssRule struct{ prop, val string }

func px(v float64) string { return num(v) + "px" }

func cssColor(hex string) string {
	rgb, a := splitColor(hex)
	if a >= 1 {
		return rgb
	}
	var r, g, b int
	_, _ = fmt.Sscanf(rgb, "#%02x%02x%02x", &r, &g, &b)
	return fmt.Sprintf("rgba(%d, %d, %d, %s)", r, g, b, num(a))
}

func cssPaint(p Paint) string {
	switch p.Type {
	case "solid":
		return cssColor(p.Color)
	case "image":
		size := "cover"
		switch p.Fit {
		case "contain":
			size = "contain"
		case "stretch":
			size = "100% 100%"
		}
		return fmt.Sprintf("url(%q) center / %s no-repeat", p.Src, size)
	}
	stops := make([]string, len(p.Stops))
	for i, s := range p.Stops {
		stops[i] = fmt.Sprintf("%s %s%%", cssColor(s.Color), num(s.Offset*100))
	}
	if p.Type == "radial" {
		return "radial-gradient(circle, " + strings.Join(stops, ", ") + ")"
	}
	angle := 90.0
	if p.Angle != nil {
		angle = *p.Angle
	}
	return fmt.Sprintf("linear-gradient(%sdeg, %s)", num(angle+90), strings.Join(stops, ", "))
}

func cssRules(n, parent *Node) []cssRule {
	var d []cssRule
	add := func(p, v string) { d = append(d, cssRule{p, v}) }
	w, h := sizeOf(n)
	inFlow := parent != nil && parent.Layout != nil && !n.Absolute
	if !inFlow && parent != nil {
		add("position", "absolute")
		add("left", px(n.X))
		add("top", px(n.Y))
	}
	row := parent != nil && parent.Layout != nil && parent.Layout.Dir == "row"
	fillX, fillY := inFlow && n.SizingX == "fill", inFlow && n.SizingY == "fill"
	if (row && fillX) || (!row && inFlow && fillY && parent.Layout != nil) {
		add("flex", "1 1 0")
	}
	switch {
	case n.SizingX == "hug":
		add("width", "fit-content")
	case fillX && !row:
		add("width", "100%")
	default:
		add("width", px(w))
	}
	if n.Type != TypeText || n.AutoResize == "none" {
		switch {
		case n.SizingY == "hug":
			add("height", "fit-content")
		case fillY && row:
			add("height", "100%")
		default:
			add("height", px(h))
		}
	}
	if n.Rotation != 0 {
		add("transform", "rotate("+num(n.Rotation)+"deg)")
	}
	if n.Opacity != nil && *n.Opacity < 1 {
		add("opacity", num(*n.Opacity))
	}
	if n.Type == TypeEllipse {
		add("border-radius", "50%")
	} else if n.Radius != nil {
		c := n.Radius.Corners
		if n.Radius.Each {
			add("border-radius", fmt.Sprintf("%s %s %s %s", px(c[0]), px(c[1]), px(c[2]), px(c[3])))
		} else if c[0] > 0 {
			add("border-radius", px(c[0]))
		}
	}
	var fills []string
	for i := len(n.Fills) - 1; i >= 0; i-- {
		if p := n.Fills[i]; p.Visible == nil || *p.Visible {
			fills = append(fills, cssPaint(p))
		}
	}
	if n.Type == TypeText {
		if len(fills) > 0 {
			add("color", fills[len(fills)-1])
		}
		if f := n.Font; f != nil {
			add("font-family", fmt.Sprintf("'%s', sans-serif", f.Family))
			add("font-size", px(f.Size))
			add("font-weight", fmt.Sprint(f.Weight))
			if f.Style == "italic" {
				add("font-style", "italic")
			}
		}
		if n.LineHeight != nil && *n.LineHeight > 0 {
			add("line-height", px(*n.LineHeight))
		}
		if n.LetterSpacing != nil && *n.LetterSpacing != 0 {
			add("letter-spacing", px(*n.LetterSpacing))
		}
		if n.Align == "center" || n.Align == "right" {
			add("text-align", n.Align)
		}
		switch n.Decoration {
		case "underline":
			add("text-decoration", "underline")
		case "strike":
			add("text-decoration", "line-through")
		}
		switch n.Transform {
		case "upper":
			add("text-transform", "uppercase")
		case "lower":
			add("text-transform", "lowercase")
		}
		add("margin", "0")
		if n.AutoResize != "height" {
			add("white-space", "pre")
		} else {
			add("white-space", "pre-wrap")
		}
	} else if len(fills) > 0 && n.Type != TypeImage {
		add("background", strings.Join(fills, ", "))
	}
	for _, s := range n.Strokes {
		if (s.Visible == nil || *s.Visible) && s.Width > 0 && n.Type != TypeText {
			style := "solid"
			if len(s.Dash) > 0 {
				style = "dashed"
			}
			add("border", fmt.Sprintf("%s %s %s", px(s.Width), style, cssColor(s.Color)))
			add("box-sizing", "border-box")
			break
		}
	}
	var shadows []string
	for _, e := range n.Effects {
		if e.Visible != nil && !*e.Visible {
			continue
		}
		switch e.Type {
		case "blur":
			add("filter", "blur("+px(e.Blur/2)+")")
		default:
			inset := ""
			if e.Type == "inner-shadow" {
				inset = "inset "
			}
			x, y, sp := 0.0, 0.0, 0.0
			if e.X != nil {
				x = *e.X
			}
			if e.Y != nil {
				y = *e.Y
			}
			if e.Spread != nil {
				sp = *e.Spread
			}
			col := e.Color
			if col == "" {
				col = "#00000040"
			}
			shadows = append(shadows, fmt.Sprintf("%s%s %s %s %s %s", inset, px(x), px(y), px(e.Blur), px(sp), cssColor(col)))
		}
	}
	if len(shadows) > 0 {
		add("box-shadow", strings.Join(shadows, ", "))
	}
	if n.Clip != nil && *n.Clip && len(n.Children) > 0 && n.Type != TypeGroup {
		add("overflow", "hidden")
	}
	if l := n.Layout; l != nil {
		add("display", "flex")
		add("flex-direction", l.Dir)
		if l.Gap > 0 {
			add("gap", px(l.Gap))
		}
		if l.Pad != (Pad{}) {
			add("padding", fmt.Sprintf("%s %s %s %s", px(l.Pad[0]), px(l.Pad[1]), px(l.Pad[2]), px(l.Pad[3])))
		}
		add("justify-content", map[string]string{"start": "flex-start", "center": "center", "end": "flex-end", "between": "space-between"}[l.Main])
		add("align-items", map[string]string{"start": "flex-start", "center": "center", "end": "flex-end", "stretch": "stretch"}[l.Cross])
	} else if len(n.Children) > 0 && n.Type != TypeGroup {
		add("position", "relative")
	}
	if n.Type == TypeGroup && parent != nil && len(n.Children) > 0 {
		add("position", "absolute")
	}
	return d
}

var classClean = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

func className(n *Node) string {
	base := classClean.ReplaceAllString(strings.ToLower(n.Name), "-")
	base = strings.Trim(base, "-")
	if base == "" {
		base = string(n.Type)
	}
	return base + "-" + classClean.ReplaceAllString(n.ID, "")
}

// GenerateCode writes HTML (or a React component when react is true) for a
// node and everything in it, with a stylesheet that positions it the way
// the design does: flexbox where a frame has auto layout, absolute
// positions elsewhere.
func GenerateCode(d *Doc, id string, react bool) (Code, error) {
	resolved := Resolved(d)
	at := resolved.Find(id)
	if at == nil {
		return Code{}, fmt.Errorf("no node %q", id)
	}
	var css, body strings.Builder
	attr := "class"
	if react {
		attr = "className"
	}
	var emit func(n, parent *Node, depth int)
	emit = func(n, parent *Node, depth int) {
		if !n.IsVisible() {
			return
		}
		pad := strings.Repeat("  ", depth)
		cls := className(n)
		rules := cssRules(n, parent)
		if parent == nil {
			rules = append([]cssRule{{"position", "relative"}}, filterRules(rules, "position", "left", "top")...)
		}
		fmt.Fprintf(&css, ".%s {\n", cls)
		for _, r := range rules {
			fmt.Fprintf(&css, "  %s: %s;\n", r.prop, r.val)
		}
		css.WriteString("}\n")
		switch n.Type {
		case TypeText:
			lines := strings.Split(n.TextValue(), "\n")
			text := html.EscapeString(strings.Join(lines, "\n"))
			if react {
				text = "{" + jsString(n.TextValue()) + "}"
			}
			fmt.Fprintf(&body, "%s<p %s=%s>%s</p>\n", pad, attr, q(cls), text)
		case TypeImage:
			src := n.Src
			for _, f := range n.Fills {
				if f.Type == "image" {
					src = f.Src
				}
			}
			alt := n.Name
			fit := "cover"
			if n.Fit == "contain" || n.Fit == "stretch" {
				fit = map[string]string{"contain": "contain", "stretch": "fill"}[n.Fit]
			}
			fmt.Fprintf(&body, "%s<img %s=%s src=%s alt=%s style=%s%s", pad, attr, q(cls), q(src), q(alt), styleAttr(react, "object-fit", fit), selfClose(react))
		case TypePath:
			vb := [2]float64{n.Width, n.Height}
			if n.ViewBox != nil {
				vb = *n.ViewBox
			}
			fill, stroke := "none", "none"
			sw := 0.0
			if len(n.Fills) > 0 && n.Fills[0].Type == "solid" {
				fill = cssColor(n.Fills[0].Color)
			}
			if len(n.Strokes) > 0 {
				stroke, sw = cssColor(n.Strokes[0].Color), n.Strokes[0].Width
			}
			fmt.Fprintf(&body, "%s<svg %s=%s viewBox=\"0 0 %s %s\" preserveAspectRatio=\"none\"><path d=%s fill=%s stroke=%s stroke-width=%s/></svg>\n", pad, attr, q(cls), num(vb[0]), num(vb[1]), q(n.D), q(fill), q(stroke), q(num(sw)))
		default:
			if len(n.Children) == 0 {
				fmt.Fprintf(&body, "%s<div %s=%s></div>\n", pad, attr, q(cls))
				return
			}
			fmt.Fprintf(&body, "%s<div %s=%s>\n", pad, attr, q(cls))
			for _, c := range n.Children {
				emit(c, n, depth+1)
			}
			fmt.Fprintf(&body, "%s</div>\n", pad)
		}
	}
	emit(at.Node, nil, 1)
	markup := strings.TrimRight(body.String(), "\n")
	if react {
		name := reactName(at.Node.Name)
		markup = fmt.Sprintf("import \"./%s.css\";\n\nexport default function %s() {\n  return (\n%s\n  );\n}", name, name, indent(markup, 2))
	}
	return Code{Markup: markup, CSS: css.String()}, nil
}

func filterRules(rules []cssRule, drop ...string) []cssRule {
	var out []cssRule
next:
	for _, r := range rules {
		for _, d := range drop {
			if r.prop == d {
				continue next
			}
		}
		out = append(out, r)
	}
	return out
}

func indent(s string, n int) string {
	pad := strings.Repeat("  ", n)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = pad + l
	}
	return strings.Join(lines, "\n")
}

func jsString(s string) string {
	return fmt.Sprintf("%q", s)
}

func styleAttr(react bool, prop, val string) string {
	if react {
		camel := strings.ReplaceAll(prop, "-", "")
		if prop == "object-fit" {
			camel = "objectFit"
		}
		return fmt.Sprintf("{{ %s: %q }}", camel, val)
	}
	return q(prop + ": " + val)
}

func selfClose(react bool) string {
	if react {
		return " />\n"
	}
	return ">\n"
}

func reactName(name string) string {
	var b strings.Builder
	up := true
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9':
			if up && r >= 'a' && r <= 'z' {
				r -= 'a' - 'A'
			}
			b.WriteRune(r)
			up = false
		default:
			up = true
		}
	}
	out := b.String()
	if out == "" || (out[0] >= '0' && out[0] <= '9') {
		out = "Design" + out
	}
	return out
}

func q(s string) string { return "\"" + html.EscapeString(s) + "\"" }
