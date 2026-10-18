// Package envprobe detects the toolchains installed on the machine and
// their versions, once per process, so the model does not spend turns on
// commands that cannot run ("python: command not found") or write code for a
// runtime version that is not there.
//
// Detection runs in the background when it is first requested; until it
// finishes, Block returns "" and the request goes on without it. Every probe
// has a short timeout, and a tool that is missing or slow is simply left out.
package envprobe

import (
	"bytes"
	"context"
	"os/exec"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"
)

// probeTimeout bounds one version command; the whole detection runs the
// probes in parallel.
const probeTimeout = 4 * time.Second

// Tool is one detected toolchain.
type Tool struct {
	Name    string
	Version string
}

type probe struct {
	name string
	// commands are tried in order; the first that runs wins, so python3
	// and python count once.
	commands [][]string
}

var probes = []probe{
	{"go", [][]string{{"go", "version"}}},
	{"node", [][]string{{"node", "--version"}}},
	{"npm", [][]string{{"npm", "--version"}}},
	{"pnpm", [][]string{{"pnpm", "--version"}}},
	{"yarn", [][]string{{"yarn", "--version"}}},
	{"bun", [][]string{{"bun", "--version"}}},
	{"deno", [][]string{{"deno", "--version"}}},
	{"python", [][]string{{"python3", "--version"}, {"python", "--version"}, {"py", "--version"}}},
	{"uv", [][]string{{"uv", "--version"}}},
	{"rustc", [][]string{{"rustc", "--version"}}},
	{"cargo", [][]string{{"cargo", "--version"}}},
	{"java", [][]string{{"java", "-version"}}},
	{"dotnet", [][]string{{"dotnet", "--version"}}},
	{"gcc", [][]string{{"gcc", "--version"}}},
	{"clang", [][]string{{"clang", "--version"}}},
	{"make", [][]string{{"make", "--version"}}},
	{"cmake", [][]string{{"cmake", "--version"}}},
	{"ruby", [][]string{{"ruby", "--version"}}},
	{"php", [][]string{{"php", "--version"}}},
	{"zig", [][]string{{"zig", "version"}}},
	{"docker", [][]string{{"docker", "--version"}}},
	{"git", [][]string{{"git", "--version"}}},
}

var versionPattern = regexp.MustCompile(`\d+\.\d+(?:\.\d+)?(?:[-+.][\w.]+)?`)

var (
	once     sync.Once
	mu       sync.Mutex
	done     bool
	detected []Tool
	missing  []string
)

// Start begins detection in the background if it has not started yet.
func Start() {
	once.Do(func() {
		go func() {
			tools, absent := Detect(context.Background())
			mu.Lock()
			detected, missing, done = tools, absent, true
			mu.Unlock()
		}()
	})
}

// Detect runs every probe and returns the tools found, sorted by name, and
// the names of those not found.
func Detect(ctx context.Context) ([]Tool, []string) {
	var wg sync.WaitGroup
	results := make([]Tool, len(probes))
	for i, p := range probes {
		wg.Go(func() {
			results[i] = Tool{Name: p.name, Version: run(ctx, p)}
		})
	}
	wg.Wait()
	var found []Tool
	var absent []string
	for _, t := range results {
		if t.Version == "" {
			absent = append(absent, t.Name)
			continue
		}
		found = append(found, t)
	}
	slices.SortFunc(found, func(a, b Tool) int { return strings.Compare(a.Name, b.Name) })
	slices.Sort(absent)
	return found, absent
}

func run(ctx context.Context, p probe) string {
	for _, command := range p.commands {
		path, err := exec.LookPath(command[0])
		if err != nil {
			continue
		}
		if runtime.GOOS == "windows" && strings.Contains(strings.ToLower(path), `\windowsapps\`) && strings.HasPrefix(command[0], "python") {
			continue // The Store alias opens the Store instead of running Python.
		}
		probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
		cmd := exec.CommandContext(probeCtx, path, command[1:]...)
		var out bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &out
		err = cmd.Run()
		cancel()
		if err != nil {
			continue
		}
		if version := versionPattern.FindString(firstLine(out.String())); version != "" {
			return version
		}
	}
	return ""
}

func firstLine(s string) string {
	for line := range strings.SplitSeq(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

// Block is the system prompt part listing the detected toolchains, or ""
// while detection is still running.
func Block() string {
	Start()
	mu.Lock()
	defer mu.Unlock()
	if !done || len(detected) == 0 {
		return ""
	}
	parts := make([]string, 0, len(detected))
	for _, t := range detected {
		parts = append(parts, t.Name+" "+t.Version)
	}
	text := "\n\n<installed_toolchains>\nOn PATH: " + strings.Join(parts, ", ") + "."
	if len(missing) > 0 {
		text += " Not found: " + strings.Join(missing, ", ") + "."
	}
	return text + " Use what is installed; when the work needs something missing, say so instead of assuming it exists. Write code for these versions.\n</installed_toolchains>"
}
