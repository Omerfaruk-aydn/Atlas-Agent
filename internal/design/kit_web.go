package design

import (
	"strings"
)

// Web blocks: page sections at 1440px with 120px margins, and the
// components inside them. Copy is specific, never filler, so a block reads
// as a real product until the agent writes the brief's own words in.

const (
	webWidth  = 1440.0
	webMargin = 120.0
	webInner  = webWidth - 2*webMargin
)

func (b *builder) section(name string, gap float64, pad Pad, kids ...*Node) *Node {
	n := fixW(b.col(name, gap, pad, kids...), webWidth)
	n.Layout.Cross = "stretch"
	return b.bg(n, "bg")
}

// framed gives cards and buttons the theme's hard border when it has one.
func (b *builder) framed(n *Node, token string) *Node {
	if b.t.BorderWidth > 0 {
		return b.border(n, "border", b.t.BorderWidth)
	}
	if token != "" {
		return b.border(n, token, 1)
	}
	return n
}

func (b *builder) button(name, label, variant, size, iconName string) *Node {
	h, padX, slot, iconSize := 40.0, 16.0, "small-strong", 16.0
	switch size {
	case "sm":
		h, padX = 32, 12
	case "lg":
		h, padX, slot, iconSize = 48, 24, "body-strong", 18
	}
	fg, bgToken, line := "on-accent", "accent", ""
	switch variant {
	case "secondary":
		fg, bgToken = "text", "muted"
	case "outline":
		fg, bgToken, line = "text", "surface", "border-strong"
	case "ghost":
		fg, bgToken = "text", ""
	case "inverse":
		fg, bgToken = "accent", "on-accent"
	}
	n := b.row(name, 8, Pad{0, padX, 0, padX}, b.text("Label", slot, fg, label))
	if iconName != "" {
		n.Children = append(n.Children, b.icon(iconName, iconSize, fg))
	}
	n = center(fixH(n, h))
	radius(n, b.t.Radius.MD)
	if bgToken != "" {
		b.bg(n, bgToken)
	}
	if variant != "ghost" {
		b.framed(n, line)
	}
	return n
}

func (b *builder) badge(name, label, iconName string) *Node {
	n := b.row(name, 6, Pad{4, 12, 4, 12})
	if iconName != "" {
		n.Children = append(n.Children, b.icon(iconName, 14, "on-accent-soft"))
	}
	n.Children = append(n.Children, b.text("Label", "small-strong", "on-accent-soft", label))
	return radius(b.bg(n, "accent-soft"), 999)
}

func (b *builder) logo(name string) *Node {
	mark := center(fixH(fixW(b.row("Mark", 0, Pad{}, b.icon(b.s("logo_icon", "hexagon"), 18, "on-accent")), 32), 32))
	radius(b.bg(mark, "accent"), b.t.Radius.MD)
	return b.row("Logo", 10, Pad{}, mark, b.text("Name", "title", "text", name))
}

// imagePlaceholder is themed abstract art seeded from the layer name, so a
// block does not ship a flat grey box. The same seed always draws the same
// picture.
func (b *builder) imagePlaceholder(name string, w, h float64) *Node {
	n := b.node(TypeFrame, name)
	if w > 0 {
		n.Width = w
	} else {
		n.SizingX = "fill"
		n.Width = 320
	}
	if h <= 0 {
		h = 180
	}
	n.Height = h
	b.bg(n, "muted")
	seed := uint64(14695981039346656037)
	for _, r := range name {
		seed = seed*1099511628211 ^ uint64(r)
	}
	tokens := []string{"accent", "accent-soft", "info"}
	for i := 0; i < 3; i++ {
		seed = seed*1664525 + 1013904223
		e := b.node(TypeEllipse, "Decor")
		e.Width = n.Width * (0.4 + float64(i)*0.12)
		e.Height = n.Height * (0.5 + float64(seed%25)/100)
		e.X = n.Width*(0.05+float64(i)*0.28) - e.Width*0.15
		e.Y = n.Height*float64((seed>>10)%50)/100 - e.Height*0.1
		op := 0.4 + float64(i)*0.15
		e.Opacity = &op
		e.Fills = []Paint{b.paint(tokens[i])}
		n.Children = append(n.Children, e)
	}
	return clip(radius(n, b.t.Radius.XL))
}

func (b *builder) avatar(name, initials string, size float64) *Node {
	slot := "small-strong"
	if size < 32 {
		slot = "caption"
	}
	n := center(fixH(fixW(b.row(name, 0, Pad{}, b.text("Initials", slot, "on-accent-soft", initials)), size), size))
	return radius(b.bg(n, "accent-soft"), size/2)
}

func (b *builder) sectionHeader(eyebrow, title, lead string, centered bool) *Node {
	w := 720.0
	h := b.col("Header", 16, Pad{},
		b.text("Eyebrow", "overline", "on-accent-soft", eyebrow),
		b.para("Title", "h2", "text", title, w),
		b.para("Lead", "lead", "text-muted", lead, w))
	upper(h.Children[0])
	if centered {
		align(h, "start", "center")
		for _, c := range h.Children {
			textAlign(c, "center")
		}
	}
	return h
}

