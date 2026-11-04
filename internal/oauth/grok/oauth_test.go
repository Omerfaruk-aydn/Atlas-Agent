package grok

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// server stands in for auth.x.ai. token answers each poll in turn.
func server(t *testing.T, token func(call int32, w http.ResponseWriter, r *http.Request)) {
	t.Helper()
	var calls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("POST /device/code", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		require.Equal(t, clientID, r.Form.Get("client_id"))
		require.Equal(t, scopes, r.Form.Get("scope"))
		require.Equal(t, referrer, r.Form.Get("referrer"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"device_code":               "device",
			"user_code":                 "ABCD-1234",
			"verification_uri":          "https://accounts.x.ai/oauth2/device",
			"verification_uri_complete": "https://accounts.x.ai/oauth2/device?user_code=ABCD-1234",
			"expires_in":                1800,
			"interval":                  1,
		})
	})
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		token(calls.Add(1), w, r)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	previous := issuer
	issuer = srv.URL
	t.Cleanup(func() { issuer = previous })
}

func refuse(w http.ResponseWriter, code string) {
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}

// Each test swaps the package's issuer, so none of them run in parallel.
func TestDeviceFlowWaitsForApproval(t *testing.T) {
	server(t, func(call int32, w http.ResponseWriter, r *http.Request) {
		require.Equal(t, deviceGrant, r.Form.Get("grant_type"))
		require.Equal(t, "device", r.Form.Get("device_code"))
		if call == 1 {
			refuse(w, "authorization_pending")
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "access", "refresh_token": "refresh", "expires_in": 3600})
	})

	dc, err := RequestDeviceCode(t.Context())
	require.NoError(t, err)
	require.Equal(t, "ABCD-1234", dc.UserCode)
	require.Contains(t, dc.VerificationURL(), "user_code=ABCD-1234")

	token, err := PollForToken(t.Context(), dc)
	require.NoError(t, err)
	require.Equal(t, "access", token.AccessToken)
	require.Equal(t, "refresh", token.RefreshToken)
	require.NotZero(t, token.ExpiresAt)
}

func TestDeviceFlowStopsWhenRefused(t *testing.T) {
	for code, want := range map[string]string{
		"access_denied": "refused",
		"expired_token": "expired",
		"server_error":  "server_error",
	} {
		server(t, func(_ int32, w http.ResponseWriter, _ *http.Request) { refuse(w, code) })
		dc, err := RequestDeviceCode(t.Context())
		require.NoError(t, err)
		_, err = PollForToken(t.Context(), dc)
		require.ErrorContains(t, err, want)
	}
}

func TestRefreshKeepsATokenThatIsNotRotated(t *testing.T) {
	server(t, func(_ int32, w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "refresh_token", r.Form.Get("grant_type"))
		require.Equal(t, "old", r.Form.Get("refresh_token"))
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "new", "expires_in": 3600})
	})
	token, err := RefreshToken(t.Context(), "old")
	require.NoError(t, err)
	require.Equal(t, "new", token.AccessToken)
	require.Equal(t, "old", token.RefreshToken)
}

func TestRefreshReportsTheServerError(t *testing.T) {
	server(t, func(_ int32, w http.ResponseWriter, _ *http.Request) { refuse(w, "invalid_grant") })
	_, err := RefreshToken(t.Context(), "old")
	require.ErrorContains(t, err, "invalid_grant")
}
