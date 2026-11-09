// Package design reads, edits and writes Atlas design files: pages of frames,
// shapes, text, images, groups, components and instances, stored as JSON in
// the project under .atlas/design/files. The desktop app's design tool
// (app/renderer/src/features/design/model.ts) edits the same format, so the
// agent and the person work on one file. Keep the two in step.
package design

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
)

// Format and Version identify a design file.
const (
	Format  = "atlas-design"
	Version = 1
)

// Limits of a document.
const (
	MaxDepth = 64
	MaxNodes = 20000
	limit    = 1_000_000
)

// NodeType is the kind of a node.
type NodeType string

// Node types.
const (
	TypeFrame     NodeType = "frame"
	TypeGroup     NodeType = "group"
	TypeRect      NodeType = "rect"
	TypeEllipse   NodeType = "ellipse"
	TypeLine      NodeType = "line"
	TypePolygon   NodeType = "polygon"
	TypeStar      NodeType = "star"
	TypePath      NodeType = "path"
	TypeText      NodeType = "text"
	TypeImage     NodeType = "image"
	TypeComponent NodeType = "component"
	TypeInstance  NodeType = "instance"
)

var nodeTypes = map[NodeType]bool{
	TypeFrame: true, TypeGroup: true, TypeRect: true, TypeEllipse: true, TypeLine: true, TypePolygon: true,
	TypeStar: true, TypePath: true, TypeText: true, TypeImage: true, TypeComponent: true, TypeInstance: true,
}

// IsContainer reports whether nodes of this type hold children.
func (t NodeType) IsContainer() bool {
	return t == TypeFrame || t == TypeGroup || t == TypeComponent
}

// Stop is one colour of a gradient.
type Stop struct {
	Offset float64 `json:"offset"`
	Color  string  `json:"color"`
}

// Paint fills a shape: a solid colour or a linear or radial gradient.
type Paint struct {
	Type    string   `json:"type"`
	Color   string   `json:"color,omitempty"`
	Stops   []Stop   `json:"stops,omitempty"`
	Angle   *float64 `json:"angle,omitempty"`
	Opacity *float64 `json:"opacity,omitempty"`
	Visible *bool    `json:"visible,omitempty"`
	// Variable names a colour variable that sets Color in the active mode.
	Variable string `json:"variable,omitempty"`
	// Src and Fit are an image paint's picture and how it fills the shape.
	Src string `json:"src,omitempty"`
	Fit string `json:"fit,omitempty"`
}

// Stroke outlines a shape.
type Stroke struct {
	Color   string    `json:"color"`
	Width   float64   `json:"width"`
	Align   string    `json:"align,omitempty"`
	Dash    []float64 `json:"dash,omitempty"`
	Opacity *float64  `json:"opacity,omitempty"`
	Visible *bool     `json:"visible,omitempty"`
	// Variable names a colour variable that sets Color in the active mode.
	Variable string `json:"variable,omitempty"`
}

// Effect is a shadow or a blur.
type Effect struct {
	Type    string   `json:"type"`
	X       *float64 `json:"x,omitempty"`
	Y       *float64 `json:"y,omitempty"`
	Blur    float64  `json:"blur"`
	Spread  *float64 `json:"spread,omitempty"`
	Color   string   `json:"color,omitempty"`
	Visible *bool    `json:"visible,omitempty"`
}

// Font is a text node's typeface.
type Font struct {
	Family string  `json:"family"`
	Size   float64 `json:"size"`
	Weight int     `json:"weight"`
	Style  string  `json:"style,omitempty"`
}

// Radius is a corner radius: one for all corners, or four (top left,
// top right, bottom right, bottom left).
type Radius struct {
	Corners [4]float64
	Each    bool
}

// MarshalJSON writes one number or four.
func (r Radius) MarshalJSON() ([]byte, error) {
	if r.Each {
		return json.Marshal(r.Corners)
	}
	return json.Marshal(r.Corners[0])
}

