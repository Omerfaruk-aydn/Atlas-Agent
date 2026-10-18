package workplan

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Models remember the APIs of the library versions they were trained on.
// When a project pins a different version, code written from memory calls
// functions that were renamed, removed or never existed. Dependencies reads
// the versions a project actually uses from its manifests, preferring the
// installed version where one is recorded, so the agent can check the
// documentation for those versions instead of guessing.

// Dependency is one direct dependency and its version.
type Dependency struct {
	Ecosystem string
	Name      string
	Version   string
}

const maxDependencies = 24

// Dependencies lists direct dependencies from the manifests at root:
// package.json (with versions from node_modules when installed), go.mod,
// requirements.txt, pyproject.toml and Cargo.toml. Unreadable manifests are
// skipped; this is a hint, never a reason to fail.
func Dependencies(root string) []Dependency {
	var out []Dependency
	out = append(out, npmDependencies(root)...)
	out = append(out, goDependencies(root)...)
	out = append(out, pythonDependencies(root)...)
	out = append(out, cargoDependencies(root)...)
	return out[:min(len(out), maxDependencies)]
}

func npmDependencies(root string) []Dependency {
	var manifest struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	data, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil || json.Unmarshal(data, &manifest) != nil {
		return nil
	}
	var out []Dependency
	for _, deps := range []map[string]string{manifest.Dependencies, manifest.DevDependencies} {
		names := make([]string, 0, len(deps))
		for name := range deps {
			names = append(names, name)
		}
		slices.Sort(names)
		for _, name := range names {
			version := deps[name]
			var installed struct {
				Version string `json:"version"`
			}
			if data, err := os.ReadFile(filepath.Join(root, "node_modules", filepath.FromSlash(name), "package.json")); err == nil &&
				json.Unmarshal(data, &installed) == nil && installed.Version != "" {
				version = installed.Version
			}
			out = append(out, Dependency{"npm", name, version})
		}
	}
	return out
}

var goRequire = regexp.MustCompile(`^\s*([^\s()]+)\s+(v[^\s]+)(\s*//\s*indirect)?`)

func goDependencies(root string) []Dependency {
	f, err := os.Open(filepath.Join(root, "go.mod"))
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []Dependency
	inBlock := false
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case strings.HasPrefix(line, "require ("):
			inBlock = true
			continue
		case inBlock && line == ")":
			inBlock = false
			continue
		case strings.HasPrefix(line, "require "):
			line = strings.TrimPrefix(line, "require ")
		case !inBlock:
			continue
		}
		if m := goRequire.FindStringSubmatch(line); m != nil && m[3] == "" {
			out = append(out, Dependency{"go", m[1], m[2]})
		}
	}
	return out
}

var pythonPin = regexp.MustCompile(`^\s*["']?([A-Za-z0-9_.\-\[\]]+)\s*(==|>=|~=|<=|>|<)\s*([^;,"'\s]+)`)

func pythonDependencies(root string) []Dependency {
	var out []Dependency
	if data, err := os.ReadFile(filepath.Join(root, "requirements.txt")); err == nil {
		for line := range strings.SplitSeq(string(data), "\n") {
			if m := pythonPin.FindStringSubmatch(line); m != nil && !strings.HasPrefix(strings.TrimSpace(line), "#") {
				out = append(out, Dependency{"pypi", m[1], m[2] + m[3]})
			}
		}
	}
	if data, err := os.ReadFile(filepath.Join(root, "pyproject.toml")); err == nil {
		inDeps := false
		for line := range strings.SplitSeq(string(data), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "dependencies") && strings.Contains(trimmed, "[") {
				inDeps = true
			}
			if inDeps {
				if m := pythonPin.FindStringSubmatch(strings.TrimPrefix(trimmed, "dependencies = [")); m != nil {
					out = append(out, Dependency{"pypi", m[1], m[2] + m[3]})
				}
				if strings.Contains(trimmed, "]") {
					inDeps = false
				}
			}
		}
	}
	return out
}

var cargoDep = regexp.MustCompile(`^\s*([A-Za-z0-9_\-]+)\s*=\s*(?:"([^"]+)"|\{[^}]*version\s*=\s*"([^"]+)")`)

func cargoDependencies(root string) []Dependency {
	data, err := os.ReadFile(filepath.Join(root, "Cargo.toml"))
	if err != nil {
		return nil
	}
	var out []Dependency
	inDeps := false
	for line := range strings.SplitSeq(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			inDeps = trimmed == "[dependencies]" || trimmed == "[dev-dependencies]"
			continue
		}
		if inDeps {
			if m := cargoDep.FindStringSubmatch(trimmed); m != nil {
				out = append(out, Dependency{"crates", m[1], m[2] + m[3]})
			}
		}
	}
	return out
}
