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

