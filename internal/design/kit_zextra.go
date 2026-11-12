package design

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

func init() {
	sec := func(bl *Block) *Block { bl.Section = true; return bl }
	register(
		sec(&Block{ID: "web/bento", Platform: "web", Title: "Bento", Description: "A wide card over a row of two smaller ones, for a product story that is not three identical cards.", build: func(b *builder) *Node {
			wide := b.storyCard(b.s("wide_title", "One place for the week's work"), b.s("wide_text", "Plans, notes and the people on them, without a second tool."), 0)
			left := b.storyCard(b.s("left_title", "Shared with the room"), b.s("left_text", "A link is enough. No accounts for guests."), 360)
			right := b.storyCard(b.s("right_title", "Ready by Friday"), b.s("right_text", "The open items are listed, not buried in a thread."), 360)
			row := fillX(b.row("Pair", 24, Pad{}, left, right))
			return b.section("Bento", 24, Pad{96, webMargin, 96, webMargin}, wide, row)
		}}),
		sec(&Block{ID: "web/comparison", Platform: "web", Title: "Comparison", Description: "A header and rows that compare two options on the same facts.", build: func(b *builder) *Node {
			head := fillX(b.row("Head", 16, Pad{0, 16, 0, 16}, b.text("Fact", "small-strong", "text-muted", " "), fillX(b.text("A", "small-strong", "text", b.s("a", "Studio"))), fillX(b.text("B", "small-strong", "text", b.s("b", "Studio Pro")))))
			list := b.col("Rows", 0, Pad{})
			list.Layout.Cross = "stretch"
			for _, row := range b.items("rows", []map[string]string{
				{"fact": "Seats", "a": "3", "b": "Unlimited"},
				{"fact": "History", "a": "30 days", "b": "Unlimited"},
				{"fact": "Support", "a": "Email", "b": "A named person"},
			}) {
				line := fillX(b.row("Row", 16, Pad{12, 16, 12, 16}, b.text("Fact", "body", "text", row["fact"]), fillX(b.text("A", "body", "text-muted", row["a"])), fillX(b.text("B", "body", "text", row["b"]))))
				list.Children = append(list.Children, line, fillX(b.rect("Line", 100, 1, "border")))
			}
			card := fillX(b.col("Table", 0, Pad{8, 0, 8, 0}, head, list))
			card.Layout.Cross = "stretch"
			radius(b.bg(card, "card"), b.t.Radius.LG)
			b.framed(card, "border")
			return b.section("Comparison", 32, Pad{96, webMargin, 96, webMargin}, b.sectionHeader(b.s("eyebrow", "Compare"), b.s("title", "What changes when you upgrade"), b.s("lead", "The same product. More room and a person to call."), true), card)
		}}),
		sec(&Block{ID: "web/timeline", Platform: "web", Title: "Timeline", Description: "A vertical sequence of dated steps.", build: func(b *builder) *Node {
			col := b.col("Steps", 0, Pad{})
			col.Layout.Cross = "stretch"
			for _, step := range b.items("steps", []map[string]string{
				{"when": "March", "title": "Private beta", "text": "Forty teams, one build a week."},
				{"when": "June", "title": "Open to everyone", "text": "The same editor, with shared libraries."},
				{"when": "October", "title": "Companies", "text": "SSO, an audit log and a named contact."},
			}) {
				dot := radius(b.rect("Dot", 12, 12, "accent"), 6)
				text := fillX(b.col("Text", 4, Pad{}, b.text("When", "caption", "text-muted", step["when"]), b.text("Title", "h4", "text", step["title"]), b.para("Body", "small", "text-muted", step["text"], 0)))
				row := fillX(b.row("Step", 16, Pad{0, 0, 24, 0}, dot, text))
				row.Layout.Cross = "start"
				col.Children = append(col.Children, row)
			}
			return b.section("Timeline", 32, Pad{96, webMargin, 96, webMargin}, b.sectionHeader(b.s("eyebrow", "Path"), b.s("title", "How we got here"), b.s("lead", "Three marks on a calendar, not a slogan."), false), col)
		}}),
		sec(&Block{ID: "web/team", Platform: "web", Title: "Team", Description: "People with a role, not a row of identical portraits.", build: func(b *builder) *Node {
			row := fillX(b.row("People", 24, Pad{}))
			for _, p := range b.items("people", []map[string]string{
				{"initials": "AK", "name": "Amara Kone", "role": "Design"},
				{"initials": "JM", "name": "Jonas Meyer", "role": "Engineering"},
				{"initials": "SR", "name": "Sofia Ruiz", "role": "Research"},
			}) {
				card := b.col("Person", 12, Pad{24, 24, 24, 24}, b.avatar("Avatar", p["initials"], 56), b.text("Name", "h4", "text", p["name"]), b.text("Role", "small", "text-muted", p["role"]))
				card.Layout.Cross = "start"
				radius(b.bg(card, "card"), b.t.Radius.LG)
				b.framed(card, "border")
				row.Children = append(row.Children, fillX(card))
			}
			return b.section("Team", 40, Pad{96, webMargin, 96, webMargin}, b.sectionHeader(b.s("eyebrow", "People"), b.s("title", "Who you will work with"), b.s("lead", "A small group. You will know their names."), false), row)
		}}),
		sec(&Block{ID: "web/gallery", Platform: "web", Title: "Gallery", Description: "Three themed pictures with captions.", build: func(b *builder) *Node {
			row := fillX(b.row("Pictures", 24, Pad{}))
			for i, cap := range []string{b.s("one", "The studio in March"), b.s("two", "A desk by the window"), b.s("three", "Prints on the wall")} {
				shot := b.imagePlaceholder("Picture "+strconv.Itoa(i+1), 0, 280)
				card := fillX(b.col("Shot", 12, Pad{}, shot, b.text("Caption", "small", "text-muted", cap)))
				row.Children = append(row.Children, card)
			}
			return b.section("Gallery", 32, Pad{96, webMargin, 96, webMargin}, b.text("Title", "h2", "text", b.s("title", "Recent work")), row)
		}}),
		sec(&Block{ID: "web/blog", Platform: "web", Title: "Blog list", Description: "Three articles: picture, title, date and a line.", build: func(b *builder) *Node {
			col := b.col("Posts", 24, Pad{})
			col.Layout.Cross = "stretch"
			for i, post := range b.items("posts", []map[string]string{
				{"title": "What we cut from the editor", "date": "12 March", "text": "Six tools became two. The work got faster."},
				{"title": "Pricing without a grid of cards", "date": "2 March", "text": "People asked what they were paying for. We wrote it down."},
				{"title": "A week with the support queue", "date": "18 February", "text": "The same five questions, and the answers we now ship in the product."},
			}) {
				img := fixW(b.imagePlaceholder("Cover "+strconv.Itoa(i+1), 280, 180), 280)
				text := fillX(b.col("Text", 8, Pad{}, b.text("Date", "caption", "text-muted", post["date"]), b.text("Title", "h4", "text", post["title"]), b.para("Body", "small", "text-muted", post["text"], 0)))
				col.Children = append(col.Children, fillX(b.row("Post", 24, Pad{}, img, text)))
			}
			return b.section("Journal", 40, Pad{96, webMargin, 96, webMargin}, b.text("Title", "h2", "text", b.s("title", "Notes")), col)
		}}),
		sec(&Block{ID: "web/form", Platform: "web", Title: "Contact form", Description: "Name, email, a message and a send button.", build: func(b *builder) *Node {
			form := fixW(b.col("Form", 16, Pad{},
				b.input(b.s("name_label", "Name"), b.s("name", "Amara Kone"), "", "", 480),
				b.input(b.s("email_label", "Email"), b.s("email", "amara@studio.test"), "", "", 480),
				b.input(b.s("message_label", "Message"), b.s("message", "We need a design system for the mobile app."), "", "", 480),
				b.button("Send", b.s("cta", "Send"), "primary", "lg", ""),
			), 480)
			return b.section("Contact", 32, Pad{96, webMargin, 96, webMargin}, b.sectionHeader(b.s("eyebrow", "Contact"), b.s("title", "Write to the studio"), b.s("lead", "We reply on business days."), false), form)
		}}),
		sec(&Block{ID: "web/newsletter", Platform: "web", Title: "Newsletter", Description: "One line of copy, an email field and a button.", build: func(b *builder) *Node {
			row := b.row("Join", 12, Pad{}, b.input("", b.s("placeholder", "you@studio.test"), "", "", 360), b.button("Join", b.s("cta", "Subscribe"), "primary", "lg", ""))
			card := b.col("Card", 16, Pad{32, 32, 32, 32}, b.text("Title", "h3", "text", b.s("title", "A short letter, once a month")), b.para("Text", "small", "text-muted", b.s("text", "No product launches dressed up as advice. Unsubscribe in one click."), 0), row)
			radius(b.bg(card, "card"), b.t.Radius.LG)
			b.framed(card, "border")
			return b.section("Newsletter", 0, Pad{64, webMargin, 96, webMargin}, card)
		}}),
		sec(&Block{ID: "web/cart", Platform: "web", Title: "Cart", Description: "Lines in a basket with a quantity and a total.", build: func(b *builder) *Node {
			lines := b.col("Lines", 0, Pad{})
			lines.Layout.Cross = "stretch"
			for _, item := range b.items("items", []map[string]string{
				{"title": "Oak desk lamp", "qty": "1", "price": "€86"},
				{"title": "Linen shade", "qty": "2", "price": "€48"},
			}) {
				row := fillX(b.row("Line", 16, Pad{16, 0, 16, 0}, fillX(b.text("Title", "body", "text", item["title"])), b.text("Qty", "body", "text-muted", "× "+item["qty"]), b.text("Price", "body-strong", "text", item["price"])))
				lines.Children = append(lines.Children, row, fillX(b.rect("Rule", 100, 1, "border")))
			}
			total := fillX(b.row("Total", 16, Pad{8, 0, 0, 0}, fillX(b.text("Label", "h4", "text", "Total")), b.text("Sum", "h4", "text", b.s("total", "€182"))))
			return b.section("Cart", 24, Pad{64, webMargin, 64, webMargin}, b.text("Title", "h2", "text", b.s("title", "Your basket")), lines, total, b.button("Checkout", b.s("cta", "Checkout"), "primary", "lg", ""))
		}}),
		sec(&Block{ID: "web/checkout", Platform: "web", Title: "Checkout", Description: "Delivery fields beside an order summary.", build: func(b *builder) *Node {
			form := fillX(b.col("Details", 16, Pad{}, b.text("Heading", "h3", "text", b.s("heading", "Delivery")), b.input("Full name", b.s("name", "Amara Kone"), "", "", 0), b.input("Address", b.s("address", "14 Harbour Street"), "", "", 0), b.input("City", b.s("city", "Lisbon"), "", "", 0), b.button("Pay", b.s("cta", "Pay €182"), "primary", "lg", "")))
			summary := fixW(b.col("Summary", 12, Pad{24, 24, 24, 24}, b.text("Title", "h4", "text", "Order"), b.text("Line", "body", "text", b.s("line", "Oak desk lamp")), b.text("Ship", "small", "text-muted", b.s("ship", "Ships in two days")), b.text("Due", "h4", "text", b.s("due", "€182"))), 320)
			radius(b.bg(summary, "muted"), b.t.Radius.LG)
			row := fillX(b.row("Checkout", 48, Pad{}, form, summary))
			row.Layout.Cross = "start"
			return b.section("Checkout", 0, Pad{64, webMargin, 80, webMargin}, row)
		}}),
		sec(&Block{ID: "web/pricing-toggle", Platform: "web", Title: "Pricing toggle", Description: "Monthly or yearly, with the saving written out.", build: func(b *builder) *Node {
			month := center(fixH(b.row("Month", 0, Pad{0, 16, 0, 16}, b.text("Label", "small-strong", "text-muted", b.s("monthly", "Monthly"))), 36))
			year := center(fixH(b.row("Year", 0, Pad{0, 16, 0, 16}, b.text("Label", "small-strong", "on-accent", b.s("yearly", "Yearly"))), 36))
			radius(b.bg(year, "accent"), 999)
			toggle := b.row("Toggle", 4, Pad{4, 4, 4, 4}, month, year)
			radius(b.bg(toggle, "muted"), 999)
			note := b.text("Note", "small", "text-muted", b.s("note", "Yearly saves two months."))
			wrap := align(b.col("Toggle block", 12, Pad{}, toggle, note), "start", "center")
			return b.section("Billing", 0, Pad{48, webMargin, 24, webMargin}, wrap)
		}}),
		&Block{ID: "web/sidebar", Platform: "web", Title: "Sidebar", Description: "A narrow navigation column with a wordmark and links.", build: func(b *builder) *Node {
			return b.sideColumn(b.s("brand", "Lumen"))
		}},
		&Block{ID: "web/desktop", Platform: "web", Title: "Desktop shell", Description: "A sidebar and a detail column at 1440 wide.", build: func(b *builder) *Node {
			main := fillX(b.col("Detail", 16, Pad{32, 40, 32, 40}, b.text("Title", "h2", "text", b.s("title", "Settings")), b.para("Lead", "body", "text-muted", b.s("lead", "Account, notifications and the people who can see this workspace."), 0), b.input("Workspace name", b.s("workspace", "Studio North"), "", "", 0)))
			main.Layout.Cross = "stretch"
			row := fixH(fixW(b.row("Desktop", 0, Pad{}, b.sideColumn(b.s("brand", "Lumen")), main), webWidth), 900)
			row.Layout.Cross = "stretch"
			return clip(b.bg(row, "bg"))
		}},
		&Block{ID: "web/tablet", Platform: "web", Title: "Tablet shell", Description: "A compact sidebar and a detail column at 834 wide.", build: func(b *builder) *Node {
			side := fixW(b.col("Sidebar", 16, Pad{24, 16, 24, 16}, b.text("Brand", "h4", "text", b.s("brand", "Lumen")), b.text("Link", "body", "text", "Projects"), b.text("Link", "body", "text-muted", "Archive")), 200)
			b.bg(side, "surface")
			main := fillX(b.col("Detail", 12, Pad{24, 24, 24, 24}, b.text("Title", "h3", "text", b.s("title", "Projects")), b.para("Lead", "small", "text-muted", b.s("lead", "Three active jobs this week."), 0)))
			row := fixH(fixW(b.row("Tablet", 0, Pad{}, side, main), 834), 1112)
			row.Layout.Cross = "stretch"
			return clip(b.bg(row, "bg"))
		}},
		&Block{ID: "web/chart-bar", Platform: "web", Title: "Bar chart", Description: "Bars from a list of numbers, in the theme accent.", build: func(b *builder) *Node {
			return b.chartCard("bar", b.s("title", "Weekly orders"), b.s("values", "24|40|36|52|48|60|44"))
		}},
		&Block{ID: "web/chart-line", Platform: "web", Title: "Line chart", Description: "A line through the same kind of number list.", build: func(b *builder) *Node {
			return b.chartCard("line", b.s("title", "Response time"), b.s("values", "30|28|26|22|24|18|16"))
		}},
		&Block{ID: "web/chart-donut", Platform: "web", Title: "Donut chart", Description: "Shares of a whole, drawn as arcs.", build: func(b *builder) *Node {
			return b.chartCard("donut", b.s("title", "Traffic"), b.s("values", "46|32|22"))
		}},
		&Block{ID: "web/sparkline", Platform: "web", Title: "Sparkline", Description: "A small line for a single metric.", build: func(b *builder) *Node {
			return b.spark(b.s("values", "4|6|5|8|7|9|8"))
		}},
		sec(&Block{ID: "ios/form", Platform: "ios", Title: "Form", Description: "Labelled fields and a primary action for a short phone form.", build: func(b *builder) *Node {
			return b.pad16("Form", 16,
				b.text("Title", "h3", "text", b.s("title", "Your details")),
				b.phoneField(b.s("name_label", "Name"), b.s("name", "Amara Kone")),
				b.phoneField(b.s("email_label", "Email"), b.s("email", "amara@studio.test")),
				b.button("Save", b.s("cta", "Save"), "primary", "lg", ""),
			)
		}}),
		sec(&Block{ID: "ios/stepper", Platform: "ios", Title: "Stepper", Description: "A count with minus and plus, and a caption.", build: func(b *builder) *Node {
			minus := center(fixH(fixW(b.row("Minus", 0, Pad{}, b.icon("minus", 18, "text")), 44), 44))
			radius(b.bg(minus, "muted"), 22)
			plus := center(fixH(fixW(b.row("Plus", 0, Pad{}, b.icon("plus", 18, "on-accent")), 44), 44))
			radius(b.bg(plus, "accent"), 22)
			row := b.row("Count", 16, Pad{}, minus, b.text("Value", "h3", "text", b.s("value", "2")), plus)
			return b.pad16("Stepper", 8, b.text("Label", "body", "text", b.s("label", "Guests")), row)
		}}),
		sec(&Block{ID: "ios/camera", Platform: "ios", Title: "Camera", Description: "A viewfinder and a shutter control.", build: func(b *builder) *Node {
			finder := b.imagePlaceholder("Viewfinder", 358, 420)
			shutter := radius(b.rect("Shutter", 64, 64, "text"), 32)
			return b.pad16("Camera", 16, finder, align(b.row("Controls", 0, Pad{}, shutter), "start", "center"))
		}}),
		sec(&Block{ID: "ios/player", Platform: "ios", Title: "Player", Description: "Artwork, a title, a progress line and play.", build: func(b *builder) *Node {
			art := b.imagePlaceholder("Artwork", 358, 240)
			bar := fillX(radius(b.rect("Progress", 358, 4, "accent"), 2))
			play := center(fixH(fixW(b.row("Play", 0, Pad{}, b.icon("play", 18, "on-accent")), 56), 56))
			radius(b.bg(play, "accent"), 28)
			return b.pad16("Player", 16, art, b.text("Title", "h4", "text", b.s("title", "Evening session")), b.text("Meta", "small", "text-muted", b.s("meta", "Studio North · 42 min")), bar, align(b.row("Transport", 0, Pad{}, play), "start", "center"))
		}}),
		sec(&Block{ID: "ios/chat", Platform: "ios", Title: "Chat", Description: "A short thread: their bubble, yours, and a field.", build: func(b *builder) *Node {
			theirs := b.bubble(b.s("theirs", "The deck is ready for review."), false)
			ours := b.bubble(b.s("ours", "I will read it before lunch."), true)
			field := fillX(fixH(b.row("Composer", 8, Pad{0, 12, 0, 12}, fillX(b.text("Placeholder", "body", "text-muted", b.s("placeholder", "Message"))), b.icon("send", 18, "accent")), 44))
			radius(b.bg(field, "card"), 22)
			b.border(field, "border", 1)
			return b.pad16("Chat", 12, b.text("Title", "h4", "text", b.s("title", "Amara Kone")), theirs, ours, field)
		}}),
		sec(&Block{ID: "ios/carousel", Platform: "ios", Title: "Carousel", Description: "Cards in a row that can scroll sideways.", build: func(b *builder) *Node {
			row := b.row("Cards", 12, Pad{})
			for _, c := range b.items("cards", []map[string]string{{"title": "Monday", "text": "Three meetings"}, {"title": "Tuesday", "text": "A clear morning"}, {"title": "Wednesday", "text": "Ship the notes"}}) {
				card := fixW(b.col("Card", 8, Pad{16, 16, 16, 16}, b.text("Title", "h4", "text", c["title"]), b.text("Text", "small", "text-muted", c["text"])), 200)
				radius(b.bg(card, "card"), 16)
				b.framed(card, "border")
				row.Children = append(row.Children, card)
			}
			frame := fillX(b.row("Scroller", 0, Pad{}, row))
			frame.Scroll = "horizontal"
			return b.pad16("Carousel", 8, b.text("Title", "h4", "text", b.s("title", "This week")), frame)
		}}),
		sec(&Block{ID: "ios/empty", Platform: "ios", Title: "Empty", Description: "An explanation and one action when a list has no rows.", build: func(b *builder) *Node {
			return b.pad16("Empty", 12, b.text("Title", "h4", "text", b.s("title", "No notes yet")), b.para("Text", "body", "text-muted", b.s("text", "Notes you add on this phone show up here."), 0), b.button("Add", b.s("cta", "New note"), "primary", "lg", ""))
		}}),
		sec(&Block{ID: "ios/error", Platform: "ios", Title: "Error", Description: "What failed, in words, and a way to try again.", build: func(b *builder) *Node {
			return b.pad16("Error", 12, b.icon("circle-x", 28, "error"), b.text("Title", "h4", "text", b.s("title", "Could not refresh")), b.para("Text", "body", "text-muted", b.s("text", "The studio is offline. Your last copy is still on this phone."), 0), b.button("Retry", b.s("cta", "Try again"), "primary", "lg", ""))
		}}),
		sec(&Block{ID: "ios/sheet", Platform: "ios", Title: "Bottom sheet", Description: "A card that sits at the bottom of a phone: title, text, action.", build: func(b *builder) *Node {
			card := fillX(b.col("Sheet", 12, Pad{12, 20, 24, 20}, radius(b.rect("Handle", 36, 4, "border-strong"), 2), b.text("Title", "h4", "text", b.s("title", "Share this board")), b.para("Text", "small", "text-muted", b.s("text", "Anyone with the link can comment. They cannot delete."), 0), b.button("Share", b.s("cta", "Copy link"), "primary", "lg", "")))
			card.Layout.Cross = "center"
			card.Children[0].SizingX = ""
			radius(b.bg(card, "card"), 20)
			return b.pad16("Sheet", 0, card)
		}}),
		sec(&Block{ID: "material/shell", Platform: "android", Title: "App shell", Description: "An empty Material screen: status bar, top app bar, a body (parent <id>-body) and the navigation bar.", build: func(b *builder) *Node {
			body := fillY(fillX(b.col("Body", 12, Pad{8, 0, 8, 0})))
			body.Layout.Cross = "stretch"
			n := fixH(fixW(b.col("Screen", 0, Pad{}, b.m3StatusBar(), b.m3TopBar(b.s("title", "Home"), b.s("lead", "menu"), []string{}, "small"), body, b.m3NavBar(b.items("tabs", m3Tabs()))), m3Width), m3Tall)
			n.Layout.Cross = "stretch"
			return clip(b.bg(n, "bg"))
		}}),
		sec(&Block{ID: "material/empty", Platform: "android", Title: "Empty", Description: "A short empty state for a Material list.", build: func(b *builder) *Node {
			return b.pad16("Empty", 12, b.text("Title", "h4", "text", b.s("title", "Nothing here")), b.para("Text", "body", "text-muted", b.s("text", "Files you add in Drive will be listed in this folder."), 0), b.m3Button(b.s("cta", "Add a file"), "filled", "plus"))
		}}),
	)
}

