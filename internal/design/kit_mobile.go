package design

// iOS blocks follow Apple's Human Interface Guidelines (390pt wide iPhone,
// 44pt rows, 49pt tab bar, inset grouped lists); Material blocks follow the
// Material 3 specs (412dp wide, 64dp top app bar, 80dp navigation bar, 56dp
// list rows and FAB). Both read their colours and type from the theme.

const (
	iosWidth = 390.0
	iosTall  = 844.0
	m3Width  = 412.0
	m3Tall   = 915.0
)

func (b *builder) iosStatusBar() *Node { return b.iosStatusBarOn("text", "") }

// iosStatusBarOn draws the status bar in a colour token, over a background
// token when it sits on a coloured header.
func (b *builder) iosStatusBarOn(fg, bgToken string) *Node {
	icons := b.row("Icons", 6, Pad{}, b.icon("signal", 17, fg), b.icon("wifi", 17, fg), b.icon("battery-full", 24, fg))
	n := fixH(fixW(b.row("Status bar", 0, Pad{0, 28, 0, 32}, b.text("Time", "title", fg, "9:41"), icons), iosWidth), 54)
	n.Layout.Main = "between"
	if bgToken != "" {
		b.bg(n, bgToken)
	}
	return n
}

func (b *builder) iosHomeIndicator() *Node {
	bar := radius(b.rect("Bar", 134, 5, "text"), 3)
	n := fixH(fixW(b.row("Home indicator", 0, Pad{}, bar), iosWidth), 34)
	return center(n)
}

func (b *builder) iosNavBar(title, back, action string, large bool) *Node {
	left := b.row("Back", 4, Pad{}, b.icon("chevron-left", 24, "accent"), b.text("Label", "body", "accent", back))
	if back == "" {
		left = b.row("Back", 0, Pad{})
	}
	right := b.row("Actions", 16, Pad{})
	if action != "" {
		right.Children = append(right.Children, b.icon(action, 22, "accent"))
	}
	bar := fillX(fixH(b.row("Bar", 0, Pad{0, 16, 0, 8}, left, b.spacer("row"), right), 44))
	if !large {
		t := absolute(b.text("Title", "title", "text", title), 0, 11)
		t.X = (iosWidth - t.Width) / 2
		bar.Children = append(bar.Children, t)
		return fixW(b.col("Navigation bar", 0, Pad{}, bar), iosWidth)
	}
	heading := b.col("Title", 0, Pad{0, 16, 0, 16}, b.text("Large title", "display", "text", title))
	return fixW(b.col("Navigation bar", 4, Pad{0, 0, 8, 0}, bar, heading), iosWidth)
}

func (b *builder) iosSearch(placeholder string) *Node {
	field := fillX(fixH(b.row("Field", 6, Pad{0, 8, 0, 8}, b.icon("search", 17, "text-muted"), b.text("Placeholder", "body", "text-muted", placeholder)), 36))
	radius(b.bg(field, "muted"), 10)
	return fixW(b.col("Search", 0, Pad{8, 16, 8, 16}, field), iosWidth)
}

