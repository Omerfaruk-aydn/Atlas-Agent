package design

import (
	"math"
	"regexp"
)

var refRe = regexp.MustCompile(`^[\w:-]{1,64}$`)

func validRef(s string) bool { return refRe.MatchString(s) }

// FillStyle is a shared set of fills.
type FillStyle struct {
	Name  string  `json:"name"`
	Fills []Paint `json:"fills"`
}

// EffectStyle is a shared set of effects.
type EffectStyle struct {
	Name    string   `json:"name"`
	Effects []Effect `json:"effects"`
}

// TextStyle is a shared typeface and paragraph setting.
type TextStyle struct {
	Name          string   `json:"name"`
	Font          Font     `json:"font"`
	LineHeight    *float64 `json:"line_height,omitempty"`
	LetterSpacing *float64 `json:"letter_spacing,omitempty"`
	Align         string   `json:"align,omitempty"`
	Decoration    string   `json:"decoration,omitempty"`
	Transform     string   `json:"transform,omitempty"`
}

// Styles are the looks nodes share, by id.
type Styles struct {
	Fills   map[string]*FillStyle   `json:"fills,omitempty"`
	Text    map[string]*TextStyle   `json:"text,omitempty"`
	Effects map[string]*EffectStyle `json:"effects,omitempty"`
}

// Variable is a colour with a value per mode.
type Variable struct {
	Name   string            `json:"name"`
	Values map[string]string `json:"values"`
}

// Variables are colour variables; Mode is the active mode.
type Variables struct {
	Modes []string             `json:"modes"`
	Mode  string               `json:"mode"`
	Items map[string]*Variable `json:"items"`
}

// FontOverride changes parts of a font; zero fields keep the original.
type FontOverride struct {
	Family string  `json:"family,omitempty"`
	Size   float64 `json:"size,omitempty"`
	Weight int     `json:"weight,omitempty"`
	Style  string  `json:"style,omitempty"`
}

// Override is what an instance changes in one layer of its component.
type Override struct {
	Visible     *bool         `json:"visible,omitempty"`
	Opacity     *float64      `json:"opacity,omitempty"`
	Fills       *[]Paint      `json:"fills,omitempty"`
	Strokes     *[]Stroke     `json:"strokes,omitempty"`
	Effects     *[]Effect     `json:"effects,omitempty"`
	Text        *string       `json:"text,omitempty"`
	Font        *FontOverride `json:"font,omitempty"`
	Radius      *Radius       `json:"radius,omitempty"`
	FillStyle   string        `json:"fill_style,omitempty"`
	TextStyle   string        `json:"text_style,omitempty"`
	EffectStyle string        `json:"effect_style,omitempty"`
}

func sanitizeVariant(v map[string]string) map[string]string {
	if len(v) == 0 {
		return nil
	}
	out := map[string]string{}
	for k, val := range v {
		if len(out) == 16 {
			break
		}
		out[cut(k, 40)] = cut(val, 80)
	}
	return out
}

