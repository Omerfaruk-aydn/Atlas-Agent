package harnessmetrics

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Strategy statistics answer "what verified work like this before?". Each
// time the verification contract holds, the passing commands are recorded
// with the kinds of project they ran in (detected from marker files). The
// commands seen most often for the current project's kinds are suggested
// when a plan has no checks yet, and listed by `atlas stats harness`.

// ProjectKinds detects the kinds of project at root from its marker files,
// sorted. It reads only the top directory.
func ProjectKinds(root string) []string {
	markers := []struct{ file, kind string }{
		{"go.mod", "go"},
		{"Cargo.toml", "rust"},
		{"package.json", "node"},
		{"deno.json", "deno"},
		{"pyproject.toml", "python"},
		{"requirements.txt", "python"},
		{"setup.py", "python"},
		{"pom.xml", "jvm"},
		{"build.gradle", "jvm"},
		{"build.gradle.kts", "jvm"},
		{"Gemfile", "ruby"},
		{"composer.json", "php"},
		{"Package.swift", "swift"},
		{"mix.exs", "elixir"},
		{"pubspec.yaml", "dart"},
		{"CMakeLists.txt", "cmake"},
		{"Makefile", "make"},
		{"meson.build", "meson"},
		{"build.zig", "zig"},
		{"Dockerfile", "docker"},
	}
	var kinds []string
	for _, m := range markers {
		if _, err := os.Stat(filepath.Join(root, m.file)); err == nil && !slices.Contains(kinds, m.kind) {
			kinds = append(kinds, m.kind)
		}
	}
	if entries, err := os.ReadDir(root); err == nil {
		for _, e := range entries {
			name := strings.ToLower(e.Name())
			if (strings.HasSuffix(name, ".csproj") || strings.HasSuffix(name, ".sln")) && !slices.Contains(kinds, "dotnet") {
				kinds = append(kinds, "dotnet")
			}
		}
	}
	slices.Sort(kinds)
	return kinds
}

// CheckAttrs encodes a passing contract for a ContractPassed event.
func CheckAttrs(kinds, commands []string) map[string]string {
	attrs := map[string]string{"checks": strconv.Itoa(len(commands)), "kind": strings.Join(kinds, ",")}
	for i, command := range commands[:min(len(commands), 8)] {
		if c := portableCommand(command); c != "" {
			attrs["c"+strconv.Itoa(i)] = c
		}
	}
	return attrs
}

var (
	cdPrefix   = regexp.MustCompile(`^\s*(cd|pushd)\s+\S+\s*(&&|;)\s*`)
	absPathArg = regexp.MustCompile(`(^|\s)(/|[A-Za-z]:[/\\])\S*`)
)

// portableCommand strips what ties a command to one checkout (a leading cd,
// absolute paths); a command that is mostly path is dropped.
func portableCommand(command string) string {
	command = strings.Join(strings.Fields(command), " ")
	for cdPrefix.MatchString(command) {
		command = cdPrefix.ReplaceAllString(command, "")
	}
	if absPathArg.MatchString(command) || len(command) > 160 {
		return ""
	}
	return command
}

// checkCount is how often a command verified a kind of project.
type checkCount struct {
	command string
	count   int
}

// checksByKind counts passing contract commands per project kind, once per
// session so one long session does not dominate.
func checksByKind(events []Event) map[string][]checkCount {
	seen := map[string]bool{}
	counts := map[string]map[string]int{}
	for _, e := range events {
		if e.Kind != ContractPassed || e.Attrs["kind"] == "" {
			continue
		}
		for key, command := range e.Attrs {
			if !strings.HasPrefix(key, "c") || key == "checks" || command == "" {
				continue
			}
			for kind := range strings.SplitSeq(e.Attrs["kind"], ",") {
				id := e.Session + "\x00" + kind + "\x00" + command
				if seen[id] {
					continue
				}
				seen[id] = true
				if counts[kind] == nil {
					counts[kind] = map[string]int{}
				}
				counts[kind][command]++
			}
		}
	}
	out := map[string][]checkCount{}
	for kind, commands := range counts {
		for command, n := range commands {
			out[kind] = append(out[kind], checkCount{command: command, count: n})
		}
		slices.SortFunc(out[kind], func(a, b checkCount) int {
			if a.count != b.count {
				return b.count - a.count
			}
			return strings.Compare(a.command, b.command)
		})
	}
	return out
}

// hintCache keeps the last hints per data directory until the events file
// changes.
var hintCache sync.Map // root -> cachedHints

type cachedHints struct {
	stamp string
	by    map[string][]checkCount
}

// strategyWindow bounds how far back hints look.
const strategyWindow = 90 * 24 * time.Hour

// CheckHints returns up to limit commands that verified projects of the
// given kinds before, most frequent first.
func CheckHints(root string, kinds []string, limit int) []string {
	if root == "" || len(kinds) == 0 {
		return nil
	}
	stamp := ""
	for _, name := range []string{"events.1.jsonl", File} {
		if info, err := os.Stat(filepath.Join(root, Dir, name)); err == nil {
			stamp += info.ModTime().String() + strconv.FormatInt(info.Size(), 10)
		}
	}
	if stamp == "" {
		return nil
	}
	var by map[string][]checkCount
	if cached, ok := hintCache.Load(root); ok && cached.(cachedHints).stamp == stamp {
		by = cached.(cachedHints).by
	} else {
		events, err := Load(root, time.Now().Add(-strategyWindow))
		if err != nil {
			return nil
		}
		by = checksByKind(events)
		hintCache.Store(root, cachedHints{stamp: stamp, by: by})
	}
	merged := map[string]int{}
	for _, kind := range kinds {
		for _, c := range by[kind] {
			merged[c.command] = max(merged[c.command], c.count)
		}
	}
	commands := make([]string, 0, len(merged))
	for command := range merged {
		commands = append(commands, command)
	}
	slices.SortFunc(commands, func(a, b string) int {
		if merged[a] != merged[b] {
			return merged[b] - merged[a]
		}
		return strings.Compare(a, b)
	})
	return commands[:min(len(commands), limit)]
}