func (b *builder) storyCard(title, text string, width float64) *Node {
	card := b.col("Card", 8, Pad{24, 24, 24, 24}, b.text("Title", "h4", "text", title), b.para("Text", "small", "text-muted", text, 0))
	card.Layout.Cross = "stretch"
	radius(b.bg(card, "card"), b.t.Radius.LG)
	b.framed(card, "border")
	if width > 0 {
		return fixW(card, width)
	}
	return fillX(card)
}

func (b *builder) sideColumn(brand string) *Node {
	links := b.col("Links", 8, Pad{},
		b.text("Link", "body-strong", "text", "Home"),
		b.text("Link", "body", "text-muted", "Projects"),
		b.text("Link", "body", "text-muted", "Archive"),
		b.text("Link", "body", "text-muted", "Settings"),
	)
	col := fixW(b.col("Sidebar", 28, Pad{24, 20, 24, 20}, b.text("Brand", "h4", "text", brand), links), 240)
	col.Layout.Cross = "stretch"
	b.bg(col, "surface")
	return col
}

func (b *builder) phoneField(label, value string) *Node {
	box := fillX(fixH(b.row("Field", 0, Pad{0, 12, 0, 12}, b.text("Value", "body", "text", value)), 44))
	radius(b.bg(box, "card"), 10)
	b.border(box, "border", 1)
	n := b.col("Group", 6, Pad{}, b.text("Label", "caption", "text-muted", label), box)
	n.Layout.Cross = "stretch"
	n.Children[0].SizingX = ""
	return n
}