func sanitizeOverrides(in map[string]*Override) map[string]*Override {
	out := map[string]*Override{}
	for id, o := range in {
		if o == nil || !validRef(id) || len(out) == 500 {
			continue
		}
		o.Opacity = clampPtr(o.Opacity, 0, 1)
		if o.Fills != nil {
			f := sanitizePaints(*o.Fills)
			o.Fills = &f
		}
		if o.Strokes != nil {
			s := sanitizeStrokes(*o.Strokes)
			o.Strokes = &s
		}
		if o.Effects != nil {
			e := sanitizeEffects(*o.Effects)
			o.Effects = &e
		}
		if o.Text != nil {
			t := cut(*o.Text, 100_000)
			o.Text = &t
		}
		if o.Font != nil {
			o.Font.Family = cut(o.Font.Family, 120)
			if o.Font.Size != 0 {
				o.Font.Size = clamp(o.Font.Size, 1, 2000)
			}
			if o.Font.Weight != 0 {
				o.Font.Weight = int(math.Round(clamp(float64(o.Font.Weight), 100, 900)/100)) * 100
			}
		}
		if !validRef(o.FillStyle) {
			o.FillStyle = ""
		}
		if !validRef(o.TextStyle) {
			o.TextStyle = ""
		}
		if !validRef(o.EffectStyle) {
			o.EffectStyle = ""
		}
		out[id] = o
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func sanitizeStyles(s *Styles) *Styles {
	if s == nil {
		return nil
	}
	out := &Styles{}
	for id, f := range s.Fills {
		if f == nil || !validRef(id) || len(out.Fills) == 500 {
			continue
		}
		if fills := sanitizePaints(f.Fills); len(fills) > 0 {
			if out.Fills == nil {
				out.Fills = map[string]*FillStyle{}
			}
			out.Fills[id] = &FillStyle{Name: nameOr(f.Name, "Fill"), Fills: fills}
		}
	}
	for id, e := range s.Effects {
		if e == nil || !validRef(id) || len(out.Effects) == 500 {
			continue
		}
		if effects := sanitizeEffects(e.Effects); len(effects) > 0 {
			if out.Effects == nil {
				out.Effects = map[string]*EffectStyle{}
			}
			out.Effects[id] = &EffectStyle{Name: nameOr(e.Name, "Effect"), Effects: effects}
		}
	}
	for id, t := range s.Text {
		if t == nil || !validRef(id) || len(out.Text) == 500 {
			continue
		}
		f := t.Font
		f.Family = cut(f.Family, 120)
		if f.Family == "" {
			f.Family = "Inter"
		}
		if f.Size == 0 {
			f.Size = 16
		}
		f.Size = clamp(f.Size, 1, 2000)
		if f.Weight == 0 {
			f.Weight = 400
		}
		f.Weight = int(math.Round(clamp(float64(f.Weight), 100, 900)/100)) * 100
		if f.Style != "italic" {
			f.Style = ""
		}
		c := &TextStyle{Name: nameOr(t.Name, "Text"), Font: f, LineHeight: clampPtr(t.LineHeight, 0, 10000), LetterSpacing: clampPtr(t.LetterSpacing, -1000, 1000)}
		c.Align = oneOf(t.Align, "", "center", "right")
		c.Decoration = oneOf(t.Decoration, "", "underline", "strike")
		c.Transform = oneOf(t.Transform, "", "upper", "lower")
		if out.Text == nil {
			out.Text = map[string]*TextStyle{}
		}
		out.Text[id] = c
	}
	if out.Fills == nil && out.Text == nil && out.Effects == nil {
		return nil
	}
	return out
}

func nameOr(s, fallback string) string {
	if s = cut(s, 80); s == "" {
		return fallback
	}
	return s
}

func sanitizeVariables(v *Variables) *Variables {
	if v == nil {
		return nil
	}
	out := &Variables{Items: map[string]*Variable{}}
	seen := map[string]bool{}
	for _, m := range v.Modes {
		if m = cut(m, 40); m != "" && !seen[m] && len(out.Modes) < 8 {
			seen[m] = true
			out.Modes = append(out.Modes, m)
		}
	}
	if len(out.Modes) == 0 {
		out.Modes = []string{"Default"}
	}
	out.Mode = out.Modes[0]
	if seen[v.Mode] {
		out.Mode = v.Mode
	}
	for id, item := range v.Items {
		if item == nil || !validRef(id) || len(out.Items) == 500 {
			continue
		}
		values := map[string]string{}
		for _, m := range out.Modes {
			values[m] = color(item.Values[m], "#000000")
		}
		out.Items[id] = &Variable{Name: nameOr(item.Name, id), Values: values}
	}
	return out
}

// Resolved returns a copy of the document as it is drawn: colour variables
// take the active mode's colour, styles copy their fills, text and effects
// onto the nodes that use them, and instances carry their component's
// layers with the instance's overrides and variant applied. The original is
// not changed. It mirrors the app's tokens.ts.
func Resolved(d *Doc) *Doc {
	out, err := clone(d)
	if err != nil {
		return d
	}
	masters := map[string]*Node{}
	var list []*Node
	for _, p := range d.Pages {
		Walk(p.Children, func(n *Node, _ *Node, _ int) bool {
			if n.Type == TypeComponent {
				masters[n.ID] = n
				list = append(list, n)
			}
			return true
		})
	}
	r := &resolver{doc: d, masters: masters, list: list}
	for _, p := range out.Pages {
		for i, n := range p.Children {
			p.Children[i] = r.node(n, nil, 0)
		}
	}
	return out
}

type resolver struct {
	doc     *Doc
	masters map[string]*Node
	list    []*Node
}

func (r *resolver) master(n *Node) *Node {
	own := r.masters[n.ComponentID]
	if own == nil || own.Set == "" || len(n.Variant) == 0 {
		return own
	}
	for _, m := range r.list {
		if m.Set != own.Set {
			continue
		}
		ok := true
		for k, v := range n.Variant {
			if m.Variant[k] != v {
				ok = false
				break
			}
		}
		if ok {
			return m
		}
	}
	return own
}

func (r *resolver) variable(id, fallback string) string {
	v := r.doc.Variables
	if v == nil || id == "" {
		return fallback
	}
	if item := v.Items[id]; item != nil {
		if c, ok := item.Values[v.Mode]; ok {
			return c
		}
	}
	return fallback
}

func (r *resolver) node(n *Node, over map[string]*Override, depth int) *Node {
	if o := over[n.ID]; o != nil {
		applyOverride(n, o)
	}
	if s := r.doc.Styles; s != nil {
		if f := s.Fills[n.FillStyle]; f != nil && n.FillStyle != "" {
			n.Fills = append([]Paint(nil), f.Fills...)
		}
		if e := s.Effects[n.EffectStyle]; e != nil && n.EffectStyle != "" {
			n.Effects = append([]Effect(nil), e.Effects...)
		}
		if t := s.Text[n.TextStyle]; t != nil && n.TextStyle != "" && n.Type == TypeText {
			f := t.Font
			n.Font = &f
			n.LineHeight, n.LetterSpacing = t.LineHeight, t.LetterSpacing
			if t.Align != "" {
				n.Align = t.Align
			}
			if t.Decoration != "" {
				n.Decoration = t.Decoration
			}
			if t.Transform != "" {
				n.Transform = t.Transform
			}
		}
	}
	for i := range n.Fills {
		if n.Fills[i].Variable != "" {
			n.Fills[i].Color = r.variable(n.Fills[i].Variable, n.Fills[i].Color)
		}
	}
	for i := range n.Strokes {
		if n.Strokes[i].Variable != "" {
			n.Strokes[i].Color = r.variable(n.Strokes[i].Variable, n.Strokes[i].Color)
		}
	}
	if n.Type == TypeInstance && depth < 8 {
		if m := r.master(n); m != nil {
			copyOf, err := clone(&Doc{Pages: []*Page{{Children: m.Children}}})
			if err == nil {
				kids := copyOf.Pages[0].Children
				for i, c := range kids {
					kids[i] = r.node(c, n.Overrides, depth+1)
				}
				n.Children = kids
			}
		}
		return n
	}
	for i, c := range n.Children {
		n.Children[i] = r.node(c, over, depth)
	}
	return n
}

func applyOverride(n *Node, o *Override) {
	if o.Visible != nil {
		n.Visible = o.Visible
	}
	if o.Opacity != nil {
		n.Opacity = o.Opacity
	}
	if o.Fills != nil {
		n.Fills = *o.Fills
	}
	if o.Strokes != nil {
		n.Strokes = *o.Strokes
	}
	if o.Effects != nil {
		n.Effects = *o.Effects
	}
	if o.Text != nil && n.Type == TypeText {
		t := *o.Text
		n.Text = &t
	}
	if o.Font != nil && n.Font != nil {
		f := *n.Font
		if o.Font.Family != "" {
			f.Family = o.Font.Family
		}
		if o.Font.Size != 0 {
			f.Size = o.Font.Size
		}
		if o.Font.Weight != 0 {
			f.Weight = o.Font.Weight
		}
		if o.Font.Style != "" {
			f.Style = o.Font.Style
		}
		n.Font = &f
	}
	if o.Radius != nil {
		n.Radius = o.Radius
	}
	if o.FillStyle != "" {
		n.FillStyle = o.FillStyle
	}
	if o.TextStyle != "" {
		n.TextStyle = o.TextStyle
	}
	if o.EffectStyle != "" {
		n.EffectStyle = o.EffectStyle
	}
}
