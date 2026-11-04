package dots

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// markerFile is how a running agent recognizes a dot workspace. The
// server process does not have the dots store, so the marker has to live
// in the workspace itself.
const markerFile = ".atlas/dot.json"

// IsWorkspace reports whether dir is a dot's own workspace.
func IsWorkspace(dir string) bool {
	if dir == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(markerFile)))
	return err == nil && !info.IsDir()
}

func writeMarker(d Dot) error {
	dir := filepath.Join(d.Workspace, ".atlas")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(map[string]string{"id": d.ID})
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(dir, "dot.json"), raw)
}

// WriteMarkers creates the workspace marker for every stored dot, so a
// dot created before markers existed is still recognized.
func (s *Store) WriteMarkers() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.dots {
		if d.Workspace == "" {
			continue
		}
		if err := writeMarker(d); err != nil {
			return err
		}
	}
	return nil
}
