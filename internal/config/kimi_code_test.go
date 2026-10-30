package config

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/oauth"
	"github.com/stretchr/testify/require"
)

func TestSetupKimiCodeAddsIdentificationHeaders(t *testing.T) {
	t.Setenv("ATLAS_AGENT_GLOBAL_DATA", t.TempDir())
	pc := ProviderConfig{ID: "kimi-code", OAuthToken: &oauth.Token{AccessToken: "at"}}
	pc.SetupKimiCode()
	require.Equal(t, "kimi_cli", pc.ExtraHeaders["X-Msh-Platform"])
	require.Equal(t, "atlas-agent", pc.ExtraHeaders["X-Msh-Version"])
	require.NotEmpty(t, pc.ExtraHeaders["X-Msh-Device-Id"])
	require.NotEmpty(t, pc.ExtraHeaders["X-Msh-Device-Name"])

	none := ProviderConfig{ID: "kimi-code"}
	none.SetupKimiCode()
	require.Empty(t, none.ExtraHeaders, "no token means no headers")
}

func TestExchangeRoutesKimiCodeToKimiRefresh(t *testing.T) {
	t.Setenv("ATLAS_AGENT_GLOBAL_DATA", t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/oauth/token", r.URL.Path)
		require.NoError(t, r.ParseForm())
		require.Equal(t, "refresh_token", r.Form.Get("grant_type"))
		require.Equal(t, "old-refresh", r.Form.Get("refresh_token"))
		_, _ = w.Write([]byte(`{"access_token":"fresh","refresh_token":"new-refresh","expires_in":3600}`))
	}))
	defer srv.Close()
	t.Setenv("KIMI_CODE_OAUTH_HOST", srv.URL)

	store := &ConfigStore{}
	tok, err := store.exchange(t.Context(), "kimi-code", "old-refresh")
	require.NoError(t, err)
	require.Equal(t, "fresh", tok.AccessToken)
	require.Equal(t, "new-refresh", tok.RefreshToken)
}
