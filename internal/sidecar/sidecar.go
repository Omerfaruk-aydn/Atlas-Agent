// Package sidecar manages long-running helper processes that live in a
// project: a dashboard, a watcher, a bridge to another system. Each is a
// folder under .atlas/sidecars/<id>/ with a sidecar.json describing the
// command. A sidecar never starts on its own: the user enables it
// (user_config.json), and the agent has no way to flip that switch. A running
// sidecar can drive the agent through `atlas agentapi`.
package sidecar

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Dir is where a project keeps its sidecars.
func Dir(project string) string { return filepath.Join(project, ".atlas", "sidecars") }

// Restart policies.
const (
	RestartAlways    = "always"
	RestartOnFailure = "on-failure"
	RestartNever     = "never"
)

// Definition is sidecar.json.
type Definition struct {
	Command       string            `json:"command"`
	Args          []string          `json:"args,omitempty"`
	Env           map[string]string `json:"env,omitempty"`
	RestartPolicy string            `json:"restart_policy,omitempty"`
	DisplayName   string            `json:"display_name,omitempty"`
	Description   string            `json:"description,omitempty"`
}

// userConfig is user_config.json: what only the user decides.
type userConfig struct {
	Enabled bool `json:"enabled"`
}

// Sidecar is a discovered definition with its on/off state.
type Sidecar struct {
	ID      string
	Dir     string
	Def     Definition
	Enabled bool
	// Version changes whenever the definition or the enabled state does, so
	// the manager knows to restart.
	Version string
}

var idRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,47}$`)

// Validate checks a definition.
func (d Definition) Validate() error {
	if strings.TrimSpace(d.Command) == "" {
		return fmt.Errorf("command is required")
	}
	cmd := filepath.ToSlash(d.Command)
	if filepath.IsAbs(d.Command) || strings.HasPrefix(cmd, "/") || (len(cmd) > 1 && cmd[1] == ':') || strings.Contains(cmd, "..") {
		return fmt.Errorf("command %q must be a program name or a path inside the sidecar folder", d.Command)
	}
	switch d.Policy() {
	case RestartAlways, RestartOnFailure, RestartNever:
	default:
		return fmt.Errorf("restart_policy must be always, on-failure or never, got %q", d.RestartPolicy)
	}
	return nil
}

// Policy returns the restart policy, defaulting to always.
func (d Definition) Policy() string {
	if d.RestartPolicy == "" {
		return RestartAlways
	}
	return d.RestartPolicy
}

// Discover reads every sidecar folder under root. A folder with a bad
// definition is reported in errs and skipped, never started.
func Discover(root string) (found []Sidecar, errs []error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if !os.IsNotExist(err) {
			errs = append(errs, err)
		}
		return nil, errs
	}
	for _, e := range entries {
		if !e.IsDir() || e.Type()&os.ModeSymlink != 0 {
			continue
		}
		id := e.Name()
		if !idRe.MatchString(id) {
			errs = append(errs, fmt.Errorf("sidecar folder %q: the name must be lowercase letters, digits and dashes", id))
			continue
		}
		dir := filepath.Join(root, id)
		raw, err := os.ReadFile(filepath.Join(dir, "sidecar.json"))
		if err != nil {
			errs = append(errs, fmt.Errorf("sidecar %s: %w", id, err))
			continue
		}
		var def Definition
		if err := json.Unmarshal(raw, &def); err != nil {
			errs = append(errs, fmt.Errorf("sidecar %s: sidecar.json: %w", id, err))
			continue
		}
		if err := def.Validate(); err != nil {
			errs = append(errs, fmt.Errorf("sidecar %s: %w", id, err))
			continue
		}
		var cfg userConfig
		cfgRaw, _ := os.ReadFile(filepath.Join(dir, "user_config.json"))
		_ = json.Unmarshal(cfgRaw, &cfg)
		found = append(found, Sidecar{ID: id, Dir: dir, Def: def, Enabled: cfg.Enabled, Version: fmt.Sprintf("%x|%v", raw, cfg.Enabled)})
	}
	sort.Slice(found, func(i, j int) bool { return found[i].ID < found[j].ID })
	return found, errs
}

// SetEnabled records the user's choice. It is the only writer of
// user_config.json and is reached only from the command line.
func SetEnabled(root, id string, enabled bool) error {
	if !idRe.MatchString(id) {
		return fmt.Errorf("invalid sidecar id %q", id)
	}
	dir := filepath.Join(root, id)
	if _, err := os.Stat(filepath.Join(dir, "sidecar.json")); err != nil {
		return fmt.Errorf("no sidecar %q", id)
	}
	data, err := json.MarshalIndent(userConfig{Enabled: enabled}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "user_config.json"), append(data, '\n'), 0o644)
}

// RuntimeDir is where a sidecar's data, logs and agentapi events live.
func RuntimeDir(project, id string) string {
	return filepath.Join(project, ".atlas", "sidecar_data", id)
}
