// Package importer brings customizations from other coding agents (Claude
// Code, Codex, Cursor, Gemini CLI, Antigravity) into Atlas as one plugin that
// starts disabled and can be removed in one command. It copies instructions,
// commands, skills and agents. It never copies credentials: any file that
// looks like it holds a secret is skipped and reported, and MCP servers are
// listed by name only, since their definitions carry tokens.
package importer

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Sources are the agents that can be imported.
var Sources = []string{"claude", "codex", "cursor", "gemini", "agy"}

// Kind is what an imported item becomes.
type Kind string

const (
	KindSkill Kind = "skill"
	KindAgent Kind = "agent"
)

// Item is one thing to import.
type Item struct {
	Kind        Kind
	Name        string
	Description string
	Body        string
	Origin      string
}

// Bundle is what a scan found.
type Bundle struct {
	Source  string
	Items   []Item
	Skipped []string
	Notes   []string
}

const (
	maxItems    = 200
	maxItemSize = 128 * 1024
)

var secretRe = regexp.MustCompile(`(?i)(sk-[a-z0-9_-]{20,}|ghp_[a-z0-9]{30,}|github_pat_[a-z0-9_]{30,}|AKIA[0-9A-Z]{16}|xox[baprs]-[a-z0-9-]{10,}|-----BEGIN [A-Z ]*PRIVATE KEY-----|(api[_-]?key|secret|token|password)["']?\s*[:=]\s*["']?[a-z0-9_\-./+=]{16,})`)

// Scan reads one source from the user's home and the project directory.
func Scan(source, home, project string) (Bundle, error) {
	b := Bundle{Source: source}
	switch source {
	case "claude":
		b.scanClaude(home, project)
	case "codex":
		b.scanCodex(home, project)
	case "cursor":
		b.scanCursor(home, project)
	case "gemini":
		b.scanGemini(home, project)
	case "agy":
		b.scanAgy(home, project)
	default:
		return b, fmt.Errorf("unknown source %q (expected one of %s)", source, strings.Join(Sources, ", "))
	}
	sort.Slice(b.Items, func(i, j int) bool { return b.Items[i].Name < b.Items[j].Name })
	return b, nil
}

var nameClean = regexp.MustCompile(`[^a-z0-9]+`)

func slug(s string) string {
	s = strings.Trim(nameClean.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if s == "" {
		return "item"
	}
	if len(s) > 48 {
		s = strings.Trim(s[:48], "-")
	}
	return s
}

// add records an item unless it is empty, too large, over the count limit,
// or looks like it holds a secret.
func (b *Bundle) add(kind Kind, name, description, body, origin string) {
	body = strings.TrimSpace(strings.ReplaceAll(body, "\r\n", "\n"))
	switch {
	case body == "":
		return
	case len(body) > maxItemSize:
		b.Skipped = append(b.Skipped, origin+" (larger than 128 KiB)")
		return
	case secretRe.MatchString(body):
		b.Skipped = append(b.Skipped, origin+" (looks like it contains a credential)")
		return
	case len(b.Items) >= maxItems:
		b.Skipped = append(b.Skipped, origin+" (too many items)")
		return
	}
	name = slug(b.Source + "-" + name)
	for _, it := range b.Items {
		if it.Name == name && it.Kind == kind {
			name = slug(name + "-2")
			break
		}
	}
	if description == "" {
		description = firstLine(body)
	}
	b.Items = append(b.Items, Item{Kind: kind, Name: name, Description: description, Body: body, Origin: origin})
}

func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "#>-* "))
		if line != "" {
			if len(line) > 160 {
				line = line[:160]
			}
			return line
		}
	}
	return "Imported instructions"
}

// splitFrontMatter separates leading --- front matter from the body and
// returns its description value, if any.
func splitFrontMatter(text string) (description, body string) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	rest, ok := strings.CutPrefix(text, "---\n")
	if !ok {
		return "", text
	}
	head, after, ok := strings.Cut(rest, "\n---")
	if !ok {
		return "", text
	}
	for _, line := range strings.Split(head, "\n") {
		if k, v, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(k), "description") {
			description = strings.Trim(strings.TrimSpace(v), "\"'")
		}
	}
	return description, strings.TrimPrefix(after, "\n")
}

func readFile(path string) (string, bool) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(data), true
}

// markdownDir imports every .md/.mdc file directly under dir.
func (b *Bundle) markdownDir(dir string, kind Kind, prefix string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if e.IsDir() || (ext != ".md" && ext != ".mdc") {
			continue
		}
		text, ok := readFile(filepath.Join(dir, e.Name()))
		if !ok {
			continue
		}
		desc, body := splitFrontMatter(text)
		b.add(kind, prefix+strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())), desc, body, filepath.Join(dir, e.Name()))
	}
}