func (b *builder) bubble(text string, ours bool) *Node {
	token := "text"
	bg := "muted"
	if ours {
		token = "on-accent"
		bg = "accent"
	}
	n := b.row("Bubble", 0, Pad{10, 14, 10, 14}, b.text("Text", "body", token, text))
	radius(b.bg(n, bg), 16)
	return n
}

func (b *builder) chartCard(kind, title, values string) *Node {
	vals := parseValues(values)
	var plot *Node
	switch kind {
	case "line":
		plot = b.linePlot(vals, 420, 180)
	case "donut":
		plot = b.donut(vals, 180)
	default:
		plot = b.bars(vals, 420, 180)
	}
	card := fixW(b.col("Chart", 16, Pad{20, 20, 20, 20}, b.text("Title", "h4", "text", title), plot), 460)
	card.Layout.Cross = "stretch"
	radius(b.bg(card, "card"), b.t.Radius.LG)
	b.framed(card, "border")
	return card
}

func (b *builder) spark(values string) *Node {
	return b.linePlot(parseValues(values), 120, 36)
}

func parseValues(raw string) []float64 {
	var out []float64
	for _, part := range strings.Split(raw, "|") {
		f, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if err != nil || f < 0 {
			continue
		}
		out = append(out, f)
	}
	if len(out) == 0 {
		return []float64{1}
	}
	return out
}