func (b *builder) checkItem(name, label, token string) *Node {
	return b.row(name, 10, Pad{}, b.icon("check", 16, "on-accent-soft"), b.text("Label", "small", token, label))
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, "|") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func init() {
	register(
		&Block{ID: "web/navbar", Platform: "web", Title: "Navbar", Description: "Logo, four links and two actions across the top of a page.", build: func(b *builder) *Node {
			links := b.row("Links", 32, Pad{})
			for _, l := range b.items("links", []map[string]string{{"title": "Product"}, {"title": "Pricing"}, {"title": "Customers"}, {"title": "Docs"}}) {
				links.Children = append(links.Children, b.text("Link", "small-strong", "text-muted", l["title"]))
			}
			actions := b.row("Actions", 8, Pad{}, b.button("Sign in", b.s("secondary", "Sign in"), "ghost", "md", ""), b.button("Get started", b.s("cta", "Get started"), "primary", "md", ""))
			n := fixW(b.row("Navbar", 0, Pad{16, webMargin, 16, webMargin}, b.logo(b.s("brand", "Lumen")), links, actions), webWidth)
			n.Layout.Main = "between"
			return b.bg(n, "bg")
		}},
		&Block{ID: "web/hero", Platform: "web", Title: "Hero", Description: "Centered headline, supporting line, two buttons and a note.", build: func(b *builder) *Node {
			title := textAlign(b.para("Title", "display", "text", b.s("title", "Analytics your whole team can actually trust"), 960), "center")
			lead := textAlign(b.para("Lead", "lead", "text-muted", b.s("lead", "Lumen turns raw product events into answers in seconds. No SQL, no stale dashboards, no waiting on the data team."), 640), "center")
			buttons := b.row("Buttons", 12, Pad{}, b.button("Primary", b.s("cta", "Start free trial"), "primary", "lg", "arrow-right"), b.button("Secondary", b.s("secondary", "Book a demo"), "outline", "lg", ""))
			note := b.text("Note", "small", "text-muted", b.s("note", "Free for 14 days · No credit card required"))
			inner := align(b.col("Content", 24, Pad{}, b.badge("Badge", b.s("badge", "New: real-time funnels"), "sparkles"), title, lead, buttons, note), "start", "center")
			n := b.section("Hero", 0, Pad{128, webMargin, 112, webMargin}, inner)
			n.Layout.Cross = "center"
			return n
		}},
		&Block{ID: "web/hero-split", Platform: "web", Title: "Hero, split", Description: "Headline and actions on the left, a product image on the right.", build: func(b *builder) *Node {
			avatars := b.row("Avatars", 0, Pad{})
			for _, in := range []string{"AK", "JM", "SR", "LT"} {
				a := b.avatar("Avatar", in, 32)
				b.border(a, "bg", 2)
				avatars.Children = append(avatars.Children, a)
			}
			proof := b.row("Social proof", 12, Pad{}, avatars, b.text("Text", "small", "text-muted", b.s("proof", "Trusted by 2,400+ product teams")))
			left := fillX(b.col("Copy", 24, Pad{},
				b.badge("Badge", b.s("badge", "Version 3.0 is here"), "sparkles"),
				b.para("Title", "display", "text", b.s("title", "Ship the right features, faster"), 0),
				b.para("Lead", "lead", "text-muted", b.s("lead", "See how people really use your product, find where they drop off, and act on it the same afternoon."), 0),
				b.row("Buttons", 12, Pad{}, b.button("Primary", b.s("cta", "Start free trial"), "primary", "lg", "arrow-right"), b.button("Secondary", b.s("secondary", "Watch the tour"), "outline", "lg", "play")),
				proof))
			img := b.imagePlaceholder("Image", 0, 560)
			b.shadow(img, "lg")
			row := fillX(b.row("Columns", 64, Pad{}, left, img))
			return b.section("Hero", 0, Pad{96, webMargin, 96, webMargin}, row)
		}},
		&Block{ID: "web/logos", Platform: "web", Title: "Logo cloud", Description: "A line of social proof and six customer wordmarks.", build: func(b *builder) *Node {
			marks := b.row("Logos", 0, Pad{})
			marks.Layout.Main = "between"
			fillX(marks)
			for _, l := range b.items("logos", []map[string]string{{"title": "Northwind"}, {"title": "Halcyon"}, {"title": "Vertex"}, {"title": "Orbital"}, {"title": "Kestrel"}, {"title": "Monarch"}}) {
				marks.Children = append(marks.Children, b.text("Logo", "h4", "text-muted", l["title"]))
			}
			n := b.section("Logo cloud", 32, Pad{56, webMargin, 56, webMargin}, textAlign(b.text("Caption", "small-strong", "text-muted", b.s("caption", "Powering product decisions at fast-moving teams")), "center"), marks)
			n.Layout.Cross = "center"
			return n
		}},
		&Block{ID: "web/features", Platform: "web", Title: "Feature grid", Description: "Section header and a grid of features, each with an icon, title and text.", build: func(b *builder) *Node {
			items := b.items("features", []map[string]string{
				{"icon": "zap", "title": "Answers in seconds", "text": "Queries run on a columnar engine built for event data, so even a year of history comes back before you blink."},
				{"icon": "layers", "title": "One source of truth", "text": "Define a metric once and every chart, report and alert uses the same definition."},
				{"icon": "users", "title": "Built for the whole team", "text": "Product, design and support ask their own questions without waiting on an analyst."},
				{"icon": "shield-check", "title": "Private by default", "text": "SOC 2 Type II, EU hosting and field-level controls for anything sensitive."},
				{"icon": "workflow", "title": "Alerts that act", "text": "Send a Slack message or open a ticket the moment a metric moves outside its range."},
				{"icon": "plug", "title": "Connects in minutes", "text": "Stream events from your SDKs, warehouse or CDP. No pipelines to babysit."},
			})
			cols := 3
			grid := fillX(b.col("Grid", 32, Pad{}))
			grid.Layout.Cross = "stretch"
			var line *Node
			for i, it := range items {
				if i%cols == 0 {
					line = b.row("Row", 32, Pad{})
					line.Layout.Cross = "start"
					grid.Children = append(grid.Children, line)
				}
				tile := center(fixH(fixW(b.row("Icon tile", 0, Pad{}, b.icon(it["icon"], 20, "on-accent-soft")), 40), 40))
				radius(b.bg(tile, "accent-soft"), b.t.Radius.MD)
				card := fillX(b.col("Feature", 12, Pad{}, tile, b.para("Title", "title", "text", it["title"], 0), b.para("Text", "body", "text-muted", it["text"], 0)))
				card.Children[1].Name = "Title"
				line.Children = append(line.Children, card)
			}
			n := b.section("Features", 64, Pad{112, webMargin, 112, webMargin},
				b.sectionHeader(b.s("eyebrow", "Why Lumen"), b.s("title", "Everything you need to understand your users"), b.s("lead", "Fast where it matters, simple where it should be, and trustworthy all the way down."), true),
				grid)
			align(n, "start", "center")
			return n
		}},
		&Block{ID: "web/feature-split", Platform: "web", Title: "Feature, split", Description: "A feature explained with a checklist beside a large image.", build: func(b *builder) *Node {
			list := b.col("Checklist", 14, Pad{})
			for _, it := range b.items("points", []map[string]string{{"title": "Funnels, retention and paths in one place"}, {"title": "Break down any chart by any property"}, {"title": "Share a live link instead of a screenshot"}}) {
				list.Children = append(list.Children, b.checkItem("Point", it["title"], "text"))
			}
			copy := fillX(b.col("Copy", 20, Pad{},
				upper(b.text("Eyebrow", "overline", "on-accent-soft", b.s("eyebrow", "Explore"))),
				b.para("Title", "h2", "text", b.s("title", "Go from a hunch to an answer without leaving the chart"), 0),
				b.para("Text", "lead", "text-muted", b.s("text", "Click any point to see who is behind it, what they did before and what they did next."), 0),
				list,
				b.button("Link", b.s("cta", "See how it works"), "ghost", "md", "arrow-right")))
			copy.Children[4].Layout.Pad = Pad{}
			img := b.imagePlaceholder("Image", 0, 480)
			row := fillX(b.row("Columns", 80, Pad{}, img, copy))
			if b.s("image_side", "left") == "right" {
				row.Children = []*Node{copy, img}
			}
			return b.section("Feature", 0, Pad{96, webMargin, 96, webMargin}, row)
		}},
		&Block{ID: "web/stats", Platform: "web", Title: "Stats", Description: "Four large numbers with labels, on a muted band.", build: func(b *builder) *Node {
			row := fillX(b.row("Stats", 32, Pad{}))
			row.Layout.Cross = "start"
			items := b.items("stats", []map[string]string{{"value": "2.4k", "label": "Teams shipping with Lumen"}, {"value": "38B", "label": "Events analysed every month"}, {"value": "1.2s", "label": "Median query time"}, {"value": "99.99%", "label": "Uptime over the last year"}})
			for i, it := range items {
				if i > 0 {
					row.Children = append(row.Children, b.vdivider("Divider"))
				}
				row.Children = append(row.Children, fillX(b.col("Stat", 8, Pad{}, b.text("Value", "h1", "text", it["value"]), b.para("Label", "small", "text-muted", it["label"], 0))))
			}
			return b.bg(b.section("Stats", 0, Pad{72, webMargin, 72, webMargin}, row), "muted")
		}},
		&Block{ID: "web/pricing", Platform: "web", Title: "Pricing", Description: "Section header and three plans; the middle one is highlighted.", build: func(b *builder) *Node {
			plans := b.items("plans", []map[string]string{
				{"name": "Starter", "price": "$0", "period": "/month", "text": "For side projects and early experiments.", "cta": "Start for free", "features": "Up to 1M events|3 seats|30 days of history|Community support"},
				{"name": "Growth", "price": "$49", "period": "/month", "text": "For teams that ship every week.", "cta": "Start free trial", "features": "Up to 25M events|Unlimited seats|2 years of history|Alerts and Slack|Priority support", "featured": "true"},
				{"name": "Enterprise", "price": "Custom", "period": "", "text": "For companies with scale and security needs.", "cta": "Talk to sales", "features": "Unlimited events|SSO and SCIM|EU or US hosting|Dedicated success manager"},
			})
			row := fillX(b.row("Plans", 24, Pad{}))
			row.Layout.Cross = "stretch"
			for _, p := range plans {
				featured := p["featured"] == "true"
				price := b.row("Price", 4, Pad{}, b.text("Amount", "h1", "text", p["price"]), b.text("Period", "small", "text-muted", p["period"]))
				price.Layout.Cross = "end"
				head := b.row("Head", 8, Pad{}, b.text("Name", "title", "text", p["name"]))
				if featured {
					head.Children = append(head.Children, b.badge("Badge", "Most popular", ""))
				}
				feats := b.col("Features", 12, Pad{})
				for _, f := range splitList(p["features"]) {
					feats.Children = append(feats.Children, b.checkItem("Feature", f, "text"))
				}
				variant := "outline"
				if featured {
					variant = "primary"
				}
				btn := fillX(b.button("Button", p["cta"], variant, "lg", ""))
				card := fillX(b.col("Plan", 24, Pad{32, 32, 32, 32}, head, price, b.para("Text", "small", "text-muted", p["text"], 0), btn, b.divider("Divider"), feats))
				card.Layout.Cross = "stretch"
				for _, c := range card.Children {
					if c.Type == TypeFrame && c.Name != "Button" {
						c.SizingX = ""
					}
				}
				radius(b.bg(card, "card"), b.t.Radius.LG)
				if featured {
					b.border(card, "accent", 2)
					b.shadow(card, "lg")
				} else {
					b.framed(card, "border")
				}
				row.Children = append(row.Children, card)
			}
			n := b.section("Pricing", 64, Pad{112, webMargin, 112, webMargin}, b.sectionHeader(b.s("eyebrow", "Pricing"), b.s("title", "Simple pricing that grows with you"), b.s("lead", "Start free. Upgrade when your team needs more history, seats or control."), true), row)
			align(n, "start", "center")
			return n
		}},
		&Block{ID: "web/testimonials", Platform: "web", Title: "Testimonials", Description: "Section header and three quote cards with the person behind each.", build: func(b *builder) *Node {
			quotes := b.items("quotes", []map[string]string{
				{"quote": "We replaced three tools and a weekly reporting meeting. Everyone just looks it up now.", "name": "Amara Kone", "role": "Head of Product, Halcyon"},
				{"quote": "The first analytics tool our designers open without being asked. That alone paid for it.", "name": "Jonas Meyer", "role": "Design Lead, Vertex"},
				{"quote": "Setup took an afternoon. A week later we found the onboarding step losing 30% of signups.", "name": "Sofia Ruiz", "role": "Founder, Kestrel"},
			})
			row := fillX(b.row("Quotes", 24, Pad{}))
			row.Layout.Cross = "stretch"
			for _, q := range quotes {
				initials := ""
				for _, w := range strings.Fields(q["name"]) {
					initials += string([]rune(w)[0])
				}
				person := b.row("Person", 12, Pad{}, b.avatar("Avatar", initials, 40), b.col("Who", 2, Pad{}, b.text("Name", "small-strong", "text", q["name"]), b.text("Role", "small", "text-muted", q["role"])))
				card := fillX(b.col("Testimonial", 24, Pad{28, 28, 28, 28}, b.icon("quote", 24, "on-accent-soft"), b.para("Quote", "body", "text", q["quote"], 0), b.spacer("column"), person))
				radius(b.bg(card, "card"), b.t.Radius.LG)
				b.framed(card, "border")
				row.Children = append(row.Children, card)
			}
			n := b.section("Testimonials", 64, Pad{112, webMargin, 112, webMargin}, b.sectionHeader(b.s("eyebrow", "Customers"), b.s("title", "Teams that stopped guessing"), b.s("lead", "From two-person startups to public companies, people use Lumen to decide what to build next."), true), row)
			align(n, "start", "center")
			return n
		}},
		&Block{ID: "web/faq", Platform: "web", Title: "FAQ", Description: "Questions with answers in a single readable column.", build: func(b *builder) *Node {
			list := fixW(b.col("Questions", 0, Pad{}), 768)
			list.Layout.Cross = "stretch"
			for i, q := range b.items("questions", []map[string]string{
				{"q": "How long does setup take?", "a": "Most teams send their first events within an hour. Our SDKs cover web, iOS, Android and every major backend."},
				{"q": "Can I import historical data?", "a": "Yes. Load events from your warehouse or a CSV and they appear alongside live data."},
				{"q": "Is my data used to train models?", "a": "Never. Your data stays yours, is encrypted at rest and can be deleted at any time."},
				{"q": "What happens when my trial ends?", "a": "You move to the free plan automatically. Nothing is deleted and you can upgrade whenever you like."},
			}) {
				if i > 0 {
					list.Children = append(list.Children, b.divider("Divider"))
				}
				head := fillX(b.row("Question", 16, Pad{}, fillX(b.para("Text", "title", "text", q["q"], 0)), b.icon("plus", 20, "text-muted")))
				item := b.col("Item", 8, Pad{24, 0, 24, 0}, head, b.para("Answer", "body", "text-muted", q["a"], 0))
				item.Layout.Cross = "stretch"
				list.Children = append(list.Children, item)
			}
			n := b.section("FAQ", 48, Pad{112, webMargin, 112, webMargin}, b.sectionHeader(b.s("eyebrow", "FAQ"), b.s("title", "Questions, answered"), b.s("lead", "Anything else? Our team replies within a few hours on weekdays."), true), list)
			align(n, "start", "center")
			return n
		}},
		&Block{ID: "web/cta", Platform: "web", Title: "Call to action", Description: "A bold accent panel asking for the one action that matters.", build: func(b *builder) *Node {
			panel := fillX(b.col("Panel", 24, Pad{80, 64, 80, 64},
				textAlign(b.para("Title", "h1", "on-accent", b.s("title", "Your next decision deserves real data"), 720), "center"),
				textAlign(opacity(b.para("Lead", "lead", "on-accent", b.s("lead", "Join 2,400 teams who know exactly what to build next."), 560), 0.85), "center"),
				b.row("Buttons", 12, Pad{}, b.button("Primary", b.s("cta", "Start free trial"), "inverse", "lg", "arrow-right"))))
			align(panel, "start", "center")
			clip(radius(b.bg(panel, "accent"), b.t.Radius.XL))
			return b.section("CTA", 0, Pad{64, webMargin, 96, webMargin}, panel)
		}},
		&Block{ID: "web/footer", Platform: "web", Title: "Footer", Description: "Brand, three link columns and a bottom line with legal text.", build: func(b *builder) *Node {
			brand := b.col("Brand", 16, Pad{}, b.logo(b.s("brand", "Lumen")), b.para("Text", "small", "text-muted", b.s("tagline", "Product analytics for teams who would rather build than wait for reports."), 280))
			cols := b.row("Columns", 64, Pad{})
			cols.Layout.Cross = "start"
			for _, c := range b.items("columns", []map[string]string{
				{"title": "Product", "links": "Features|Pricing|Integrations|Changelog"},
				{"title": "Company", "links": "About|Customers|Careers|Contact"},
				{"title": "Resources", "links": "Docs|Guides|API status|Security"},
			}) {
				col := b.col("Column", 12, Pad{}, b.text("Title", "small-strong", "text", c["title"]))
				for _, l := range splitList(c["links"]) {
					col.Children = append(col.Children, b.text("Link", "small", "text-muted", l))
				}
				cols.Children = append(cols.Children, col)
			}
			top := fillX(b.row("Top", 0, Pad{}, brand, cols))
			top.Layout.Main, top.Layout.Cross = "between", "start"
			social := b.row("Social", 16, Pad{}, b.icon("at-sign", 18, "text-muted"), b.icon("rss", 18, "text-muted"), b.icon("globe", 18, "text-muted"))
			bottom := fillX(b.row("Bottom", 0, Pad{}, b.text("Legal", "caption", "text-muted", b.s("legal", "© 2026 Lumen Labs, Inc. All rights reserved.")), social))
			bottom.Layout.Main = "between"
			return b.section("Footer", 40, Pad{64, webMargin, 40, webMargin}, top, b.divider("Divider"), bottom)
		}},
		&Block{ID: "web/button", Platform: "web", Title: "Button", Description: "One button: variant primary, secondary, outline or ghost; size sm, md or lg; optional icon.", build: func(b *builder) *Node {
			return b.button("Button", b.s("label", "Continue"), b.s("variant", "primary"), b.s("size", "md"), b.s("icon", ""))
		}},
		&Block{ID: "web/badge", Platform: "web", Title: "Badge", Description: "A small pill label in the accent's soft tone.", build: func(b *builder) *Node {
			return b.badge("Badge", b.s("label", "New"), b.s("icon", ""))
		}},
		&Block{ID: "web/input", Platform: "web", Title: "Input", Description: "Labelled text field with placeholder and hint.", build: func(b *builder) *Node {
			return b.input(b.s("label", "Work email"), b.s("placeholder", "you@company.com"), b.s("hint", "We'll never share it."), b.s("icon", "mail"), 360)
		}},
		&Block{ID: "web/card", Platform: "web", Title: "Card", Description: "Content card: image, tag, title, text and a link.", build: func(b *builder) *Node {
			body := b.col("Body", 12, Pad{24, 24, 24, 24},
				upper(b.text("Tag", "overline", "on-accent-soft", b.s("tag", "Guide"))),
				b.para("Title", "title", "text", b.s("title", "Five funnels every SaaS team should track"), 0),
				b.para("Text", "small", "text-muted", b.s("text", "A practical walkthrough from signup to the moment a team invites its first colleague."), 0),
				b.row("Link", 6, Pad{}, b.text("Label", "small-strong", "text", b.s("link", "Read the guide")), b.icon("arrow-right", 16, "text")))
			body.Layout.Cross = "stretch"
			for _, c := range body.Children {
				if c.Name == "Link" {
					c.SizingX = "hug"
				}
			}
			img := fixH(b.imagePlaceholder("Image", 0, 200), 200)
			img.Radius = nil
			card := fixW(b.col("Card", 0, Pad{}, img, body), 380)
			card.Layout.Cross = "stretch"
			clip(radius(b.bg(card, "card"), b.t.Radius.LG))
			b.framed(card, "border")
			return b.shadow(card, "sm")
		}},
		&Block{ID: "web/signin", Platform: "web", Title: "Sign in", Description: "Centered sign-in card with email, password and a social option.", build: func(b *builder) *Node {
			sep := fillX(b.row("Separator", 12, Pad{}, b.divider("Line"), b.text("Or", "caption", "text-muted", "OR"), b.divider("Line")))
			sep.Children[0].SizingX, sep.Children[2].SizingX = "fill", "fill"
			card := fixW(b.col("Card", 20, Pad{40, 40, 40, 40},
				b.logo(b.s("brand", "Lumen")),
				b.col("Heading", 6, Pad{}, b.text("Title", "h3", "text", b.s("title", "Welcome back")), b.text("Text", "small", "text-muted", b.s("subtitle", "Sign in to keep exploring your data."))),
				b.input("Email", "you@company.com", "", "mail", 0),
				b.input("Password", "••••••••", "", "lock", 0),
				fillX(b.button("Submit", b.s("cta", "Sign in"), "primary", "lg", "")),
				sep,
				fillX(b.button("Google", "Continue with Google", "outline", "lg", "globe")),
				textAlign(b.para("Footer", "small", "text-muted", b.s("footer", "No account yet? Start a free trial"), 0), "center")), 440)
			card.Layout.Cross = "stretch"
			for _, c := range card.Children {
				if c.Name == "Logo" || c.Name == "Heading" {
					c.SizingX = "hug"
				}
			}
			radius(b.bg(card, "card"), b.t.Radius.XL)
			b.framed(card, "border")
			b.shadow(card, "md")
			n := center(fixH(fixW(b.col("Sign in", 0, Pad{}, card), webWidth), 900))
			return b.bg(n, "bg")
		}},
		&Block{ID: "web/app-shell", Platform: "web", Title: "App shell", Description: "Dashboard screen: sidebar, top bar, four stat cards and a table.", build: func(b *builder) *Node {
			return b.appShell()
		}},
		&Block{ID: "web/stat-card", Platform: "web", Title: "Stat card", Description: "A metric with its label, change and trend icon.", build: func(b *builder) *Node {
			return fixW(b.statCard(b.s("label", "Active users"), b.s("value", "12,480"), b.s("change", "+8.2%"), b.s("trend", "up")), 280)
		}},
		&Block{ID: "web/table", Platform: "web", Title: "Table", Description: "A card holding a table with a header row and status badges.", build: func(b *builder) *Node {
			return fixW(b.table(), 960)
		}},
		&Block{ID: "web/alert", Platform: "web", Title: "Alert", Description: "An inline message with an icon, title and text; tone info, success, warning or error.", build: func(b *builder) *Node {
			tone := b.s("tone", "info")
			icons := map[string]string{"info": "info", "success": "circle-check", "warning": "info", "error": "circle-x"}
			if icons[tone] == "" {
				tone = "info"
			}
			n := fixW(b.row("Alert", 12, Pad{16, 16, 16, 16}, b.icon(icons[tone], 20, tone), fillX(b.col("Text", 4, Pad{}, b.text("Title", "small-strong", "text", b.s("title", "Your export is ready")), b.para("Body", "small", "text-muted", b.s("text", "We emailed a link to the CSV. It stays available for seven days."), 0)))), 480)
			n.Layout.Cross = "start"
			n.Children[1].Layout.Cross = "stretch"
			radius(b.bg(n, "card"), b.t.Radius.MD)
			return b.border(n, tone, 1)
		}},
		&Block{ID: "web/tabs", Platform: "web", Title: "Tabs", Description: "A segmented tab bar with the first tab selected.", build: func(b *builder) *Node {
			n := b.row("Tabs", 4, Pad{4, 4, 4, 4})
			for i, t := range b.items("tabs", []map[string]string{{"title": "Overview"}, {"title": "Funnels"}, {"title": "Retention"}, {"title": "Users"}}) {
				tab := center(fixH(b.row("Tab", 0, Pad{0, 14, 0, 14}, b.text("Label", "small-strong", "text-muted", t["title"])), 32))
				if i == 0 {
					tab.Children[0].Fills = []Paint{b.paint("text")}
					radius(b.shadow(b.bg(tab, "surface"), "sm"), b.t.Radius.SM)
				}
				n.Children = append(n.Children, tab)
			}
			return radius(b.bg(n, "muted"), b.t.Radius.MD)
		}},
		&Block{ID: "web/modal", Platform: "web", Title: "Dialog", Description: "A confirmation dialog with title, text and two actions.", build: func(b *builder) *Node {
			actions := fillX(b.row("Actions", 8, Pad{}, b.button("Cancel", b.s("cancel", "Cancel"), "outline", "md", ""), b.button("Confirm", b.s("confirm", "Delete report"), "primary", "md", "")))
			actions.Layout.Main = "end"
			n := fixW(b.col("Dialog", 20, Pad{24, 24, 24, 24}, b.text("Title", "h4", "text", b.s("title", "Delete this report?")), b.para("Text", "small", "text-muted", b.s("text", "Everyone with access loses it too. Charts saved to dashboards stay, but stop updating."), 0), actions), 440)
			n.Layout.Cross = "stretch"
			n.Children[0].SizingX = ""
			radius(b.bg(n, "card"), b.t.Radius.LG)
			b.framed(n, "border")
			return b.shadow(n, "lg")
		}},
		&Block{ID: "web/empty-state", Platform: "web", Title: "Empty state", Description: "Icon, message and one action for a screen with nothing in it yet.", build: func(b *builder) *Node {
			tile := center(fixH(fixW(b.row("Icon tile", 0, Pad{}, b.icon(b.s("icon", "chart-column"), 24, "on-accent-soft")), 56), 56))
			radius(b.bg(tile, "accent-soft"), b.t.Radius.LG)
			n := align(fixW(b.col("Empty state", 16, Pad{48, 32, 48, 32}, tile, textAlign(b.para("Title", "title", "text", b.s("title", "No reports yet"), 360), "center"), textAlign(b.para("Text", "small", "text-muted", b.s("text", "Create your first report to see how people move through your product."), 360), "center"), b.button("Action", b.s("cta", "New report"), "primary", "md", "plus")), 480), "start", "center")
			return n
		}},
	)
}

