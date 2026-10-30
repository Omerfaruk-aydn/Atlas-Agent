package kimi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/oauth"
	"github.com/stretchr/testify/require"
)

// Tests in this package mutate package-level knobs and environment
// variables, so none of them run in parallel.

func setup(t *testing.T, h http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	t.Setenv("KIMI_CODE_OAUTH_HOST", srv.URL)
	dir := t.TempDir()
	oldDir, oldPoll, oldBackoff := DataDir, minPollIntervalSecs, retryBackoff
	DataDir = func() string { return dir }
	minPollIntervalSecs = 0
	retryBackoff = time.Millisecond
	t.Cleanup(func() { DataDir, minPollIntervalSecs, retryBackoff = oldDir, oldPoll, oldBackoff })
}

func TestAuthHostOverrides(t *testing.T) {
	t.Setenv("KIMI_CODE_OAUTH_HOST", "")
	t.Setenv("KIMI_OAUTH_HOST", "")
	require.Equal(t, "https://auth.kimi.com", AuthHost())
	t.Setenv("KIMI_OAUTH_HOST", "http://b/")
	require.Equal(t, "http://b", AuthHost())
	t.Setenv("KIMI_CODE_OAUTH_HOST", "http://a")
	require.Equal(t, "http://a", AuthHost())
}

func TestRequestDeviceCode(t *testing.T) {
	setup(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/oauth/device_authorization", r.URL.Path)
		require.NoError(t, r.ParseForm())
		require.Equal(t, clientID, r.Form.Get("client_id"))
		require.Empty(t, r.Form.Get("scope"))
		require.Equal(t, "kimi_cli", r.Header.Get("X-Msh-Platform"))
		require.NotEmpty(t, r.Header.Get("X-Msh-Device-Id"))
		_, _ = w.Write([]byte(`{"device_code":"dc","user_code":"AB-CD","verification_uri":"https://v","verification_uri_complete":"https://v?c=AB-CD","expires_in":600,"interval":5}`))
	})
	dc, err := RequestDeviceCode(context.Background())
	require.NoError(t, err)
	require.Equal(t, "dc", dc.DeviceCode)
	require.Equal(t, "https://v?c=AB-CD", dc.VerificationURL())
}

func TestRequestDeviceCodeErrors(t *testing.T) {
	setup(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"user_code":"x"}`))
	})
	_, err := RequestDeviceCode(context.Background())
	require.ErrorContains(t, err, "missing device_code")

	setup(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	_, err = RequestDeviceCode(context.Background())
	require.ErrorContains(t, err, "status 500")
}

func TestPollForTokenPendingThenSuccess(t *testing.T) {
	var calls atomic.Int32
	setup(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/oauth/token", r.URL.Path)
		require.NoError(t, r.ParseForm())
		require.Equal(t, deviceGrantType, r.Form.Get("grant_type"))
		require.Equal(t, "dc", r.Form.Get("device_code"))
		require.Equal(t, clientID, r.Form.Get("client_id"))
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"authorization_pending"}`))
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"at","refresh_token":"rt","expires_in":3600,"token_type":"Bearer"}`))
	})
	tok, err := PollForToken(context.Background(), &DeviceCode{DeviceCode: "dc", ExpiresIn: 60})
	require.NoError(t, err)
	require.Equal(t, "at", tok.AccessToken)
	require.Equal(t, "rt", tok.RefreshToken)
	require.NotZero(t, tok.ExpiresAt)
	require.EqualValues(t, 2, calls.Load())
}

func TestPollForTokenTerminalErrors(t *testing.T) {
	cases := map[string]func(*testing.T, error){
		`{"error":"access_denied"}`: func(t *testing.T, err error) { require.ErrorIs(t, err, ErrAccessDenied) },
		`{"error":"expired_token"}`: func(t *testing.T, err error) { require.ErrorContains(t, err, "expired") },
	}
	for payload, check := range cases {
		setup(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(payload))
		})
		_, err := PollForToken(context.Background(), &DeviceCode{DeviceCode: "dc"})
		check(t, err)
	}
}

func TestPollForTokenContextCancel(t *testing.T) {
	setup(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"authorization_pending"}`))
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := PollForToken(ctx, &DeviceCode{DeviceCode: "dc"})
	require.Error(t, err)
}

func TestRefreshToken(t *testing.T) {
	setup(t, func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		require.Equal(t, "refresh_token", r.Form.Get("grant_type"))
		require.Equal(t, "old", r.Form.Get("refresh_token"))
		_, _ = w.Write([]byte(`{"access_token":"new","expires_in":100}`))
	})
	tok, err := RefreshToken(context.Background(), "old")
	require.NoError(t, err)
	require.Equal(t, "new", tok.AccessToken)
	require.Equal(t, "old", tok.RefreshToken, "unrotated refresh token is kept")
}

func TestRefreshTokenRejected(t *testing.T) {
	for _, code := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		setup(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
		})
		_, err := RefreshToken(context.Background(), "old")
		var ex *oauth.TokenExchangeError
		require.ErrorAs(t, err, &ex)
		require.Equal(t, code, ex.StatusCode)
	}
	_, err := RefreshToken(context.Background(), "")
	require.Error(t, err)
}

func TestRefreshTokenRetriesTransient(t *testing.T) {
	var calls atomic.Int32
	setup(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"new","refresh_token":"r2","expires_in":1}`))
	})
	tok, err := RefreshToken(context.Background(), "old")
	require.NoError(t, err)
	require.Equal(t, "r2", tok.RefreshToken)
	require.EqualValues(t, 3, calls.Load())
}

func TestRefreshTokenTransientGivesUpAsPlainError(t *testing.T) {
	setup(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTooManyRequests) })
	_, err := RefreshToken(context.Background(), "old")
	require.Error(t, err)
	var ex *oauth.TokenExchangeError
	require.NotErrorAs(t, err, &ex)
}

func TestDeviceIDPersistsAndHeaders(t *testing.T) {
	dir := t.TempDir()
	old := DataDir
	DataDir = func() string { return dir }
	t.Cleanup(func() { DataDir = old })

	id := DeviceID()
	require.Len(t, id, 32)
	require.Equal(t, id, DeviceID())
	data, err := os.ReadFile(filepath.Join(dir, "kimi_device_id"))
	require.NoError(t, err)
	require.Equal(t, id, string(data))

	h := Headers()
	require.Equal(t, "kimi_cli", h["X-Msh-Platform"])
	require.Equal(t, "atlas-agent", h["X-Msh-Version"])
	require.Equal(t, id, h["X-Msh-Device-Id"])
	require.Equal(t, "ab", asciiHeader("aöb"))
	require.Equal(t, "unknown", asciiHeader("öç"))
}
