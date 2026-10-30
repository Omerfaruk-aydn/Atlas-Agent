// Package reviewbot implements verified, idempotent pull-request findings.
package reviewbot

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

const Marker = "<!-- atlas-review:"

type Finding struct {
	Path      string `json:"path"`
	Line      int    `json:"line"`
	Severity  string `json:"severity"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	Evidence  string `json:"evidence"`
	Confirmed bool   `json:"confirmed"`
}

var severityRank = map[string]int{"low": 1, "medium": 2, "high": 3, "critical": 4}

func Fingerprint(f Finding) string {
	canonical := strings.Join([]string{strings.TrimSpace(f.Path), fmt.Sprint(f.Line), strings.ToLower(strings.Join(strings.Fields(f.Title), " "))}, "\x00")
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:16])
}

// Publishable rejects unconfirmed model claims, missing evidence and low-value
// comments. The second model pass must explicitly confirm each finding.
func Publishable(candidates []Finding, minimum string) ([]Finding, error) {
	threshold, ok := severityRank[minimum]
	if !ok {
		return nil, fmt.Errorf("unknown severity %q", minimum)
	}
	seen := map[string]bool{}
	var findings []Finding
	for _, finding := range candidates {
		rank := severityRank[finding.Severity]
		if !finding.Confirmed || rank < threshold || strings.TrimSpace(finding.Path) == "" || finding.Line < 1 || strings.TrimSpace(finding.Title) == "" || strings.TrimSpace(finding.Body) == "" || strings.TrimSpace(finding.Evidence) == "" {
			continue
		}
		id := Fingerprint(finding)
		if seen[id] {
			continue
		}
		seen[id] = true
		findings = append(findings, finding)
	}
	slices.SortStableFunc(findings, func(a, b Finding) int { return strings.Compare(Fingerprint(a), Fingerprint(b)) })
	return findings, nil
}

func Comment(f Finding) string {
	return fmt.Sprintf("%s%s -->\n**%s: %s**\n\n%s\n\nVerified evidence: %s", Marker, Fingerprint(f), f.Severity, f.Title, f.Body, f.Evidence)
}

// NewFindings removes findings already posted to the same PR, including
// retries and repeated Action runs.
func NewFindings(candidates []Finding, existingComments []string) []Finding {
	existing := map[string]bool{}
	for _, body := range existingComments {
		for start := strings.Index(body, Marker); start >= 0; {
			rest := body[start+len(Marker):]
			end := strings.Index(rest, " -->")
			if end < 0 {
				break
			}
			existing[strings.TrimSpace(rest[:end])] = true
			next := strings.Index(rest[end+4:], Marker)
			if next < 0 {
				break
			}
			start += len(Marker) + end + 4 + next
		}
	}
	return slices.DeleteFunc(slices.Clone(candidates), func(f Finding) bool { return existing[Fingerprint(f)] })
}

func DecodeFindings(data string) ([]Finding, error) {
	var response struct {
		Findings []Finding `json:"findings"`
	}
	decoder := json.NewDecoder(strings.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil {
		return nil, fmt.Errorf("decode review findings: %w", err)
	}
	return response.Findings, nil
}
