package design

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// Op is one edit of a document. Ops apply in order; a batch either applies
// whole or not at all.
type Op struct {
	// Op is add, update, delete, move, make_component, add_page,
	// update_page, delete_page, set_style, delete_style, set_variable,
	// delete_variable, set_modes, add_comment, reply_comment,
	// resolve_comment or delete_comment.
	Op string `json:"op"`
	// ID is the node an update, delete, move or make_component acts on.
	ID string `json:"id,omitempty"`
	// Parent is the frame, group or component an add or move puts the node
	// in; empty means the page.
	Parent string `json:"parent,omitempty"`
	// Index is the place among the parent's children (0 is the back); none
	// means the front.
	Index *int `json:"index,omitempty"`
	// Node is the node an add creates, children included.
	Node json.RawMessage `json:"node,omitempty"`
	// Props are the fields an update changes; "font" merges into the font.
	Props map[string]json.RawMessage `json:"props,omitempty"`
	// Page is the page an op works on (id or name); empty means the first,
	// or the one the batch is for.
	Page string `json:"page,omitempty"`
	// Name and Background are a new or changed page's.
	Name       string `json:"name,omitempty"`
	Background string `json:"background,omitempty"`
	// Kind is the style kind (fill, text or effect) of set_style and
	// delete_style.
	Kind string `json:"kind,omitempty"`
	// Modes and Mode are set_modes': the variable modes, and the active one.
	Modes []string `json:"modes,omitempty"`
	Mode  string   `json:"mode,omitempty"`
	// Text, Author, NodeID, X, Y and Resolved are the comment ops'.
	Text     string   `json:"text,omitempty"`
	Author   string   `json:"author,omitempty"`
	NodeID   string   `json:"node_id,omitempty"`
	X        *float64 `json:"x,omitempty"`
	Y        *float64 `json:"y,omitempty"`
	Resolved *bool    `json:"resolved,omitempty"`
	// Block is the kit block an insert places; Props fill its slots, ID
	// names its root (and prefixes its layers' ids), Parent, Index, X and Y
	// say where it goes.
	Block string `json:"block,omitempty"`
	// Accent is set_theme's accent hue (a Radix colour name); Name is the
	// theme id and Mode the mode to show. Color, when set, derives a custom
	// theme from that brand colour instead of a built-in theme.
	Accent string `json:"accent,omitempty"`
	Color  string `json:"color,omitempty"`
}

// Result says what a batch did.
type Result struct {
	// Created maps each op index that made a node or page to its id.
	Created map[int]string
	// Touched lists the ids of nodes the batch made or changed.
	Touched []string
	// Notes are warnings worth reading, such as text that overflows its box.
	Notes []string
}

