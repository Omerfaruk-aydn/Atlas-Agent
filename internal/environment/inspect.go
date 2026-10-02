package environment

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"golang.org/x/mod/modfile"
)

type executionOSKey struct{}

func WithExecutionOS(ctx context.Context, value string) context.Context {
	return context.WithValue(ctx, executionOSKey{}, value)
}

func Inspect(ctx context.Context, root string) (EnvironmentPlan, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return EnvironmentPlan{}, err
	}
	osName, _ := ctx.Value(executionOSKey{}).(string)
	if osName == "" {
		osName = runtime.GOOS
	}
	if osName != "linux" && osName != "windows" && osName != "darwin" {
		return EnvironmentPlan{}, fmt.Errorf("unsupported environment execution OS")
	}
	plan := EnvironmentPlan{Root: filepath.Clean(root), ExecutionOS: osName, Sources: []engineering.SourceReference{}, Requirements: []ToolRequirement{}, Commands: []Command{}, Conflicts: []string{}}
	files := map[string][]byte{}
	for _, name := range []string{"go.mod", "go.sum", "go.work", "package.json", "package-lock.json", "npm-shrinkwrap.json", "pnpm-lock.yaml", "yarn.lock", "bun.lock", "bun.lockb", "requirements.txt", "pyproject.toml", "uv.lock", "poetry.lock", "Pipfile.lock", "Cargo.toml", "Cargo.lock"} {
		data, err := engineering.ReadProjectEvidence(ctx, root, name)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return plan, err
		}
		if len(data) > 512*1024 {
			return plan, fmt.Errorf("environment manifest exceeds inspection limit")
		}
		files[name] = data
		plan.Sources = append(plan.Sources, engineering.SourceReference{Path: name, Fingerprint: engineering.Hash(string(data))})
	}
	add := func(network bool, argv ...string) {
		plan.Commands = append(plan.Commands, Command{Argv: argv, Directory: root, NeedsNetwork: network})
	}
	if data, ok := files["go.mod"]; ok {
		module, err := modfile.ParseLax("go.mod", data, nil)
		if err != nil {
			return plan, fmt.Errorf("invalid go.mod: %w", err)
		}
		constraint := ""
		if module.Go != nil {
			constraint = ">=" + module.Go.Version
		}
		plan.Requirements = append(plan.Requirements, ToolRequirement{Name: "go", Constraint: constraint, Manifest: "go.mod"})
		add(true, "go", "mod", "download")
		plan.Commands[len(plan.Commands)-1].Env = []string{"GOTOOLCHAIN=local"}
	}
	if data, ok := files["package.json"]; ok {
		var pkg struct {
			Engines        map[string]string `json:"engines"`
			PackageManager string            `json:"packageManager"`
		}
		if err := json.Unmarshal(data, &pkg); err != nil {
			return plan, fmt.Errorf("invalid package.json: %w", err)
		}
		plan.Requirements = append(plan.Requirements, ToolRequirement{Name: "node", Constraint: pkg.Engines["node"], Manifest: "package.json"})
		locks := []string{}
		manager := "npm"
		for _, entry := range []struct{ path, manager string }{{"package-lock.json", "npm"}, {"npm-shrinkwrap.json", "npm"}, {"pnpm-lock.yaml", "pnpm"}, {"yarn.lock", "yarn"}, {"bun.lock", "bun"}, {"bun.lockb", "bun"}} {
			if _, ok := files[entry.path]; ok {
				locks = append(locks, entry.path)
				manager = entry.manager
			}
		}
		if len(locks) > 1 {
			plan.Conflicts = append(plan.Conflicts, "Multiple Node lockfiles require an explicit package-manager choice")
		}
		if pkg.PackageManager != "" {
			declared, version, ok := strings.Cut(pkg.PackageManager, "@")
			if !ok || declared != "npm" && declared != "pnpm" && declared != "yarn" && declared != "bun" {
				plan.Conflicts = append(plan.Conflicts, "Unsupported packageManager declaration")
			} else {
				if len(locks) > 0 && declared != manager {
					plan.Conflicts = append(plan.Conflicts, "packageManager conflicts with the lockfile")
				}
				manager = declared
				plan.Requirements = append(plan.Requirements, ToolRequirement{Name: manager, Constraint: version, Manifest: "package.json"})
			}
		}
		if len(locks) == 0 {
			plan.Conflicts = append(plan.Conflicts, "Node dependency preparation requires a committed lockfile")
		} else {
			switch manager {
			case "npm":
				add(true, "npm", "ci")
			case "pnpm":
				add(true, "pnpm", "install", "--frozen-lockfile")
			case "yarn":
				add(true, "yarn", "install", "--frozen-lockfile")
			case "bun":
				add(true, "bun", "install", "--frozen-lockfile")
			}
		}
	}
	_, requirements := files["requirements.txt"]
	if pyproject, ok := files["pyproject.toml"]; ok || requirements {
		constraint := ""
		if ok {
			var valid bool
			constraint, valid = tomlString(pyproject, "project", "requires-python")
			if !valid {
				plan.Conflicts = append(plan.Conflicts, "Unsupported or malformed Python version declaration")
			}
		}
		python := "python3"
		if osName == "windows" {
			python = "python"
		}
		manifest := "requirements.txt"
		if ok {
			manifest = "pyproject.toml"
		}
		plan.Requirements = append(plan.Requirements, ToolRequirement{Name: python, Constraint: constraint, Manifest: manifest})
		add(false, python, "-m", "venv", "--copies", ".venv")
		local := "./.venv/bin/python"
		if osName == "windows" {
			local = "./.venv/Scripts/python.exe"
		}
		_, uv := files["uv.lock"]
		if uv {
			plan.Requirements = append(plan.Requirements, ToolRequirement{Name: "uv", Manifest: "uv.lock"})
			add(true, "uv", "sync", "--frozen", "--cache-dir", ".atlas-env/uv", "--python", local, "--no-python-downloads", "--link-mode", "copy")
		} else {
			if _, poetry := files["poetry.lock"]; poetry {
				plan.Conflicts = append(plan.Conflicts, "Poetry requires a verified project-local virtualenv plan")
			}
			if _, pipfile := files["Pipfile.lock"]; pipfile {
				plan.Conflicts = append(plan.Conflicts, "Pipenv requires a verified project-local virtualenv plan")
			}
			if requirements {
				add(true, local, "-m", "pip", "install", "-r", "requirements.txt")
			} else {
				add(true, local, "-m", "pip", "install", ".")
			}
		}
	}
	if data, ok := files["Cargo.toml"]; ok {
		constraint, valid := tomlString(data, "package", "rust-version")
		if !valid {
			plan.Conflicts = append(plan.Conflicts, "Unsupported or malformed Rust version declaration")
		}
		if constraint != "" {
			constraint = ">=" + constraint
		}
		plan.Requirements = append(plan.Requirements, ToolRequirement{Name: "rustc", Constraint: constraint, Manifest: "Cargo.toml"})
		if _, ok := files["Cargo.lock"]; !ok {
			plan.Conflicts = append(plan.Conflicts, "Rust dependency preparation requires a committed Cargo.lock")
		} else {
			add(true, "cargo", "fetch", "--locked")
		}
	}
	if len(plan.Requirements) == 0 {
		plan.Conflicts = append(plan.Conflicts, "No supported root dependency manifest was found")
	}
	if len(plan.Conflicts) > 0 {
		plan.Commands = []Command{}
	}
	data, err := json.Marshal(plan.Sources)
	if err != nil {
		return plan, err
	}
	plan.SourceFingerprint = engineering.Hash(string(data))
	return plan, ctx.Err()
}

func tomlString(data []byte, section, key string) (string, bool) {
	current := ""
	value, found := "", false
	pattern := regexp.MustCompile(`^` + regexp.QuoteMeta(key) + `\s*=\s*["']([^"']+)["']\s*(?:#.*)?$`)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			current = strings.TrimSpace(strings.Trim(line, "[]"))
			continue
		}
		if current == section {
			if match := pattern.FindStringSubmatch(line); len(match) == 2 {
				if found {
					return "", false
				}
				value, found = match[1], true
			} else if strings.HasPrefix(line, key) {
				return "", false
			}
		}
	}
	return value, true
}
