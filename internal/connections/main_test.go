package connections

import (
	"os"
	"testing"
)

// Linux and macOS seal credentials with ATLAS_VAULT_KEY. CI has no secret
// manager, so tests use a fixed key. Windows uses DPAPI and ignores it.
func TestMain(m *testing.M) {
	if os.Getenv("ATLAS_VAULT_KEY") == "" {
		_ = os.Setenv("ATLAS_VAULT_KEY", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	}
	os.Exit(m.Run())
}