func (b *builder) bars(vals []float64, w, h float64) *Node {
	maxV := 1.0
	for _, v := range vals {
		if v > maxV {
			maxV = v
		}
	}
	n := b.node(TypeFrame, "Bars")
	n.Width, n.Height = w, h
	n.Fills = []Paint{}
	gap := 8.0
	bw := (w - gap*float64(len(vals)-1)) / float64(len(vals))
	if bw < 4 {
		bw = 4
	}
	for i, v := range vals {
		bh := math.Max(4, h*v/maxV)
		bar := b.rect("Bar", bw, bh, "accent")
		bar.X = float64(i) * (bw + gap)
		bar.Y = h - bh
		radius(bar, 4)
		n.Children = append(n.Children, bar)
	}
	return n
}

func (b *builder) linePlot(vals []float64, w, h float64) *Node {
	maxV := 1.0
	for _, v := range vals {
		if v > maxV {
			maxV = v
		}
	}
	var d strings.Builder
	for i, v := range vals {
		x := 0.0
		if len(vals) > 1 {
			x = w * float64(i) / float64(len(vals)-1)
		}
		y := h - h*v/maxV
		if i == 0 {
			fmt.Fprintf(&d, "M%.1f %.1f", x, y)
		} else {
			fmt.Fprintf(&d, "L%.1f %.1f", x, y)
		}
	}
	p := b.node(TypePath, "Line")
	p.D = d.String()
	p.ViewBox = &[2]float64{w, h}
	p.Width, p.Height = w, h
	p.Fills = []Paint{}
	paint := b.paint("accent")
	p.Strokes = []Stroke{{Color: paint.Color, Variable: paint.Variable, Width: 2, Cap: "round", Join: "round"}}
	return p
}

