package connections

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

func TestSocialAccountOAuth(t *testing.T) {
	for _, id := range []string{"slack", "github", "notion"} {
		t.Run(id, func(t *testing.T) {
			const accessToken = "social-access-token-must-be-encrypted"
			providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.ParseForm() != nil || r.Form.Get("code") != "account-code" {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"access_token":"` + accessToken + `","token_type":"Bearer"}`))
			}))
			defer providerServer.Close()
			svc := New(config.NewTestStore(&config.Config{}), t.TempDir())
			svc.SocialOAuthEndpoints = map[string]oauth2.Endpoint{id: {AuthURL: "https://provider.example.test/auth", TokenURL: providerServer.URL}}
			require.NoError(t, svc.Save(id, SaveRequest{ClientID: "client-id", ClientSecret: "client-secret"}))
			start, err := svc.StartAccountOAuth(context.Background(), id)
			require.NoError(t, err)
			auth, err := url.Parse(start.AuthURL)
			require.NoError(t, err)
			require.Equal(t, "S256", auth.Query().Get("code_challenge_method"))
			callback, err := url.Parse(auth.Query().Get("redirect_uri"))
			require.NoError(t, err)
			query := callback.Query()
			query.Set("state", auth.Query().Get("state"))
			query.Set("code", "account-code")
			callback.RawQuery = query.Encode()
			resp, err := httpGet(t, nil, callback.String())
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, resp.StatusCode)
			require.NoError(t, resp.Body.Close())
			stored, err := svc.secrets().Get(socialOAuth[id].token)
			require.NoError(t, err)
			require.Equal(t, accessToken, stored)
			for _, view := range svc.List() {
				if view.ID == id {
					require.True(t, view.Configured)
				}
			}
		})
	}
}
