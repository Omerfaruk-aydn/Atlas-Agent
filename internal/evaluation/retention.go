package evaluation

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

//go:embed retention_cases.json
var retentionFixtures []byte

type RetentionQuestion struct {
	ID             string   `json:"id"`
	Category       string   `json:"category"`
	Answer         string   `json:"answer"`
	Contradictions []string `json:"contradictions,omitempty"`
}

type RetentionCase struct {
	ID         string              `json:"id"`
	Transcript string              `json:"transcript"`
	Questions  []RetentionQuestion `json:"questions"`
	Summary    string              `json:"summary"`
	Handoff    string              `json:"handoff"`
}

type RetentionReport struct {
	Case       string         `json:"case"`
	Method     string         `json:"method"`
	Total      int            `json:"total"`
	Passed     int            `json:"passed"`
	Missing    []string       `json:"missing"`
	Categories map[string]int `json:"categories_passed"`
}

func RetentionCases() ([]RetentionCase, error) {
	var cases []RetentionCase
	if err := json.Unmarshal(retentionFixtures, &cases); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, c := range cases {
		if c.ID == "" || seen[c.ID] || len(c.Questions) == 0 {
			return nil, fmt.Errorf("invalid retention case %q", c.ID)
		}
		seen[c.ID] = true
		questions := map[string]bool{}
		for _, q := range c.Questions {
			if q.ID == "" || questions[q.ID] || q.Answer == "" || !strings.Contains(c.Transcript, q.Answer) {
				return nil, fmt.Errorf("retention question %q lacks transcript evidence", q.ID)
			}
			questions[q.ID] = true
		}
	}
	return cases, nil
}

// ScoreRetention checks explicit source-backed facts, not semantic quality.
func ScoreRetention(c RetentionCase, summary string) RetentionReport {
	report := RetentionReport{Case: c.ID, Method: "normalized-required-facts-v1", Total: len(c.Questions), Missing: []string{}, Categories: map[string]int{}}
	normalize := func(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }
	text := normalize(summary)
	for _, q := range c.Questions {
		passed := strings.Contains(text, normalize(q.Answer))
		for _, bad := range q.Contradictions {
			if strings.Contains(text, normalize(bad)) {
				passed = false
			}
		}
		if passed {
			report.Passed++
			report.Categories[q.Category]++
		} else {
			report.Missing = append(report.Missing, q.ID)
		}
	}
	return report
}