func (b *builder) input(label, placeholder, hint, iconName string, width float64) *Node {
	field := fillX(fixH(b.row("Field", 8, Pad{0, 12, 0, 12}), 40))
	if iconName != "" {
		field.Children = append(field.Children, b.icon(iconName, 16, "text-subtle"))
	}
	field.Children = append(field.Children, b.text("Placeholder", "small", "text-muted", placeholder))
	radius(b.bg(field, "surface"), b.t.Radius.MD)
	b.border(field, "border-strong", max(1, b.t.BorderWidth))
	n := b.col("Input/"+label, 6, Pad{}, b.text("Label", "small-strong", "text", label), field)
	n.Layout.Cross = "stretch"
	n.Children[0].SizingX = ""
	if hint != "" {
		n.Children = append(n.Children, b.text("Hint", "caption", "text-muted", hint))
	}
	if width > 0 {
		fixW(n, width)
	} else {
		fillX(n)
	}
	return n
}

func (b *builder) statCard(label, value, change, trend string) *Node {
	token, iconName := "success", "trending-up"
	if trend == "down" {
		token, iconName = "error", "trending-down"
	}
	delta := b.row("Change", 4, Pad{}, b.icon(iconName, 14, token), b.text("Value", "small-strong", token, change), b.text("Period", "small", "text-muted", "vs last week"))
	n := b.col("Stat card", 8, Pad{20, 20, 20, 20}, b.text("Label", "small-strong", "text-muted", label), b.text("Value", "h2", "text", value), delta)
	radius(b.bg(n, "card"), b.t.Radius.LG)
	return b.framed(n, "border")
}