func clone(d *Doc) (*Doc, error) {
	data, err := json.Marshal(d)
	if err != nil {
		return nil, err
	}
	var out Doc
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (d *Doc) ids() map[string]bool {
	seen := map[string]bool{}
	for _, p := range d.Pages {
		seen[p.ID] = true
		Walk(p.Children, func(n *Node, _ *Node, _ int) bool { seen[n.ID] = true; return true })
	}
	return seen
}

func insert(list []*Node, at *int, nodes ...*Node) []*Node {
	i := len(list)
	if at != nil {
		i = max(0, min(len(list), *at))
	}
	out := make([]*Node, 0, len(list)+len(nodes))
	out = append(out, list[:i]...)
	out = append(out, nodes...)
	return append(out, list[i:]...)
}

func remove(list []*Node, id string) []*Node {
	out := list[:0:0]
	for _, n := range list {
		if n.ID != id {
			out = append(out, n)
		}
	}
	return out
}

// newNode builds a node from an add op's JSON, filling in what a drawing
// tool would: a grey fill for shapes, white for frames, black text.
func newNode(raw json.RawMessage, seen map[string]bool, depth int) (*Node, error) {
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		return nil, fmt.Errorf("node must be a JSON object: %w", err)
	}
	var n Node
	if err := json.Unmarshal(raw, &n); err != nil {
		return nil, fmt.Errorf("node: %w", err)
	}
	if !nodeTypes[n.Type] {
		return nil, fmt.Errorf("node type %q is not one of frame, group, rect, ellipse, line, polygon, star, path, text, image, component, instance", n.Type)
	}
	if depth > MaxDepth {
		return nil, fmt.Errorf("nodes are nested deeper than %d", MaxDepth)
	}
	if n.ID != "" && !ValidID(n.ID) {
		return nil, fmt.Errorf("id %q: use letters, digits, '_', ':' and '-', up to 64", n.ID)
	}
	if n.ID != "" && seen[n.ID] {
		return nil, fmt.Errorf("id %q is already used in this design", n.ID)
	}
	if n.ID == "" {
		n.ID = NewID()
		for seen[n.ID] {
			n.ID = NewID()
		}
	}
	seen[n.ID] = true
	if raw, ok := keys["icon"]; ok {
		if err := iconNode(&n, raw, keys); err != nil {
			return nil, err
		}
	}
	_, hasFills := keys["fills"]
	_, hasStrokes := keys["strokes"]
	_, hasW := keys["width"]
	_, hasH := keys["height"]
	if _, icon := keys["icon"]; icon {
		hasW, hasH, hasFills, hasStrokes = true, true, true, true
	}
	switch n.Type {
	case TypeFrame, TypeComponent:
		if !hasFills {
			n.Fills = []Paint{{Type: "solid", Color: "#FFFFFF"}}
		}
	case TypeRect, TypeEllipse, TypePolygon, TypeStar, TypePath:
		if !hasFills {
			n.Fills = []Paint{{Type: "solid", Color: "#D9D9D9"}}
		}
	case TypeLine:
		if !hasStrokes {
			n.Strokes = []Stroke{{Color: "#000000", Width: 1}}
		}
	}
	if !hasW && n.Type != TypeText {
		n.Width = 100
	}
	if !hasH && n.Type != TypeText && n.Type != TypeLine {
		n.Height = 100
	}
	// Children are made the same way, so their ids are checked too.
	var rawKids []json.RawMessage
	if k, ok := keys["children"]; ok {
		if err := json.Unmarshal(k, &rawKids); err != nil {
			return nil, fmt.Errorf("children must be a list of nodes: %w", err)
		}
	}
	n.Children = nil
	if len(rawKids) > 0 && !n.Type.IsContainer() {
		return nil, fmt.Errorf("a %s cannot hold children; use a frame or group", n.Type)
	}
	for i, k := range rawKids {
		child, err := newNode(k, seen, depth+1)
		if err != nil {
			return nil, fmt.Errorf("child %d of %q: %w", i, n.Name, err)
		}
		n.Children = append(n.Children, child)
	}
	if n.Type.IsContainer() && n.Children == nil {
		n.Children = []*Node{}
	}
	return &n, nil
}

// iconNode makes a node an icon of the kit's set: {"icon":"heart"} becomes a
// stroked path in the theme's text colour, 24px unless a size is given.
func iconNode(n *Node, raw json.RawMessage, keys map[string]json.RawMessage) error {
	var name string
	if err := json.Unmarshal(raw, &name); err != nil {
		return fmt.Errorf("icon must be an icon name")
	}
	ic, err := LookupIcon(name)
	if err != nil {
		return err
	}
	n.Type = TypePath
	n.D = ic.D
	n.ViewBox = &[2]float64{24, 24}
	if n.Name == "" {
		n.Name = "Icon/" + ic.Name
	}
	size := 24.0
	if _, ok := keys["width"]; ok && n.Width > 0 {
		size = n.Width
	} else if _, ok := keys["height"]; ok && n.Height > 0 {
		size = n.Height
	}
	n.Width, n.Height = size, size
	if _, ok := keys["fills"]; !ok {
		n.Fills = []Paint{}
	}
	if _, ok := keys["strokes"]; !ok {
		n.Strokes = []Stroke{{Color: "#000000", Variable: colorVar("text"), Width: iconStroke(size), Cap: "round", Join: "round"}}
	}
	for i := range n.Strokes {
		if n.Strokes[i].Cap == "" {
			n.Strokes[i].Cap, n.Strokes[i].Join = "round", "round"
		}
	}
	return nil
}

