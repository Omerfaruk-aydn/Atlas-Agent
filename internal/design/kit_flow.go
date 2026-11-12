package design

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// Autofix returns update ops for the safe lint findings: layout gap and
// padding that are off the even-pixel grid, snapped to the nearest even
// pixel. It does not rewrite copy or colours.
func Autofix(d *Doc, pageRef, id string) ([]Op, error) {
	page := d.PageByRef(pageRef)
	if page == nil {
		return nil, fmt.Errorf("no page %q", pageRef)
	}
	roots := page.Children
	if id != "" {
		at := d.Find(id)
		if at == nil {
			return nil, fmt.Errorf("no node %q", id)
		}
		roots = []*Node{at.Node}
	}
	var ops []Op
	Walk(roots, func(n *Node, _ *Node, _ int) bool {
		if n.Layout == nil || n.ID == "" {
			return true
		}
		next := *n.Layout
		changed := false
		if snap := snapEven(next.Gap); snap != next.Gap && offGrid(next.Gap) {
			next.Gap = snap
			changed = true
		}
		for i, v := range next.Pad {
			if snap := snapEven(v); snap != v && offGrid(v) {
				next.Pad[i] = snap
				changed = true
			}
		}
		if !changed {
			return true
		}
		raw, err := json.Marshal(next)
		if err != nil {
			return true
		}
		ops = append(ops, Op{Op: "update", ID: n.ID, Props: map[string]json.RawMessage{"layout": raw}})
		return true
	})
	return ops, nil
}

func offGrid(v float64) bool {
	return v != math.Trunc(v) || int(math.Trunc(v))%2 != 0
}

func snapEven(v float64) float64 {
	return math.Round(v/2) * 2
}

// linkScreens wires top-level frames to each other. A tab or a text label
// whose words match another frame's name navigates there. A control named
// Back goes back. The first frame is the flow start when none is set.
func (d *Doc) linkScreens(page *Page) error {
	var frames []*Node
	byName := map[string]*Node{}
	for _, c := range page.Children {
		if c.Type != TypeFrame && c.Type != TypeComponent {
			continue
		}
		frames = append(frames, c)
		byName[normName(c.Name)] = c
	}
	if len(frames) == 0 {
		return fmt.Errorf("link_screens needs top-level frames to connect")
	}
	start := false
	for _, f := range frames {
		if f.FlowStart {
			start = true
		}
	}
	if !start {
		frames[0].FlowStart = true
	}
	for _, f := range frames {
		Walk([]*Node{f}, func(n *Node, parent *Node, _ int) bool {
			if isBack(n) {
				n.Interactions = []*Interaction{{Trigger: "click", Action: "back"}}
				return true
			}
			label := normName(n.TextValue())
			if label == "" {
				label = normName(n.Name)
			}
			dest := byName[label]
			if dest == nil || dest == f || label == "" || label == normName(f.Name) {
				return true
			}
			target := n
			if parent != nil && (parent.Name == "Tab" || strings.HasPrefix(parent.Name, "Tab")) {
				target = parent
			}
			target.Interactions = []*Interaction{{
				Trigger: "click", Action: "navigate", To: dest.ID,
				Transition: "push", Direction: "left",
			}}
			return true
		})
	}
	return nil
}

