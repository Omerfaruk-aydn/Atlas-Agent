package securityaudit

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const (
	// ManifestVersion is the findings manifest schema version.
	ManifestVersion = 1
	// ManifestFile is the manifest name inside the state directory.
	ManifestFile = "findings.json"

	maxManifestBytes = 4 << 20
)

// Manifest is the file the agent writes: the findings it claims, each
// with the proof test that is supposed to back it.
type Manifest struct {
	Version  int       `json:"version"`
	Findings []Finding `json:"findings"`
}

// DefaultStateDir is where audit artifacts live under a project root.
func DefaultStateDir(root string) string {
	return filepath.Join(root, ".atlas", "security")
}

// ManifestPath returns the manifest location within stateDir.
func ManifestPath(stateDir string) string {
	return filepath.Join(stateDir, ManifestFile)
}

// LoadManifest reads the agent's findings. A missing file is not an
// error: it means the agent produced none. Every computed field is reset
// so nothing the agent wrote about status or evidence survives.
func LoadManifest(path string) ([]Finding, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxManifestBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxManifestBytes {
		return nil, fmt.Errorf("findings manifest exceeds %d bytes", maxManifestBytes)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("findings manifest is not valid JSON: %w", err)
	}
	for i := range m.Findings {
		f := &m.Findings[i]
		f.Status = StatusUnconfirmed
		f.Reason = ""
		f.Evidence = ""
		f.Source = SourceAgent
		if f.Proof != nil {
			f.Proof.SHA256 = ""
		}
	}
	return m.Findings, nil
}

// SaveJSON writes v as indented JSON, creating parent directories.
func SaveJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
