package codereview

import (
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// flexNumber accepts a JSON number or a numeric string, because models
// emit both for line numbers and confidences.
type flexNumber float64

func (n *flexNumber) UnmarshalJSON(data []byte) error {
	s := strings.Trim(strings.TrimSpace(string(data)), `"`)
	if s == "" || s == "null" {
		*n = 0
		return nil
	}
	// A "12-15" range keeps its start.
	if i := strings.IndexAny(s, "-–"); i > 0 {
		s = s[:i]
	}
	v, err := strconv.ParseFloat(strings.TrimSuffix(s, "%"), 64)
	if err != nil {
		*n = 0
		return nil
	}
	*n = flexNumber(v)
	return nil
}

type wireFinding struct {
	Path       string     `json:"path"`
	File       string     `json:"file"`
	Line       flexNumber `json:"line"`
	EndLine    flexNumber `json:"end_line"`
	Severity   string     `json:"severity"`
	Category   string     `json:"category"`
	Title      string     `json:"title"`
	Scenario   string     `json:"scenario"`
	Suggestion string     `json:"suggestion"`
	Confidence flexNumber `json:"confidence"`
}

type wireReview struct {
	Summary  string        `json:"summary"`
	Findings []wireFinding `json:"findings"`
}

type wireVerdict struct {
	ID         string     `json:"id"`
	Verdict    string     `json:"verdict"`
	Confidence flexNumber `json:"confidence"`
	Reason     string     `json:"reason"`
	Severity   string     `json:"severity"`
}

type wireVerify struct {
	Verdicts []wireVerdict `json:"verdicts"`
}

// decodeJSON finds the first JSON document in text that decodes into v and
// satisfies ok. Models wrap answers in prose and code fences, so scanning
// for candidate start positions is more robust than trimming fences.
func decodeJSON(text string, newValue func() any, ok func(any) bool) (any, bool) {
	for i := 0; i < len(text); i++ {
		if text[i] != '{' && text[i] != '[' {
			continue
		}
		v := newValue()
		dec := json.NewDecoder(strings.NewReader(text[i:]))
		if err := dec.Decode(v); err != nil {
			continue
		}
		if ok(v) {
			return v, true
		}
	}
	return nil, false
}

// parseReview reads a reviewer pass answer: an object with "findings", or
// a bare array of findings.
func parseReview(text string) (wireReview, bool) {
	v, found := decodeJSON(text, func() any { return new(wireReview) }, func(v any) bool {
		r := v.(*wireReview)
		return r.Findings != nil || r.Summary != ""
	})
	if found {
		return *v.(*wireReview), true
	}
	v, found = decodeJSON(text, func() any { return new([]wireFinding) }, func(any) bool { return true })
	if found {
		return wireReview{Findings: *v.(*[]wireFinding)}, true
	}
	return wireReview{}, false
}

// parseVerify reads a verifier answer.
func parseVerify(text string) (wireVerify, bool) {
	v, found := decodeJSON(text, func() any { return new(wireVerify) }, func(v any) bool {
		return v.(*wireVerify).Verdicts != nil
	})
	if !found {
		return wireVerify{}, false
	}
	return *v.(*wireVerify), true
}

// normalizeConfidence maps 0-1 and 0-100 scales onto 0-1.
func normalizeConfidence(c float64) float64 {
	switch {
	case c <= 0:
		return 0
	case c > 1 && c <= 100:
		return c / 100
	case c > 1:
		return 1
	default:
		return c
	}
}

var driveLetter = regexp.MustCompile(`^[A-Za-z]:/`)

// normalizePath turns a model-reported path into a repository-relative
// slash path. Absolute paths under root are made relative; "a/" and "b/"
// diff prefixes are only stripped when the bare path is not a known file.
func normalizePath(p, root string, known map[string]bool) string {
	p = strings.TrimSpace(strings.Trim(p, "`\"'"))
	p = strings.ReplaceAll(p, `\`, "/")
	if i := strings.LastIndex(p, ":"); i > 1 {
		// Tolerate "file.go:12".
		if _, err := strconv.Atoi(p[i+1:]); err == nil {
			p = p[:i]
		}
	}
	rootSlash := strings.TrimRight(strings.ReplaceAll(root, `\`, "/"), "/")
	if rootSlash != "" && strings.HasPrefix(strings.ToLower(p), strings.ToLower(rootSlash)+"/") {
		p = p[len(rootSlash)+1:]
	}
	p = strings.TrimPrefix(p, "./")
	if p == "" || strings.HasPrefix(p, "/") || driveLetter.MatchString(p) {
		return ""
	}
	p = path.Clean(p)
	if p == "." || p == ".." || strings.HasPrefix(p, "../") {
		return ""
	}
	if !known[p] && (strings.HasPrefix(p, "a/") || strings.HasPrefix(p, "b/")) && known[p[2:]] {
		p = p[2:]
	}
	return p
}

// toFinding converts a wire finding, reporting why it is unusable.
func toFinding(w wireFinding, root string, known map[string]bool) (Finding, string) {
	p := w.Path
	if p == "" {
		p = w.File
	}
	f := Finding{
		Path:       normalizePath(p, root, known),
		Line:       int(w.Line),
		EndLine:    int(w.EndLine),
		Severity:   normalizeSeverity(w.Severity),
		Category:   strings.ToLower(strings.TrimSpace(w.Category)),
		Title:      strings.TrimSpace(w.Title),
		Scenario:   strings.TrimSpace(w.Scenario),
		Suggestion: strings.TrimSpace(w.Suggestion),
		Confidence: normalizeConfidence(float64(w.Confidence)),
		Passes:     1,
	}
	switch {
	case f.Title == "":
		return f, "missing title"
	case f.Path == "":
		return f, "missing or invalid file path"
	case f.Line <= 0:
		return f, "missing line number"
	case f.Scenario == "":
		return f, "missing failure scenario"
	}
	if f.EndLine < f.Line {
		f.EndLine = 0
	}
	if f.Confidence == 0 {
		f.Confidence = 0.5
	}
	return f, ""
}

func (f Finding) String() string {
	return fmt.Sprintf("%s:%d %s", f.Path, f.Line, f.Title)
}
