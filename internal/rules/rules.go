// Package rules loads triggered instruction files from a project's
// .atlas/rules directory. Each rule is a markdown file whose front matter
// says when it applies:
//
//	always_on       inlined into every session's context
//	glob            applies when the work touches matching files; listed
//	                with its globs so the agent reads it on demand
//	model_decision  listed with its description; the agent reads it when the
//	                description fits the task
//	manual          never listed; used only when the user names it
//
// Listing instead of inlining keeps the standing context small: only
// always_on rules cost tokens in every request.
package rules

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Trigger is when a rule applies.
type Trigger string

const (
	AlwaysOn      Trigger = "always_on"
	Glob          Trigger = "glob"
	ModelDecision Trigger = "model_decision"
	Manual        Trigger = "manual"
)

// Rule is one parsed rule file.
type Rule struct {
	ID          string
	Path        string
	Trigger     Trigger
	Globs       []string
	Description string
	Body        string
}

const (
	maxRules    = 64
	maxRuleSize = 32 * 1024
	// alwaysOnBudget caps the bytes of always_on bodies inlined at once;
	// over that, a rule is demoted to a listed pointer.
	alwaysOnBudget = 24 * 1024
)

// Load reads every *.md file directly under dir. A missing directory is
// not an error. A file with no explicit trigger is glob when it has globs,
// model_decision when it has a description, and manual otherwise.
func Load(dir string) ([]Rule, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Rule
	for _, e := range entries {
		if e.IsDir() || e.Type()&os.ModeSymlink != 0 || !strings.EqualFold(filepath.Ext(e.Name()), ".md") {
			continue
		}
		if len(out) >= maxRules {
			break
		}
		path := filepath.Join(dir, e.Name())
		info, err := e.Info()
		if err != nil || info.Size() > maxRuleSize {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		r := parse(strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())), string(data))
		r.Path = path
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func parse(id, text string) Rule {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	r := Rule{ID: id, Trigger: Manual}
	body := text
	if rest, ok := strings.CutPrefix(text, "---\n"); ok {
		if head, after, ok := strings.Cut(rest, "\n---"); ok {
			body = strings.TrimPrefix(after, "\n")
			explicit := false
			for _, line := range strings.Split(head, "\n") {
				key, value, ok := strings.Cut(line, ":")
				if !ok {
					continue
				}
				value = strings.TrimSpace(value)
				switch strings.ToLower(strings.TrimSpace(key)) {
				case "trigger":
					switch t := Trigger(strings.ToLower(value)); t {
					case AlwaysOn, Glob, ModelDecision, Manual:
						r.Trigger, explicit = t, true
					}
				case "globs", "glob":
					for _, g := range strings.Split(strings.Trim(value, "[]"), ",") {
						if g = strings.Trim(strings.TrimSpace(g), "\"'"); g != "" {
							r.Globs = append(r.Globs, g)
						}
					}
				case "description":
					r.Description = strings.Trim(value, "\"'")
				}
			}
			if !explicit {
				switch {
				case len(r.Globs) > 0:
					r.Trigger = Glob
				case r.Description != "":
					r.Trigger = ModelDecision
				}
			}
		}
	}
	r.Body = strings.TrimSpace(body)
	return r
}

// Matches reports whether a glob rule applies to rel, a path relative to
// the project. A pattern without a slash matches the base name; "**/x"
// matches x at any depth; "dir/**" matches anything under dir.
func (r Rule) Matches(rel string) bool {
	rel = filepath.ToSlash(rel)
	for _, g := range r.Globs {
		g = filepath.ToSlash(g)
		if ok, _ := filepath.Match(g, rel); ok {
			return true
		}
		if !strings.Contains(g, "/") {
			if ok, _ := filepath.Match(g, filepath.Base(rel)); ok {
				return true
			}
		}
		if tail, ok := strings.CutPrefix(g, "**/"); ok {
			if m, _ := filepath.Match(tail, filepath.Base(rel)); m {
				return true
			}
			if rel == tail || strings.HasSuffix(rel, "/"+tail) {
				return true
			}
		}
		if dir, ok := strings.CutSuffix(g, "/**"); ok && strings.HasPrefix(rel, dir+"/") {
			return true
		}
	}
	return false
}

// For returns the glob rules that apply to any of the given paths.
func For(rs []Rule, paths []string) []Rule {
	var out []Rule
	for _, r := range rs {
		if r.Trigger != Glob {
			continue
		}
		for _, p := range paths {
			if r.Matches(p) {
				out = append(out, r)
				break
			}
		}
	}
	return out
}

// Render produces the text for the standing context: always_on rules
// inlined (within budget), everything else as a short pointer list. It
// returns "" when there is nothing to show.
func Render(rs []Rule) string {
	var inline, listed []string
	used := 0
	for _, r := range rs {
		switch r.Trigger {
		case AlwaysOn:
			if used+len(r.Body) > alwaysOnBudget {
				listed = append(listed, fmt.Sprintf("- %s (always-on, too large to inline) -- read %s", r.ID, r.Path))
				continue
			}
			used += len(r.Body)
			inline = append(inline, fmt.Sprintf("## Rule: %s\n%s", r.ID, r.Body))
		case Glob:
			listed = append(listed, fmt.Sprintf("- %s (when working on %s)%s -- read %s", r.ID, strings.Join(r.Globs, ", "), describe(r), r.Path))
		case ModelDecision:
			listed = append(listed, fmt.Sprintf("- %s%s -- read %s when it fits the task", r.ID, describe(r), r.Path))
		}
	}
	if len(inline) == 0 && len(listed) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(strings.Join(inline, "\n\n"))
	if len(listed) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("## More project rules (not inlined; read the file with the view tool when it applies)\n")
		b.WriteString(strings.Join(listed, "\n"))
	}
	return b.String() + "\n"
}

func describe(r Rule) string {
	if r.Description == "" {
		return ""
	}
	return ": " + r.Description
}