func (b *builder) iosList(header string, rows []map[string]string, footer string) *Node {
	card := fillX(b.col("Group", 0, Pad{}))
	card.Layout.Cross = "stretch"
	for i, r := range rows {
		line := fillX(fixH(b.row("Row", 12, Pad{0, 16, 0, 16}), 44))
		if r["text"] != "" {
			line.Height = 60
		}
		if r["icon"] != "" {
			tile := center(fixH(fixW(b.row("Icon", 0, Pad{}, b.icon(r["icon"], 18, "on-accent")), 30), 30))
			tint := r["tint"]
			if tint == "" {
				tint = "accent"
			}
			line.Children = append(line.Children, radius(b.bg(tile, tint), 7))
		}
		label := fillX(b.text("Label", "body", "text", r["title"]))
		if r["text"] != "" {
			label = fillX(b.col("Text", 2, Pad{}, b.text("Label", "body", "text", r["title"]), b.text("Detail", "small", "text-muted", r["text"])))
		}
		line.Children = append(line.Children, label)
		switch r["accessory"] {
		case "check":
			line.Children = append(line.Children, b.icon("check", 20, "accent"))
		case "toggle":
			line.Children = append(line.Children, b.iosSwitch(r["value"] != "off"))
		default:
			if r["value"] != "" {
				line.Children = append(line.Children, b.text("Value", "body", "text-muted", r["value"]))
			}
			if r["chevron"] != "off" {
				line.Children = append(line.Children, b.icon("chevron-right", 18, "text-subtle"))
			}
		}
		if i > 0 {
			inset := 16.0
			if r["icon"] != "" {
				inset = 58
			}
			sep := b.row("Separator", 0, Pad{0, 0, 0, inset}, fillX(b.rect("Line", 100, 1, "border")))
			card.Children = append(card.Children, fillX(sep))
		}
		card.Children = append(card.Children, line)
	}
	clip(radius(b.bg(card, "surface"), b.t.Radius.MD))
	n := fixW(b.col("List", 6, Pad{16, 16, 16, 16}), iosWidth)
	n.Layout.Cross = "stretch"
	if header != "" {
		h := upper(b.text("Header", "overline", "text-muted", header))
		n.Children = append(n.Children, b.col("Header", 0, Pad{0, 16, 0, 16}, h))
	}
	n.Children = append(n.Children, card)
	if footer != "" {
		n.Children = append(n.Children, b.col("Footer", 0, Pad{0, 16, 0, 16}, b.para("Text", "overline", "text-muted", footer, 326)))
	}
	for _, c := range n.Children {
		if c.Name != "Group" {
			c.SizingX = ""
		}
	}
	return n
}

func (b *builder) iosSwitch(on bool) *Node {
	knob := radius(b.rect("Knob", 27, 27, "on-accent"), 13.5)
	knob.Fills = []Paint{{Type: "solid", Color: "#FFFFFF"}}
	b.shadow(knob, "sm")
	track := fixH(fixW(b.row("Switch", 0, Pad{2, 2, 2, 2}, knob), 51), 31)
	if on {
		track.Layout.Main = "end"
		b.bg(track, "success")
	} else {
		b.bg(track, "muted")
	}
	return radius(track, 15.5)
}

func (b *builder) iosTabBar(items []map[string]string, active int) *Node {
	row := fillX(b.row("Items", 0, Pad{}))
	row.Layout.Main = "between"
	for i, it := range items {
		token := "text-muted"
		if i == active {
			token = "accent"
		}
		cell := align(fixW(b.col("Tab", 2, Pad{}, b.icon(it["icon"], 26, token), b.text("Label", "caption", token, it["title"])), 72), "start", "center")
		cell.Children[1].Font.Size = 10
		cell.Children[1].Font.Weight = 500
		cell.Children[1].TextStyle = ""
		lh := 12.0
		cell.Children[1].LineHeight = &lh
		Measure(cell.Children[1])
		row.Children = append(row.Children, cell)
	}
	n := fixH(fixW(b.col("Tab bar", 0, Pad{0, 16, 0, 16}, row, b.spacer("column"), b.iosHomeIndicator()), iosWidth), 83)
	n.Layout.Cross = "stretch"
	n.Children[2].SizingX = ""
	b.bg(n, "surface")
	sep := fillX(b.rect("Separator", iosWidth, 1, "border"))
	sep.Absolute = true
	sep.SizingX = ""
	n.Children = append(n.Children, sep)
	return n
}

func (b *builder) iosButton(label, style string) *Node {
	fg, bgToken := "on-accent", "accent"
	switch style {
	case "gray":
		fg, bgToken = "accent", "muted"
	case "tinted":
		fg, bgToken = "on-accent-soft", "accent-soft"
	case "plain":
		fg, bgToken = "accent", ""
	}
	n := center(fixH(b.row("Button", 0, Pad{0, 20, 0, 20}, b.text("Label", "title", fg, label)), 50))
	if bgToken != "" {
		b.bg(n, bgToken)
	}
	return radius(n, 12)
}