// UnmarshalJSON reads one number or four.
func (r *Radius) UnmarshalJSON(data []byte) error {
	var one float64
	if err := json.Unmarshal(data, &one); err == nil {
		*r = Radius{Corners: [4]float64{one, one, one, one}}
		return nil
	}
	var four [4]float64
	if err := json.Unmarshal(data, &four); err != nil {
		return fmt.Errorf("radius must be a number or four numbers")
	}
	*r = Radius{Corners: four, Each: true}
	return nil
}

// Node is one layer. X and Y are relative to the parent's top left corner,
// before rotation; Rotation is in degrees, clockwise, about the center.
type Node struct {
	ID       string   `json:"id"`
	Type     NodeType `json:"type"`
	Name     string   `json:"name"`
	X        float64  `json:"x"`
	Y        float64  `json:"y"`
	Width    float64  `json:"width"`
	Height   float64  `json:"height"`
	Rotation float64  `json:"rotation,omitempty"`
	Opacity  *float64 `json:"opacity,omitempty"`
	Visible  *bool    `json:"visible,omitempty"`
	Locked   bool     `json:"locked,omitempty"`
	Blend    string   `json:"blend,omitempty"`
	Fills    []Paint  `json:"fills,omitempty"`
	Strokes  []Stroke `json:"strokes,omitempty"`
	Effects  []Effect `json:"effects,omitempty"`
	Radius   *Radius  `json:"radius,omitempty"`
	Children []*Node  `json:"children,omitempty"`
	Clip     *bool    `json:"clip,omitempty"`

	Layout      *Layout      `json:"layout,omitempty"`
	SizingX     string       `json:"sizing_x,omitempty"`
	SizingY     string       `json:"sizing_y,omitempty"`
	Absolute    bool         `json:"absolute,omitempty"`
	Constraints *Constraints `json:"constraints,omitempty"`

	Text          *string  `json:"text,omitempty"`
	Font          *Font    `json:"font,omitempty"`
	LineHeight    *float64 `json:"line_height,omitempty"`
	LetterSpacing *float64 `json:"letter_spacing,omitempty"`
	Align         string   `json:"align,omitempty"`
	VAlign        string   `json:"valign,omitempty"`
	AutoResize    string   `json:"auto_resize,omitempty"`
	Decoration    string   `json:"decoration,omitempty"`
	Transform     string   `json:"transform,omitempty"`

	Src string `json:"src,omitempty"`
	Fit string `json:"fit,omitempty"`

	D       string      `json:"d,omitempty"`
	ViewBox *[2]float64 `json:"view_box,omitempty"`

	Sides *int     `json:"sides,omitempty"`
	Inner *float64 `json:"inner,omitempty"`

	ComponentID string `json:"component_id,omitempty"`

	FillStyle    string               `json:"fill_style,omitempty"`
	TextStyle    string               `json:"text_style,omitempty"`
	EffectStyle  string               `json:"effect_style,omitempty"`
	Scroll       string               `json:"scroll,omitempty"`
	FlowStart    bool                 `json:"flow_start,omitempty"`
	Interactions []*Interaction       `json:"interactions,omitempty"`
	BoolOp       string               `json:"bool_op,omitempty"`
	Mask         bool                 `json:"mask,omitempty"`
	Set          string               `json:"set,omitempty"`
	Variant      map[string]string    `json:"variant,omitempty"`
	Overrides    map[string]*Override `json:"overrides,omitempty"`
}

// Page is one canvas of a document.
type Page struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Background string  `json:"background"`
	Children   []*Node `json:"children"`
}

// Doc is a design file.
type Doc struct {
	Format  string  `json:"format"`
	Version int     `json:"version"`
	Name    string  `json:"name"`
	Pages   []*Page `json:"pages"`

	Comments  []Comment  `json:"comments,omitempty"`
	Styles    *Styles    `json:"styles,omitempty"`
	Variables *Variables `json:"variables,omitempty"`
}

// IsVisible reports whether the node is drawn.
func (n *Node) IsVisible() bool { return n.Visible == nil || *n.Visible }