// Apply runs a batch of ops on the document's page `page` (id or name;
// empty is the first). The document is changed only when every op works.
func Apply(d *Doc, page string, ops []Op) (*Doc, Result, error) {
	work, err := clone(d)
	if err != nil {
		return nil, Result{}, err
	}
	res := Result{Created: map[int]string{}}
	touched := map[string]bool{}
	seen := work.ids()
	for i, op := range ops {
		if err := work.apply(i, op, page, seen, &res, touched); err != nil {
			return nil, Result{}, fmt.Errorf("op %d (%s): %w", i, op.Op, err)
		}
	}
	Sanitize(work)
	if work.Count() > MaxNodes {
		return nil, Result{}, fmt.Errorf("the design would have more than %d nodes", MaxNodes)
	}
	for _, p := range work.Pages {
		FitGroups(p.Children)
	}
	ResolveLayout(work, d)
	for _, p := range work.Pages {
		FitGroups(p.Children)
		Walk(p.Children, func(n *Node, _ *Node, _ int) bool {
			if touched[n.ID] {
				res.Touched = append(res.Touched, n.ID)
				if note := overflowNote(n); note != "" {
					res.Notes = append(res.Notes, note)
				}
			}
			return true
		})
	}
	return work, res, nil
}

func (d *Doc) pageFor(op Op, batch string) (*Page, error) {
	ref := op.Page
	if ref == "" {
		ref = batch
	}
	p := d.PageByRef(ref)
	if p == nil {
		return nil, fmt.Errorf("no page %q", ref)
	}
	return p, nil
}

func (d *Doc) children(p *Page, parent string) (*[]*Node, *Node, error) {
	if parent == "" {
		return &p.Children, nil, nil
	}
	at := d.Find(parent)
	if at == nil {
		return nil, nil, fmt.Errorf("no node %q", parent)
	}
	if !at.Node.Type.IsContainer() {
		return nil, nil, fmt.Errorf("%q is a %s; only frames, groups and components hold children", parent, at.Node.Type)
	}
	return &at.Node.Children, at.Node, nil
}