func normName(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func isBack(n *Node) bool {
	name := normName(n.Name)
	return name == "back" || strings.HasPrefix(name, "back ") || normName(n.TextValue()) == "back"
}

// applyRecipe expands a named page or flow into kit blocks. Web pages stack
// in one column. Phone flows are one shell per screen, then linked.
func (d *Doc) applyRecipe(i int, op Op, batch string, seen map[string]bool, res *Result, touched map[string]bool) error {
	name := op.Name
	if name == "stitch" {
		switch strings.ToLower(propString(op.Props, "platform")) {
		case "ios", "iphone", "mobile":
			name = "app"
		case "android":
			name = "android"
		default:
			name = "landing"
		}
	}
	steps, err := recipeSteps(name, op.Props)
	if err != nil {
		return err
	}
	if d.Theme == nil {
		theme := "stone"
		switch name {
		case "onboarding", "app":
			theme = "ios"
		case "android":
			theme = "material"
		}
		if err := d.setTheme(Op{Name: theme, Color: propString(op.Props, "color")}); err != nil {
			return err
		}
	}
	page, err := d.pageFor(op, batch)
	if err != nil {
		return err
	}
	parent := ""
	if stacksOnAPage(name) {
		parent = "page"
		if d.Find(parent) == nil {
			if err := d.addPageColumn(page, seen, parent, orStr(propString(op.Props, "product"), "Page")); err != nil {
				return err
			}
		}
	}
	for _, step := range steps {
		if parent != "" && step.Parent == "" {
			step.Parent = parent
		}
		if err := d.insertBlock(i, step, batch, seen, res, touched); err != nil {
			return err
		}
	}
	if name == "app" || name == "android" {
		for id, title := range map[string]string{"home": "Home", "plan": "Plan", "you": "You", "explore": "Explore"} {
			if at := d.Find(id); at != nil {
				at.Node.Name = title
			}
		}
		if err := d.linkScreens(page); err != nil {
			return err
		}
	}
	return nil
}

func stacksOnAPage(name string) bool {
	switch name {
	case "landing", "pricing", "product":
		return true
	default:
		return false
	}
}

func (d *Doc) addPageColumn(page *Page, seen map[string]bool, id, name string) error {
	if seen[id] {
		return fmt.Errorf("id %q is already used", id)
	}
	bg := "#F7F6F3"
	if t, ok := ThemeOf(d); ok {
		bg = t.Colors["bg"][modeIndex(d)]
	}
	clip := true
	page.Children = append(page.Children, &Node{
		ID: id, Type: TypeFrame, Name: name, Width: 1440, SizingY: "hug",
		Layout: &Layout{Dir: "column", Gap: 0},
		Fills:  []Paint{{Type: "solid", Color: bg, Variable: "c-bg"}},
		Clip:   &clip,
	})
	seen[id] = true
	return nil
}

func recipeSteps(name string, props map[string]json.RawMessage) ([]Op, error) {
	product := orStr(propString(props, "product"), orStr(propString(props, "title"), "Northwind"))
	promise := orStr(propString(props, "promise"), orStr(propString(props, "title"), product+" for the people who use it every day"))
	detail := orStr(propString(props, "detail"), "A specific product, with its own nouns. Not a template.")
	switch name {
	case "landing":
		return []Op{
			block("web/navbar", "nav", propsOf("brand", product, "cta", "Start")),
			block("web/hero-split", "hero", propsOf("title", promise, "lead", detail, "cta", "See it", "secondary", "Talk to us", "badge", product, "proof", "In use this week")),
			block("web/feature-split", "feature", mergeProps(propsOf("eyebrow", product, "title", "One job, done properly", "text", detail, "cta", "How it works"), map[string]json.RawMessage{
				"points": jraw([]map[string]string{{"title": "The first step is obvious"}, {"title": "The second step is short"}, {"title": "The last step keeps a record"}}),
			})),
			block("web/cta", "cta", propsOf("title", "Start with "+product, "lead", detail, "cta", "Create an account")),
			block("web/footer", "footer", propsOf("brand", product, "tagline", detail, "legal", "© "+product)),
		}, nil
	case "pricing":
		return []Op{
			block("web/navbar", "nav", nil),
			block("web/pricing", "pricing", propOnly(props, "title")),
			block("web/faq", "faq", nil),
			block("web/footer", "footer", nil),
		}, nil
	case "dashboard":
		return []Op{block("web/app-shell", "app", propOnly(props, "title"))}, nil
	case "onboarding":
		return appScreens(product, promise, detail), nil
	case "app":
		return appScreens(product, promise, detail), nil
	case "android":
		return androidScreens(product, promise, detail), nil
	case "signin":
		return []Op{block("web/signin", "signin", propOnly(props, "title"))}, nil
	case "settings":
		return []Op{block("web/desktop", "settings", propOnly(props, "title"))}, nil
	case "product":
		return []Op{
			block("web/navbar", "nav", nil),
			block("web/hero-split", "product", propOnly(props, "title")),
			block("web/cart", "cart", nil),
			block("web/checkout", "checkout", nil),
		}, nil
	case "desktop":
		return []Op{block("web/desktop", "desk", propOnly(props, "title"))}, nil
	default:
		return nil, fmt.Errorf("no recipe %q; use stitch, landing, app, android, pricing, dashboard, onboarding, signin, settings, product or desktop", name)
	}
}

func appScreens(product, promise, detail string) []Op {
	tabs := jraw([]map[string]string{
		{"icon": "house", "title": "Home"},
		{"icon": "list", "title": "Plan"},
		{"icon": "circle-user", "title": "You"},
	})
	return []Op{
		block("ios/shell", "home", map[string]json.RawMessage{"title": jstr("Home"), "active": jstr("0"), "tabs": tabs}),
		blockIn("ios/highlight", "home-card", "home-body", propsOf("label", product, "value", "3", "title", promise, "text", detail, "meta", "This week")),
		block("ios/shell", "plan", map[string]json.RawMessage{"title": jstr("Plan"), "active": jstr("1"), "tabs": tabs}),
		blockIn("ios/schedule", "plan-list", "plan-body", map[string]json.RawMessage{"items": jraw([]map[string]string{
			{"time": "09:00", "end": "10:00", "title": "Open " + product, "meta": "Studio", "zone": "accent", "now": "true"},
			{"time": "13:00", "end": "14:00", "title": "Review the list", "meta": "Room 2", "zone": "green"},
		})}),
		block("ios/shell", "you", map[string]json.RawMessage{"title": jstr("You"), "active": jstr("2"), "tabs": tabs}),
		blockIn("ios/form", "you-form", "you-body", propsOf("title", "Your details", "name", "Amara Kone", "email", "amara@studio.test", "cta", "Save")),
	}
}

func androidScreens(product, promise, detail string) []Op {
	tabs := jraw([]map[string]string{
		{"icon": "house", "title": "Home"},
		{"icon": "compass", "title": "Explore"},
		{"icon": "user", "title": "You"},
	})
	return []Op{
		block("material/shell", "home", map[string]json.RawMessage{"title": jstr(product), "tabs": tabs}),
		blockIn("material/list", "home-list", "home-body", map[string]json.RawMessage{"rows": jraw([]map[string]string{
			{"initials": "AK", "title": promise, "text": detail, "meta": "Now"},
			{"initials": "JM", "title": "Next up", "text": "The following item on the list", "meta": "13:00"},
		})}),
		block("material/shell", "explore", map[string]json.RawMessage{"title": jstr("Explore"), "tabs": tabs}),
		blockIn("material/card", "explore-card", "explore-body", propsOf("title", product, "text", detail)),
		block("material/shell", "you", map[string]json.RawMessage{"title": jstr("You"), "tabs": tabs}),
		blockIn("material/empty", "you-empty", "you-body", propsOf("title", "Nothing waiting", "text", detail, "cta", "Add")),
	}
}

func block(id, root string, props map[string]json.RawMessage) Op {
	return Op{Op: "insert", Block: id, ID: root, Props: props}
}

func blockIn(id, root, parent string, props map[string]json.RawMessage) Op {
	op := block(id, root, props)
	op.Parent = parent
	return op
}

func propsOf(kv ...string) map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	for i := 0; i+1 < len(kv); i += 2 {
		out[kv[i]] = jstr(kv[i+1])
	}
	return out
}

func jstr(s string) json.RawMessage {
	raw, _ := json.Marshal(s)
	return raw
}

func jraw(v any) json.RawMessage {
	raw, _ := json.Marshal(v)
	return raw
}

func mergeProps(a map[string]json.RawMessage, b map[string]json.RawMessage) map[string]json.RawMessage {
	if a == nil {
		a = map[string]json.RawMessage{}
	}
	for k, v := range b {
		a[k] = v
	}
	return a
}

func propString(props map[string]json.RawMessage, key string) string {
	raw := props[key]
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return ""
}

func propOnly(props map[string]json.RawMessage, key string) map[string]json.RawMessage {
	if props[key] == nil {
		return nil
	}
	return map[string]json.RawMessage{key: props[key]}
}

func mapProp(key, value string) map[string]json.RawMessage {
	raw, _ := json.Marshal(value)
	return map[string]json.RawMessage{key: raw}
}

func orStr(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}