// TextValue is a text node's text.
func (n *Node) TextValue() string {
	if n.Text == nil {
		return ""
	}
	return *n.Text
}

var (
	idPattern  = regexp.MustCompile(`^[\w:-]{1,64}$`)
	hexPattern = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{4}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$`)
)

// ValidID reports whether s can be a node or page id.
func ValidID(s string) bool { return idPattern.MatchString(s) }

// IsColor reports whether s is #RGB, #RGBA, #RRGGBB or #RRGGBBAA.
func IsColor(s string) bool { return hexPattern.MatchString(s) }

// NewID returns a short random id.
func NewID() string {
	const digits = "0123456789abcdefghijklmnopqrstuvwxyz"
	var b [8]byte
	_, _ = rand.Read(b[:])
	out := make([]byte, len(b))
	for i, v := range b {
		out[i] = digits[int(v)%len(digits)]
	}
	return string(out)
}

// DefaultName is the name a new node of a type gets.
func DefaultName(t NodeType) string {
	switch t {
	case TypeRect:
		return "Rectangle"
	case TypePath:
		return "Vector"
	default:
		s := string(t)
		return strings.ToUpper(s[:1]) + s[1:]
	}
}

// New returns an empty document with one page.
func New(name string) *Doc {
	if strings.TrimSpace(name) == "" {
		name = "Untitled"
	}
	return &Doc{Format: Format, Version: Version, Name: name, Pages: []*Page{{ID: NewID(), Name: "Page 1", Background: "#F5F5F5", Children: []*Node{}}}}
}

func clamp(v, lo, hi float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return lo
	}
	return math.Max(lo, math.Min(hi, v))
}

func clampPtr(p *float64, lo, hi float64) *float64 {
	if p == nil {
		return nil
	}
	v := clamp(*p, lo, hi)
	return &v
}

func color(s, fallback string) string {
	if IsColor(s) {
		return strings.ToUpper(s)
	}
	return fallback
}

func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	// Cut on a rune boundary.
	for n > 0 && !utf8Start(s[n]) {
		n--
	}
	return s[:n]
}

func utf8Start(b byte) bool { return b&0xC0 != 0x80 }

type sanitizer struct {
	seen  map[string]bool
	count int
}

func (s *sanitizer) id(id string) string {
	if !ValidID(id) || s.seen[id] {
		id = NewID()
		for s.seen[id] {
			id = NewID()
		}
	}
	s.seen[id] = true
	return id
}

func sanitizePaints(in []Paint) []Paint {
	if in == nil {
		return nil
	}
	out := make([]Paint, 0, len(in))
	for _, p := range in {
		if len(out) == 16 {
			break
		}
		p.Opacity = clampPtr(p.Opacity, 0, 1)
		if p.Visible != nil && *p.Visible {
			p.Visible = nil
		}
		switch p.Type {
		case "linear", "radial":
			var stops []Stop
			for _, s := range p.Stops {
				if len(stops) == 16 {
					break
				}
				stops = append(stops, Stop{Offset: clamp(s.Offset, 0, 1), Color: color(s.Color, "#000000")})
			}
			if len(stops) < 2 {
				continue
			}
			p.Stops = stops
			p.Color = ""
			p.Angle = clampPtr(p.Angle, -360, 360)
		case "image":
			if p.Src == "" {
				continue
			}
			p.Src = cut(p.Src, 16<<20)
			p.Fit = oneOf(p.Fit, "", "contain", "stretch")
			p.Color, p.Stops, p.Angle, p.Variable = "", nil, nil, ""
		case "solid", "":
			if p.Type == "" && !IsColor(p.Color) {
				continue
			}
			p.Type = "solid"
			p.Color = color(p.Color, "#000000")
			p.Stops = nil
			p.Angle = nil
			if !validRef(p.Variable) {
				p.Variable = ""
			}
		default:
			continue
		}
		out = append(out, p)
	}
	return out
}

func sanitizeStrokes(in []Stroke) []Stroke {
	if in == nil {
		return nil
	}
	out := make([]Stroke, 0, len(in))
	for _, s := range in {
		if len(out) == 8 {
			break
		}
		s.Color = color(s.Color, "#000000")
		s.Width = clamp(s.Width, 0, 1000)
		if s.Align != "inside" && s.Align != "outside" && s.Align != "center" {
			s.Align = ""
		}
		var dash []float64
		for _, d := range s.Dash {
			if d >= 0 && !math.IsInf(d, 0) && len(dash) < 8 {
				dash = append(dash, d)
			}
		}
		s.Dash = dash
		s.Opacity = clampPtr(s.Opacity, 0, 1)
		if s.Visible != nil && *s.Visible {
			s.Visible = nil
		}
		out = append(out, s)
	}
	return out
}

func sanitizeEffects(in []Effect) []Effect {
	if in == nil {
		return nil
	}
	out := make([]Effect, 0, len(in))
	for _, e := range in {
		if len(out) == 8 {
			break
		}
		if e.Type != "drop-shadow" && e.Type != "inner-shadow" && e.Type != "blur" {
			continue
		}
		e.Blur = clamp(e.Blur, 0, 1000)
		if e.Type == "blur" {
			e.X, e.Y, e.Spread, e.Color = nil, nil, nil, ""
		} else {
			zero := 0.0
			if e.X == nil {
				e.X = &zero
			}
			if e.Y == nil {
				e.Y = &zero
			}
			if e.Spread == nil {
				e.Spread = &zero
			}
			e.X = clampPtr(e.X, -10000, 10000)
			e.Y = clampPtr(e.Y, -10000, 10000)
			e.Spread = clampPtr(e.Spread, -1000, 1000)
			e.Color = color(e.Color, "#00000040")
		}
		if e.Visible != nil && *e.Visible {
			e.Visible = nil
		}
		out = append(out, e)
	}
	return out
}

func (s *sanitizer) node(n *Node, depth int) *Node {
	if n == nil || depth > MaxDepth || s.count >= MaxNodes || !nodeTypes[n.Type] {
		return nil
	}
	s.count++
	n.ID = s.id(n.ID)
	n.Name = cut(n.Name, 200)
	if n.Name == "" {
		n.Name = DefaultName(n.Type)
	}
	n.X = clamp(n.X, -limit, limit)
	n.Y = clamp(n.Y, -limit, limit)
	n.Width = clamp(n.Width, 0, limit)
	n.Height = clamp(n.Height, 0, limit)
	if n.Type == TypeLine {
		n.Height = 0
	}
	n.Rotation = math.Mod(math.Mod(clamp(n.Rotation, -3600, 3600), 360)+360, 360)
	if math.Abs(n.Rotation) < 1e-9 || math.Abs(n.Rotation-360) < 1e-9 {
		n.Rotation = 0
	}
	n.Opacity = clampPtr(n.Opacity, 0, 1)
	if n.Visible != nil && *n.Visible {
		n.Visible = nil
	}
	if n.Blend == "normal" || len(n.Blend) > 20 {
		n.Blend = ""
	}
	n.Fills = sanitizePaints(n.Fills)
	n.Strokes = sanitizeStrokes(n.Strokes)
	n.Effects = sanitizeEffects(n.Effects)
	if n.Radius != nil {
		for i := range n.Radius.Corners {
			n.Radius.Corners[i] = clamp(n.Radius.Corners[i], 0, limit)
		}
	}
	if n.Type == TypeFrame || n.Type == TypeComponent {
		n.Layout = sanitizeLayout(n.Layout)
	} else {
		n.Layout = nil
	}
	n.SizingX = oneOf(n.SizingX, "", "hug", "fill")
	n.SizingY = oneOf(n.SizingY, "", "hug", "fill")
	n.Constraints = sanitizeConstraints(n.Constraints)
	if !validRef(n.FillStyle) {
		n.FillStyle = ""
	}
	if !validRef(n.EffectStyle) {
		n.EffectStyle = ""
	}
	if !validRef(n.TextStyle) || n.Type != TypeText {
		n.TextStyle = ""
	}
	if n.Type == TypeComponent || n.Type == TypeInstance {
		n.Variant = sanitizeVariant(n.Variant)
	} else {
		n.Variant = nil
	}
	if n.Type == TypeFrame || n.Type == TypeComponent {
		n.Scroll = oneOf(n.Scroll, "", "vertical", "horizontal", "both")
	} else {
		n.Scroll = ""
	}
	n.FlowStart = n.FlowStart && n.Type == TypeFrame
	n.Interactions = sanitizeInteractions(n.Interactions)
	n.BoolOp = oneOf(n.BoolOp, "", "union", "subtract", "intersect", "exclude")
	if n.Type != TypeGroup {
		n.BoolOp = ""
	}
	if n.Type == TypeText {
		n.Mask = false
	}
	n.Set = cut(n.Set, 80)
	if n.Type != TypeComponent {
		n.Set = ""
	}
	if n.Type == TypeInstance {
		n.Overrides = sanitizeOverrides(n.Overrides)
	} else {
		n.Overrides = nil
	}
	if n.Type.IsContainer() {
		kids := make([]*Node, 0, len(n.Children))
		for _, c := range n.Children {
			if c = s.node(c, depth+1); c != nil {
				kids = append(kids, c)
			}
		}
		n.Children = kids
		if n.Type != TypeGroup {
			clip := n.Clip == nil || *n.Clip
			n.Clip = &clip
		} else {
			n.Clip = nil
		}
	} else {
		n.Children = nil
		n.Clip = nil
	}
	if n.Type == TypeText {
		text := cut(n.TextValue(), 100_000)
		n.Text = &text
		f := Font{Family: "Inter", Size: 16, Weight: 400}
		if n.Font != nil {
			f = *n.Font
		}
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
		n.Font = &f
		n.LineHeight = clampPtr(n.LineHeight, 0, 10000)
		n.LetterSpacing = clampPtr(n.LetterSpacing, -1000, 1000)
		if n.Align != "center" && n.Align != "right" {
			n.Align = ""
		}
		if n.VAlign != "middle" && n.VAlign != "bottom" {
			n.VAlign = ""
		}
		if n.AutoResize != "height" && n.AutoResize != "none" {
			n.AutoResize = "width"
		}
		if n.Decoration != "underline" && n.Decoration != "strike" {
			n.Decoration = ""
		}
		if n.Transform != "upper" && n.Transform != "lower" {
			n.Transform = ""
		}
		if n.Fills == nil {
			n.Fills = []Paint{{Type: "solid", Color: "#000000"}}
		}
	} else {
		n.Text, n.Font, n.LineHeight, n.LetterSpacing = nil, nil, nil, nil
		n.Align, n.VAlign, n.AutoResize, n.Decoration, n.Transform = "", "", "", "", ""
	}
	if n.Type == TypeImage {
		n.Src = cut(n.Src, 16<<20)
		if n.Fit != "contain" && n.Fit != "stretch" {
			n.Fit = "cover"
		}
	} else {
		n.Src, n.Fit = "", ""
	}
	if n.Type == TypePath {
		n.D = cut(n.D, 1<<20)
		vb := [2]float64{math.Max(n.Width, 1), math.Max(n.Height, 1)}
		if n.ViewBox != nil {
			vb = *n.ViewBox
		}
		vb[0] = clamp(vb[0], 0.0001, limit)
		vb[1] = clamp(vb[1], 0.0001, limit)
		n.ViewBox = &vb
	} else {
		n.D, n.ViewBox = "", nil
	}
	if n.Type == TypePolygon || n.Type == TypeStar {
		sides := 3
		if n.Type == TypeStar {
			sides = 5
		}
		if n.Sides != nil {
			sides = int(clamp(float64(*n.Sides), 3, 100))
		}
		n.Sides = &sides
		if n.Type == TypeStar {
			inner := 0.38
			if n.Inner != nil {
				inner = clamp(*n.Inner, 0.01, 1)
			}
			n.Inner = &inner
		} else {
			n.Inner = nil
		}
	} else {
		n.Sides, n.Inner = nil, nil
	}
	if n.Type == TypeInstance {
		n.ComponentID = cut(n.ComponentID, 64)
	} else {
		n.ComponentID = ""
	}
	return n
}

// Sanitize repairs a document in place: unknown node types are dropped,
// ids made valid and unique, numbers clamped and colours normalized. It
// returns the document for chaining.
func Sanitize(d *Doc) *Doc {
	if d == nil {
		return New("")
	}
	d.Format = Format
	d.Version = Version
	d.Name = cut(d.Name, 200)
	if d.Name == "" {
		d.Name = "Untitled"
	}
	s := &sanitizer{seen: map[string]bool{}}
	pages := make([]*Page, 0, len(d.Pages))
	for i, p := range d.Pages {
		if p == nil || len(pages) == 200 {
			continue
		}
		p.ID = s.id(p.ID)
		p.Name = cut(p.Name, 200)
		if p.Name == "" {
			p.Name = fmt.Sprintf("Page %d", i+1)
		}
		p.Background = color(p.Background, "#F5F5F5")
		kids := make([]*Node, 0, len(p.Children))
		for _, c := range p.Children {
			if c = s.node(c, 0); c != nil {
				kids = append(kids, c)
			}
		}
		p.Children = kids
		pages = append(pages, p)
	}
	if len(pages) == 0 {
		pages = New("").Pages
	}
	d.Pages = pages
	sanitizeComments(d)
	d.Styles = sanitizeStyles(d.Styles)
	d.Variables = sanitizeVariables(d.Variables)
	return d
}

// Parse reads and sanitizes a document from JSON.
func Parse(data []byte) (*Doc, error) {
	var d Doc
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("not a design file: %w", err)
	}
	if d.Format != "" && d.Format != Format {
		return nil, fmt.Errorf("not a design file: format is %q", d.Format)
	}
	return Sanitize(&d), nil
}

// Marshal writes a document as indented JSON.
func Marshal(d *Doc) ([]byte, error) {
	data, err := json.MarshalIndent(d, "", " ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// Walk visits every node of the given list depth first. visit returns false
// to skip a node's children.
func Walk(nodes []*Node, visit func(n *Node, parent *Node, depth int) bool) {
	var rec func([]*Node, *Node, int)
	rec = func(list []*Node, parent *Node, depth int) {
		for _, n := range list {
			if visit(n, parent, depth) {
				rec(n.Children, n, depth+1)
			}
		}
	}
	rec(nodes, nil, 0)
}

// Located is a node with where it sits.
type Located struct {
	Page   *Page
	Node   *Node
	Parent *Node
	Index  int
	// Path holds the node's ancestors, page child first, and the node last.
	Path []*Node
}

// Find looks a node up by id on every page.
func (d *Doc) Find(id string) *Located {
	for _, p := range d.Pages {
		var found *Located
		var rec func(list []*Node, parent *Node, path []*Node) bool
		rec = func(list []*Node, parent *Node, path []*Node) bool {
			for i, n := range list {
				here := append(append([]*Node{}, path...), n)
				if n.ID == id {
					found = &Located{Page: p, Node: n, Parent: parent, Index: i, Path: here}
					return true
				}
				if rec(n.Children, n, here) {
					return true
				}
			}
			return false
		}
		if rec(p.Children, nil, nil) {
			return found
		}
	}
	return nil
}

// PageByRef finds a page by id or by name (case-insensitive); empty means the first.
func (d *Doc) PageByRef(ref string) *Page {
	if ref == "" {
		return d.Pages[0]
	}
	for _, p := range d.Pages {
		if p.ID == ref {
			return p
		}
	}
	for _, p := range d.Pages {
		if strings.EqualFold(p.Name, ref) {
			return p
		}
	}
	return nil
}

// Count is the number of nodes in the document.
func (d *Doc) Count() int {
	total := 0
	for _, p := range d.Pages {
		Walk(p.Children, func(*Node, *Node, int) bool { total++; return true })
	}
	return total
}
