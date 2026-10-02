package skills

import (
	"encoding/csv"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode"
)

var designDomains = map[string]string{
	"style": "styles", "color": "colors", "typography": "typography",
	"chart": "charts", "ux": "ux-guidelines", "landing": "landing",
	"product": "products", "icons": "icons", "reasoning": "ui-reasoning",
	"react-performance": "react-performance", "web-interface": "web-interface",
}

var designStacks = strings.Fields("astro flutter html-tailwind jetpack-compose nextjs nuxt-ui nuxtjs react-native react shadcn svelte swiftui vue")

// DesignMatch is a ranked reference row, not an authoritative recommendation.
type DesignMatch struct {
	Source string            `json:"source"`
	Row    int               `json:"row"`
	Score  float64           `json:"score"`
	Fields map[string]string `json:"fields"`
}

func designRows(domain, stack string) ([][]string, error) {
	file, ok := designDomains[domain]
	if domain == "stack" {
		for _, s := range designStacks {
			if s == stack {
				file = "stacks/" + s
				ok = true
				break
			}
		}
	} else if stack != "" {
		return nil, fmt.Errorf("stack requires domain stack")
	}
	if !ok {
		return nil, fmt.Errorf("unknown design domain or stack; use documented names")
	}
	raw, err := builtinFS.ReadFile("builtin/ui-ux-pro-max/data/" + file + ".csv")
	if err != nil {
		return nil, err
	}
	reader := csv.NewReader(strings.NewReader(string(raw)))
	// Supplied datasets contain literal quotes in unquoted example fields.
	reader.LazyQuotes = true
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}
	return records, nil
}

// SearchDesign searches embedded references using deterministic lexical BM25.
// It reads no user paths, starts no processes, and writes no files.
func SearchDesign(query, domain, stack string, limit int) ([]DesignMatch, error) {
	if len(query) > 4096 {
		return nil, fmt.Errorf("query exceeds 4096 bytes")
	}
	tokens := designTokens(query)
	if len(tokens) == 0 {
		return nil, fmt.Errorf("query requires words")
	}
	if limit == 0 {
		limit = 5
	}
	if limit < 1 || limit > 20 {
		return nil, fmt.Errorf("limit must be between 1 and 20")
	}
	records, err := designRows(domain, stack)
	if err != nil {
		return nil, err
	}
	headers, rows := records[0], records[1:]
	frequencies := make([]map[string]int, len(rows))
	lengths := make([]int, len(rows))
	df := map[string]int{}
	total := 0
	for i, row := range rows {
		frequencies[i] = map[string]int{}
		for _, token := range designTokens(strings.Join(row, " ")) {
			frequencies[i][token]++
			lengths[i]++
			total++
		}
		for token := range frequencies[i] {
			df[token]++
		}
	}
	avg := float64(total) / float64(len(rows))
	matches := []DesignMatch{}
	for i, row := range rows {
		score := 0.0
		for _, token := range tokens {
			tf := float64(frequencies[i][token])
			if tf == 0 {
				continue
			}
			idf := math.Log(1 + (float64(len(rows)-df[token])+0.5)/(float64(df[token])+0.5))
			score += idf * (tf * 2.5) / (tf + 1.5*(0.25+0.75*float64(lengths[i])/avg))
		}
		if score == 0 {
			continue
		}
		fields := make(map[string]string, len(headers))
		if len(row) == len(headers) {
			for j, h := range headers {
				fields[h] = row[j]
			}
		} else {
			// Preserve malformed source rows without assigning misleading columns.
			fields["Unstructured reference"] = strings.Join(row, " | ")
			fields["Data warning"] = "Source field count differs from header; consult source."
		}
		source := designDomains[domain]
		if domain == "stack" {
			source = "stacks/" + stack
		}
		matches = append(matches, DesignMatch{Source: BuiltinPrefix + "ui-ux-pro-max/data/" + source + ".csv", Row: i + 2, Score: score, Fields: fields})
	}
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].Score > matches[j].Score })
	if len(matches) > limit {
		matches = matches[:limit]
	}
	return matches, nil
}

func designTokens(text string) []string {
	text = strings.ToLower(text)
	tokens := strings.FieldsFunc(text, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
	aliases := map[string]string{"erişilebilirlik": "accessibility", "renk": "color", "yazıtipi": "typography", "tipografi": "typography", "mobil": "mobile", "karanlık": "dark", "açık": "light", "sade": "minimal", "sağlık": "healthcare", "gösterge": "dashboard"}
	for i, token := range tokens {
		if alias, ok := aliases[token]; ok {
			tokens[i] = alias
		}
	}
	return tokens
}
