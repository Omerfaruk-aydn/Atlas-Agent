package codemap

import (
	"encoding/json"
	"path"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Build and task files say how a project runs: npm scripts, make and just
// targets, Taskfile tasks, Docker stages, Compose services, CI jobs and
// the entry points package manifests declare. Each becomes a definition,
// linked to the tasks it runs and the files its commands use.

// Containers group tasks by the tool that runs them, so npm.build and
// make.build stay apart.
const (
	toolNpm     = "npm"
	toolMake    = "make"
	toolJust    = "just"
	toolTask    = "task"
	toolDocker  = "docker"
	toolCompose = "compose"
	toolJob     = "job"
	toolPython  = "py"
	toolCargo   = "cargo"
)

func extractBuild(out *File, content string) {
	switch out.Language {
	case "npm":
		extractPackageJSON(out, content)
	case "make":
		extractMakefile(out, content, false)
	case "just":
		extractMakefile(out, content, true)
	case "taskfile":
		extractTaskfile(out, content)
	case "docker":
		extractDockerfile(out, content)
	case "compose":
		extractCompose(out, content)
	case "workflow":
		extractWorkflow(out, content)
	case "gitlab-ci":
		extractGitLabCI(out, content)
	case "pyproject", "cargo":
		extractToml(out, content)
	}
}

func addTask(out *File, name, kind, tool string, line, end int, sig string) {
	if name == "" || len(out.Symbols) >= maxSymbolsPerFile {
		return
	}
	out.Symbols = append(out.Symbols, Symbol{Name: name, Kind: kind, Container: tool, Line: line, EndLine: max(line, end), Signature: clipSignature(sig), Exported: true})
}

func taskRef(out *File, name, tool string, line int) {
	if name != "" && !strings.ContainsAny(name, "$/ ") {
		addRef(out, Ref{Name: name, Qualifier: tool, Kind: RefTask, Line: line})
	}
}

var (
	shellSplitRe = regexp.MustCompile(`&&|\|\||;|\||\n`)
	runnerRe     = regexp.MustCompile(`^(?:npm|pnpm|yarn|bun)(?:\s+(?:run|run-script))?\s+([\w:.-]+)`)
)

// scanCommand records what a shell command runs: another project task
// (npm run build, make test, task lint, just fmt) and the project files it
// names (node scripts/build.js, ./deploy.sh).
func scanCommand(out *File, cmd string, line int) {
	for k, part := range shellSplitRe.Split(cmd, -1) {
		at := line + min(k, strings.Count(cmd, "\n"))
		fields := strings.Fields(strings.TrimSpace(part))
		for len(fields) > 0 && (strings.Contains(fields[0], "=") || fields[0] == "sudo" || fields[0] == "exec" || fields[0] == "$" || fields[0] == "time") {
			fields = fields[1:]
		}
		if len(fields) == 0 {
			continue
		}
		joined := strings.Join(fields, " ")
		tool := strings.TrimPrefix(path.Base(fields[0]), "./")
		switch {
		case runnerRe.MatchString(joined):
			name := runnerRe.FindStringSubmatch(joined)[1]
			if (tool == "npm" || tool == "yarn" || tool == "pnpm" || tool == "bun") && (len(fields) < 2 || fields[1] != "run" && fields[1] != "run-script") {
				switch name {
				case "install", "i", "ci", "add", "remove", "publish", "exec", "dlx", "x", "init", "create", "link", "update", "upgrade", "audit", "outdated", "pack", "version", "login", "whoami", "config", "cache", "global", "why", "list", "ls":
					name = ""
				}
			}
			taskRef(out, name, toolNpm, at)
		case tool == "make" || tool == "gmake" || tool == "$(MAKE)" || fields[0] == "$(MAKE)":
			for _, f := range fields[1:] {
				if !strings.HasPrefix(f, "-") && !strings.Contains(f, "=") {
					taskRef(out, f, toolMake, at)
				}
			}
		case tool == "task" || tool == "go-task":
			for _, f := range fields[1:] {
				if !strings.HasPrefix(f, "-") && !strings.Contains(f, "=") {
					taskRef(out, f, toolTask, at)
				}
			}
		case tool == "just":
			if len(fields) > 1 && !strings.HasPrefix(fields[1], "-") {
				taskRef(out, fields[1], toolJust, at)
			}
		}
		for _, f := range fields {
			f = strings.Trim(f, `"'`)
			if strings.HasPrefix(f, "-") || hasScheme(f) || strings.ContainsAny(f, "$*{}") {
				continue
			}
			if ext := strings.ToLower(path.Ext(f)); ext != "" && (linkableExts[ext] || isCodePath(f)) && LanguageOf(f) != "" {
				addLink(out, FileLink{Target: strings.TrimPrefix(f, "./"), Kind: LinkRuns, Line: at})
			}
		}
	}
}

// jsonWalk calls fn for every string value of a JSON document with the
// keys (and array indexes) leading to it and the line it ends on.
func jsonWalk(content string, fn func(keys []string, value string, line int)) {
	dec := json.NewDecoder(strings.NewReader(content))
	dec.UseNumber()
	lines := newLineIndex(content)
	var walk func(keys []string, depth int) bool
	walk = func(keys []string, depth int) bool {
		tok, err := dec.Token()
		if err != nil || depth > 64 {
			return false
		}
		switch v := tok.(type) {
		case json.Delim:
			switch v {
			case '{':
				for dec.More() {
					kt, err := dec.Token()
					if err != nil {
						return false
					}
					k, _ := kt.(string)
					if !walk(append(keys, k), depth+1) {
						return false
					}
				}
			case '[':
				for idx := 0; dec.More(); idx++ {
					if !walk(append(keys, strconv.Itoa(idx)), depth+1) {
						return false
					}
				}
			}
			_, err := dec.Token()
			return err == nil
		case string:
			fn(keys, v, lines.at(int(dec.InputOffset())-1))
		}
		return true
	}
	walk(nil, 0)
}

func extractPackageJSON(out *File, content string) {
	entries := words("main module types typings browser bin exports source unpkg jsdelivr")
	jsonWalk(content, func(keys []string, value string, line int) {
		if len(keys) == 0 {
			return
		}
		switch {
		case len(keys) == 2 && keys[0] == "scripts":
			addTask(out, keys[1], "script", toolNpm, line, line, keys[1]+": "+value)
			scanCommand(out, value, line)
		case entries[keys[0]] && (strings.HasPrefix(value, "./") || strings.Contains(value, "/") || path.Ext(value) != ""):
			if !hasScheme(value) && path.Ext(value) != "" {
				addLink(out, FileLink{Target: strings.TrimPrefix(value, "./"), Kind: LinkEntry, Line: line})
			}
		}
	})
}

var (
	makeRuleRe = regexp.MustCompile(`^([^\s:=#][^:=#]*?)\s*(::?)(?:[^=]|$)(.*)$`)
	justRuleRe = regexp.MustCompile(`^@?([A-Za-z_][\w-]*)(?:\s+[^:=]*)?:(?:[^=]|$)(.*)$`)
)

// extractMakefile reads make or just rules: targets, their prerequisites
// and their recipes.
func extractMakefile(out *File, content string, just bool) {
	tool := toolMake
	if just {
		tool = toolJust
	}
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	current := -1
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		n := i + 1
		for strings.HasSuffix(line, "\\") && i+1 < len(lines) {
			i++
			line = strings.TrimSuffix(line, "\\") + " " + lines[i]
		}
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if line[0] == '\t' || just && (line[0] == ' ') {
			if current >= 0 {
				out.Symbols[current].EndLine = i + 1
				scanCommand(out, strings.TrimLeft(strings.TrimSpace(line), "@-+"), n)
			}
			continue
		}
		current = -1
		trimmed := strings.TrimSpace(line)
		if !just {
			if f := strings.Fields(trimmed); len(f) > 1 && (f[0] == "include" || f[0] == "-include" || f[0] == "sinclude") {
				for _, spec := range f[1:] {
					if !strings.Contains(spec, "$") {
						out.Imports = append(out.Imports, Import{Spec: spec, Line: n})
					}
				}
				continue
			}
			m := makeRuleRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			prereqs := m[3]
			if k := strings.IndexByte(prereqs, ';'); k >= 0 {
				scanCommand(out, prereqs[k+1:], n)
				prereqs = prereqs[:k]
			}
			if k := strings.IndexByte(prereqs, '|'); k >= 0 {
				prereqs = prereqs[:k] + " " + prereqs[k+1:]
			}
			first := -1
			for _, target := range strings.Fields(m[1]) {
				if strings.HasPrefix(target, ".") && strings.ToUpper(target) == target || strings.ContainsAny(target, "%$") {
					continue
				}
				addTask(out, target, "target", tool, n, n, trimmed)
				if first < 0 {
					first = len(out.Symbols) - 1
				}
			}
			if first < 0 {
				continue
			}
			current = len(out.Symbols) - 1
			for _, p := range strings.Fields(prereqs) {
				if strings.Contains(p, "$") {
					continue
				}
				if path.Ext(p) != "" || strings.Contains(p, "/") {
					if LanguageOf(p) != "" {
						addLink(out, FileLink{Target: p, Kind: LinkNames, Line: n})
					}
					continue
				}
				taskRef(out, p, tool, n)
			}
			continue
		}
		if strings.Contains(trimmed, ":=") || strings.HasPrefix(trimmed, "set ") || strings.HasPrefix(trimmed, "export ") || strings.HasPrefix(trimmed, "import ") || strings.HasPrefix(trimmed, "mod ") || strings.HasPrefix(trimmed, "alias ") {
			if f := strings.Fields(trimmed); len(f) > 1 && (f[0] == "import" || f[0] == "mod") {
				out.Imports = append(out.Imports, Import{Spec: strings.Trim(f[len(f)-1], `"'?`), Line: n})
			}
			continue
		}
		m := justRuleRe.FindStringSubmatch(trimmed)
		if m == nil {
			continue
		}
		addTask(out, m[1], "recipe", tool, n, n, trimmed)
		current = len(out.Symbols) - 1
		for _, dep := range strings.Fields(m[2]) {
			dep = strings.TrimLeft(strings.Trim(dep, "()"), "&")
			if isCSSName(dep) {
				taskRef(out, dep, tool, n)
			}
		}
	}
}

// yamlRoot parses a YAML document and returns its top mapping.
func yamlRoot(content string) *yaml.Node {
	var doc yaml.Node
	if yaml.Unmarshal([]byte(content), &doc) != nil || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil
	}
	return doc.Content[0]
}

// yamlCommands scans every string under n as a command.
func yamlCommands(out *File, n *yaml.Node) {
	if n == nil {
		return
	}
	switch n.Kind {
	case yaml.ScalarNode:
		scanCommand(out, n.Value, n.Line)
	case yaml.SequenceNode, yaml.MappingNode:
		for _, c := range n.Content {
			yamlCommands(out, c)
		}
	}
}