func (b *builder) m3Icon(name, token string) *Node {
	return center(fixH(fixW(b.row("Icon button", 0, Pad{}, b.icon(name, 24, token)), 48), 48))
}

func (b *builder) m3TopBar(title, lead string, actions []string, size string) *Node {
	bar := fillX(fixH(b.row("Bar", 0, Pad{0, 4, 0, 4}, b.m3Icon(lead, "text")), 64))
	if size == "small" {
		bar.Children = append(bar.Children, fillX(b.col("Title", 0, Pad{0, 0, 0, 4}, b.text("Title", "h4", "text", title))))
	} else {
		bar.Children = append(bar.Children, b.spacer("row"))
	}
	for _, a := range actions {
		bar.Children = append(bar.Children, b.m3Icon(a, "text-muted"))
	}
	n := fixW(b.col("Top app bar", 0, Pad{}, bar), m3Width)
	n.Layout.Cross = "stretch"
	if size != "small" {
		slot := "h2"
		if size == "large" {
			slot = "h1"
		}
		n.Children = append(n.Children, b.col("Headline", 0, Pad{0, 16, 24, 16}, b.text("Title", slot, "text", title)))
		n.Children[1].SizingX = ""
	}
	return b.bg(n, "bg")
}

func (b *builder) m3StatusBar() *Node {
	icons := b.row("Icons", 4, Pad{}, b.icon("wifi", 16, "text"), b.icon("signal", 16, "text"), b.icon("battery-full", 18, "text"))
	n := fixH(fixW(b.row("Status bar", 0, Pad{0, 24, 0, 24}, b.text("Time", "small-strong", "text", "9:30"), icons), m3Width), 40)
	n.Layout.Main = "between"
	return b.bg(n, "bg")
}

func (b *builder) m3NavBar(items []map[string]string) *Node {
	n := fixH(fixW(b.row("Navigation bar", 8, Pad{12, 8, 16, 8}), m3Width), 80)
	n.Layout.Cross = "start"
	for i, it := range items {
		token, pill := "text-muted", b.row("Indicator", 0, Pad{4, 20, 4, 20}, b.icon(it["icon"], 24, "text-muted"))
		if i == 0 {
			token = "text"
			pill = b.row("Indicator", 0, Pad{4, 20, 4, 20}, b.icon(it["icon"], 24, "on-accent-soft"))
			radius(b.bg(pill, "accent-soft"), 16)
		}
		label := b.text("Label", "overline", token, it["title"])
		if i == 0 {
			label.Font.Weight = 700
			label.TextStyle = ""
		}
		n.Children = append(n.Children, align(fillX(b.col("Destination", 4, Pad{}, pill, label)), "start", "center"))
	}
	return b.bg(n, "card")
}

func (b *builder) m3Button(label, style, iconName string) *Node {
	fg, bgToken, line := "on-accent", "accent", ""
	switch style {
	case "tonal":
		fg, bgToken = "on-accent-soft", "accent-soft"
	case "outlined":
		fg, bgToken, line = "accent", "", "border-strong"
	case "text":
		fg, bgToken = "accent", ""
	case "elevated":
		fg, bgToken = "accent", "surface"
	}
	pad := Pad{0, 24, 0, 24}
	if iconName != "" {
		pad = Pad{0, 24, 0, 16}
	}
	if style == "text" {
		pad = Pad{0, 12, 0, 12}
	}
	n := center(fixH(b.row("Button", 8, pad), 40))
	if iconName != "" {
		n.Children = append(n.Children, b.icon(iconName, 18, fg))
	}
	n.Children = append(n.Children, b.text("Label", "label", fg, label))
	if bgToken != "" {
		b.bg(n, bgToken)
	}
	if line != "" {
		b.border(n, line, 1)
	}
	if style == "elevated" {
		b.shadow(n, "sm")
	}
	return radius(n, 20)
}