func (b *builder) table() *Node {
	cols := []struct {
		title string
		width float64
	}{{"Customer", 0}, {"Plan", 140}, {"Status", 140}, {"MRR", 120}}
	header := fillX(b.row("Header", 16, Pad{12, 20, 12, 20}))
	for _, c := range cols {
		cell := upper(b.text(c.title, "overline", "text-muted", c.title))
		header.Children = append(header.Children, b.cell(cell, c.width))
	}
	b.bg(header, "muted")
	n := b.col("Table", 0, Pad{})
	n.Layout.Cross = "stretch"
	title := fillX(b.row("Title", 0, Pad{16, 20, 16, 20}, b.text("Text", "title", "text", b.s("title", "Recent customers")), b.button("Action", "View all", "outline", "sm", "")))
	title.Layout.Main = "between"
	n.Children = append(n.Children, title, header)
	tones := map[string]string{"Active": "success", "Trial": "info", "Past due": "warning", "Churned": "error"}
	for _, r := range b.items("rows", []map[string]string{
		{"name": "Halcyon", "email": "ops@halcyon.io", "plan": "Growth", "status": "Active", "mrr": "$1,240"},
		{"name": "Vertex Labs", "email": "billing@vertex.dev", "plan": "Enterprise", "status": "Active", "mrr": "$8,900"},
		{"name": "Kestrel", "email": "hello@kestrel.app", "plan": "Growth", "status": "Trial", "mrr": "$0"},
		{"name": "Orbital", "email": "team@orbital.co", "plan": "Starter", "status": "Past due", "mrr": "$49"},
		{"name": "Monarch", "email": "finance@monarch.com", "plan": "Growth", "status": "Churned", "mrr": "$0"},
	}) {
		tone := tones[r["status"]]
		if tone == "" {
			tone = "info"
		}
		dot := radius(b.rect("Dot", 6, 6, tone), 3)
		status := b.row("Status", 6, Pad{}, dot, b.text("Label", "small", "text", r["status"]))
		who := b.row("Customer", 12, Pad{}, b.avatar("Avatar", string([]rune(r["name"])[0]), 32), b.col("Who", 0, Pad{}, b.text("Name", "small-strong", "text", r["name"]), b.text("Email", "caption", "text-muted", r["email"])))
		line := fillX(b.row("Row", 16, Pad{14, 20, 14, 20}, b.cell(who, 0), b.cell(b.text("Plan", "small", "text", r["plan"]), 140), b.cell(status, 140), b.cell(b.text("MRR", "small-strong", "text", r["mrr"]), 120)))
		n.Children = append(n.Children, b.divider("Divider"), line)
	}
	radius(clip(b.bg(n, "card")), b.t.Radius.LG)
	return b.framed(n, "border")
}

