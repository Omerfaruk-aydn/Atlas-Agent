// Package planspec parses and lints the structured plan a model submits with
// exit_plan_mode: file changes tagged [NEW], [MODIFY] or [DELETE], and an
// optional "Re-plan if:" list of conditions that should send work back to
// planning. A plan with no tagged entries is still valid free-form text.
package planspec

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// Action is what a plan entry does to a path.
type Action string

const (
	New    Action = "NEW"
	Modify Action = "MODIFY"
	Delete Action = "DELETE"
)

// Entry is one planned file change.
type Entry struct {
	Action Action `json:"action"`
	Path   string `json:"path"`
	Note   string `json:"note,omitempty"`
	Line   int    `json:"line"`
}

// Plan is a parsed plan.
type Plan struct {
	Entries  []Entry  `json:"entries"`
	ReplanIf []string `json:"replan_if,omitempty"`
	Digest   string   `json:"digest"`
}

var entryRe = regexp.MustCompile(`^\s*(?:[-*]\s*)?\[(NEW|MODIFY|DELETE)\]\s+` + "`?([^`\\s]+)`?" + `\s*(?:[-:–—]\s*(.*))?$`)

var replanRe = regexp.MustCompile(`(?i)^\s*(?:#+\s*)?\**re-?plan if:?\**\s*(.*)$`)

// Parse extracts the tagged entries and the "Re-plan if:" conditions. It
// never fails: Lint reports what is wrong with the result.
func Parse(text string) Plan {
	sum := sha256.Sum256([]byte(strings.ReplaceAll(text, "\r\n", "\n")))
	p := Plan{Digest: hex.EncodeToString(sum[:8])}
	inReplan := false
	for i, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if m := replanRe.FindStringSubmatch(line); m != nil {
			inReplan = true
			if rest := strings.TrimSpace(m[1]); rest != "" {
				p.ReplanIf = append(p.ReplanIf, rest)
			}
			continue
		}
		if m := entryRe.FindStringSubmatch(line); m != nil {
			inReplan = false
			p.Entries = append(p.Entries, Entry{Action: Action(m[1]), Path: m[2], Note: strings.TrimSpace(m[3]), Line: i + 1})
			continue
		}
		trimmed := strings.TrimSpace(line)
		if inReplan {
			item := strings.TrimSpace(strings.TrimLeft(trimmed, "-*• "))
			switch {
			case trimmed == "":
			case strings.HasPrefix(trimmed, "-"), strings.HasPrefix(trimmed, "*"), strings.HasPrefix(trimmed, "•"):
				p.ReplanIf = append(p.ReplanIf, item)
			default:
				inReplan = false
			}
		}
	}
	return p
}

// Lint checks every entry against the working tree rooted at dir and returns
// one message per problem: unsafe or duplicate paths, MODIFY/DELETE of a
// file that does not exist, NEW over a file that already does.
func (p Plan) Lint(dir string) []string {
	var problems []string
	seen := map[string]int{}
	for _, e := range p.Entries {
		clean := path.Clean(strings.ReplaceAll(e.Path, `\`, "/"))
		switch {
		case clean == "." || path.IsAbs(clean) || filepath.IsAbs(e.Path) || clean == ".." || strings.HasPrefix(clean, "../"):
			problems = append(problems, fmt.Sprintf("line %d: [%s] %q must be a relative path inside the project", e.Line, e.Action, e.Path))
			continue
		}
		if first, dup := seen[clean]; dup {
			problems = append(problems, fmt.Sprintf("line %d: %q is already planned on line %d; list each file once", e.Line, e.Path, first))
			continue
		}
		seen[clean] = e.Line
		if dir == "" {
			continue
		}
		_, err := os.Stat(filepath.Join(dir, filepath.FromSlash(clean)))
		exists := err == nil
		switch e.Action {
		case Modify, Delete:
			if !exists {
				problems = append(problems, fmt.Sprintf("line %d: [%s] %q does not exist; use [NEW] to create it", e.Line, e.Action, e.Path))
			}
		case New:
			if exists {
				problems = append(problems, fmt.Sprintf("line %d: [NEW] %q already exists; use [MODIFY]", e.Line, e.Path))
			}
		}
	}
	return problems
}

// Summary is a one-line description for the approval prompt and the
// tool result.
func (p Plan) Summary() string {
	var n, m, d int
	for _, e := range p.Entries {
		switch e.Action {
		case New:
			n++
		case Modify:
			m++
		case Delete:
			d++
		}
	}
	return fmt.Sprintf("plan %s: %d new, %d modified, %d deleted, %d re-plan conditions", p.Digest, n, m, d, len(p.ReplanIf))
}

// Paths returns the planned paths, for seeding an ownership or work list.
func (p Plan) Paths() []string {
	out := make([]string, 0, len(p.Entries))
	for _, e := range p.Entries {
		out = append(out, path.Clean(strings.ReplaceAll(e.Path, `\`, "/")))
	}
	return out
}

// Approval modes for exit_plan_mode.
const (
	// ApprovalAlways asks the user every time (the default).
	ApprovalAlways = "always"
	// ApprovalAuto lets small, low-risk plans proceed without a prompt.
	ApprovalAuto = "auto"
	// ApprovalTurbo never asks; the plan is still linted.
	ApprovalTurbo = "turbo"
)

// maxAutoEntries is the most files a plan may touch and still proceed
// unasked in auto mode.
const maxAutoEntries = 8

var sensitivePlanPaths = []string{
	".github/", ".gitlab-ci", ".circleci/", "dockerfile", "docker-compose", "go.mod", "go.sum",
	"package.json", "package-lock.json", "pnpm-lock.yaml", "yarn.lock", "cargo.toml", "cargo.lock",
	".env", ".ssh/", "secrets", "credentials", "id_rsa", ".npmrc", "makefile", "taskfile",
}

// AutoProceed reports whether a plan may proceed without asking the user,
// and when it may not, why. The rules are deterministic: the plan must name
// its files (a free-form plan cannot be judged), delete nothing, touch at
// most eight files, and stay clear of build, CI, dependency and credential
// files.
func (p Plan) AutoProceed() (bool, string) {
	switch {
	case len(p.Entries) == 0:
		return false, "the plan does not list its file changes"
	case len(p.Entries) > maxAutoEntries:
		return false, fmt.Sprintf("it changes %d files (auto mode allows %d)", len(p.Entries), maxAutoEntries)
	}
	for _, e := range p.Entries {
		if e.Action == Delete {
			return false, fmt.Sprintf("it deletes %s", e.Path)
		}
		lower := strings.ToLower(strings.ReplaceAll(e.Path, `\`, "/"))
		for _, s := range sensitivePlanPaths {
			if strings.Contains(lower, s) {
				return false, fmt.Sprintf("it touches %s, a build, CI, dependency or credential file", e.Path)
			}
		}
	}
	return true, ""
}
