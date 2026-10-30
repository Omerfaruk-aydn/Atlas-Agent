package config

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExchangeRoutesMiniMaxPlanToMiniMaxRefresh(t *testing.T) {
	for _, id := range []string{"minimax-plan", "minimax-plan-cn"} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "/oauth2/token", r.URL.Path)
			require.NoError(t, r.ParseForm())
			require.Equal(t, "refresh_token", r.Form.Get("grant_type"))
			require.Equal(t, "old-refresh", r.Form.Get("refresh_token"))
			_, _ = w.Write([]byte(`{"access_token":"fresh","refresh_token":"new-refresh","expires_in":3600}`))
		}))
		t.Setenv("MINIMAX_OAUTH_HOST", srv.URL)

		store := &ConfigStore{}
		tok, err := store.exchange(t.Context(), id, "old-refresh")
		srv.Close()
		require.NoError(t, err, id)
		require.Equal(t, "fresh", tok.AccessToken, id)
		require.Equal(t, "new-refresh", tok.RefreshToken, id)
	}
}