func (b *builder) m3ListItem(r map[string]string) *Node {
	h := 56.0
	text := fillX(b.col("Text", 0, Pad{}, b.text("Headline", "lead", "text", r["title"])))
	if r["text"] != "" {
		h = 72
		text.Children = append(text.Children, b.text("Supporting", "small", "text-muted", r["text"]))
	}
	n := fillX(fixH(b.row("List item", 16, Pad{8, 24, 8, 16}), h))
	if r["initials"] != "" {
		n.Children = append(n.Children, b.avatar("Avatar", r["initials"], 40))
	} else if r["icon"] != "" {
		n.Children = append(n.Children, b.icon(r["icon"], 24, "text-muted"))
	}
	n.Children = append(n.Children, text)
	if r["meta"] != "" {
		n.Children = append(n.Children, b.text("Trailing", "caption", "text-muted", r["meta"]))
	}
	return n
}

func (b *builder) m3Fab(iconName, label string) *Node {
	n := center(fixH(b.row("FAB", 12, Pad{0, 16, 0, 16}, b.icon(iconName, 24, "on-accent-soft")), 56))
	if label != "" {
		n.Layout.Pad = Pad{0, 20, 0, 16}
		n.Children = append(n.Children, b.text("Label", "label", "on-accent-soft", label))
	} else {
		fixW(n, 56)
	}
	radius(b.bg(n, "accent-soft"), 16)
	return b.shadow(n, "md")
}

func (b *builder) m3Chip(label string, selected bool) *Node {
	n := center(fixH(b.row("Chip", 8, Pad{0, 16, 0, 16}), 32))
	if selected {
		n.Layout.Pad = Pad{0, 16, 0, 8}
		n.Children = append(n.Children, b.icon("check", 18, "on-accent-soft"))
		b.bg(n, "accent-soft")
	} else {
		b.border(n, "border-strong", 1)
	}
	token := "text-muted"
	if selected {
		token = "on-accent-soft"
	}
	n.Children = append(n.Children, b.text("Label", "label", token, label))
	return radius(n, 8)
}

func iosTabs() []map[string]string {
	return []map[string]string{{"icon": "house", "title": "Home"}, {"icon": "search", "title": "Search"}, {"icon": "bookmark", "title": "Saved"}, {"icon": "circle-user", "title": "Profile"}}
}

func m3Tabs() []map[string]string {
	return []map[string]string{{"icon": "house", "title": "Home"}, {"icon": "compass", "title": "Explore"}, {"icon": "bookmark", "title": "Saved"}, {"icon": "user", "title": "Profile"}}
}

