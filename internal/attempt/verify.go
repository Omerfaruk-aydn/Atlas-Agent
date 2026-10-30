package attempt

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workplan"
)

// Verify-command sources reported in a Report.
const (
	SourceFlag     = "flag"
	SourceWorkPlan = "work.json"
	SourceDetected = "detected"
	SourceNone     = "none"
)

// ResolveVerify decides which commands prove an attempt works. Explicit
// commands win, then the verify list of .atlas/work.json, then the test
// command detected from the project's files.
func ResolveVerify(root string, explicit []string) ([]string, string) {
	if cmds := cleanCommands(explicit); len(cmds) > 0 {
		return cmds, SourceFlag
	}
	if plan, _, err := workplan.Load(root); err == nil && plan != nil {
		var cmds []string
		for _, check := range plan.Verify {
			cmds = append(cmds, check.Command)
		}
		if cmds = cleanCommands(cmds); len(cmds) > 0 {
			return cmds, SourceWorkPlan
		}
	}
	if cmd := detectTestCommand(root); cmd != "" {
		return []string{cmd}, SourceDetected
	}
	return nil, SourceNone
}

// ResolveLint returns explicit lint commands or the cheap lint command the
// project's files imply.
func ResolveLint(root string, explicit []string) []string {
	if cmds := cleanCommands(explicit); len(cmds) > 0 {
		return cmds
	}
	switch {
	case exists(root, "go.mod"):
		return []string{"go vet ./..."}
	case hasNPMScript(root, "lint"):
		return []string{"npm run lint"}
	}
	return nil
}

func cleanCommands(in []string) []string {
	var out []string
	for _, c := range in {
		if c = strings.TrimSpace(c); c != "" {
			out = append(out, c)
		}
	}
	return out
}

func exists(root, name string) bool {
	_, err := os.Stat(filepath.Join(root, name))
	return err == nil
}

func detectTestCommand(root string) string {
	switch {
	case exists(root, "go.mod"):
		return "go test ./..."
	case exists(root, "Cargo.toml"):
		return "cargo test"
	case hasNPMScript(root, "test"):
		return "npm test"
	case exists(root, "pytest.ini"), exists(root, "tox.ini"), pyprojectUsesPytest(root):
		return "python -m pytest -q"
	case makefileHasTarget(root, "test"):
		return "make test"
	}
	return ""
}

func hasNPMScript(root, name string) bool {
	data, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		return false
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if json.Unmarshal(data, &pkg) != nil {
		return false
	}
	script := strings.TrimSpace(pkg.Scripts[name])
	// The "npm init" placeholder always fails and proves nothing.
	return script != "" && !strings.Contains(script, "no test specified")
}

func pyprojectUsesPytest(root string) bool {
	data, err := os.ReadFile(filepath.Join(root, "pyproject.toml"))
	return err == nil && strings.Contains(string(data), "pytest")
}

func makefileHasTarget(root, target string) bool {
	data, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, target+":") {
			return true
		}
	}
	return false
}
