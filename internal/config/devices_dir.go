package config

import (
	"os"
	"path/filepath"
)

// GlobalDevicesDir is where device automation state lives: paired nodes,
// node identity, isolation leases, screen marks and phone pairings. It is
// per user, not per project, because devices outlive a single workspace.
// ATLAS_DEVICES_DIR overrides it (tests, portable installs).
func GlobalDevicesDir() string {
	if dir := os.Getenv("ATLAS_DEVICES_DIR"); dir != "" {
		return dir
	}
	return filepath.Join(filepath.Dir(GlobalConfigData()), "devices")
}

// GlobalMarksDir holds screen marks (crop images and their records).
func GlobalMarksDir() string { return filepath.Join(GlobalDevicesDir(), "marks") }
