package connections

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSecretStoreRoundTripAndEmptyEnvFallback(t *testing.T) {
	dataDir := t.TempDir()
	dir := filepath.Join(dataDir, "connections", "secrets")
	store := SecretStore{Dir: dir}
	require.NoError(t, store.Set(ElevenLabsAPIKey, "first-secret"))
	value, err := store.Get(ElevenLabsAPIKey)
	require.NoError(t, err)
	require.Equal(t, "first-secret", value)

	data, err := os.ReadFile(filepath.Join(dir, "elevenlabs-api-key.secret"))
	require.NoError(t, err)
	require.NotContains(t, string(data), "first-secret")

	t.Setenv(ElevenLabsAPIKey, "environment-secret")
	require.Equal(t, "first-secret", Env(dataDir)(ElevenLabsAPIKey))
	require.NoError(t, store.Set(ElevenLabsAPIKey, "replacement-secret"))
	value, err = store.Get(ElevenLabsAPIKey)
	require.NoError(t, err)
	require.Equal(t, "replacement-secret", value)
}

func TestSecretStoreRejectsUnknownAndEmptyValues(t *testing.T) {
	t.Parallel()
	store := SecretStore{Dir: t.TempDir()}
	require.Error(t, store.Set("unknown", "value"))
	require.Error(t, store.Set(ElevenLabsAPIKey, " "))
	_, err := store.Get("unknown")
	require.Error(t, err)
}

func TestEnvFallsBackForLegacyEnvironmentConfiguration(t *testing.T) {
	t.Setenv(ElevenLabsAPIKey, "legacy-secret")
	require.Equal(t, "legacy-secret", Env(t.TempDir())(ElevenLabsAPIKey))
}
