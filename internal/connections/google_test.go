package connections

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

func TestGoogleOAuthExchangeAndRedaction(t *testing.T) {
	const (
		clientID     = "test-client.apps.googleusercontent.com"
		clientSecret = "test-client-secret"
		accessToken  = "access-token-must-stay-encrypted"
		refreshToken = "refresh-token-must-stay-encrypted"
	)
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		require.Equal(t, "authorization_code", r.Form.Get("grant_type"))
		require.Equal(t, "test-code", r.Form.Get("code"))
		require.NotEmpty(t, r.Form.Get("code_verifier"))
		user, _, ok := r.BasicAuth()
		require.True(t, ok)
		require.Equal(t, clientID, user)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": accessToken, "refresh_token": refreshToken,
			"token_type": "Bearer", "expires_in": 3600,
		})
	}))
	defer tokenServer.Close()

	dataDir := t.TempDir()
	svc := New(config.NewTestStore(&config.Config{}), dataDir)
	svc.GoogleEndpoint = oauth2.Endpoint{AuthURL: "https://accounts.example.test/auth", TokenURL: tokenServer.URL}
	require.NoError(t, svc.Save("google", SaveRequest{ClientID: clientID, ClientSecret: clientSecret}))
	for _, view := range svc.List() {
		if view.ID == "google" {
			require.True(t, view.Available)
			require.False(t, view.Configured)
		}
	}
	start, err := svc.StartGoogleOAuth(context.Background(), "http://127.0.0.1:7788")
	require.NoError(t, err)
	auth, err := url.Parse(start.AuthURL)
	require.NoError(t, err)
	require.Equal(t, "S256", auth.Query().Get("code_challenge_method"))
	require.NotEmpty(t, auth.Query().Get("code_challenge"))
	require.Equal(t, "offline", auth.Query().Get("access_type"))
	callback, err := url.Parse(auth.Query().Get("redirect_uri"))
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1", callback.Hostname())

	badCallback := *callback
	badQuery := badCallback.Query()
	badQuery.Set("state", "wrong-state")
	badQuery.Set("code", "test-code")
	badCallback.RawQuery = badQuery.Encode()
	resp, err := httpGet(t, nil, badCallback.String())
	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.NoError(t, resp.Body.Close())

	goodQuery := callback.Query()
	goodQuery.Set("state", auth.Query().Get("state"))
	goodQuery.Set("code", "test-code")
	callback.RawQuery = goodQuery.Encode()
	client := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err = httpGet(t, client, callback.String())
	require.NoError(t, err)
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	require.Equal(t, "http://127.0.0.1:7788/", resp.Header.Get("Location"))
	require.NoError(t, resp.Body.Close())

	stored, err := svc.secrets().Get(googleToken)
	require.NoError(t, err)
	require.Contains(t, stored, accessToken)
	require.Contains(t, stored, refreshToken)
	ciphertext, err := os.ReadFile(filepath.Join(dataDir, "connections", "secrets", "google-oauth-token.secret"))
	require.NoError(t, err)
	require.NotContains(t, string(ciphertext), accessToken)
	require.NotContains(t, string(ciphertext), refreshToken)
	views, err := json.Marshal(svc.List())
	require.NoError(t, err)
	require.NotContains(t, string(views), accessToken)
	require.NotContains(t, string(views), refreshToken)
	require.NotContains(t, string(views), clientSecret)
	for _, view := range svc.List() {
		if view.ID == "google" {
			require.True(t, view.Configured)
		}
	}
}

func TestGoogleOAuthRequiresUserCredentials(t *testing.T) {
	t.Parallel()
	svc := New(config.NewTestStore(&config.Config{}), t.TempDir())
	_, err := svc.StartGoogleOAuth(context.Background(), "")
	require.ErrorContains(t, err, "client ID")
	require.Error(t, svc.Save("google", SaveRequest{}))
	require.Error(t, svc.Save("google", SaveRequest{ClientID: "bad\nclient"}))
	_, err = svc.Test(context.Background(), "google")
	require.ErrorContains(t, err, "client ID")
	require.NoError(t, svc.Save("google", SaveRequest{ClientID: "client", ClientSecret: "secret"}))
	_, err = svc.StartGoogleOAuth(context.Background(), "https://example.com")
	require.ErrorContains(t, err, "local Atlas")
}
