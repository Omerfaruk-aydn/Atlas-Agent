package design

import (
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"
)

// iOS app blocks: the screen shell and the pieces that fill it. They are the
// parts a real app is made of (a hero, a day strip, a schedule, a plan, a
// badge, status rows, notifications), each built to the HIG's sizes and
// spacing, so an app is a shell and a stack of these with its own words.

func atoiOr(s string, def int) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return def
	}
	return n
}

func atofOr(s string, def float64) float64 {
	n, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return def
	}
	return n
}

// soft is a translucent fill of a colour token.
func (b *builder) soft(n *Node, token string, alpha float64) *Node {
	p := b.paint(token)
	p.Opacity = &alpha
	n.Fills = []Paint{p}
	return n
}

// pill is a small rounded label. The wash carries the tone; the words
// stay in the text colour so they remain readable on the tint.
func (b *builder) pill(label, token string) *Node {
	n := b.row("Pill", 0, Pad{4, 10, 4, 10}, b.text("Label", "caption", "text", label))
	n.Children[0].Font.Weight = 600
	return radius(b.soft(n, token, 0.14), 999)
}

// pad16 is a full-width stack of a block's parts with the HIG's 16pt margins.
func (b *builder) pad16(name string, gap float64, kids ...*Node) *Node {
	n := fixW(b.col(name, gap, Pad{0, 16, 0, 16}, kids...), iosWidth)
	n.Layout.Cross = "stretch"
	for _, c := range n.Children {
		if c.Type == TypeFrame && c.SizingX == "hug" {
			c.SizingX = ""
		}
	}
	return n
}

func (b *builder) sectionTitle(title, action string) *Node {
	row := fillX(b.row("Section title", 8, Pad{}, fillX(b.text("Title", "h3", "text", title))))
	if action != "" {
		row.Children = append(row.Children, b.text("Action", "small-strong", "accent", action))
	}
	return row
}

// zoneToken maps a zone or tone name to the colour token that draws it.
func zoneToken(kind string) string {
	switch kind {
	case "blue", "info":
		return "info"
	case "green", "success":
		return "success"
	case "warning", "orange":
		return "warning"
	case "error", "red":
		return "error"
	}
	return "accent"
}

func (b *builder) qr(code string, size float64) *Node {
	const grid = 25
	h := fnv.New64a()
	_, _ = h.Write([]byte(code))
	seed := h.Sum64()
	next := func() uint64 {
		seed ^= seed << 13
		seed ^= seed >> 7
		seed ^= seed << 17
		return seed
	}
	finder := func(gx, gy int) bool {
		in := func(x, y int) bool { return x >= 0 && x < 7 && y >= 0 && y < 7 }
		for _, o := range [][2]int{{0, 0}, {grid - 7, 0}, {0, grid - 7}} {
			x, y := gx-o[0], gy-o[1]
			if in(x, y) {
				return true
			}
		}
		return false
	}
	var d strings.Builder
	rect := func(x, y, w, h int) {
		fmt.Fprintf(&d, "M%d %dh%dv%dh%dz", x, y, w, h, -w)
	}
	for gy := 0; gy < grid; gy++ {
		for gx := 0; gx < grid; gx++ {
			if finder(gx, gy) {
				continue
			}
			if next()%100 < 46 {
				rect(gx, gy, 1, 1)
			}
		}
	}
	for _, o := range [][2]int{{0, 0}, {grid - 7, 0}, {0, grid - 7}} {
		rect(o[0], o[1], 7, 7)
		rect(o[0]+1, o[1]+1, 5, 5)
		rect(o[0]+2, o[1]+2, 3, 3)
	}
	n := b.node(TypeFrame, "QR code")
	n.Width, n.Height = size, size
	n.Fills = []Paint{{Type: "solid", Color: "#FFFFFF"}}
	p := b.node(TypePath, "Modules")
	p.D = d.String()
	p.ViewBox = &[2]float64{grid, grid}
	p.Width, p.Height = size, size
	p.Fills = []Paint{b.paint("text")}
	// The finder gaps are holes: even-odd would need a fill rule the
	// format does not have, so the gaps are white squares on top.
	n.Children = []*Node{p}
	cell := size / grid
	for _, o := range [][2]int{{0, 0}, {grid - 7, 0}, {0, grid - 7}} {
		gap := b.rect("Finder gap", 5*cell, 5*cell, "card")
		gap.Fills = []Paint{{Type: "solid", Color: "#FFFFFF"}}
		gap.X, gap.Y = float64(o[0]+1)*cell, float64(o[1]+1)*cell
		core := b.rect("Finder core", 3*cell, 3*cell, "text")
		core.X, core.Y = float64(o[0]+2)*cell, float64(o[1]+2)*cell
		n.Children = append(n.Children, gap, core)
	}
	return n
}

