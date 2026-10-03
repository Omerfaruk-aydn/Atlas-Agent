package vault

import (
	"encoding/json"
	"runtime"
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agentstate"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/db"
	"github.com/stretchr/testify/require"
)

func TestVaultProtectsPasswordAndBindsExactOrigin(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Setenv("ATLAS_VAULT_KEY", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	}
	dir := t.TempDir()
	conn, err := db.Connect(t.Context(), dir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Release(dir)) })
	state := &agentstate.Store{DB: conn}
	v := &Store{State: state, Namespace: "vault-fixture"}
	require.NoError(t, v.Add(t.Context(), "login", "https://example.test", "user", "vault-fixture-password"))
	rows, err := state.List(t.Context(), "vault-fixture")
	require.NoError(t, err)
	require.False(t, strings.Contains(string(rows[0].Payload), "vault-fixture-password"))
	items, err := v.List(t.Context())
	require.NoError(t, err)
	encoded, err := json.Marshal(items)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "sealed")
	calls := 0
	require.ErrorContains(t, v.Fill(t.Context(), "login", "https://evil.test", "fixture", func(string, string) error { calls++; return nil }), "origin")
	require.Zero(t, calls)
	require.NoError(t, v.Fill(t.Context(), "login", "https://example.test", "fixture", func(origin, password string) error {
		calls++
		require.Equal(t, "vault-fixture-password", password)
		require.Equal(t, "https://example.test", origin)
		return nil
	}))
	require.Equal(t, 1, calls)
	require.Equal(t, "[REDACTED]", Redact("another-session", "vault-fixture-password"))
	require.True(t, Sensitive(""))
	require.NoError(t, v.Remove(t.Context(), "login"))
	require.Error(t, v.Fill(t.Context(), "login", "https://example.test", "fixture", func(string, string) error { return nil }))
}

func TestVaultRejectsBindingTamper(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Setenv("ATLAS_VAULT_KEY", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	}
	sealed, err := seal([]byte("secret"))
	require.NoError(t, err)
	sealed[len(sealed)-1] ^= 1
	_, err = unseal(sealed)
	require.Error(t, err)
	for _, origin := range []string{"http://example.test", "https://u:p@example.test", "https://example.test/path", "https://example.test?query"} {
		_, err := Origin(origin)
		require.Error(t, err)
	}
	normalized, err := Origin("https://EXAMPLE.test:443/")
	require.NoError(t, err)
	require.Equal(t, "https://example.test", normalized)
}
