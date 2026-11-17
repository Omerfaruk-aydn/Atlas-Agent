package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

// StateFile records which installed packages are switched on. It lives next
// to the packages, not inside one, so a package cannot ship its own state.
const StateFile = "state.json"

// State maps a package name to an explicit on/off choice. A package with no
// entry follows its default: packages that only add skills, commands, styles
// or agents are on, packages that can run code (hooks, MCP) are off until a
// person runs `atlas plugin enable`.
type State struct {
	Enabled map[string]bool `json:"enabled"`
}

// LoadState reads the state file. A missing or unreadable file yields an
// empty state, so defaults apply and a damaged file never blocks startup.
func LoadState(pluginsRoot string) State {
	state := State{Enabled: map[string]bool{}}
	data, err := os.ReadFile(filepath.Join(pluginsRoot, StateFile))
	if err != nil {
		return state
	}
	var loaded State
	if json.Unmarshal(data, &loaded) != nil || loaded.Enabled == nil {
		return state
	}
	return loaded
}

func saveState(pluginsRoot string, state State) error {
	if err := os.MkdirAll(pluginsRoot, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(pluginsRoot, StateFile)
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

// SetEnabled records an explicit choice for an installed package. It is only
// reachable from the command line: the agent has no tool that calls it, so a
// package cannot switch itself on.
func SetEnabled(pluginsRoot, name string, enabled bool) error {
	if !validName.MatchString(name) {
		return fmt.Errorf("invalid plugin name %q", name)
	}
	if _, err := Load(filepath.Join(pluginsRoot, name)); err != nil {
		return fmt.Errorf("plugin %s is not installed: %w", name, err)
	}
	state := LoadState(pluginsRoot)
	state.Enabled[name] = enabled
	return saveState(pluginsRoot, state)
}

// forget drops a package's explicit choice, used when it is installed or
// replaced so new content never inherits an earlier approval.
func forget(pluginsRoot, name string) error {
	state := LoadState(pluginsRoot)
	if _, ok := state.Enabled[name]; !ok {
		return nil
	}
	delete(state.Enabled, name)
	return saveState(pluginsRoot, state)
}

// IsEnabled reports whether a package is switched on.
func IsEnabled(state State, manifest Manifest) bool {
	if on, ok := state.Enabled[manifest.Name]; ok {
		return on
	}
	return !manifest.Executable
}

// Installed is a package found under the plugins root.
type Installed struct {
	Manifest Manifest
	Dir      string
	Enabled  bool
}

// List returns valid packages under pluginsRoot with their on/off state.
// Packages that fail validation are skipped here; `atlas plugin validate`
// and `atlas doctor` report why.
func List(pluginsRoot string) []Installed {
	entries, err := os.ReadDir(pluginsRoot)
	if err != nil {
		return nil
	}
	state := LoadState(pluginsRoot)
	var out []Installed
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(pluginsRoot, entry.Name())
		manifest, err := Load(dir)
		if err != nil || manifest.Name != entry.Name() {
			continue
		}
		out = append(out, Installed{Manifest: manifest, Dir: dir, Enabled: IsEnabled(state, manifest)})
	}
	return out
}

// ContentDirs returns the sub-directory of every enabled package that
// declares the given content kind ("skills", "agents", ...) and has it on
// disk. Skills and subagents found this way join the normal discovery paths.
func ContentDirs(pluginsRoot, kind string) []string {
	var dirs []string
	for _, p := range List(pluginsRoot) {
		if !p.Enabled || !slices.Contains(p.Manifest.Contents, kind) {
			continue
		}
		dir := filepath.Join(p.Dir, kind)
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

// Remove deletes an installed package and its on/off choice.
func Remove(pluginsRoot, name string) error {
	if !validName.MatchString(name) {
		return fmt.Errorf("invalid plugin name %q", name)
	}
	if err := os.RemoveAll(filepath.Join(pluginsRoot, name)); err != nil {
		return err
	}
	return forget(pluginsRoot, name)
}
