// Package issues is a session's open-issues ledger. A worker records a
// problem it found with a severity; the issue stays open until it is closed
// with evidence, meaning the command that was run and what it printed. The
// completion gate refuses to finish a turn while serious issues remain open.
package issues

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Severity orders how serious an issue is.
type Severity int

const (
	Minor Severity = iota + 1
	Major
	Critical
)

func (s Severity) String() string {
	switch s {
	case Critical:
		return "critical"
	case Major:
		return "major"
	}
	return "minor"
}

// ParseSeverity reads "minor", "major" or "critical".
func ParseSeverity(s string) (Severity, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "minor", "low":
		return Minor, nil
	case "major", "medium", "high":
		return Major, nil
	case "critical", "blocker":
		return Critical, nil
	}
	return 0, fmt.Errorf("unknown severity %q (use minor, major or critical)", s)
}

// Issue is one recorded problem.
type Issue struct {
	ID       int
	Title    string
	Severity Severity
	Where    string
	Opened   time.Time
	// Closed is the zero time while the issue is open.
	Closed time.Time
	// Command and Output are the evidence that closed it.
	Command string
	Output  string
}

// Open reports whether the issue has not been closed.
func (i Issue) Open() bool { return i.Closed.IsZero() }

const (
	maxIssues      = 200
	minEvidenceLen = 8
)

// Ledger holds a session's issues. It is safe for concurrent use.
type Ledger struct {
	mu     sync.Mutex
	issues []Issue
	next   int
	now    func() time.Time
}

// New returns an empty ledger.
func New() *Ledger { return &Ledger{next: 1, now: time.Now} }

// Add records an open issue and returns its ID.
func (l *Ledger) Add(title string, sev Severity, where string) (int, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return 0, errors.New("an issue needs a title")
	}
	if sev < Minor || sev > Critical {
		return 0, errors.New("an issue needs a severity")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.issues) >= maxIssues {
		return 0, fmt.Errorf("at most %d issues per session", maxIssues)
	}
	for _, i := range l.issues {
		if i.Open() && strings.EqualFold(i.Title, title) && i.Where == where {
			return i.ID, nil
		}
	}
	id := l.next
	l.next++
	l.issues = append(l.issues, Issue{ID: id, Title: title, Severity: sev, Where: strings.TrimSpace(where), Opened: l.now()})
	return id, nil
}

// Close closes an issue. It needs the command that was run and a
// non-trivial piece of its output: a claim of "fixed" is not evidence.
func (l *Ledger) Close(id int, command, output string) error {
	command, output = strings.TrimSpace(command), strings.TrimSpace(output)
	if len(command) < 2 {
		return errors.New("closing an issue needs the command you ran as evidence")
	}
	if len(output) < minEvidenceLen {
		return errors.New("closing an issue needs the command's output as evidence, not just a claim")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := range l.issues {
		if l.issues[i].ID != id {
			continue
		}
		if !l.issues[i].Open() {
			return fmt.Errorf("issue %d is already closed", id)
		}
		l.issues[i].Closed, l.issues[i].Command = l.now(), command
		if len(output) > 2000 {
			output = output[:2000] + " [cut]"
		}
		l.issues[i].Output = output
		return nil
	}
	return fmt.Errorf("no issue %d", id)
}

// All returns every issue, open ones first, most serious first.
func (l *Ledger) All() []Issue {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := append([]Issue(nil), l.issues...)
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].Open() != out[b].Open() {
			return out[a].Open()
		}
		if out[a].Severity != out[b].Severity {
			return out[a].Severity > out[b].Severity
		}
		return out[a].ID < out[b].ID
	})
	return out
}

// OpenAtLeast returns the open issues at or above a severity.
func (l *Ledger) OpenAtLeast(min Severity) []Issue {
	var out []Issue
	for _, i := range l.All() {
		if i.Open() && i.Severity >= min {
			out = append(out, i)
		}
	}
	return out
}

// Summary renders the ledger as text for a tool result or a gate message.
func Summary(list []Issue) string {
	if len(list) == 0 {
		return "No issues recorded."
	}
	var b strings.Builder
	for _, i := range list {
		state := "OPEN"
		if !i.Open() {
			state = "closed"
		}
		fmt.Fprintf(&b, "#%d [%s] %s: %s", i.ID, i.Severity, state, i.Title)
		if i.Where != "" {
			fmt.Fprintf(&b, " (%s)", i.Where)
		}
		if !i.Open() {
			fmt.Fprintf(&b, " -- closed by `%s`", i.Command)
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
