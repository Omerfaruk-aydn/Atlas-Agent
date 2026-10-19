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