func (d *Doc) apply(i int, op Op, batch string, seen map[string]bool, res *Result, touched map[string]bool) error {
	switch op.Op {
	case "add":
		if len(op.Node) == 0 {
			return fmt.Errorf("add needs a node")
		}
		p, err := d.pageFor(op, batch)
		if err != nil {
			return err
		}
		list, holder, err := d.children(p, op.Parent)
		if err != nil {
			return err
		}
		if holder != nil && d.Find(holder.ID).Page != p && op.Page != "" {
			return fmt.Errorf("parent %q is on another page", op.Parent)
		}
		n, err := newNode(op.Node, seen, 0)
		if err != nil {
			return err
		}
		if n.Type == TypeInstance {
			master := d.Find(n.ComponentID)
			if master == nil || master.Node.Type != TypeComponent {
				return fmt.Errorf("instance needs component_id of a component; %q is not one", n.ComponentID)
			}
			var keys map[string]json.RawMessage
			_ = json.Unmarshal(op.Node, &keys)
			if _, ok := keys["width"]; !ok {
				n.Width = master.Node.Width
			}
			if _, ok := keys["height"]; !ok {
				n.Height = master.Node.Height
			}
			if _, ok := keys["fills"]; !ok {
				n.Fills = master.Node.Fills
			}
			if n.Name == "" {
				n.Name = master.Node.Name
			}
		}
		Walk([]*Node{n}, func(c *Node, _ *Node, _ int) bool {
			if c.Type == TypeText {
				Measure(c)
			}
			touched[c.ID] = true
			return true
		})
		*list = insert(*list, op.Index, n)
		res.Created[i] = n.ID
	case "update":
		at := d.Find(op.ID)
		if at == nil {
			return fmt.Errorf("no node %q", op.ID)
		}
		if len(op.Props) == 0 {
			return fmt.Errorf("update needs props")
		}
		next, err := merge(at.Node, op.Props)
		if err != nil {
			return err
		}
		if next.Type == TypeText {
			if _, w := op.Props["width"]; w && next.AutoResize == "width" {
				if _, set := op.Props["auto_resize"]; !set {
					next.AutoResize = "height"
				}
			}
			Measure(next)
		}
		if _, width := op.Props["width"]; width || op.Props["height"] != nil {
			if owner := kitOwner(at.Path); owner != nil && owner.ID != at.Node.ID {
				res.Notes = append(res.Notes, fmt.Sprintf("update of %q changes the size of a layer inside block %q; prefer set_block_props, a fixed child taller than its row is what clips text", at.Node.ID, owner.Kit.ID))
			}
		}
		*at.Node = *next
		touched[op.ID] = true
	case "delete":
		at := d.Find(op.ID)
		if at == nil {
			return fmt.Errorf("no node %q", op.ID)
		}
		if at.Parent != nil {
			at.Parent.Children = remove(at.Parent.Children, op.ID)
		} else {
			at.Page.Children = remove(at.Page.Children, op.ID)
		}
	case "move":
		at := d.Find(op.ID)
		if at == nil {
			return fmt.Errorf("no node %q", op.ID)
		}
		p := at.Page
		if op.Page != "" {
			if p = d.PageByRef(op.Page); p == nil {
				return fmt.Errorf("no page %q", op.Page)
			}
		}
		if op.Parent != "" {
			target := d.Find(op.Parent)
			if target == nil {
				return fmt.Errorf("no node %q", op.Parent)
			}
			for _, a := range target.Path {
				if a.ID == op.ID {
					return fmt.Errorf("cannot move %q into itself", op.ID)
				}
			}
			p = target.Page
		}
		// Keep the node where it is on the page while its parent changes.
		oldOrigin := worldOrigin(at.Path[:len(at.Path)-1])
		if at.Parent != nil {
			at.Parent.Children = remove(at.Parent.Children, op.ID)
		} else {
			at.Page.Children = remove(at.Page.Children, op.ID)
		}
		list, holder, err := d.children(p, op.Parent)
		if err != nil {
			return err
		}
		if holder == nil || at.Parent == nil || holder.ID != at.Parent.ID {
			newOrigin := [2]float64{}
			if holder != nil {
				newOrigin = worldOrigin(d.Find(holder.ID).Path)
			}
			at.Node.X += oldOrigin[0] - newOrigin[0]
			at.Node.Y += oldOrigin[1] - newOrigin[1]
		}
		*list = insert(*list, op.Index, at.Node)
		touched[op.ID] = true
	case "make_component":
		at := d.Find(op.ID)
		if at == nil {
			return fmt.Errorf("no node %q", op.ID)
		}
		if at.Node.Type != TypeFrame && at.Node.Type != TypeGroup {
			return fmt.Errorf("only a frame or group becomes a component; %q is a %s", op.ID, at.Node.Type)
		}
		if at.Node.Type == TypeGroup {
			no := false
			at.Node.Clip = &no
		}
		at.Node.Type = TypeComponent
		touched[op.ID] = true
	case "add_page":
		name := strings.TrimSpace(op.Name)
		if name == "" {
			name = fmt.Sprintf("Page %d", len(d.Pages)+1)
		}
		if other := d.pageNamed(name, nil); other != nil {
			return fmt.Errorf("a page named %q already exists (%s): pass page %q to work on it, or pick another name", other.Name, other.ID, other.ID)
		}
		id := NewID()
		for seen[id] {
			id = NewID()
		}
		seen[id] = true
		d.Pages = append(d.Pages, &Page{ID: id, Name: name, Background: color(op.Background, "#F5F5F5"), Children: []*Node{}})
		res.Created[i] = id
	case "update_page":
		p, err := d.pageFor(op, batch)
		if err != nil {
			return err
		}
		if op.Name != "" {
			if other := d.pageNamed(op.Name, p); other != nil {
				return fmt.Errorf("a page named %q already exists (%s): pick another name", other.Name, other.ID)
			}
			p.Name = op.Name
		}
		if op.Background != "" {
			if !IsColor(op.Background) {
				return fmt.Errorf("background %q is not a #RRGGBB colour", op.Background)
			}
			p.Background = strings.ToUpper(op.Background)
		}
	case "delete_page":
		p, err := d.pageFor(op, batch)
		if err != nil {
			return err
		}
		if len(d.Pages) == 1 {
			return fmt.Errorf("a design keeps at least one page")
		}
		pages := d.Pages[:0:0]
		for _, q := range d.Pages {
			if q != p {
				pages = append(pages, q)
			}
		}
		d.Pages = pages
	case "set_theme":
		if op.Name == "" && op.Color == "" {
			return fmt.Errorf("set_theme needs name: a theme id from the themes action, or color: a brand #RRGGBB")
		}
		return d.setTheme(op)
	case "insert":
		if op.Block == "" {
			return fmt.Errorf("insert needs block: a block id from the blocks action")
		}
		return d.insertBlock(i, op, batch, seen, res, touched)
	case "set_block_props":
		return d.setBlockProps(i, op, seen, res, touched)
	case "link_screens":
		p, err := d.pageFor(op, batch)
		if err != nil {
			return err
		}
		return d.linkScreens(p)
	case "recipe":
		if op.Name == "" {
			return fmt.Errorf("recipe needs name: landing, pricing, dashboard, onboarding, signin, settings, product or desktop")
		}
		return d.applyRecipe(i, op, batch, seen, res, touched)
	default:
		if ok, err := d.tokenOp(i, op, seen, res); ok {
			return err
		}
		if ok, err := d.commentOp(i, op, seen, res, batch); ok {
			return err
		}
		return fmt.Errorf("unknown op %q: use add, insert, set_block_props, update, delete, move, make_component, set_theme, recipe, link_screens, add_page, update_page, delete_page, set_style, delete_style, set_variable, delete_variable, set_modes, add_comment, reply_comment, resolve_comment or delete_comment", op.Op)
	}
	return nil
}

