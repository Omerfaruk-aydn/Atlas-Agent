//go:build windows

package computer

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Discovery on the real computer is read-only: it reads the registry,
// Start menu entries and window titles and launches nothing.
func TestDiscoverAppsOnThisComputer(t *testing.T) {
	if testing.Short() {
		t.Skip("reads the registry and Start menu")
	}
	b := &windowsBackend{}
	entries, err := b.Apps().List(t.Context(), "", true)
	require.NoError(t, err)
	require.NotEmpty(t, entries)
	seen := map[string]bool{}
	for _, e := range entries {
		require.NotEmpty(t, e.ID)
		require.False(t, seen[e.ID], "duplicate id %s", e.ID)
		seen[e.ID] = true
	}
	t.Logf("Discovered %d applications", len(entries))
	for _, name := range []string{"Google Chrome", "Hesap Makinesi", "Calculator", "notepad"} {
		e, err := b.Apps().Resolve(t.Context(), name)
		t.Logf("%s -> %s %v", name, e.ID, err)
	}
	if chrome, err := b.Apps().Resolve(t.Context(), "chrome"); err == nil {
		t.Logf("Chrome: id=%s scope=%s sources=%v profiles=%d running=%d", chrome.ID, chrome.Scope, chrome.Sources, len(chrome.Profiles), len(chrome.Running))
	} else {
		t.Logf("Chrome not resolved: %v", err)
	}
}
