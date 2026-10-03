package browser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfiguredProfilesAreIsolatedAndReusedPerChat(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "Default"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "Default", "Preferences"), []byte("original"), 0o600))
	var profiles []string
	m := newManager(Options{UserDataDir: root, IsolateProfiles: true}, func(opts Options) (Session, error) {
		profiles = append(profiles, opts.UserDataDir)
		return &fakeSession{}, nil
	})
	_, err := m.Session("one")
	require.NoError(t, err)
	_, err = m.Session("two")
	require.NoError(t, err)
	require.NotEqual(t, profiles[0], profiles[1])
	first := profiles[0]
	preferences := filepath.Join(first, "Default", "Preferences")
	data, err := os.ReadFile(preferences)
	require.NoError(t, err)
	require.Equal(t, "original", string(data))
	require.NoError(t, os.WriteFile(preferences, []byte("retained login state"), 0o600))
	m.Close("one")
	_, err = m.Session("one")
	require.NoError(t, err)
	require.Equal(t, first, profiles[2])
	data, err = os.ReadFile(preferences)
	require.NoError(t, err)
	require.Equal(t, "retained login state", string(data))
	m.CloseAll()
}
