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

// yamlNames calls fn for a scalar or each scalar of a sequence, or each
// key of a mapping.
func yamlNames(n *yaml.Node, fn func(name string, line int)) {
	if n == nil {
		return
	}
	switch n.Kind {
	case yaml.ScalarNode:
		fn(n.Value, n.Line)
	case yaml.SequenceNode:
		for _, c := range n.Content {
			switch c.Kind {
			case yaml.ScalarNode:
				fn(c.Value, c.Line)
			case yaml.MappingNode:
				for k := 0; k+1 < len(c.Content); k += 2 {
					if v := c.Content[k+1]; v.Kind == yaml.ScalarNode && (c.Content[k].Value == "job" || c.Content[k].Value == "task") {
						fn(v.Value, v.Line)
					}
				}
			}
		}
	case yaml.MappingNode:
		for k := 0; k+1 < len(n.Content); k += 2 {
			fn(n.Content[k].Value, n.Content[k].Line)
		}
	}
}

func extractTaskfile(out *File, content string) {
	root := yamlRoot(content)
	if root == nil {
		return
	}
	if includes := yamlGet(root, "includes"); includes != nil {
		for k := 0; k+1 < len(includes.Content); k += 2 {
			v := includes.Content[k+1]
			spec := v.Value
			if v.Kind == yaml.MappingNode {
				if t := yamlGet(v, "taskfile"); t != nil {
					spec = t.Value
				}
			}
			if spec != "" {
				out.Imports = append(out.Imports, Import{Spec: spec, Line: v.Line})
			}
		}
	}
	tasks := yamlGet(root, "tasks")
	if tasks == nil {
		return
	}
	for k := 0; k+1 < len(tasks.Content); k += 2 {
		key, body := tasks.Content[k], tasks.Content[k+1]
		sig := key.Value + ":"
		if d := yamlGet(body, "desc"); d != nil {
			sig += " " + d.Value
		}
		addTask(out, key.Value, "task", toolTask, key.Line, yamlEnd(body), sig)
		yamlNames(yamlGet(body, "deps"), func(name string, line int) { taskRef(out, name, toolTask, line) })
		cmds := yamlGet(body, "cmds")
		if cmds == nil {
			if body.Kind == yaml.SequenceNode || body.Kind == yaml.ScalarNode {
				cmds = body
			}
		}
		if cmds == nil {
			continue
		}
		for _, c := range append([]*yaml.Node{cmds}, cmds.Content...) {
			switch c.Kind {
			case yaml.ScalarNode:
				scanCommand(out, c.Value, c.Line)
			case yaml.MappingNode:
				if t := yamlGet(c, "task"); t != nil {
					taskRef(out, t.Value, toolTask, t.Line)
				}
				if t := yamlGet(c, "cmd"); t != nil {
					scanCommand(out, t.Value, t.Line)
				}
			}
		}
	}
}

func extractDockerfile(out *File, content string) {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	current := -1
	for i := 0; i < len(lines); i++ {
		n := i + 1
		line := strings.TrimSpace(lines[i])
		for strings.HasSuffix(line, "\\") && i+1 < len(lines) {
			i++
			line = strings.TrimSuffix(line, "\\") + " " + strings.TrimSpace(lines[i])
		}
		if line == "" || line[0] == '#' {
			continue
		}
		fields := strings.Fields(line)
		instr := strings.ToUpper(fields[0])
		args := fields[1:]
		if current >= 0 {
			out.Symbols[current].EndLine = i + 1
		}
		switch instr {
		case "FROM":
			var image string
			for _, a := range args {
				if !strings.HasPrefix(a, "--") {
					image = a
					break
				}
			}
			taskRef(out, image, toolDocker, n)
			for k, a := range args {
				if strings.EqualFold(a, "as") && k+1 < len(args) {
					addTask(out, args[k+1], "stage", toolDocker, n, n, line)
					current = len(out.Symbols) - 1
				}
			}
		case "COPY", "ADD":
			var sources []string
			from := false
			for _, a := range args {
				if v, ok := strings.CutPrefix(a, "--from="); ok {
					taskRef(out, v, toolDocker, n)
					from = true
					continue
				}
				if !strings.HasPrefix(a, "--") {
					sources = append(sources, strings.Trim(a, `[]",`))
				}
			}
			if len(sources) > 1 && !from {
				for _, s := range sources[:len(sources)-1] {
					if path.Ext(s) != "" && LanguageOf(s) != "" && !strings.ContainsAny(s, "*$") {
						addLink(out, FileLink{Target: strings.TrimPrefix(s, "./"), Kind: LinkIncludes, Line: n})
					}
				}
			}
		case "RUN", "CMD", "ENTRYPOINT":
			cmd := strings.Join(args, " ")
			if strings.HasPrefix(cmd, "[") {
				var parts []string
				if json.Unmarshal([]byte(cmd), &parts) == nil {
					cmd = strings.Join(parts, " ")
				}
			}
			scanCommand(out, cmd, n)
		}
	}
}

func extractCompose(out *File, content string) {
	root := yamlRoot(content)
	services := yamlGet(root, "services")
	if services == nil {
		return
	}
	for k := 0; k+1 < len(services.Content); k += 2 {
		key, body := services.Content[k], services.Content[k+1]
		sig := key.Value + ":"
		if img := yamlGet(body, "image"); img != nil {
			sig += " " + img.Value
		}
		addTask(out, key.Value, "service", toolCompose, key.Line, yamlEnd(body), sig)
		yamlNames(yamlGet(body, "depends_on"), func(name string, line int) { taskRef(out, name, toolCompose, line) })
		if ext := yamlGet(body, "extends"); ext != nil {
			if s := yamlGet(ext, "service"); s != nil {
				taskRef(out, s.Value, toolCompose, s.Line)
			}
		}
		if build := yamlGet(body, "build"); build != nil {
			ctx, file, line := build.Value, "Dockerfile", build.Line
			if build.Kind == yaml.MappingNode {
				ctx = "."
				if c := yamlGet(build, "context"); c != nil {
					ctx, line = c.Value, c.Line
				}
				if d := yamlGet(build, "dockerfile"); d != nil {
					file, line = d.Value, d.Line
				}
			}
			if ctx != "" && !hasScheme(ctx) {
				addLink(out, FileLink{Target: path.Join(ctx, file), Kind: LinkRuns, Line: line})
			}
		}
		for _, field := range []string{"command", "entrypoint"} {
			yamlCommands(out, yamlGet(body, field))
		}
	}
}

func extractWorkflow(out *File, content string) {
	root := yamlRoot(content)
	jobs := yamlGet(root, "jobs")
	if jobs == nil {
		return
	}
	for k := 0; k+1 < len(jobs.Content); k += 2 {
		key, body := jobs.Content[k], jobs.Content[k+1]
		sig := key.Value + ":"
		if name := yamlGet(body, "name"); name != nil {
			sig += " " + name.Value
		}
		addTask(out, key.Value, "job", toolJob, key.Line, yamlEnd(body), sig)
		yamlNames(yamlGet(body, "needs"), func(name string, line int) { taskRef(out, name, toolJob, line) })
		if steps := yamlGet(body, "steps"); steps != nil {
			for _, step := range steps.Content {
				if run := yamlGet(step, "run"); run != nil {
					for j, cmd := range strings.Split(run.Value, "\n") {
						line := run.Line + j
						if run.Style == yaml.LiteralStyle || run.Style == yaml.FoldedStyle {
							line++
						}
						scanCommand(out, cmd, line)
					}
				}
			}
		}
	}
}

var gitlabReserved = words("stages variables include default workflow image services before_script after_script cache")