func (b *builder) donut(vals []float64, size float64) *Node {
	sum := 0.0
	for _, v := range vals {
		sum += v
	}
	if sum <= 0 {
		sum = 1
	}
	n := b.node(TypeFrame, "Donut")
	n.Width, n.Height = size, size
	n.Fills = []Paint{}
	start := -90.0
	tokens := []string{"accent", "info", "success", "warning"}
	for i, v := range vals {
		sweep := 360 * v / sum
		if sweep > 359 {
			sweep = 359
		}
		if sweep < 1 {
			continue
		}
		p := b.node(TypePath, "Slice")
		p.D = arcPath(50, 50, 36, start, sweep)
		p.ViewBox = &[2]float64{100, 100}
		p.Width, p.Height = size, size
		p.Fills = []Paint{}
		paint := b.paint(tokens[i%len(tokens)])
		p.Strokes = []Stroke{{Color: paint.Color, Variable: paint.Variable, Width: 12, Cap: "round"}}
		n.Children = append(n.Children, p)
		start += sweep
	}
	return n
}

func arcPath(cx, cy, r, start, sweep float64) string {
	rad := func(d float64) float64 { return d * math.Pi / 180 }
	x0 := cx + r*math.Cos(rad(start))
	y0 := cy + r*math.Sin(rad(start))
	x1 := cx + r*math.Cos(rad(start+sweep))
	y1 := cy + r*math.Sin(rad(start+sweep))
	large := 0
	if sweep > 180 {
		large = 1
	}
	return fmt.Sprintf("M%.2f %.2f A%.2f %.2f 0 %d 1 %.2f %.2f", x0, y0, r, r, large, x1, y1)
}