// skillDirs imports <dir>/<name>/SKILL.md folders.
func (b *Bundle) skillDirs(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		text, ok := readFile(filepath.Join(dir, e.Name(), "SKILL.md"))
		if !ok {
			continue
		}
		desc, body := splitFrontMatter(text)
		b.add(KindSkill, e.Name(), desc, body, filepath.Join(dir, e.Name(), "SKILL.md"))
	}
}

func (b *Bundle) instructionFile(path, label string) {
	if text, ok := readFile(path); ok {
		b.add(KindSkill, label, "Instructions imported from "+label+"; read when they apply to the task", text, path)
	}
}

func (b *Bundle) scanClaude(home, project string) {
	for _, base := range []string{filepath.Join(home, ".claude"), filepath.Join(project, ".claude")} {
		b.markdownDir(filepath.Join(base, "commands"), KindSkill, "cmd-")
		b.markdownDir(filepath.Join(base, "agents"), KindAgent, "")
		b.skillDirs(filepath.Join(base, "skills"))
	}
	b.instructionFile(filepath.Join(home, ".claude", "CLAUDE.md"), "claude-user-instructions")
	b.mcpNamesJSON(filepath.Join(home, ".claude.json"), "mcpServers")
	b.mcpNamesJSON(filepath.Join(project, ".mcp.json"), "mcpServers")
}

func (b *Bundle) scanCodex(home, project string) {
	b.markdownDir(filepath.Join(home, ".codex", "prompts"), KindSkill, "prompt-")
	b.instructionFile(filepath.Join(home, ".codex", "AGENTS.md"), "codex-user-instructions")
	if text, ok := readFile(filepath.Join(home, ".codex", "config.toml")); ok {
		for _, m := range regexp.MustCompile(`(?m)^\[mcp_servers\.([A-Za-z0-9_-]+)\]`).FindAllStringSubmatch(text, -1) {
			b.Notes = append(b.Notes, "MCP server "+m[1]+" not imported (definitions carry credentials); add it with `atlas mcp`.")
		}
	}
}

func (b *Bundle) scanCursor(home, project string) {
	b.markdownDir(filepath.Join(project, ".cursor", "rules"), KindSkill, "rule-")
	b.instructionFile(filepath.Join(project, ".cursorrules"), "cursor-rules")
	b.mcpNamesJSON(filepath.Join(home, ".cursor", "mcp.json"), "mcpServers")
	b.mcpNamesJSON(filepath.Join(project, ".cursor", "mcp.json"), "mcpServers")
}

var (
	tomlPromptRe = regexp.MustCompile(`(?s)prompt\s*=\s*(?:"""(.*?)"""|"((?:[^"\\]|\\.)*)")`)
	tomlDescRe   = regexp.MustCompile(`(?m)^description\s*=\s*"([^"]*)"`)
)

func (b *Bundle) scanGemini(home, project string) {
	b.instructionFile(filepath.Join(home, ".gemini", "GEMINI.md"), "gemini-user-instructions")
	for _, dir := range []string{filepath.Join(home, ".gemini", "commands"), filepath.Join(project, ".gemini", "commands")} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".toml") {
				continue
			}
			text, ok := readFile(filepath.Join(dir, e.Name()))
			if !ok {
				continue
			}
			m := tomlPromptRe.FindStringSubmatch(text)
			if m == nil {
				continue
			}
			body := m[1]
			if body == "" {
				body = strings.NewReplacer(`\n`, "\n", `\"`, `"`, `\\`, `\`).Replace(m[2])
			}
			desc := ""
			if d := tomlDescRe.FindStringSubmatch(text); d != nil {
				desc = d[1]
			}
			b.add(KindSkill, "cmd-"+strings.TrimSuffix(e.Name(), ".toml"), desc, body, filepath.Join(dir, e.Name()))
		}
	}
	b.mcpNamesJSON(filepath.Join(home, ".gemini", "settings.json"), "mcpServers")
}

func (b *Bundle) scanAgy(home, project string) {
	for _, base := range []string{filepath.Join(project, ".agent"), filepath.Join(project, ".agents"), filepath.Join(home, ".gemini", "antigravity")} {
		b.markdownDir(filepath.Join(base, "rules"), KindSkill, "rule-")
		b.markdownDir(filepath.Join(base, "workflows"), KindSkill, "workflow-")
		b.skillDirs(filepath.Join(base, "skills"))
	}
	b.instructionFile(filepath.Join(home, ".gemini", "GEMINI.md"), "agy-user-instructions")
}

// Render returns the SKILL.md text for an item.
func (it Item) Render() string {
	desc := strings.ReplaceAll(strings.TrimSpace(it.Description), "\n", " ")
	return fmt.Sprintf("---\nname: %s\ndescription: %q\n---\n%s\n", it.Name, desc, it.Body)
}