// cell wraps a table cell's content at a column width (0 takes the rest).
func (b *builder) cell(content *Node, width float64) *Node {
	c := b.row("Cell", 0, Pad{}, content)
	if width > 0 {
		return fixW(c, width)
	}
	return fillX(c)
}

func (b *builder) appShell() *Node {
	nav := b.col("Nav", 4, Pad{})
	nav.Layout.Cross = "stretch"
	for i, it := range b.items("nav", []map[string]string{
		{"icon": "layout-dashboard", "title": "Overview"},
		{"icon": "chart-column", "title": "Reports"},
		{"icon": "users", "title": "Customers"},
		{"icon": "inbox", "title": "Inbox"},
		{"icon": "calendar", "title": "Schedule"},
		{"icon": "settings", "title": "Settings"},
	}) {
		token := "text-muted"
		if i == 0 {
			token = "on-accent-soft"
		}
		item := fixH(b.row("Item", 10, Pad{0, 12, 0, 12}, b.icon(it["icon"], 18, token), b.text("Label", "small-strong", token, it["title"])), 36)
		radius(item, b.t.Radius.MD)
		if i == 0 {
			b.bg(item, "accent-soft")
		}
		nav.Children = append(nav.Children, item)
	}
	user := b.row("User", 10, Pad{12, 8, 0, 8}, b.avatar("Avatar", "AK", 32), b.col("Who", 0, Pad{}, b.text("Name", "small-strong", "text", "Amara Kone"), b.text("Email", "caption", "text-muted", "amara@halcyon.io")))
	side := fixW(b.col("Sidebar", 24, Pad{20, 16, 20, 16}, b.logo(b.s("brand", "Lumen")), nav, b.spacer("column"), user), 256)
	side.Layout.Cross = "stretch"
	side.Children[0].SizingX = "hug"
	side.Children[3].SizingX = "hug"
	fillY(side)
	b.bg(side, "surface")
	search := fixW(fixH(b.row("Search", 8, Pad{0, 12, 0, 12}, b.icon("search", 16, "text-muted"), b.text("Placeholder", "small", "text-muted", "Search reports…")), 36), 280)
	radius(b.bg(search, "muted"), b.t.Radius.MD)
	top := fillX(b.row("Top bar", 12, Pad{}, b.col("Heading", 2, Pad{}, b.text("Title", "h3", "text", b.s("title", "Overview")), b.text("Subtitle", "small", "text-muted", b.s("subtitle", "Last 30 days · All products"))), b.spacer("row"), search, b.button("New", "New report", "primary", "md", "plus")))
	stats := fillX(b.row("Stats", 16, Pad{}))
	stats.Layout.Cross = "stretch"
	for _, s := range b.items("stats", []map[string]string{
		{"label": "Active users", "value": "12,480", "change": "+8.2%", "trend": "up"},
		{"label": "Conversion", "value": "4.6%", "change": "+0.4%", "trend": "up"},
		{"label": "Avg. session", "value": "6m 12s", "change": "-3.1%", "trend": "down"},
		{"label": "MRR", "value": "$84.2k", "change": "+12%", "trend": "up"},
	}) {
		stats.Children = append(stats.Children, fillX(b.statCard(s["label"], s["value"], s["change"], s["trend"])))
	}
	main := fillY(fillX(b.col("Main", 24, Pad{28, 32, 28, 32}, top, stats, fillX(b.table()))))
	main.Layout.Cross = "stretch"
	b.bg(main, "bg")
	n := fixH(fixW(b.row("App", 0, Pad{}, side, b.vdivider("Divider"), main), webWidth), 900)
	n.Layout.Cross = "stretch"
	return clip(b.bg(n, "bg"))
}