func init() {
	register(
		&Block{ID: "ios/screen", Platform: "ios", Title: "iOS screen", Description: "A full iPhone screen: status bar, large title, search, a grouped list and the tab bar.", build: func(b *builder) *Node {
			list := b.iosList(b.s("section", "Account"), b.items("rows", []map[string]string{
				{"icon": "user", "title": "Profile", "value": ""},
				{"icon": "bell", "title": "Notifications", "accessory": "toggle"},
				{"icon": "lock", "title": "Privacy & Security"},
				{"icon": "credit-card", "title": "Subscription", "value": "Pro"},
			}), "")
			n := fixH(fixW(b.col("Screen", 0, Pad{}, b.iosStatusBar(), b.iosNavBar(b.s("title", "Settings"), "", b.s("action", "ellipsis"), true), b.iosSearch(b.s("search", "Search")), list, b.spacer("column"), b.iosTabBar(b.items("tabs", iosTabs()), 0)), iosWidth), iosTall)
			return clip(b.bg(n, "bg"))
		}},
		&Block{ID: "ios/status-bar", Platform: "ios", Title: "Status bar", Description: "Time, signal, Wi-Fi and battery, 54pt tall.", build: func(b *builder) *Node {
			return b.iosStatusBar()
		}},
		&Block{ID: "ios/nav-bar", Platform: "ios", Title: "Navigation bar", Description: "Back button, title (large or inline) and a trailing action.", build: func(b *builder) *Node {
			return b.bg(b.iosNavBar(b.s("title", "Inbox"), b.s("back", "Back"), b.s("action", "square-pen"), b.s("style", "large") == "large"), "bg")
		}},
		&Block{ID: "ios/search", Platform: "ios", Title: "Search field", Description: "The rounded search field under a navigation bar.", build: func(b *builder) *Node {
			return b.bg(b.iosSearch(b.s("placeholder", "Search")), "bg")
		}},
		&Block{ID: "ios/list", Platform: "ios", Title: "Grouped list", Description: "Inset grouped rows with icons, values, chevrons or switches, a header and a footer.", build: func(b *builder) *Node {
			return b.bg(b.iosList(b.s("header", ""), b.items("rows", []map[string]string{
				{"icon": "wifi", "title": "Wi-Fi", "value": "Home"},
				{"icon": "moon", "title": "Dark Mode", "accessory": "toggle"},
				{"icon": "bell", "title": "Notifications"},
				{"icon": "globe", "title": "Language", "value": "English"},
			}), b.s("footer", "")), "bg")
		}},
		&Block{ID: "ios/tab-bar", Platform: "ios", Title: "Tab bar", Description: "Bottom tab bar with four tabs and the home indicator.", build: func(b *builder) *Node {
			return b.iosTabBar(b.items("tabs", iosTabs()), 0)
		}},
		&Block{ID: "ios/button", Platform: "ios", Title: "Button", Description: "A 50pt button: style filled, tinted, gray or plain.", build: func(b *builder) *Node {
			return b.pad16("Button", 0, fillX(b.iosButton(b.s("label", "Continue"), b.s("style", "filled"))))
		}},
		&Block{ID: "ios/segmented", Platform: "ios", Title: "Segmented control", Description: "Two to five segments with the first selected.", build: func(b *builder) *Node {
			n := fillX(fixH(b.row("Segmented control", 0, Pad{2, 2, 2, 2}), 32))
			n.Layout.Cross = "stretch"
			selected := atoiOr(b.s("selected", "0"), 0)
			for i, s := range b.items("segments", []map[string]string{{"title": "Day"}, {"title": "Week"}, {"title": "Month"}}) {
				seg := center(fillX(b.row("Segment", 0, Pad{}, b.text("Label", "label", "text", s["title"]))))
				if i == selected {
					seg.Children[0].Font.Weight = 600
					seg.Children[0].TextStyle = ""
					radius(b.shadow(b.bg(seg, "surface"), "sm"), 7)
				}
				n.Children = append(n.Children, seg)
			}
			return b.pad16("Segmented control", 0, radius(b.bg(n, "muted"), 9))
		}},
		&Block{ID: "ios/switch", Platform: "ios", Title: "Switch", Description: "The 51Ã—31 toggle, on or off.", build: func(b *builder) *Node {
			return b.iosSwitch(b.s("state", "on") == "on")
		}},
		&Block{ID: "ios/card", Platform: "ios", Title: "Card", Description: "A rounded content card with image, title, text and a button.", build: func(b *builder) *Node {
			img := fixH(b.imagePlaceholder("Image", 0, 180), 180)
			img.Radius = nil
			body := b.col("Body", 6, Pad{16, 16, 16, 16},
				upper(b.text("Eyebrow", "overline", "text-muted", b.s("eyebrow", "Today"))),
				b.para("Title", "h3", "text", b.s("title", "Morning stretch, 10 minutes"), 0),
				b.para("Text", "small", "text-muted", b.s("text", "Loosen up your back and shoulders before the day starts."), 0),
				b.col("Gap", 0, Pad{6, 0, 0, 0}, b.iosButton(b.s("cta", "Start"), "tinted")))
			body.Layout.Cross = "stretch"
			body.Children[0].SizingX = ""
			body.Children[3].SizingX = ""
			n := fillX(b.col("Card", 0, Pad{}, img, body))
			n.Layout.Cross = "stretch"
			clip(radius(b.bg(n, "surface"), 16))
			return b.pad16("Card", 0, b.shadow(n, "sm"))
		}},
		&Block{ID: "ios/alert", Platform: "ios", Title: "Alert", Description: "A 270pt system alert with title, message and two actions.", build: func(b *builder) *Node {
			head := align(b.col("Text", 2, Pad{20, 16, 16, 16}, textAlign(b.para("Title", "title", "text", b.s("title", "Delete this note?"), 238), "center"), textAlign(b.para("Message", "label", "text", b.s("message", "This can't be undone."), 238), "center")), "start", "center")
			cancel := center(fillX(fixH(b.row("Cancel", 0, Pad{}, b.text("Label", "body", "accent", b.s("cancel", "Cancel"))), 44)))
			del := center(fillX(fixH(b.row("Destructive", 0, Pad{}, b.text("Label", "title", "error", b.s("confirm", "Delete"))), 44)))
			actions := fillX(b.row("Actions", 0, Pad{}, cancel, b.vdivider("Divider"), del))
			actions.Layout.Cross = "stretch"
			n := fixW(b.col("Alert", 0, Pad{}, head, b.divider("Divider"), actions), 270)
			n.Layout.Cross = "stretch"
			n.Children[0].SizingX = ""
			clip(radius(b.bg(n, "surface"), 14))
			return b.shadow(n, "lg")
		}},
		&Block{ID: "ios/sheet", Platform: "ios", Title: "Sheet", Description: "A bottom sheet with grabber, title, text and a full-width button.", build: func(b *builder) *Node {
			grab := radius(b.rect("Grabber", 36, 5, "border-strong"), 3)
			n := fixW(b.col("Sheet", 16, Pad{6, 16, 8, 16},
				center(fillX(b.row("Grabber area", 0, Pad{0, 0, 8, 0}, grab))),
				b.text("Title", "h2", "text", b.s("title", "Share list")),
				b.para("Text", "body", "text-muted", b.s("text", "Anyone with the link can view and add items. You can stop sharing at any time."), 0),
				fillX(b.iosButton(b.s("cta", "Copy link"), "filled")),
				b.iosHomeIndicator()), iosWidth)
			n.Layout.Cross = "stretch"
			n.Children[1].SizingX = ""
			n.Children[4].SizingX = "fill"
			n.Children[4].Width = iosWidth - 32
			radius(b.bg(n, "surface"), 0)
			n.Radius = &Radius{Corners: [4]float64{20, 20, 0, 0}, Each: true}
			return b.shadow(n, "lg")
		}},
		&Block{ID: "material/screen", Platform: "android", Title: "Material screen", Description: "A full Android screen: status bar, top app bar, search, filter chips, a list, a FAB and the navigation bar.", build: func(b *builder) *Node {
			search := fillX(fixH(b.row("Search bar", 12, Pad{0, 16, 0, 16}, b.icon("menu", 24, "text"), fillX(b.text("Placeholder", "lead", "text-muted", b.s("search", "Search messages"))), b.avatar("Avatar", "AK", 30)), 56))
			radius(b.bg(search, "muted"), 28)
			chips := b.row("Chips", 8, Pad{0, 16, 0, 16})
			for i, c := range b.items("chips", []map[string]string{{"title": "All"}, {"title": "Unread"}, {"title": "Starred"}, {"title": "Files"}}) {
				chips.Children = append(chips.Children, b.m3Chip(c["title"], i == 0))
			}
			list := fillX(b.col("List", 0, Pad{8, 0, 8, 0}))
			list.Layout.Cross = "stretch"
			for _, r := range b.items("rows", []map[string]string{
				{"initials": "AK", "title": "Amara Kone", "text": "The deck is ready for review", "meta": "10:24"},
				{"initials": "JM", "title": "Jonas Meyer", "text": "Lunch on Thursday?", "meta": "09:02"},
				{"initials": "SR", "title": "Sofia Ruiz", "text": "Invoice #2041 paid", "meta": "Yesterday"},
				{"initials": "LT", "title": "Liam Tan", "text": "New photos from the trip", "meta": "Mon"},
			}) {
				list.Children = append(list.Children, b.m3ListItem(r))
			}
			// The FAB sits 16dp above the navigation bar at the right edge,
			// in a full-width row so its own width can follow its label.
			fab := absolute(fixW(b.row("FAB area", 0, Pad{0, 16, 0, 16}, b.m3Fab(b.s("fab_icon", "pencil"), b.s("fab", "Compose"))), m3Width), 0, m3Tall-80-16-56)
			fab.Layout.Main = "end"
			body := fillY(fillX(b.col("Body", 12, Pad{0, 0, 0, 0}, b.col("Search", 0, Pad{8, 16, 0, 16}, search), chips, list)))
			body.Layout.Cross = "stretch"
			body.Children[1].SizingX = ""
			n := fixH(fixW(b.col("Screen", 0, Pad{}, b.m3StatusBar(), b.m3TopBar(b.s("title", "Inbox"), "arrow-left", []string{"search", "ellipsis-vertical"}, "small"), body, b.m3NavBar(b.items("tabs", m3Tabs())), fab), m3Width), m3Tall)
			n.Layout.Cross = "stretch"
			return clip(b.bg(n, "bg"))
		}},
		&Block{ID: "material/top-app-bar", Platform: "android", Title: "Top app bar", Description: "Leading icon, title and actions; size small, medium or large.", build: func(b *builder) *Node {
			return b.m3TopBar(b.s("title", "Photos"), b.s("lead", "arrow-left"), []string{"share-2", "ellipsis-vertical"}, b.s("size", "small"))
		}},
		&Block{ID: "material/navigation-bar", Platform: "android", Title: "Navigation bar", Description: "Bottom navigation with an active indicator pill.", build: func(b *builder) *Node {
			return b.m3NavBar(b.items("tabs", m3Tabs()))
		}},
		&Block{ID: "material/button", Platform: "android", Title: "Button", Description: "A 40dp button: filled, tonal, outlined, elevated or text, optional icon.", build: func(b *builder) *Node {
			return b.m3Button(b.s("label", "Continue"), b.s("style", "filled"), b.s("icon", ""))
		}},
		&Block{ID: "material/fab", Platform: "android", Title: "FAB", Description: "Floating action button, plain or extended with a label.", build: func(b *builder) *Node {
			return b.m3Fab(b.s("icon", "plus"), b.s("label", ""))
		}},
		&Block{ID: "material/card", Platform: "android", Title: "Card", Description: "A card (elevated, filled or outlined) with media, title, text and actions.", build: func(b *builder) *Node {
			img := fixH(b.imagePlaceholder("Media", 0, 180), 180)
			img.Radius = nil
			actions := fillX(b.row("Actions", 8, Pad{}, b.m3Button(b.s("secondary", "Details"), "outlined", ""), b.m3Button(b.s("cta", "Book"), "filled", "")))
			actions.Layout.Main = "end"
			body := b.col("Body", 8, Pad{16, 16, 16, 16}, b.text("Title", "h4", "text", b.s("title", "Cabin by the lake")), b.para("Text", "small", "text-muted", b.s("text", "Two bedrooms, a wood stove and a private dock. 40 minutes from the city."), 0), b.col("Spacer", 0, Pad{8, 0, 0, 0}, actions))
			body.Layout.Cross = "stretch"
			body.Children[0].SizingX = ""
			n := fixW(b.col("Card", 0, Pad{}, img, body), 380)
			n.Layout.Cross = "stretch"
			clip(radius(n, 12))
			switch b.s("style", "elevated") {
			case "filled":
				b.bg(n, "muted")
			case "outlined":
				b.border(b.bg(n, "bg"), "border", 1)
			default:
				b.shadow(b.bg(n, "surface"), "sm")
			}
			return n
		}},
		&Block{ID: "material/list", Platform: "android", Title: "List", Description: "One- or two-line list items with avatars or icons and trailing text.", build: func(b *builder) *Node {
			n := fixW(b.col("List", 0, Pad{8, 0, 8, 0}), m3Width)
			n.Layout.Cross = "stretch"
			for _, r := range b.items("rows", []map[string]string{
				{"icon": "image", "title": "Photos", "text": "1,248 items", "meta": "2.1 GB"},
				{"icon": "file-text", "title": "Documents", "text": "86 items", "meta": "340 MB"},
				{"icon": "list-music", "title": "Music", "text": "412 items", "meta": "1.8 GB"},
			}) {
				n.Children = append(n.Children, b.m3ListItem(r))
			}
			return b.bg(n, "bg")
		}},
		&Block{ID: "material/text-field", Platform: "android", Title: "Text field", Description: "An outlined text field with floating label and supporting text.", build: func(b *builder) *Node {
			label := absolute(b.col("Label", 0, Pad{0, 4, 0, 4}, b.text("Text", "caption", "accent", b.s("label", "Email"))), 12, -8)
			b.bg(label, "bg")
			field := fillX(fixH(b.row("Field", 12, Pad{0, 16, 0, 16}, fillX(b.text("Value", "lead", "text", b.s("value", "amara@halcyon.io"))), b.icon("circle-x", 24, "text-muted"), label), 56))
			no := false
			field.Clip = &no
			radius(b.border(field, "accent", 2), 4)
			n := fixW(b.col("Text field", 4, Pad{}, field, b.col("Supporting", 0, Pad{0, 16, 0, 16}, b.text("Text", "caption", "text-muted", b.s("hint", "We'll send a sign-in link")))), 360)
			n.Layout.Cross = "stretch"
			n.Children[1].SizingX = ""
			return n
		}},
		&Block{ID: "material/chips", Platform: "android", Title: "Filter chips", Description: "A row of filter chips, the first selected.", build: func(b *builder) *Node {
			n := b.row("Chips", 8, Pad{})
			for i, c := range b.items("chips", []map[string]string{{"title": "Nearby"}, {"title": "Open now"}, {"title": "Top rated"}, {"title": "Delivery"}}) {
				n.Children = append(n.Children, b.m3Chip(c["title"], i == 0))
			}
			return n
		}},
		&Block{ID: "material/switch", Platform: "android", Title: "Switch", Description: "The 52Ã—32 Material 3 switch, on or off.", build: func(b *builder) *Node {
			on := b.s("state", "on") == "on"
			if on {
				handle := radius(b.rect("Handle", 24, 24, "on-accent"), 12)
				n := fixH(fixW(b.row("Switch", 0, Pad{4, 4, 4, 4}, handle), 52), 32)
				n.Layout.Main = "end"
				return radius(b.bg(n, "accent"), 16)
			}
			handle := radius(b.rect("Handle", 16, 16, "border-strong"), 8)
			n := fixH(fixW(b.row("Switch", 0, Pad{8, 8, 8, 8}, handle), 52), 32)
			return radius(b.border(b.bg(n, "muted"), "border-strong", 2), 16)
		}},
		&Block{ID: "material/dialog", Platform: "android", Title: "Dialog", Description: "A basic dialog: icon, headline, supporting text and text-button actions.", build: func(b *builder) *Node {
			actions := fillX(b.row("Actions", 8, Pad{}, b.m3Button(b.s("cancel", "Cancel"), "text", ""), b.m3Button(b.s("confirm", "Reset"), "text", "")))
			actions.Layout.Main = "end"
			n := fixW(b.col("Dialog", 16, Pad{24, 24, 24, 24}, b.icon(b.s("icon", "refresh-cw"), 24, "accent"), b.para("Headline", "h3", "text", b.s("title", "Reset settings?"), 0), b.para("Text", "small", "text-muted", b.s("text", "This restores every setting to its default. Your files and messages are not affected."), 0), actions), 312)
			radius(b.bg(n, "card"), 28)
			return b.shadow(n, "md")
		}},
	)
}
