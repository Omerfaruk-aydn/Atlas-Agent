package importer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/plugin"
)

// mcpNamesJSON notes the MCP servers a JSON settings file defines. Only the
// names are read; the definitions hold tokens and stay where they are.
func (b *Bundle) mcpNamesJSON(path, key string) {
	text, ok := readFile(path)
	if !ok {
		return
	}
	var doc map[string]json.RawMessage
	if json.Unmarshal([]byte(text), &doc) != nil {
		return
	}
	var servers map[string]json.RawMessage
	if json.Unmarshal(doc[key], &servers) != nil {
		return
	}
	names := make([]string, 0, len(servers))
	for n := range servers {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		b.Notes = append(b.Notes, "MCP server "+n+" ("+filepath.Base(path)+") not imported (definitions carry credentials); add it with `atlas mcp`.")
	}
}

// PluginName is the name the imported plugin gets.
func PluginName(source string) string { return "imported-" + source }

// Write lays the bundle out as a plugin package under dir.
func (b Bundle) Write(dir string) error {
	if len(b.Items) == 0 {
		return fmt.Errorf("nothing to import from %s", b.Source)
	}
	contents := map[string]bool{}
	for _, it := range b.Items {
		var path string
		switch it.Kind {
		case KindAgent:
			contents["agents"] = true
			path = filepath.Join(dir, "agents", it.Name+".md")
		default:
			contents["skills"] = true
			path = filepath.Join(dir, "skills", it.Name, "SKILL.md")
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(it.Render()), 0o644); err != nil {
			return err
		}
	}
	var kinds []string
	for k := range contents {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	manifest := plugin.Manifest{
		Name:        PluginName(b.Source),
		Version:     "1.0.0",
		Description: fmt.Sprintf("Customizations imported from %s. Disabled by default; remove with `atlas plugin remove %s`.", b.Source, PluginName(b.Source)),
		Contents:    kinds,
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, plugin.ManifestFile), data, 0o644)
}

// Result reports an import.
type Result struct {
	Plugin   string
	Imported []string
	Skipped  []string
	Notes    []string
	Enabled  bool
}

// Import scans a source and installs the result as a disabled plugin under
// pluginsRoot. With dryRun it only reports what would be imported.
func Import(source, home, project, pluginsRoot string, dryRun bool) (Result, error) {
	b, err := Scan(source, home, project)
	if err != nil {
		return Result{}, err
	}
	res := Result{Plugin: PluginName(source), Skipped: b.Skipped, Notes: b.Notes}
	for _, it := range b.Items {
		res.Imported = append(res.Imported, string(it.Kind)+" "+it.Name+" <- "+it.Origin)
	}
	if dryRun {
		return res, nil
	}
	staging, err := os.MkdirTemp("", "atlas-import-*")
	if err != nil {
		return res, err
	}
	defer os.RemoveAll(staging)
	if err := b.Write(staging); err != nil {
		return res, err
	}
	if _, err := plugin.Install(staging, pluginsRoot, true); err != nil {
		return res, fmt.Errorf("installing %s: %w", res.Plugin, err)
	}
	if err := plugin.SetEnabled(pluginsRoot, res.Plugin, false); err != nil {
		return res, err
	}
	res.Notes = append(res.Notes, "Enable with `atlas plugin enable "+res.Plugin+"`; remove with `atlas plugin remove "+res.Plugin+"`.")
	return res, nil
}