// merge applies props to a copy of n. "font" merges into the font; id,
// type and children cannot change this way.
func merge(n *Node, props map[string]json.RawMessage) (*Node, error) {
	data, err := json.Marshal(n)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	for k, v := range props {
		switch k {
		case "id", "type", "children":
			return nil, fmt.Errorf("%q cannot be updated; delete and add the node instead (children: add or move them)", k)
		case "font":
			var base, change map[string]json.RawMessage
			_ = json.Unmarshal(fields["font"], &base)
			if err := json.Unmarshal(v, &change); err != nil {
				return nil, fmt.Errorf("font must be an object: %w", err)
			}
			if base == nil {
				base = map[string]json.RawMessage{}
			}
			for fk, fv := range change {
				base[fk] = fv
			}
			merged, _ := json.Marshal(base)
			fields["font"] = merged
		default:
			if string(v) == "null" {
				delete(fields, k)
			} else {
				fields[k] = v
			}
		}
	}
	merged, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	var out Node
	if err := json.Unmarshal(merged, &out); err != nil {
		return nil, fmt.Errorf("props: %w", err)
	}
	out.Children = n.Children
	return &out, nil
}

// Matrix is an affine transform [a b c d e f], as in SVG.
type Matrix [6]float64

// Identity leaves points where they are.
var Identity = Matrix{1, 0, 0, 1, 0, 0}

// Mul is m then n applied inside it (m × n).
func (m Matrix) Mul(n Matrix) Matrix {
	return Matrix{
		m[0]*n[0] + m[2]*n[1], m[1]*n[0] + m[3]*n[1],
		m[0]*n[2] + m[2]*n[3], m[1]*n[2] + m[3]*n[3],
		m[0]*n[4] + m[2]*n[5] + m[4], m[1]*n[4] + m[3]*n[5] + m[5],
	}
}

// Apply maps a point.
func (m Matrix) Apply(x, y float64) (float64, float64) {
	return m[0]*x + m[2]*y + m[4], m[1]*x + m[3]*y + m[5]
}

// Local is the transform from a node's box to its parent's space.
func Local(n *Node) Matrix {
	t := Matrix{1, 0, 0, 1, n.X, n.Y}
	if n.Rotation == 0 {
		return t
	}
	r := n.Rotation * math.Pi / 180
	c, s := math.Cos(r), math.Sin(r)
	cx, cy := n.Width/2, n.Height/2
	return t.Mul(Matrix{c, s, -s, c, cx - c*cx + s*cy, cy - s*cx - c*cy})
}

