package screens

import (
	"embed"
	"encoding/csv"
	"fmt"
	"sort"
	"strings"
)

// The knowledge base is what a designer keeps on the shelf: platform rules
// (written for the studio) and three lookup tables from the MIT-licensed
// ui-ux-pro-max-skill (UX rules, mobile interface rules, chart choice).

//go:embed catalog/knowledge/*.csv catalog/knowledge/platforms/*.md
var knowledgeFS embed.FS

// Platforms lists the platforms that have written rules.
func Platforms() []string { return []string{"ios", "android", "tablet", "web"} }

// PlatformRules returns the written rules of one platform.
func PlatformRules(platform string) (string, error) {
	p := strings.ToLower(strings.TrimSpace(platform))
	raw, err := knowledgeFS.ReadFile("catalog/knowledge/platforms/" + p + ".md")
	if err != nil {
		return "", fmt.Errorf("platform %q has no rules; platforms: %s", platform, strings.Join(Platforms(), ", "))
	}
	return string(raw), nil
}

// KnowledgeTopics are the lookup tables Knowledge can search.
var KnowledgeTopics = map[string]string{
	"ux":     "ux-guidelines.csv",
	"mobile": "app-interface.csv",
	"charts": "charts.csv",
}

type table struct {
	head []string
	rows [][]string
}

func loadTable(topic string) (*table, error) {
	file, ok := KnowledgeTopics[topic]
	if !ok {
		return nil, fmt.Errorf("topic %q is not one of ux, mobile, charts (or platform for a platform's rules)", topic)
	}
	raw, err := knowledgeFS.ReadFile("catalog/knowledge/" + file)
	if err != nil {
		return nil, err
	}
	r := csv.NewReader(strings.NewReader(string(raw)))
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	recs, err := r.ReadAll()
	if err != nil || len(recs) < 2 {
		return nil, fmt.Errorf("knowledge table %s is unreadable", file)
	}
	return &table{head: recs[0], rows: recs[1:]}, nil
}

func (t *table) col(name string) int {
	for i, h := range t.head {
		if strings.EqualFold(strings.TrimSpace(h), name) {
			return i
		}
	}
	return -1
}

func (t *table) get(row []string, name string) string {
	i := t.col(name)
	if i < 0 || i >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[i])
}

var platformWords = map[string][]string{
	"ios":     {"ios", "swiftui", "react native", "mobile"},
	"android": {"android", "compose", "react native", "mobile"},
	"tablet":  {"ios", "android", "tablet", "mobile"},
	"web":     {"web", "html", "css"},
}

// Knowledge searches one lookup table for the rows that best match the query
// and, for the rule tables, the platform.
func Knowledge(topic, query, platform string, limit int) (string, error) {
	t, err := loadTable(topic)
	if err != nil {
		return "", err
	}
	if limit <= 0 {
		limit = 6
	}
	words := strings.Fields(strings.ToLower(query))
	type hit struct {
		row   []string
		score int
	}
	var hits []hit
	for _, row := range t.rows {
		hay := strings.ToLower(strings.Join(row, " "))
		score := 0
		for _, w := range words {
			if len(w) < 3 {
				continue
			}
			if strings.Contains(hay, w) {
				score++
				for _, f := range []string{"Category", "Issue", "Keywords", "Data Type"} {
					if strings.Contains(strings.ToLower(t.get(row, f)), w) {
						score += 2
					}
				}
			}
		}
		if len(words) > 0 && score == 0 {
			continue
		}
		if pf := t.get(row, "Platform"); pf != "" && platform != "" {
			lp := strings.ToLower(pf)
			match := strings.Contains(lp, "all")
			for _, w := range platformWords[strings.ToLower(platform)] {
				if strings.Contains(lp, w) {
					match = true
				}
			}
			if !match {
				continue
			}
		}
		if sev := strings.ToLower(t.get(row, "Severity")); sev == "critical" {
			score += 2
		} else if sev == "high" {
			score++
		}
		hits = append(hits, hit{row, score})
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	if len(hits) == 0 {
		return fmt.Sprintf("Nothing in the %s table matches %q. Try plainer words: navigation, forms, loading, touch, contrast, motion, trend, compare, proportion.", topic, query), nil
	}
	if len(hits) > limit {
		hits = hits[:limit]
	}
	var b strings.Builder
	for i, h := range hits {
		if i > 0 {
			b.WriteString("\n")
		}
		if topic == "charts" {
			fmt.Fprintf(&b, "%s: use %s", t.get(h.row, "Data Type"), t.get(h.row, "Best Chart Type"))
			if v := t.get(h.row, "Secondary Options"); v != "" {
				fmt.Fprintf(&b, " (or %s)", v)
			}
			b.WriteString("\n")
			line(&b, "When", t.get(h.row, "When to Use"))
			line(&b, "Not when", t.get(h.row, "When NOT to Use"))
			line(&b, "Volume", t.get(h.row, "Data Volume Threshold"))
			line(&b, "Colour", t.get(h.row, "Color Guidance"))
			line(&b, "Accessibility", t.get(h.row, "Accessibility Notes"))
			continue
		}
		fmt.Fprintf(&b, "[%s] %s %s (%s)\n", t.get(h.row, "Severity"), t.get(h.row, "Category"), t.get(h.row, "Issue"), t.get(h.row, "Platform"))
		line(&b, "Why", t.get(h.row, "Description"))
		line(&b, "Do", t.get(h.row, "Do"))
		line(&b, "Don't", t.get(h.row, "Don't"))
	}
	return b.String(), nil
}

func line(b *strings.Builder, label, v string) {
	if v != "" {
		fmt.Fprintf(b, "  %s: %s\n", label, v)
	}
}
