package muse

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func writeAuthFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "auth.json")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestSessionFromCLIWithAPIKey(t *testing.T) {
	expires := float64(time.Now().Add(time.Hour).Unix())
	path := writeAuthFile(t, `{"providers":{"meta":{
		"mechanism":"oauth",
		"access_token":"oidc-access",
		"api_key":"LLM|cli|key",
		"expires_at":`+strconv.FormatFloat(expires, 'f', 0, 64)+`}}}`)
	t.Setenv("MUSE_AUTH_PATH", path)

	session := SessionFromCLI()

	require.NotNil(t, session)
	require.Equal(t, "LLM|cli|key", session.APIKey)
	require.True(t, session.HasAPIKey())
	require.Equal(t, int64(expires), session.ExpiresAt)
}

func TestSessionFromCLIAccessTokenOnly(t *testing.T) {
	path := writeAuthFile(t, `{"providers":{"meta":{
		"mechanism":"oauth",
		"access_token":"oidc-access"}}}`)
	t.Setenv("MUSE_AUTH_PATH", path)

	session := SessionFromCLI()

	require.NotNil(t, session)
	require.False(t, session.HasAPIKey())
	require.Equal(t, "oidc-access", session.AccessToken)
}

func TestSessionFromCLINotSignedIn(t *testing.T) {
	t.Setenv("MUSE_AUTH_PATH", filepath.Join(t.TempDir(), "missing.json"))

	require.Nil(t, SessionFromCLI(), "a missing file means the CLI is not signed in, not an error")
}

func TestSessionFromCLIIgnoresNonOAuth(t *testing.T) {
	path := writeAuthFile(t, `{"providers":{"meta":{
		"mechanism":"api_key",
		"api_key":"LLM|other|key"}}}`)
	t.Setenv("MUSE_AUTH_PATH", path)

	require.Nil(t, SessionFromCLI())
}

func TestSessionFromCLIIgnoresMalformed(t *testing.T) {
	path := writeAuthFile(t, `not json`)
	t.Setenv("MUSE_AUTH_PATH", path)

	require.Nil(t, SessionFromCLI())
}

func TestTokenFromSessionUsesStoredKey(t *testing.T) {
	session := &CLISession{APIKey: "LLM|cli|key", ExpiresAt: time.Now().Add(time.Hour).Unix()}

	token, err := TokenFromSession(context.Background(), session)

	require.NoError(t, err)
	require.Equal(t, "LLM|cli|key", token.AccessToken)
	require.Empty(t, token.RefreshToken, "imported sessions have no refresh token")
	require.False(t, token.IsExpired())
}

func TestTokenFromSessionMintsFromAccessToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, authValue("oidc-access"), r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"api_key":"LLM|minted|from-cli"}`))
	}))
	defer srv.Close()
	pointHostsAt(t, srv)

	token, err := TokenFromSession(context.Background(), &CLISession{AccessToken: "oidc-access"})

	require.NoError(t, err)
	require.Equal(t, "LLM|minted|from-cli", token.AccessToken)
}

func TestCLIAuthPathPrecedence(t *testing.T) {
	t.Setenv("MUSE_AUTH_PATH", "/custom/auth.json")
	t.Setenv("XDG_CONFIG_HOME", "/xdg")

	require.Equal(t, "/custom/auth.json", CLIAuthPath())
}

func TestCLIAuthPathXDG(t *testing.T) {
	t.Setenv("MUSE_AUTH_PATH", "")
	t.Setenv("XDG_CONFIG_HOME", "/xdg")

	require.Equal(t, filepath.Join("/xdg", "muse", "auth.json"), CLIAuthPath())
}

func TestCLIAuthPathHomeFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MUSE_AUTH_PATH", "")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	require.Equal(t, filepath.Join(home, ".config", "muse", "auth.json"), CLIAuthPath())
}