// World is the transform from the last node of path to the page.
func World(path []*Node) Matrix {
	m := Identity
	for _, n := range path {
		m = m.Mul(Local(n))
	}
	return m
}

func worldOrigin(path []*Node) [2]float64 {
	x, y := World(path).Apply(0, 0)
	return [2]float64{x, y}
}

// Bounds is the axis-aligned box of a node in the space m maps it into.
func Bounds(m Matrix, n *Node) (x0, y0, x1, y1 float64) {
	x0, y0 = math.Inf(1), math.Inf(1)
	x1, y1 = math.Inf(-1), math.Inf(-1)
	for _, c := range [][2]float64{{0, 0}, {n.Width, 0}, {n.Width, n.Height}, {0, n.Height}} {
		x, y := m.Apply(c[0], c[1])
		x0, y0, x1, y1 = math.Min(x0, x), math.Min(y0, y), math.Max(x1, x), math.Max(y1, y)
	}
	return
}

// FitGroups sizes every unrotated group to its children, keeping them where
// they are on the page, as a group's box is always what it holds.
func FitGroups(nodes []*Node) {
	for _, n := range nodes {
		FitGroups(n.Children)
		if n.Type != TypeGroup || n.Rotation != 0 || len(n.Children) == 0 {
			continue
		}
		x0, y0 := math.Inf(1), math.Inf(1)
		x1, y1 := math.Inf(-1), math.Inf(-1)
		for _, c := range n.Children {
			a, b, cc, d := Bounds(Local(c), c)
			x0, y0, x1, y1 = math.Min(x0, a), math.Min(y0, b), math.Max(x1, cc), math.Max(y1, d)
		}
		for _, c := range n.Children {
			c.X -= x0
			c.Y -= y0
		}
		n.X += x0
		n.Y += y0
		n.Width = x1 - x0
		n.Height = y1 - y0
	}
}

// Glyph width of an unknown font as a share of its size. Kit faces use the
// advance tables in metrics.go; this remains for families those tables
// do not cover.
const glyphWidth = 0.55

// LineHeight is a text node's line height in pixels.
func LineHeight(n *Node) float64 {
	if n.LineHeight != nil && *n.LineHeight > 0 {
		return *n.LineHeight
	}
	size := 16.0
	if n.Font != nil {
		size = n.Font.Size
	}
	return math.Round(size*1.2*100) / 100
}

// Lines breaks a text node's text the way the app does: one line per
// paragraph when the box follows the text, otherwise words wrapped at the
// node's width using the font's real advances.
func Lines(n *Node) []string {
	paragraphs := strings.Split(displayText(n), "\n")
	if n.AutoResize == "width" || n.AutoResize == "" {
		return paragraphs
	}
	maxW := math.Max(1, n.Width)
	var out []string
	for _, p := range paragraphs {
		out = append(out, wrapParagraph(n, p, maxW)...)
	}
	return out
}

// Measure sets an auto-sized text node's box from its text.
func Measure(n *Node) {
	if n.Type != TypeText || n.AutoResize == "none" {
		return
	}
	lines := Lines(n)
	if n.AutoResize == "width" || n.AutoResize == "" {
		widest := 0.0
		for _, l := range lines {
			widest = math.Max(widest, textWidthOf(n, l))
		}
		n.Width = ceilPx(widest)
	}
	n.Height = ceilPx(float64(len(lines)) * LineHeight(n))
}

func overflowNote(n *Node) string {
	if n.Type != TypeText || n.AutoResize != "none" {
		return ""
	}
	need := float64(len(Lines(n))) * LineHeight(n)
	if need > n.Height+0.5 {
		return fmt.Sprintf("text %q (%s) needs about %.0fpx of height but its box is %.0fpx; use auto_resize \"height\" or make it taller", n.Name, n.ID, need, n.Height)
	}
	return ""
}

// pageNamed finds a page by name, ignoring case and surrounding space, other than `except`: page names must be unique, since exports and refs go by name.
func (d *Doc) pageNamed(name string, except *Page) *Page {
	name = strings.TrimSpace(name)
	for _, p := range d.Pages {
		if p != except && strings.EqualFold(strings.TrimSpace(p.Name), name) {
			return p
		}
	}
	return nil
}
