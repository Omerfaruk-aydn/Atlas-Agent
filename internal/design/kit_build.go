package design

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// builder makes a kit block's layers in a theme. Every colour points at the
// theme's variables and every text at its text styles, so switching theme or
// mode restyles the block; ids are the block's prefix plus a readable name.
type builder struct {
	t      Theme
	mode   int
	seen   map[string]bool
	prefix string
	props  map[string]json.RawMessage
	// slots records the slots the block reads and their defaults, for the
	// catalog.
	slots map[string]any
	err   error
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func slugOf(s string) string {
	return strings.Trim(slugRe.ReplaceAllString(strings.ToLower(s), "-"), "-")
}

func (b *builder) id(name string) string {
	base := b.prefix
	if local := slugOf(name); local != "" {
		base += "-" + local
	}
	if len(base) > 56 {
		base = strings.TrimRight(base[:56], "-")
	}
	id := base
	for i := 2; b.seen[id] || !ValidID(id); i++ {
		id = fmt.Sprintf("%s-%d", base, i)
	}
	b.seen[id] = true
	return id
}

func (b *builder) fail(err error) {
	if b.err == nil {
		b.err = err
	}
}

// s is a text slot: the prop when given, else the default.
func (b *builder) s(key, def string) string {
	if _, ok := b.slots[key]; !ok {
		b.slots[key] = def
	}
	raw, ok := b.props[key]
	if !ok {
		return def
	}
	var v string
	if err := json.Unmarshal(raw, &v); err == nil {
		return v
	}
	var n float64
	if err := json.Unmarshal(raw, &n); err == nil {
		return strconv.FormatFloat(n, 'f', -1, 64)
	}
	b.fail(fmt.Errorf("prop %q must be a string", key))
	return def
}

// items is a list slot of objects with string fields.
func (b *builder) items(key string, def []map[string]string) []map[string]string {
	if _, ok := b.slots[key]; !ok {
		b.slots[key] = def
	}
	raw, ok := b.props[key]
	if !ok {
		return def
	}
	var list []map[string]any
	if err := json.Unmarshal(raw, &list); err != nil {
		var plain []string
		if err2 := json.Unmarshal(raw, &plain); err2 != nil {
			b.fail(fmt.Errorf("prop %q must be a list like the default: %s", key, mustJSON(def)))
			return def
		}
		list = nil
		for _, p := range plain {
			list = append(list, map[string]any{"title": p})
		}
	}
	out := make([]map[string]string, 0, len(list))
	for i, item := range list {
		if i == 24 {
			break
		}
		// An item is what it says: fields it leaves out stay empty, not
		// borrowed from the sample row at the same place.
		m := map[string]string{}
		for k, v := range item {
			switch x := v.(type) {
			case string:
				m[k] = x
			case float64:
				m[k] = strconv.FormatFloat(x, 'f', -1, 64)
			case bool:
				m[k] = strconv.FormatBool(x)
			case []any:
				parts := make([]string, 0, len(x))
				for _, p := range x {
					parts = append(parts, fmt.Sprint(p))
				}
				m[k] = strings.Join(parts, "|")
			}
		}
		out = append(out, m)
	}
	return out
}

func mustJSON(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}

func (b *builder) paint(token string) Paint {
	pair, ok := b.t.Colors[token]
	if !ok {
		b.fail(fmt.Errorf("theme has no colour %q", token))
	}
	return Paint{Type: "solid", Color: pair[b.mode], Variable: colorVar(token)}
}

func (b *builder) node(t NodeType, name string) *Node {
	n := &Node{ID: b.id(name), Type: t, Name: name}
	if t.IsContainer() {
		n.Children = []*Node{}
	}
	return n
}

// stack is a transparent auto layout frame that hugs its content and does
// not clip, so shadows inside it show.
func (b *builder) stack(name, dir string, gap float64, pad Pad, kids ...*Node) *Node {
	n := b.node(TypeFrame, name)
	n.Fills = []Paint{}
	no := false
	n.Clip = &no
	n.Layout = &Layout{Dir: dir, Gap: gap, Pad: pad, Main: "start", Cross: "start"}
	n.SizingX, n.SizingY = "hug", "hug"
	for _, k := range kids {
		if k != nil {
			n.Children = append(n.Children, k)
		}
	}
	return n
}

func (b *builder) col(name string, gap float64, pad Pad, kids ...*Node) *Node {
	return b.stack(name, "column", gap, pad, kids...)
}

func (b *builder) row(name string, gap float64, pad Pad, kids ...*Node) *Node {
	n := b.stack(name, "row", gap, pad, kids...)
	n.Layout.Cross = "center"
	return n
}

// text is one line (or explicit lines) of text in a type step and colour.
func (b *builder) text(name, slot, token, s string) *Node {
	tok, ok := b.t.Type[slot]
	if !ok {
		b.fail(fmt.Errorf("theme has no type step %q", slot))
	}
	n := b.node(TypeText, name)
	n.Text = &s
	n.Font = &Font{Family: tok.Family, Size: tok.Size, Weight: tok.Weight}
	lh := tok.LineHeight
	n.LineHeight = &lh
	if tok.LetterSpacing != 0 {
		ls := tok.LetterSpacing
		n.LetterSpacing = &ls
	}
	n.TextStyle = textStyle(slot)
	n.Fills = []Paint{b.paint(token)}
	n.AutoResize = "width"
	Measure(n)
	return n
}

// para is wrapping text: a fixed width, or the parent's width when width is 0.
func (b *builder) para(name, slot, token, s string, width float64) *Node {
	n := b.text(name, slot, token, s)
	n.AutoResize = "height"
	if width > 0 {
		n.Width = width
	} else {
		n.SizingX = "fill"
		n.Width = 320
	}
	Measure(n)
	return n
}

func (b *builder) icon(name string, size float64, token string) *Node {
	ic, err := LookupIcon(name)
	if err != nil {
		// The error fails the block; the stand-in keeps the tree whole.
		b.fail(err)
		n := b.node(TypeFrame, "Missing icon")
		n.Width, n.Height = size, size
		return n
	}
	n := b.node(TypePath, "Icon/"+name)
	n.D = ic.D
	n.ViewBox = &[2]float64{24, 24}
	n.Width, n.Height = size, size
	n.Fills = []Paint{}
	p := b.paint(token)
	n.Strokes = []Stroke{{Color: p.Color, Variable: p.Variable, Width: iconStroke(size), Cap: "round", Join: "round"}}
	return n
}

// iconStroke keeps Lucide's 2px-at-24 weight readable at small sizes.
func iconStroke(size float64) float64 {
	w := 2 * size / 24
	if w < 1.25 {
		w = 1.25
	}
	return float64(int(w*100+0.5)) / 100
}

// rect is a plain shape, filled with a colour token.
func (b *builder) rect(name string, w, h float64, token string) *Node {
	n := b.node(TypeRect, name)
	n.Width, n.Height = w, h
	n.Fills = []Paint{b.paint(token)}
	return n
}

// divider is a 1px line across its parent.
func (b *builder) divider(name string) *Node {
	n := b.rect(name, 100, 1, "border")
	n.SizingX = "fill"
	return n
}

// vdivider is a 1px line down its parent.
func (b *builder) vdivider(name string) *Node {
	n := b.rect(name, 1, 24, "border")
	n.SizingY = "fill"
	return n
}

// spacer takes the room left along its parent's direction.
func (b *builder) spacer(dir string) *Node {
	n := b.node(TypeFrame, "Spacer")
	n.Fills = []Paint{}
	n.Width, n.Height = 1, 1
	if dir == "row" {
		n.SizingX = "fill"
	} else {
		n.SizingY = "fill"
	}
	return n
}

// Modifiers. Each returns the node so they nest in builder calls.

func (b *builder) bg(n *Node, token string) *Node {
	n.Fills = []Paint{b.paint(token)}
	return n
}

func (b *builder) border(n *Node, token string, width float64) *Node {
	p := b.paint(token)
	n.Strokes = []Stroke{{Color: p.Color, Variable: p.Variable, Width: width, Align: "inside"}}
	return n
}

func (b *builder) shadow(n *Node, level string) *Node {
	n.Effects = append([]Effect(nil), b.t.shadows()[level]...)
	n.EffectStyle = effectStyle(level)
	return n
}

func radius(n *Node, r float64) *Node {
	n.Radius = &Radius{Corners: [4]float64{r, r, r, r}}
	return n
}

func clip(n *Node) *Node {
	yes := true
	n.Clip = &yes
	return n
}

func fillX(n *Node) *Node {
	n.SizingX = "fill"
	return n
}

func fillY(n *Node) *Node {
	n.SizingY = "fill"
	return n
}

func fixW(n *Node, w float64) *Node {
	n.SizingX = ""
	n.Width = w
	return n
}

func fixH(n *Node, h float64) *Node {
	n.SizingY = ""
	n.Height = h
	return n
}

func align(n *Node, main, cross string) *Node {
	if n.Layout != nil {
		n.Layout.Main, n.Layout.Cross = main, cross
	}
	return n
}

func center(n *Node) *Node { return align(n, "center", "center") }

func textAlign(n *Node, a string) *Node {
	n.Align = a
	return n
}

func upper(n *Node) *Node {
	n.Transform = "upper"
	return n
}

func opacity(n *Node, v float64) *Node {
	n.Opacity = &v
	return n
}

func absolute(n *Node, x, y float64) *Node {
	n.Absolute = true
	n.X, n.Y = x, y
	return n
}

func named(n *Node, name string) *Node {
	n.Name = name
	return n
}

// Block is one entry of the kit: a ready-made, themed piece of UI.
type Block struct {
	ID          string `json:"id"`
	Platform    string `json:"platform"`
	Title       string `json:"title"`
	Description string `json:"description"`
	// Section blocks take the width of a column stack they are inserted in.
	Section bool `json:"-"`
	build   func(b *builder) *Node
}

// BlockInfo is a block as the catalog lists it, with its slots.
type BlockInfo struct {
	ID          string         `json:"id"`
	Platform    string         `json:"platform"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Slots       map[string]any `json:"slots"`
	Schema      map[string]any `json:"schema,omitempty"`
	Width       float64        `json:"width"`
	Height      float64        `json:"height"`
}

var (
	blocks     = map[string]*Block{}
	blockOrder []string
)

func register(list ...*Block) {
	for _, bl := range list {
		blocks[bl.ID] = bl
		blockOrder = append(blockOrder, bl.ID)
	}
}

// Blocks lists the kit's blocks for a platform ("" for all), with their slots.
func Blocks(platform string) []BlockInfo {
	var out []BlockInfo
	for _, id := range blockOrder {
		bl := blocks[id]
		if platform != "" && bl.Platform != platform {
			continue
		}
		t, err := LookupTheme(DefaultTheme(bl.Platform), "")
		if err != nil {
			continue
		}
		b := &builder{t: t, seen: map[string]bool{}, prefix: "x", slots: map[string]any{}}
		root := b.build(bl)
		if b.err != nil {
			panic(fmt.Sprintf("kit block %s: %v", bl.ID, b.err))
		}
		info := BlockInfo{ID: bl.ID, Platform: bl.Platform, Title: bl.Title, Description: bl.Description, Slots: b.slots, Schema: slotSchema(b.slots)}
		if root != nil {
			d := &Doc{Pages: []*Page{{Children: []*Node{root}}}}
			ResolveLayout(d, nil)
			info.Width, info.Height = root.Width, root.Height
		}
		out = append(out, info)
	}
	return out
}

// slotSchema describes a block's slots for the agent and for a props form.
func slotSchema(slots map[string]any) map[string]any {
	props := map[string]any{}
	for k, v := range slots {
		props[k] = schemaValue(v)
	}
	return map[string]any{"type": "object", "properties": props, "additionalProperties": false}
}

func schemaValue(v any) map[string]any {
	switch x := v.(type) {
	case string:
		return map[string]any{"type": "string", "default": x, "maxLength": 240}
	case []map[string]string:
		item := map[string]any{"type": "object"}
		if len(x) > 0 {
			props := map[string]any{}
			for k := range x[0] {
				props[k] = map[string]any{"type": "string"}
			}
			item["properties"] = props
		}
		return map[string]any{"type": "array", "maxItems": 24, "items": item}
	default:
		return map[string]any{"type": "string"}
	}
}

// BuildBlock makes a block's layers in a theme and mode (0 light, 1 dark),
// with ids that are not in seen.
func BuildBlock(id string, t Theme, mode int, props map[string]json.RawMessage, prefix string, seen map[string]bool) (*Node, error) {
	bl := blocks[id]
	if bl == nil {
		return nil, fmt.Errorf("no block %q; the blocks action lists them (%s)", id, strings.Join(closest(id), ", "))
	}
	if prefix == "" {
		prefix = slugOf(strings.TrimPrefix(id[strings.Index(id, "/")+1:], "/"))
	}
	if !ValidID(prefix) || len(prefix) > 40 {
		return nil, fmt.Errorf("id %q: use up to 40 letters, digits, '-' and '_'", prefix)
	}
	if seen[prefix] {
		for i := 2; ; i++ {
			if p := fmt.Sprintf("%s%d", prefix, i); !seen[p] {
				prefix = p
				break
			}
		}
	}
	b := &builder{t: t, mode: mode, seen: seen, prefix: prefix, props: props, slots: map[string]any{}}
	root := b.build(bl)
	if b.err != nil {
		return nil, fmt.Errorf("block %s: %w", id, b.err)
	}
	for key := range props {
		if _, ok := b.slots[key]; !ok {
			keys := make([]string, 0, len(b.slots))
			for k := range b.slots {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			return nil, fmt.Errorf("block %s has no slot %q; its slots: %s", id, key, strings.Join(keys, ", "))
		}
	}
	return root, nil
}

// build makes the block; its root takes the prefix as its id, which was
// kept free for it.
func (b *builder) build(bl *Block) *Node {
	b.seen[b.prefix] = true
	root := bl.build(b)
	if root == nil {
		return nil
	}
	delete(b.seen, root.ID)
	root.ID = b.prefix
	root.Name = bl.Title
	return root
}

func closest(id string) []string {
	var out []string
	want := slugOf(id)
	for _, k := range blockOrder {
		if strings.Contains(k, want) || strings.Contains(want, slugOf(k[strings.Index(k, "/")+1:])) {
			out = append(out, k)
		}
	}
	if len(out) == 0 {
		out = append(out, blockOrder[:min(8, len(blockOrder))]...)
	}
	return out
}

// insertBlock runs an insert op: the block, built in the document's theme
// (the platform's default theme is set first when it has none), placed in
// the parent or on the page.
func (d *Doc) insertBlock(i int, op Op, batch string, seen map[string]bool, res *Result, touched map[string]bool) error {
	bl := blocks[op.Block]
	if bl == nil {
		return fmt.Errorf("no block %q; the blocks action lists them (%s)", op.Block, strings.Join(closest(op.Block), ", "))
	}
	if d.Theme == nil {
		def := DefaultTheme(bl.Platform)
		if err := d.setTheme(Op{Name: def}); err != nil {
			return err
		}
		res.Notes = append(res.Notes, fmt.Sprintf("the design had no theme, so %q was set; set_theme switches it", def))
	}
	t, ok := ThemeOf(d)
	if !ok {
		return fmt.Errorf("the design's theme %q is unknown; set one with set_theme", d.Theme.ID)
	}
	p, err := d.pageFor(op, batch)
	if err != nil {
		return err
	}
	list, holder, err := d.children(p, op.Parent)
	if err != nil {
		return err
	}
	n, err := BuildBlock(op.Block, t, modeIndex(d), op.Props, op.ID, seen)
	if err != nil {
		return err
	}
	switch {
	case holder != nil && holder.Layout != nil:
		// In a stack the block follows it: sections take its width.
		if holder.Layout.Dir == "column" && (bl.Section || isSection(n)) {
			n.SizingX = "fill"
		}
	default:
		if op.X != nil {
			n.X = *op.X
		}
		if op.Y != nil {
			n.Y = *op.Y
		}
		if holder == nil && op.X == nil && op.Y == nil {
			// Land to the right of what is on the page.
			for _, c := range p.Children {
				if edge := c.X + c.Width + 120; edge > n.X {
					n.X = edge
				}
			}
		}
	}
	n.Kit = &BlockRef{ID: op.Block, Props: op.Props}
	Walk([]*Node{n}, func(c *Node, _ *Node, _ int) bool {
		touched[c.ID] = true
		return true
	})
	*list = insert(*list, op.Index, n)
	res.Created[i] = n.ID
	return nil
}

// isSection reports whether a block is a full-width page section.
func isSection(n *Node) bool { return n.Width >= 1000 && n.Type == TypeFrame }

// kitOwner is the nearest inserted block that contains the path, itself
// included.
func kitOwner(path []*Node) *Node {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i].Kit != nil && path[i].Kit.ID != "" {
			return path[i]
		}
	}
	return nil
}

// setBlockProps rebuilds an inserted block with new slot values. Ids the
// builder produces stay the same, and fills on layers that are not text
// are kept, so a manual recolor survives a title change.
func (d *Doc) setBlockProps(i int, op Op, seen map[string]bool, res *Result, touched map[string]bool) error {
	at := d.Find(op.ID)
	if at == nil {
		return fmt.Errorf("no node %q", op.ID)
	}
	if at.Node.Kit == nil || at.Node.Kit.ID == "" {
		return fmt.Errorf("set_block_props needs a block placed with insert; %q is not one", op.ID)
	}
	props := map[string]json.RawMessage{}
	for k, v := range at.Node.Kit.Props {
		props[k] = append(json.RawMessage(nil), v...)
	}
	for k, v := range op.Props {
		props[k] = v
	}
	if d.Theme == nil {
		return fmt.Errorf("the design has no theme; set_theme first")
	}
	t, ok := ThemeOf(d)
	if !ok {
		return fmt.Errorf("the design's theme %q is unknown; set one with set_theme", d.Theme.ID)
	}
	free := map[string]bool{}
	for id := range seen {
		free[id] = true
	}
	Walk([]*Node{at.Node}, func(n *Node, _ *Node, _ int) bool {
		delete(free, n.ID)
		return true
	})
	next, err := BuildBlock(at.Node.Kit.ID, t, modeIndex(d), props, at.Node.ID, free)
	if err != nil {
		return err
	}
	keepTweaks(at.Node, next)
	next.X, next.Y = at.Node.X, at.Node.Y
	next.SizingX, next.SizingY = at.Node.SizingX, at.Node.SizingY
	next.Kit = &BlockRef{ID: at.Node.Kit.ID, Props: props}
	if at.Parent != nil {
		at.Parent.Children[at.Index] = next
	} else {
		at.Page.Children[at.Index] = next
	}
	Walk([]*Node{next}, func(n *Node, _ *Node, _ int) bool {
		touched[n.ID] = true
		seen[n.ID] = true
		return true
	})
	res.Created[i] = next.ID
	return nil
}

// keepTweaks copies non-text fills from the previous build onto the new
// one, and puts back layers the builder did not emit.
func keepTweaks(old, next *Node) {
	oldByID := map[string]*Node{}
	Walk([]*Node{old}, func(n *Node, _ *Node, _ int) bool {
		oldByID[n.ID] = n
		return true
	})
	newIDs := map[string]bool{}
	Walk([]*Node{next}, func(n *Node, _ *Node, _ int) bool {
		newIDs[n.ID] = true
		prev := oldByID[n.ID]
		if prev != nil && prev.Type == n.Type && n.Type != TypeText {
			n.Fills, n.Strokes, n.Effects = prev.Fills, prev.Strokes, prev.Effects
			n.Opacity, n.Radius = prev.Opacity, prev.Radius
		}
		return true
	})
	var extras func(*Node, []*Node)
	extras = func(parent *Node, nodes []*Node) {
		for _, n := range nodes {
			if newIDs[n.ID] {
				extras(n, n.Children)
				continue
			}
			if parent == nil {
				continue
			}
			if dest := findByID(next, parent.ID); dest != nil {
				dest.Children = append(dest.Children, n)
			}
		}
	}
	extras(nil, []*Node{old})
}

func findByID(n *Node, id string) *Node {
	if n == nil {
		return nil
	}
	if n.ID == id {
		return n
	}
	for _, c := range n.Children {
		if f := findByID(c, id); f != nil {
			return f
		}
	}
	return nil
}
