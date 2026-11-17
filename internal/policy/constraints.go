package policy

import (
	"fmt"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Constraint is a prohibition the user stated, applied as a deny rule.
type Constraint struct {
	// Text is the user's phrase, repeated back in the refusal.
	Text string
	// Program is the command it applies to ("git", "rm", ...).
	Program string
	// Sub is the subcommand it applies to ("push"), or empty for any.
	Sub string
	// Programs lists extra programs for a constraint over a class of
	// commands, such as every network tool.
	Programs []string
}

// Matches reports whether the call is what the user forbade.
func (k Constraint) Matches(c Call) bool {
	name := c.Name()
	if name == "" {
		return false
	}
	if len(k.Programs) > 0 {
		for _, p := range k.Programs {
			if p == name {
				return true
			}
		}
		return false
	}
	if name != k.Program {
		return false
	}
	if k.Sub == "" {
		return true
	}
	if name == "git" {
		sub, _ := gitSubcommand(c.Args())
		return sub == k.Sub
	}
	return len(c.Args()) > 0 && c.Args()[0] == k.Sub
}

var constraintPatterns = []struct {
	re *regexp.Regexp
	k  Constraint
}{
	{regexp.MustCompile(`(?i)\b(do not|don'?t|never|no)\s+(git\s+)?push|push\s*(yapma|etme)|pushlama`), Constraint{Program: "git", Sub: "push"}},
	{regexp.MustCompile(`(?i)\b(do not|don'?t|never|no)\s+(git\s+)?commit|commit\s*(yapma|etme)`), Constraint{Program: "git", Sub: "commit"}},
	{regexp.MustCompile(`(?i)\b(do not|don'?t|never)\s+(delete|remove)|silme`), Constraint{Program: "rm"}},
	{regexp.MustCompile(`(?i)\b(do not|don'?t|never)\s+(use|access)\s+the\s+(network|internet)|no network|a[gğ]a\s*ba[gğ]lanma`), Constraint{Programs: []string{"curl", "wget", "ssh", "scp", "nc", "ncat", "iwr", "irm"}}},
}

// ConstraintsFromText extracts the prohibitions it recognizes from a user
// message. It is conservative: only a small set of clear phrases, in
// English and Turkish, produce a constraint.
func ConstraintsFromText(text string) []Constraint {
	var out []Constraint
	for _, p := range constraintPatterns {
		if m := p.re.FindString(text); m != "" {
			k := p.k
			k.Text = strings.TrimSpace(m)
			out = append(out, k)
		}
	}
	return out
}

// Grant is a typed, scoped permission: command(git push origin feat/*).
// Patterns use path.Match semantics per word; a trailing "*" word matches
// any remaining words.
type Grant struct {
	Kind    string
	Pattern string
}

// ParseGrant reads "command(...)" grant text.
func ParseGrant(s string) (Grant, error) {
	s = strings.TrimSpace(s)
	open := strings.IndexByte(s, '(')
	if open <= 0 || !strings.HasSuffix(s, ")") {
		return Grant{}, fmt.Errorf("grant must look like command(git push origin feat/*), got %q", s)
	}
	g := Grant{Kind: s[:open], Pattern: strings.TrimSpace(s[open+1 : len(s)-1])}
	if g.Kind != "command" || g.Pattern == "" {
		return Grant{}, fmt.Errorf("unsupported grant kind %q", g.Kind)
	}
	return g, nil
}

func (g Grant) String() string { return g.Kind + "(" + g.Pattern + ")" }

// Allows reports whether the grant covers a command line.
func (g Grant) Allows(command string) bool {
	pw := strings.Fields(g.Pattern)
	cw := strings.Fields(command)
	for i, p := range pw {
		if p == "*" && i == len(pw)-1 {
			return len(cw) >= i
		}
		if i >= len(cw) {
			return false
		}
		if ok, _ := path.Match(p, cw[i]); !ok {
			return false
		}
	}
	return len(pw) == len(cw)
}

// grantString is the exact grant that would allow a command.
func grantString(command string) string {
	if command == "" {
		return ""
	}
	return Grant{Kind: "command", Pattern: command}.String()
}

// Entry is one audit record.
type Entry struct {
	Time     time.Time `json:"time"`
	Command  string    `json:"command"`
	RuleID   string    `json:"rule"`
	Action   string    `json:"action"`
	Enforced bool      `json:"enforced"`
	Reason   string    `json:"reason"`
}

// Audit keeps the most recent policy findings in memory and, when given a
// file, appends them as JSON lines.
type Audit struct {
	mu      sync.Mutex
	file    string
	entries []Entry
}

const maxAuditEntries = 200

// NewAudit returns a log; file may be empty.
func NewAudit(file string) *Audit { return &Audit{file: file} }

// Entries returns a copy of the recent records, oldest first.
func (a *Audit) Entries() []Entry {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]Entry(nil), a.entries...)
}

func (a *Audit) record(command string, v Verdict) {
	if len(command) > 500 {
		command = command[:500] + "..."
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, f := range v.Findings {
		e := Entry{Time: time.Now().UTC(), Command: command, RuleID: f.RuleID, Action: f.Action.String(), Enforced: f.Enforced, Reason: f.Reason}
		a.entries = append(a.entries, e)
		if len(a.entries) > maxAuditEntries {
			a.entries = a.entries[len(a.entries)-maxAuditEntries:]
		}
		a.appendFile(e)
	}
}

var liftPatterns = []struct {
	re  *regexp.Regexp
	sub string
}{
	{regexp.MustCompile(`(?i)\b(you may|you can|go ahead and|feel free to|okay to)\s+(git\s+)?push|push\s*edebilirsin`), "push"},
	{regexp.MustCompile(`(?i)\b(you may|you can|go ahead and|feel free to|okay to)\s+(git\s+)?commit|commit\s*edebilirsin`), "commit"},
}

// Lift removes constraints the user has taken back ("you may push now").
// It reports how many were removed.
func (e *Engine) Lift(text string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	removed := 0
	for _, l := range liftPatterns {
		if !l.re.MatchString(text) {
			continue
		}
		kept := e.constraints[:0]
		for _, k := range e.constraints {
			if k.Program == "git" && k.Sub == l.sub {
				removed++
				continue
			}
			kept = append(kept, k)
		}
		e.constraints = kept
	}
	return removed
}
