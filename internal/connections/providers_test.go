package connections

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

func TestTokenConnections(t *testing.T) {
	for _, id := range []string{"slack", "discord", "github", "notion"} {
		t.Run(id, func(t *testing.T) {
			const token = "provider-token-must-not-leak"
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				expected := "Bearer " + token
				if id == "discord" {
					expected = "Bot " + token
				}
				if r.Header.Get("Authorization") != expected {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				if id == "notion" && r.Header.Get("Notion-Version") == "" {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"ok":true}`))
			}))
			defer provider.Close()
			svc := New(config.NewTestStore(&config.Config{}), t.TempDir())
			svc.ProviderTestURLs = map[string]string{id: provider.URL}
			require.NoError(t, svc.Save(id, SaveRequest{Token: token}))
			result, err := svc.Test(context.Background(), id)
			require.NoError(t, err)
			require.True(t, result.OK)
			views, err := json.Marshal(svc.List())
			require.NoError(t, err)
			require.NotContains(t, string(views), token)
		})
	}
}

func TestSlackRejectsAPIErrorInHTTP200(t *testing.T) {
	t.Parallel()
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":false,"error":"invalid_auth"}`))
	}))
	defer provider.Close()
	svc := New(config.NewTestStore(&config.Config{}), t.TempDir())
	svc.ProviderTestURLs = map[string]string{"slack": provider.URL}
	require.NoError(t, svc.Save("slack", SaveRequest{Token: "invalid"}))
	_, err := svc.Test(context.Background(), "slack")
	require.ErrorContains(t, err, "rejected")
}

func TestOutlookOAuthExchangeAndTest(t *testing.T) {
	const (
		clientID    = "microsoft-client-id"
		accessToken = "microsoft-access-token"
		refresh     = "microsoft-refresh-token"
	)
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil || r.Form.Get("code") != "microsoft-code" || r.Form.Get("code_verifier") == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": accessToken, "refresh_token": refresh, "token_type": "Bearer", "expires_in": 3600})
	}))
	defer tokenServer.Close()
	userServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+accessToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"id":"test-user"}`))
	}))
	defer userServer.Close()

	svc := New(config.NewTestStore(&config.Config{}), t.TempDir())
	svc.OutlookEndpoint = oauth2.Endpoint{AuthURL: "https://login.example.test/authorize", TokenURL: tokenServer.URL}
	svc.OutlookUserURL = userServer.URL
	require.NoError(t, svc.Save("outlook", SaveRequest{ClientID: clientID}))
	start, err := svc.StartOutlookOAuth(context.Background())
	require.NoError(t, err)
	auth, err := url.Parse(start.AuthURL)
	require.NoError(t, err)
	require.Equal(t, "S256", auth.Query().Get("code_challenge_method"))
	callback, err := url.Parse(auth.Query().Get("redirect_uri"))
	require.NoError(t, err)
	query := callback.Query()
	query.Set("state", auth.Query().Get("state"))
	query.Set("code", "microsoft-code")
	callback.RawQuery = query.Encode()
	resp, err := http.Get(callback.String())
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.NoError(t, resp.Body.Close())
	result, err := svc.Test(context.Background(), "outlook")
	require.NoError(t, err)
	require.True(t, result.OK)
	views, err := json.Marshal(svc.List())
	require.NoError(t, err)
	require.NotContains(t, string(views), accessToken)
	require.NotContains(t, string(views), refresh)
}