func init() {
	sec := func(bl *Block) *Block { bl.Section = true; return bl }
	register(
		sec(&Block{ID: "ios/shell", Platform: "ios", Title: "App screen", Description: "An empty phone screen: status bar, optional navigation bar, a body to insert blocks into (parent <id>-body) and the tab bar with one tab active.", build: func(b *builder) *Node {
			hero := b.s("status", "light") == "accent"
			// Every slot is read up front so a screen without a nav bar or tab bar still accepts them.
			title, back, action, style := b.s("title", "Today"), b.s("back", ""), b.s("action", ""), b.s("style", "large")
			active, tabs := atoiOr(b.s("active", "0"), 0), b.items("tabs", iosTabs())
			status := b.iosStatusBar()
			if hero {
				status = b.iosStatusBarOn("on-accent", "accent")
			}
			kids := []*Node{status}
			if b.s("nav", "on") != "off" {
				kids = append(kids, b.bg(b.iosNavBar(title, back, action, style == "large"), "bg"))
			}
			body := fillY(fillX(b.col("Body", 16, Pad{0, 0, 16, 0})))
			body.Layout.Cross = "stretch"
			kids = append(kids, body)
			if b.s("tabbar", "on") != "off" {
				kids = append(kids, b.iosTabBar(tabs, active))
			}
			n := fixH(fixW(b.col("Screen", 0, Pad{}, kids...), iosWidth), iosTall)
			n.Layout.Cross = "stretch"
			return clip(b.bg(n, "bg"))
		}}),
		sec(&Block{ID: "ios/hero", Platform: "ios", Title: "Hero", Description: "A full-bleed accent header with eyebrow, large title, text, optional pills and a button; sits under a status bar set to status:accent.", build: func(b *builder) *Node {
			body := b.col("Content", 12, Pad{12, 24, 32, 24},
				upper(b.text("Eyebrow", "overline", "on-accent", b.s("eyebrow", "Welcome"))),
				b.para("Title", "display", "on-accent", b.s("title", "Plan your day"), 0),
				b.para("Text", "lead", "on-accent", b.s("text", "Everything you need, in one place."), 0))
			body.Layout.Cross = "stretch"
			for _, c := range body.Children {
				c.SizingX = "fill"
			}
			if chips := b.items("chips", []map[string]string{}); len(chips) > 0 {
				row := b.row("Pills", 8, Pad{4, 0, 0, 0})
				for _, c := range chips {
					p := b.row("Pill", 0, Pad{4, 12, 4, 12}, b.text("Label", "small-strong", "accent", c["title"]))
					row.Children = append(row.Children, radius(b.bg(p, "on-accent"), 999))
				}
				body.Children = append(body.Children, row)
			}
			rings := []*Node{}
			for i, d := range []float64{280, 190} {
				r := b.node(TypeEllipse, "Decor ring")
				r.Width, r.Height = d, d
				r.X, r.Y = 230-float64(i)*20+float64(i)*40, -90+float64(i)*60
				r.Fills = []Paint{}
				p := b.paint("on-accent")
				al := 0.16
				r.Strokes = []Stroke{{Color: p.Color, Variable: p.Variable, Width: 1.5, Opacity: &al}}
				rings = append(rings, absolute(r, r.X, r.Y))
			}
			n := fixW(b.col("Hero", 0, Pad{}, body), iosWidth)
			n.Layout.Cross = "stretch"
			n.Children = append(n.Children, rings...)
			n.Radius = &Radius{Corners: [4]float64{0, 0, 28, 28}, Each: true}
			return clip(b.bg(n, "accent"))
		}}),
		sec(&Block{ID: "ios/highlight", Platform: "ios", Title: "Highlight card", Description: "An accent card with a big value tile, title, text and a progress bar: today's day, a balance, a countdown.", build: func(b *builder) *Node {
			tile := fixW(b.col("Value", 0, Pad{8, 0, 8, 0}, b.text("Label", "overline", "on-accent", strings.ToUpper(b.s("label", "Day"))), b.text("Value", "h1", "on-accent", b.s("value", "2"))), 64)
			tile = radius(b.soft(align(tile, "start", "center"), "on-accent", 0.16), 14)
			prog := atofOr(b.s("progress", "0.2"), 0.2)
			track := fillX(radius(b.soft(b.rect("Track", 100, 6, "on-accent"), "on-accent", 0.25), 3))
			bar := radius(b.rect("Progress", 200*prog, 6, "on-accent"), 3)
			bar.Absolute = true
			track.Children = []*Node{bar}
			track.Layout = nil
			text := fillX(b.col("Text", 4, Pad{}, b.text("Title", "title", "on-accent", b.s("title", "Tuesday 10 November")), b.text("Subtitle", "small", "on-accent", b.s("text", "Energy and Transport"))))
			top := fillX(b.row("Top", 14, Pad{}, tile, text))
			top.Layout.Cross = "stretch"
			card := b.col("Card", 14, Pad{16, 16, 16, 16}, top, track, b.text("Meta", "caption", "on-accent", b.s("meta", "10 days remain")))
			card.Layout.Cross = "stretch"
			card.Children[2].SizingX = ""
			radius(b.bg(card, "accent"), b.t.Radius.XL)
			return b.pad16("Highlight", 0, card)
		}}),
		sec(&Block{ID: "ios/banner", Platform: "ios", Title: "Banner", Description: "A notice with an icon; tone info, success, warning or error.", build: func(b *builder) *Node {
			tone := b.s("tone", "info")
			token := zoneToken(tone)
			if tone == "info" {
				token = "accent"
			}
			n := b.row("Banner", 12, Pad{12, 14, 12, 14}, b.icon(b.s("icon", "info"), 20, token), fillX(b.para("Text", "small", "text", b.s("text", "Heads up: something worth knowing."), 0)))
			n.Layout.Cross = "start"
			radius(b.soft(n, token, 0.12), b.t.Radius.MD)
			return b.pad16("Banner", 0, n)
		}}),
		sec(&Block{ID: "ios/text", Platform: "ios", Title: "Text", Description: "A heading and a paragraph of body text.", build: func(b *builder) *Node {
			return b.pad16("Text", 6, b.text("Title", "title", "text", b.s("title", "About")), b.para("Text", "body", "text-muted", b.s("text", "A short paragraph that explains the screen."), 0))
		}}),
		sec(&Block{ID: "ios/spacer", Platform: "ios", Title: "Spacer", Description: "Takes the room left in a screen, pushing what follows to the bottom.", build: func(b *builder) *Node {
			return b.spacer("column")
		}}),
		sec(&Block{ID: "ios/section", Platform: "ios", Title: "Section title", Description: "A section heading with an optional action link.", build: func(b *builder) *Node {
			return b.pad16("Section", 0, b.sectionTitle(b.s("title", "Now and next"), b.s("action", "")))
		}}),
		sec(&Block{ID: "ios/day-strip", Platform: "ios", Title: "Day strip", Description: "A grid of dates with a one-word theme each; one selected, others marked theme, event or plain.", build: func(b *builder) *Node {
			per := max(3, min(7, atoiOr(b.s("per_row", "6"), 6)))
			sel := atoiOr(b.s("selected", "1"), 1)
			items := b.items("days", []map[string]string{{"d": "9", "l": "Mon"}, {"d": "10", "l": "Tue"}, {"d": "11", "l": "Wed", "kind": "event"}, {"d": "12", "l": "Thu", "kind": "event"}, {"d": "13", "l": "Fri"}, {"d": "14", "l": "Sat", "kind": "theme"}})
			grid := fillX(b.col("Days", 8, Pad{}))
			grid.Layout.Cross = "stretch"
			var line *Node
			for i, it := range items {
				if i%per == 0 {
					line = b.row("Week", 8, Pad{})
					line.Layout.Cross = "stretch"
					fillX(line)
					grid.Children = append(grid.Children, line)
				}
				fg, label := "text", "text-muted"
				cell := fixH(fillX(b.col("Day", 2, Pad{8, 0, 8, 0}, b.text("Date", "body-strong", fg, it["d"]), b.text("Label", "caption", label, it["l"]))), 56)
				cell.Layout.Main, cell.Layout.Cross = "center", "center"
				switch {
				case i == sel:
					cell.Children[0].Fills = []Paint{b.paint("on-accent")}
					cell.Children[1].Fills = []Paint{b.paint("on-accent")}
					b.bg(cell, "accent")
				case it["kind"] == "theme":
					cell.Children[0].Fills = []Paint{b.paint("on-accent-soft")}
					cell.Children[1].Fills = []Paint{b.paint("on-accent-soft")}
					b.bg(cell, "accent-soft")
				case it["kind"] == "event":
					b.soft(cell, "info", 0.16)
				default:
					b.bg(cell, "card")
				}
				radius(cell, 12)
				line.Children = append(line.Children, cell)
			}
			// A short last row keeps its cells the width of the others.
			if last := len(items) % per; last != 0 {
				for k := last; k < per; k++ {
					gap := fixH(fillX(b.col("Spacer", 0, Pad{})), 56)
					gap.Fills = []Paint{}
					line.Children = append(line.Children, gap)
				}
			}
			return b.pad16("Day strip", 0, grid)
		}}),
		sec(&Block{ID: "ios/chips", Platform: "ios", Title: "Filter chips", Description: "A row of filter pills, one selected.", build: func(b *builder) *Node {
			sel := atoiOr(b.s("selected", "0"), 0)
			row := b.row("Chips", 8, Pad{})
			for i, c := range b.items("chips", []map[string]string{{"title": "All"}, {"title": "Blue Zone"}, {"title": "Green Zone"}}) {
				fg := "text"
				chip := center(fixH(b.row("Chip", 0, Pad{0, 14, 0, 14}), 32))
				if i == sel {
					fg = "on-accent"
					b.bg(chip, "accent")
				} else {
					b.bg(chip, "card")
					b.border(chip, "border", 1)
				}
				chip.Children = append(chip.Children, b.text("Label", "small-strong", fg, c["title"]))
				row.Children = append(row.Children, radius(chip, 999))
			}
			return b.pad16("Chips", 0, row)
		}}),
		sec(&Block{ID: "ios/tiles", Platform: "ios", Title: "Quick actions", Description: "Equal tiles with an icon and a label, for the four things people do most.", build: func(b *builder) *Node {
			row := fillX(b.row("Tiles", 10, Pad{}))
			row.Layout.Cross = "stretch"
			for _, it := range b.items("items", []map[string]string{{"icon": "calendar", "title": "Programme"}, {"icon": "map", "title": "Venue"}, {"icon": "bus", "title": "Shuttles"}, {"icon": "layers", "title": "Themes"}}) {
				disc := center(fixH(fixW(b.row("Icon", 0, Pad{}, b.icon(it["icon"], 20, "on-accent-soft")), 40), 40))
				radius(b.bg(disc, "accent-soft"), 20)
				label := b.text("Label", "caption", "text", it["title"])
				label.Font.Weight = 600
				tile := fillX(b.col("Tile", 8, Pad{14, 4, 12, 4}, disc, label))
				align(tile, "start", "center")
				row.Children = append(row.Children, radius(b.bg(tile, "card"), b.t.Radius.LG))
			}
			return b.pad16("Quick actions", 0, row)
		}}),
		sec(&Block{ID: "ios/schedule", Platform: "ios", Title: "Schedule", Description: "A grouped list of timed items: time, a zone-coloured bar, title and place; the first can be marked now.", build: func(b *builder) *Node {
			card := fillX(b.col("Group", 0, Pad{}))
			card.Layout.Cross = "stretch"
			for i, it := range b.items("items", []map[string]string{
				{"time": "09:00", "end": "10:15", "title": "Opening session", "meta": "Hall A", "zone": "accent", "now": "true"},
				{"time": "10:30", "end": "11:45", "title": "Panel discussion", "meta": "Room 2", "zone": "green"},
				{"time": "13:00", "end": "14:00", "title": "Workshop", "meta": "Room 4", "zone": "blue"},
			}) {
				token := zoneToken(it["zone"])
				time := b.col("Time", 0, Pad{}, b.text("Start", "small-strong", "text", it["time"]), b.text("End", "caption", "text-muted", it["end"]))
				fixW(time, 48)
				bar := radius(b.rect("Zone", 4, 44, token), 2)
				meta := b.row("Meta", 6, Pad{}, b.text("Place", "small", "text-muted", it["meta"]))
				if it["tag"] != "" {
					meta.Children = append(meta.Children, b.pill(it["tag"], token))
				}
				text := fillX(b.col("Text", 2, Pad{}, b.para("Title", "body-strong", "text", it["title"], 0), meta))
				text.Layout.Cross = "stretch"
				text.Children[1].SizingX = ""
				row := fillX(b.row("Row", 12, Pad{12, 16, 12, 16}, time, bar, text))
				row.Layout.Cross = "stretch"
				bar.SizingY = "fill"
				if it["now"] == "true" {
					b.soft(row, "accent", 0.08)
					meta.Children = append(meta.Children, b.pill("Now", "accent"))
				}
				if i > 0 {
					card.Children = append(card.Children, fillX(b.row("Separator", 0, Pad{0, 0, 0, 16}, fillX(b.rect("Line", 100, 1, "border")))))
				}
				card.Children = append(card.Children, row)
			}
			clip(radius(b.bg(card, "surface"), b.t.Radius.LG))
			return b.pad16("Schedule", 0, card)
		}}),
		sec(&Block{ID: "ios/detail-head", Platform: "ios", Title: "Detail header", Description: "A zone pill, a large title and rows of facts with icons: when, where, who.", build: func(b *builder) *Node {
			token := zoneToken(b.s("tone", "green"))
			tag := b.row("Tag", 0, Pad{}, b.pill(strings.ToUpper(b.s("tag", "Green Zone")), token))
			kids := []*Node{tag, b.para("Title", "h1", "text", b.s("title", "Session title"), 0)}
			facts := b.col("Facts", 10, Pad{4, 0, 0, 0})
			facts.Layout.Cross = "stretch"
			for _, f := range b.items("facts", []map[string]string{{"icon": "calendar", "text": "Tuesday 10 November, 10:30-11:45"}, {"icon": "map-pin", "text": "Hall B, Green Zone"}}) {
				row := b.row("Fact", 10, Pad{}, b.icon(f["icon"], 18, "text-muted"), fillX(b.para("Text", "body", "text", f["text"], 0)))
				row.Layout.Cross = "start"
				facts.Children = append(facts.Children, fillX(row))
			}
			kids = append(kids, facts)
			return b.pad16("Detail", 12, kids...)
		}}),
		sec(&Block{ID: "ios/people", Platform: "ios", Title: "People", Description: "A grouped list of people: avatar with initials, name, role and an optional tag.", build: func(b *builder) *Node {
			card := fillX(b.col("Group", 0, Pad{}))
			card.Layout.Cross = "stretch"
			for i, p := range b.items("items", []map[string]string{{"name": "Ada Lovelace", "role": "Chair", "initials": "AL"}, {"name": "Grace Hopper", "role": "Co-chair", "initials": "GH"}}) {
				text := fillX(b.col("Text", 2, Pad{}, b.text("Name", "body-strong", "text", p["name"]), b.text("Role", "small", "text-muted", p["role"])))
				row := fillX(b.row("Row", 12, Pad{12, 16, 12, 16}, b.avatar("Avatar", p["initials"], 40), text))
				if p["tag"] != "" {
					row.Children = append(row.Children, b.pill(p["tag"], "accent"))
				}
				if i > 0 {
					card.Children = append(card.Children, fillX(b.row("Separator", 0, Pad{0, 0, 0, 68}, fillX(b.rect("Line", 100, 1, "border")))))
				}
				card.Children = append(card.Children, row)
			}
			clip(radius(b.bg(card, "surface"), b.t.Radius.LG))
			return b.pad16("People", 0, card)
		}}),
		sec(&Block{ID: "ios/status-list", Platform: "ios", Title: "Status list", Description: "Rows with a title, a line of text and a coloured status pill: agenda items, tasks, services.", build: func(b *builder) *Node {
			card := fillX(b.col("Group", 0, Pad{}))
			card.Layout.Cross = "stretch"
			for i, it := range b.items("items", []map[string]string{{"title": "Item one", "text": "What it is.", "status": "In progress", "tone": "info"}, {"title": "Item two", "text": "What it is.", "status": "Done", "tone": "success"}}) {
				token := zoneToken(it["tone"])
				text := fillX(b.col("Text", 2, Pad{}, b.para("Title", "body-strong", "text", it["title"], 0), b.para("Text", "small", "text-muted", it["text"], 0)))
				text.Layout.Cross = "stretch"
				row := fillX(b.row("Row", 12, Pad{12, 16, 12, 16}, text, b.pill(it["status"], token)))
				row.Layout.Cross = "start"
				if i > 0 {
					card.Children = append(card.Children, fillX(b.row("Separator", 0, Pad{0, 0, 0, 16}, fillX(b.rect("Line", 100, 1, "border")))))
				}
				card.Children = append(card.Children, row)
			}
			clip(radius(b.bg(card, "surface"), b.t.Radius.LG))
			return b.pad16("Status list", 0, card)
		}}),
		sec(&Block{ID: "ios/grid", Platform: "ios", Title: "Card grid", Description: "Two-column cards with an icon, a title, a line of text and an optional tag.", build: func(b *builder) *Node {
			grid := fillX(b.col("Grid", 10, Pad{}))
			grid.Layout.Cross = "stretch"
			var line *Node
			for i, it := range b.items("items", []map[string]string{{"icon": "layers", "title": "One", "text": "Short text."}, {"icon": "zap", "title": "Two", "text": "Short text."}}) {
				if i%2 == 0 {
					line = fillX(b.row("Row", 10, Pad{}))
					line.Layout.Cross = "stretch"
					grid.Children = append(grid.Children, line)
				}
				disc := center(fixH(fixW(b.row("Icon", 0, Pad{}, b.icon(it["icon"], 20, "on-accent-soft")), 40), 40))
				radius(b.bg(disc, "accent-soft"), 12)
				card := fillX(b.col("Card", 6, Pad{14, 14, 14, 14}, disc, b.para("Title", "body-strong", "text", it["title"], 0), b.para("Text", "small", "text-muted", it["text"], 0)))
				card.Layout.Cross = "stretch"
				card.Children[0].SizingX = ""
				if it["tag"] != "" {
					card.Children = append(card.Children, b.col("Tag", 0, Pad{4, 0, 0, 0}, b.pill(it["tag"], zoneToken(it["tone"]))))
					card.Children[len(card.Children)-1].SizingX = ""
				}
				line.Children = append(line.Children, radius(b.bg(card, "card"), b.t.Radius.LG))
			}
			if len(line.Children) == 1 {
				gap := fillX(b.col("Spacer", 0, Pad{}))
				gap.Fills = []Paint{}
				line.Children = append(line.Children, gap)
			}
			return b.pad16("Card grid", 0, grid)
		}}),
		sec(&Block{ID: "ios/notifications", Platform: "ios", Title: "Notifications", Description: "A grouped list of updates: tinted icon, title, text and time; unread ones are bold with a dot.", build: func(b *builder) *Node {
			card := fillX(b.col("Group", 0, Pad{}))
			card.Layout.Cross = "stretch"
			for i, it := range b.items("items", []map[string]string{{"icon": "bell", "title": "Update", "text": "Something changed.", "time": "2 min", "unread": "true"}}) {
				token := zoneToken(it["tone"])
				disc := center(fixH(fixW(b.row("Icon", 0, Pad{}, b.icon(it["icon"], 18, token)), 36), 36))
				radius(b.soft(disc, token, 0.14), 18)
				text := fillX(b.col("Text", 2, Pad{}, b.para("Title", "body-strong", "text", it["title"], 0), b.para("Text", "small", "text-muted", it["text"], 0)))
				text.Layout.Cross = "stretch"
				right := b.col("Meta", 6, Pad{}, b.text("Time", "caption", "text-muted", it["time"]))
				right.Layout.Cross = "end"
				if it["unread"] == "true" {
					right.Children = append(right.Children, radius(b.rect("Unread", 8, 8, "accent"), 4))
				}
				row := fillX(b.row("Row", 12, Pad{12, 16, 12, 16}, disc, text, right))
				row.Layout.Cross = "start"
				if i > 0 {
					card.Children = append(card.Children, fillX(b.row("Separator", 0, Pad{0, 0, 0, 64}, fillX(b.rect("Line", 100, 1, "border")))))
				}
				card.Children = append(card.Children, row)
			}
			clip(radius(b.bg(card, "surface"), b.t.Radius.LG))
			return b.pad16("Notifications", 0, card)
		}}),
		sec(&Block{ID: "ios/map", Platform: "ios", Title: "Plan", Description: "A site plan: zones (blue, green, neutral, road), labelled pins and a north arrow. Positions are in a 358-wide area.", build: func(b *builder) *Node {
			h := atofOr(b.s("height", "330"), 330)
			area := b.node(TypeFrame, "Plan")
			area.Width, area.Height = 358, h
			area = clip(radius(b.bg(area, "card"), b.t.Radius.LG))
			for _, z := range b.items("zones", []map[string]string{
				{"name": "Zone A", "kind": "blue", "x": "16", "y": "24", "w": "150", "h": "200"},
				{"name": "Zone B", "kind": "green", "x": "190", "y": "24", "w": "150", "h": "200"},
			}) {
				token := zoneToken(z["kind"])
				zn := b.node(TypeFrame, "Zone "+z["name"])
				zn.X, zn.Y = atofOr(z["x"], 0), atofOr(z["y"], 0)
				zn.Width, zn.Height = atofOr(z["w"], 80), atofOr(z["h"], 60)
				switch z["kind"] {
				case "road":
					b.bg(zn, "border-strong")
					zn.Name = "Road " + z["name"]
				case "neutral":
					b.bg(zn, "muted")
					b.border(zn, "border", 1)
				default:
					b.soft(zn, token, 0.14)
					b.border(zn, token, 1.5)
				}
				radius(zn, 12)
				if z["kind"] == "road" {
					radius(zn, 3)
				}
				if z["name"] != "" && z["kind"] != "road" {
					zn.Layout = &Layout{Dir: "column", Gap: 4, Pad: Pad{10, 10, 10, 10}, Main: "start", Cross: "start"}
					tok := token
					if z["kind"] == "neutral" {
						tok = "text"
					}
					zn.Children = append(zn.Children, b.text("Name", "small-strong", tok, z["name"]))
					for _, it := range splitList(z["items"]) {
						zn.Children = append(zn.Children, b.text("Item", "caption", "text-muted", it))
					}
				}
				area.Children = append(area.Children, zn)
			}
			for _, p := range b.items("pins", []map[string]string{{"x": "120", "y": "250", "label": "You", "kind": "you"}}) {
				token := "accent"
				if p["kind"] == "entrance" {
					token = "text"
				}
				dot := radius(b.rect("Pin", 16, 16, token), 8)
				b.border(dot, "surface", 3)
				b.shadow(dot, "sm")
				dot.X, dot.Y = atofOr(p["x"], 0), atofOr(p["y"], 0)
				area.Children = append(area.Children, dot)
				if p["label"] != "" {
					tag := b.row("Pin label", 0, Pad{4, 8, 4, 8}, b.text("Label", "caption", "text", p["label"]))
					radius(b.shadow(b.bg(tag, "surface"), "sm"), 8)
					tag.X, tag.Y = dot.X+22, dot.Y-4
					area.Children = append(area.Children, tag)
				}
			}
			north := center(fixH(fixW(b.row("North", 0, Pad{}, b.icon("navigation", 16, "text-muted")), 28), 28))
			radius(b.bg(north, "surface"), 14)
			north.X, north.Y = 358-14-28, h-14-28
			area.Children = append(area.Children, north)
			return b.pad16("Plan", 0, area)
		}}),
		sec(&Block{ID: "ios/badge", Platform: "ios", Title: "Badge", Description: "An accreditation card: name, role, organisation, a QR code, and which zones it opens.", build: func(b *builder) *Node {
			head := b.col("Head", 2, Pad{16, 20, 16, 20}, upper(b.text("Event", "overline", "on-accent", b.s("event", "COP31 Antalya"))), b.text("Name", "h2", "on-accent", b.s("name", "Alex Morgan")), b.text("Role", "small", "on-accent", b.s("role", "Party delegate")))
			head.Layout.Cross = "stretch"
			for _, c := range head.Children {
				c.SizingX = ""
			}
			b.bg(head, "accent")
			qr := b.qr(b.s("code", "COP31-0001"), 176)
			qrFrame := center(fixW(b.col("QR frame", 0, Pad{12, 12, 12, 12}, qr), 200))
			qrFrame.Fills = []Paint{{Type: "solid", Color: "#FFFFFF"}}
			radius(b.border(qrFrame, "border", 1), 16)
			access := b.row("Access", 8, Pad{})
			for _, a := range b.items("access", []map[string]string{{"title": "Blue Zone", "on": "true"}, {"title": "Green Zone", "on": "true"}}) {
				on := a["on"] != "false"
				tok, ic := "success", "check"
				if !on {
					tok, ic = "text-muted", "x"
				}
				pl := b.row("Pill", 6, Pad{4, 10, 4, 10}, b.icon(ic, 14, tok), b.text("Label", "small-strong", tok, a["title"]))
				access.Children = append(access.Children, radius(b.soft(pl, tok, 0.14), 999))
			}
			body := b.col("Body", 14, Pad{20, 20, 20, 20}, qrFrame, access, b.text("Valid", "caption", "text-muted", b.s("valid", "Valid 9-20 November 2026 with photo ID")))
			align(body, "start", "center")
			card := b.col("Badge card", 0, Pad{}, head, body)
			card.Layout.Cross = "stretch"
			for _, c := range card.Children {
				c.SizingX = ""
			}
			clip(radius(b.bg(card, "card"), b.t.Radius.XL))
			b.shadow(card, "md")
			return b.pad16("Badge", 0, card)
		}}),
		sec(&Block{ID: "ios/action-bar", Platform: "ios", Title: "Action bar", Description: "A primary button and up to two square icon buttons side by side.", build: func(b *builder) *Node {
			row := fillX(b.row("Actions", 10, Pad{}, fillX(b.iosButton(b.s("cta", "Add to agenda"), "filled"))))
			for _, ic := range splitList(b.s("icons", "share-2|map-pin")) {
				sq := center(fixH(fixW(b.row("Button", 0, Pad{}, b.icon(ic, 22, "accent")), 50), 50))
				row.Children = append(row.Children, radius(b.bg(sq, "accent-soft"), 12))
			}
			return b.pad16("Action bar", 0, row)
		}}),
	)
}
