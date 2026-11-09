package design

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

func num(v float64) string {
	return strconv.FormatFloat(math.Round(v*100)/100, 'f', -1, 64)
}

func paintSummary(fills []Paint) string {
	var parts []string
	for _, p := range fills {
		if p.Visible != nil && !*p.Visible {
			continue
		}
		switch p.Type {
		case "solid":
			s := p.Color
			if p.Opacity != nil && *p.Opacity < 1 {
				s += "@" + num(*p.Opacity)
			}
			parts = append(parts, s)
		default:
			stops := make([]string, len(p.Stops))
			for i, st := range p.Stops {
				stops[i] = st.Color
			}
			parts = append(parts, p.Type+"("+strings.Join(stops, ">")+")")
		}
	}
	return strings.Join(parts, ",")
}

// Describe is one line about a node: its type, name, id, box and the
// properties that matter for its type.
func Describe(n *Node) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %q id=%s x=%s y=%s w=%s h=%s", n.Type, n.Name, n.ID, num(n.X), num(n.Y), num(n.Width), num(n.Height))
	if n.Rotation != 0 {
		fmt.Fprintf(&b, " rot=%s", num(n.Rotation))
	}
	if f := paintSummary(n.Fills); f != "" {
		fmt.Fprintf(&b, " fill=%s", f)
	}
	for _, s := range n.Strokes {
		fmt.Fprintf(&b, " stroke=%s/%s", s.Color, num(s.Width))
		if s.Align != "" && s.Align != "center" {
			b.WriteString("/" + s.Align)
		}
	}
	if n.Radius != nil {
		if n.Radius.Each {
			c := n.Radius.Corners
			fmt.Fprintf(&b, " radius=%s,%s,%s,%s", num(c[0]), num(c[1]), num(c[2]), num(c[3]))
		} else if n.Radius.Corners[0] > 0 {
			fmt.Fprintf(&b, " radius=%s", num(n.Radius.Corners[0]))
		}
	}
	for _, e := range n.Effects {
		if e.Type == "blur" {
			fmt.Fprintf(&b, " blur=%s", num(e.Blur))
			continue
		}
		fmt.Fprintf(&b, " %s=%s,%s,%s,%s", e.Type, num(deref(e.X)), num(deref(e.Y)), num(e.Blur), e.Color)
	}
	if n.Opacity != nil && *n.Opacity < 1 {
		fmt.Fprintf(&b, " opacity=%s", num(*n.Opacity))
	}
	if !n.IsVisible() {
		b.WriteString(" hidden")
	}
	if n.Locked {
		b.WriteString(" locked")
	}
	if n.Clip != nil && !*n.Clip && n.Type != TypeGroup {
		b.WriteString(" no-clip")
	}
	switch n.Type {
	case TypeText:
		f := n.Font
		fmt.Fprintf(&b, " font=%q/%s/%d", f.Family, num(f.Size), f.Weight)
		if n.LineHeight != nil && *n.LineHeight > 0 {
			fmt.Fprintf(&b, " lh=%s", num(*n.LineHeight))
		}
		if n.Align != "" {
			b.WriteString(" align=" + n.Align)
		}
		b.WriteString(" " + n.AutoResize)
		text := n.TextValue()
		if r := []rune(text); len(r) > 80 {
			text = string(r[:80]) + "…"
		}
		fmt.Fprintf(&b, " text=%q", text)
	case TypeImage:
		src := n.Src
		if strings.HasPrefix(src, "data:") {
			src = "(embedded)"
		}
		fmt.Fprintf(&b, " src=%q fit=%s", src, n.Fit)
	case TypeInstance:
		fmt.Fprintf(&b, " of=%s", n.ComponentID)
	case TypePolygon, TypeStar:
		fmt.Fprintf(&b, " sides=%d", *n.Sides)
	}
	return b.String()
}

func deref(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

// Outline describes a document as an indented tree, one line per node,
// within maxLines (0 is no limit).
func Outline(d *Doc, maxLines int) string {
	var b strings.Builder
	lines := 0
	cut := false
	fmt.Fprintf(&b, "design %q: %d page(s), %d node(s)\n", d.Name, len(d.Pages), d.Count())
	for _, p := range d.Pages {
		fmt.Fprintf(&b, "page %q id=%s background=%s\n", p.Name, p.ID, p.Background)
		Walk(p.Children, func(n *Node, _ *Node, depth int) bool {
			if maxLines > 0 && lines >= maxLines {
				cut = true
				return false
			}
			b.WriteString(strings.Repeat("  ", depth+1))
			b.WriteString("- ")
			b.WriteString(Describe(n))
			b.WriteByte('\n')
			lines++
			return true
		})
	}
	if cut {
		fmt.Fprintf(&b, "… (outline cut at %d lines; read a node_id for the rest)\n", maxLines)
	}
	if c := OpenComments(d); c != "" {
		b.WriteString("open comments:\n" + c)
	}
	return b.String()
}
