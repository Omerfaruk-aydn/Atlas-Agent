package design

import (
	"fmt"
	"strings"
)

// Brief is one fixed task the evaluation harness scores a design against.
type Brief struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Platform string `json:"platform"`
	Ask      string `json:"ask"`
}

// DesignBriefs is the fixed set a quality run is measured on. The words
// stay stable so scores from different models can be compared.
func DesignBriefs() []Brief {
	return []Brief{
		{ID: "landing", Title: "Landing page", Platform: "web", Ask: "A product landing page for a team that sells a specific tool. One hero, proof, three features that are not the same card, pricing, and a footer."},
		{ID: "dashboard", Title: "Dashboard", Platform: "web", Ask: "An internal dashboard with a sidebar, four summary numbers, a chart and a table of recent rows. Real labels, no lorem."},
		{ID: "onboarding", Title: "Mobile onboarding", Platform: "ios", Ask: "Three phone screens that take a new user through a concrete product, with a tab or pager and a way forward."},
		{ID: "commerce", Title: "E-commerce", Platform: "web", Ask: "A product page, a cart and a checkout for one real kind of goods. Price, variants and a primary action."},
		{ID: "event", Title: "Event app", Platform: "ios", Ask: "A phone app for a multi-day event: schedule, a map or room list, a badge and notifications."},
		{ID: "fintech", Title: "Fintech", Platform: "web", Ask: "A finance overview: balance, a trend, recent transfers. Restrained colour, no purple gradient."},
		{ID: "health", Title: "Health", Platform: "ios", Ask: "A health summary on a phone: today's numbers, a short list of readings, and an empty state that is not a grey box."},
		{ID: "education", Title: "Education", Platform: "web", Ask: "A course page: what the student learns, who teaches it, and how to enrol."},
		{ID: "internal", Title: "Internal tool", Platform: "web", Ask: "A settings screen for an admin: sections, switches and a save action. Dense but aligned."},
		{ID: "portfolio", Title: "Portfolio", Platform: "web", Ask: "A personal portfolio with a name, three projects and a way to get in touch. Not three identical cards."},
	}
}

// EvalRow is one design scored by lint.
type EvalRow struct {
	Name   string
	Score  int
	Kit    float64
	Errors int
	Nodes  int
	Note   string
}

// EvalDoc scores a design. A page of several screens is scored per screen
// and the row keeps the lowest score, because the lint score is meant for
// one screen, not the sum of every warning on the page.
func EvalDoc(name string, d *Doc) (EvalRow, error) {
	if d == nil || len(d.Pages) == 0 {
		return EvalRow{}, fmt.Errorf("no page")
	}
	page := d.Pages[0]
	ids := []string{""}
	if len(page.Children) > 1 {
		ids = nil
		for _, c := range page.Children {
			ids = append(ids, c.ID)
		}
	}
	row := EvalRow{Name: name, Score: 100}
	for _, id := range ids {
		report, err := Lint(d, page.ID, id)
		if err != nil {
			return EvalRow{}, err
		}
		if report.Score < row.Score {
			row.Score = report.Score
		}
		row.Nodes += report.Nodes
		if report.Kit > row.Kit {
			row.Kit = report.Kit
		}
		for _, is := range report.Issues {
			if is.Severity == "error" {
				row.Errors++
			}
		}
	}
	return row, nil
}

// FormatEval renders rows as a fixed-width table.
func FormatEval(rows []EvalRow) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%-22s %6s %8s %7s %6s  %s\n", "name", "score", "kit", "errors", "nodes", "note")
	for _, r := range rows {
		note := r.Note
		if note == "" {
			note = "—"
		}
		fmt.Fprintf(&b, "%-22s %6d %7.0f%% %7d %6d  %s\n", r.Name, r.Score, 100*r.Kit, r.Errors, r.Nodes, note)
	}
	return b.String()
}
