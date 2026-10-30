// Package webverify is the product verification agent's engine. It drives a
// locally served web application through a Driver (backed by Atlas's own
// browser tool), measures it at several viewports and returns findings that
// each carry evidence. It never depends on a test framework: the checks are
// small scripts evaluated in the page.
//
// A finding blocks completion only when it is evidenced: a screenshot path,
// a DOM excerpt, a console line, a network entry or a measurement the reader
// can check. Opinions without evidence are reported but never block.
package webverify

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
)

// Severity ranks a finding.
type Severity string

// Severities, from the most to the least serious.
const (
	SeverityHigh   Severity = "high"
	SeverityMedium Severity = "medium"
	SeverityLow    Severity = "low"
)

func (s Severity) rank() int {
	switch s {
	case SeverityHigh:
		return 3
	case SeverityMedium:
		return 2
	case SeverityLow:
		return 1
	}
	return 0
}

// AtLeast reports whether s is as serious as threshold or more.
func (s Severity) AtLeast(threshold Severity) bool {
	return s.rank() >= threshold.rank() && s.rank() > 0
}

// ParseSeverity reads a severity name, returning fallback for anything else.
func ParseSeverity(name string, fallback Severity) Severity {
	switch Severity(strings.ToLower(strings.TrimSpace(name))) {
	case SeverityHigh:
		return SeverityHigh
	case SeverityMedium:
		return SeverityMedium
	case SeverityLow:
		return SeverityLow
	}
	return fallback
}

// Kind says which check produced a finding.
type Kind string

// Finding kinds.
const (
	KindConsole Kind = "console"
	KindNetwork Kind = "network"
	KindA11y    Kind = "a11y"
	KindLoad    Kind = "load"
	KindLayout  Kind = "layout"
	KindFlow    Kind = "flow"
	KindVisual  Kind = "visual"
	KindPage    Kind = "page"
)

// Source says how a finding was obtained.
type Source string

// Finding sources.
const (
	// SourceMeasured findings come from a script, a log or a recorded
	// request.
	SourceMeasured Source = "measured"
	// SourceVision findings come from a vision model looking at a
	// screenshot. They block only at high severity.
	SourceVision Source = "vision"
)

// Evidence is what backs a finding. At least one field must be set for the
// finding to count as evidenced.
type Evidence struct {
	// Screenshot is a project-relative path to an image.
	Screenshot string `json:"screenshot,omitempty"`
	// DOM is an excerpt of the offending markup or page text.
	DOM string `json:"dom,omitempty"`
	// Console is the console line.
	Console string `json:"console,omitempty"`
	// Network is the recorded request.
	Network *NetworkEntry `json:"network,omitempty"`
	// Measure is a measurement, for example load timings as JSON.
	Measure string `json:"measure,omitempty"`
	// Error is the error a step or navigation returned.
	Error string `json:"error,omitempty"`
}

// Empty reports whether nothing backs the finding.
func (e Evidence) Empty() bool {
	return strings.TrimSpace(e.Screenshot) == "" && strings.TrimSpace(e.DOM) == "" &&
		strings.TrimSpace(e.Console) == "" && e.Network == nil &&
		strings.TrimSpace(e.Measure) == "" && strings.TrimSpace(e.Error) == ""
}

// Finding is one problem found in the running product.
type Finding struct {
	ID        string   `json:"id"`
	Kind      Kind     `json:"kind"`
	Severity  Severity `json:"severity"`
	Source    Source   `json:"source"`
	Title     string   `json:"title"`
	Detail    string   `json:"detail,omitempty"`
	Page      string   `json:"page,omitempty"`
	Viewports []string `json:"viewports,omitempty"`
	Evidence  Evidence `json:"evidence"`
	// Advisory marks a finding that is reported but never blocks, such as
	// a failed check derived heuristically from prose.
	Advisory bool `json:"advisory,omitempty"`
}

// Evidenced reports whether the finding carries evidence.
func (f Finding) Evidenced() bool { return !f.Evidence.Empty() }

// Blocks reports whether the finding must be fixed before the work counts
// as done: it is evidenced, not advisory, and at least threshold. A vision
// finding blocks only at high severity, whatever the threshold.
func (f Finding) Blocks(threshold Severity) bool {
	if f.Advisory || !f.Evidenced() {
		return false
	}
	if f.Source == SourceVision {
		return f.Severity == SeverityHigh
	}
	return f.Severity.AtLeast(threshold)
}

// key identifies a finding independently of the viewport it appeared at.
func (f Finding) key() string {
	h := sha256.New()
	e := f.Evidence
	net := ""
	if e.Network != nil {
		net = e.Network.URL + "|" + strconv.Itoa(e.Network.Status) + "|" + strconv.FormatBool(e.Network.Failed)
	}
	for _, part := range []string{string(f.Kind), f.Title, f.Page, e.DOM, e.Console, net, e.Error} {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:10]
}

// NetworkEntry is one recorded request. It mirrors the browser tool's
// network record: no bodies, headers or query strings.
type NetworkEntry struct {
	RequestID  string `json:"request_id,omitempty"`
	URL        string `json:"url,omitempty"`
	Status     int    `json:"status,omitempty"`
	Failed     bool   `json:"failed,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
}

// ConsoleMessage is one console line.
type ConsoleMessage struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Viewport is one emulated screen.
type Viewport struct {
	Name   string `json:"name"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Mobile bool   `json:"mobile,omitempty"`
}

// DefaultViewports are the three screens every run covers.
func DefaultViewports() []Viewport {
	return []Viewport{
		{Name: "mobile", Width: 390, Height: 844, Mobile: true},
		{Name: "tablet", Width: 768, Height: 1024},
		{Name: "desktop", Width: 1366, Height: 800},
	}
}

// Budgets are the technical limits a page must stay within.
type Budgets struct {
	// ConsoleErrors is how many console errors are tolerated per page.
	ConsoleErrors int `json:"console_errors"`
	// FailedRequests is how many failed requests are tolerated per page.
	FailedRequests int `json:"failed_requests"`
	// LoadMS is the longest accepted page load. Zero uses the default and
	// a negative value disables the check.
	LoadMS int `json:"load_ms"`
}

// DefaultBudgets are strict on errors and lenient on dev-server speed.
func DefaultBudgets() Budgets {
	return Budgets{LoadMS: 5000}
}
