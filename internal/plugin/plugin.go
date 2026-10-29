// Package plugin validates and installs portable Atlas customization bundles.
package plugin

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const ManifestFile = "atlas-plugin.json"

type Manifest struct {
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Contents    []string `json:"contents"`
	Permissions []string `json:"permissions"`
	Executable  bool     `json:"executable"`
}

var (
	validName       = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
	allowedContents = map[string]bool{"skills": true, "hooks": true, "mcp": true, "commands": true, "output-styles": true, "agents": true}
)

func Load(root string) (Manifest, error) {
	data, err := os.ReadFile(filepath.Join(root, ManifestFile))
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, err
	}
	if !validName.MatchString(manifest.Name) || strings.TrimSpace(manifest.Version) == "" {
		return Manifest{}, fmt.Errorf("plugin requires a lowercase name and version")
	}
	if len(manifest.Contents) == 0 {
		return Manifest{}, fmt.Errorf("plugin contents cannot be empty")
	}
	for _, kind := range manifest.Contents {
		if !allowedContents[kind] {
			return Manifest{}, fmt.Errorf("unsupported plugin content %q", kind)
		}
	}
	manifest.Executable = manifest.Executable || contains(manifest.Contents, "hooks") || contains(manifest.Contents, "mcp")
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("plugin symlinks are not allowed: %s", path)
		}
		return nil
	}); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// Install copies a validated package without following links or permitting
// destination traversal. Existing packages require explicit replacement.
func Install(source, pluginsRoot string, replace bool) (Manifest, error) {
	manifest, err := Load(source)
	if err != nil {
		return Manifest{}, err
	}
	destination := filepath.Join(pluginsRoot, manifest.Name)
	if _, err := os.Stat(destination); err == nil && !replace {
		return Manifest{}, fmt.Errorf("plugin %s already installed; use --replace after reviewing it", manifest.Name)
	}
	if replace {
		if err := os.RemoveAll(destination); err != nil {
			return Manifest{}, err
		}
	}
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return Manifest{}, err
	}
	if err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil || relative == "." {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o600)
	}); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}
