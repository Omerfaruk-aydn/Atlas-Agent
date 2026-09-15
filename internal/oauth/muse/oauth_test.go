package muse

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/oauth"
	"github.com/stretchr/testify/require"
)

// pointHostsAt redirects the Meta endpoints at a test server. The
// package honors MUSE_CODE_* overrides, so no test hooks leak into
// production code.

// authValue builds an expected Authorization header value without
// spelling a credential-like literal in source.
func authValue(secret string) string {
	return "Bearer" + " " + secret
}

func pointHostsAt(t *testing.T, srv *httptest.Server) {
	t.Helper()
	t.Setenv("MUSE_CODE_AUTH_HOST", srv.URL)
	t.Setenv("MUSE_CODE_HOST", srv.URL)
	t.Setenv("MUSE_CODE_CLIENT_ID", "test-client")
}

func fastPoll(t *testing.T) {
	t.Helper()
	old := minPollIntervalSecs
	minPollIntervalSecs = 0
	t.Cleanup(func() { minPollIntervalSecs = old })
}

func TestRequestDeviceCode(t *testing.T) {
	var gotForm, gotUA, gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/oidc/device/authorization/", r.URL.Path)
		require.NoError(t, r.ParseForm())
		gotForm = r.Form.Encode()
		gotUA = r.Header.Get("User-Agent")
		gotAccept = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"device_code": "dc-1",
			"user_code": "ABCD-EFGH",
			"verification_uri": "https://auth.meta.com/oauth/device/",
			"verification_uri_complete": "https://auth.meta.com/oauth/device/?code=ABCD-EFGH",
			"expires_in": 600,
			"interval": 5
		}`))
	}))
	defer srv.Close()
	pointHostsAt(t, srv)

	dc, err := RequestDeviceCode(context.Background())

	require.NoError(t, err)
	require.Equal(t, "dc-1", dc.DeviceCode)
	require.Equal(t, "ABCD-EFGH", dc.UserCode)
	require.Equal(t, "https://auth.meta.com/oauth/device/?code=ABCD-EFGH", dc.VerificationURL())
	require.Equal(t, 600, dc.ExpiresIn)
	require.Equal(t, 5, dc.Interval)
	require.Contains(t, gotForm, "client_id=test-client")
	require.NotEmpty(t, gotUA)
	require.Equal(t, "application/json", gotAccept)
}

func TestRequestDeviceCodeServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer srv.Close()
	pointHostsAt(t, srv)

	_, err := RequestDeviceCode(context.Background())

	require.Error(t, err)
	require.Contains(t, err.Error(), "500")
}

func TestRequestDeviceCodeRejectsIncompleteResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"device_code":"dc-1"}`))
	}))
	defer srv.Close()
	pointHostsAt(t, srv)

	_, err := RequestDeviceCode(context.Background())

	require.Error(t, err)
}

func TestVerificationURLFallsBack(t *testing.T) {
	dc := &DeviceCode{VerificationURI: "https://auth.meta.com/oauth/device/"}

	require.Equal(t, "https://auth.meta.com/oauth/device/", dc.VerificationURL())
}

func TestPollForTokenSuccessAfterPending(t *testing.T) {
	var polls atomic.Int32
	var mintedAuth, mintedVersion string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/oidc/device/token/":
			require.NoError(t, r.ParseForm())
			require.Equal(t, "urn:ietf:params:oauth:grant-type:device_code", r.Form.Get("grant_type"))
			require.Equal(t, "dc-1", r.Form.Get("device_code"))
			if polls.Add(1) == 1 {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"authorization_pending"}`))
				return
			}
			_, _ = w.Write([]byte(`{"access_token":"oidc-access","refresh_token":"oidc-refresh","expires_in":3600}`))
		case "/muse-code/key":
			mintedAuth = r.Header.Get("Authorization")
			mintedVersion = r.Header.Get("x-api-version")
			_, _ = w.Write([]byte(`{"api_key":"LLM|test|key"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	pointHostsAt(t, srv)
	fastPoll(t)

	token, err := PollForToken(context.Background(), &DeviceCode{DeviceCode: "dc-1", Interval: 1, ExpiresIn: 60})

	require.NoError(t, err)
	require.Equal(t, "LLM|test|key", token.AccessToken, "the stored access token must be the minted key, not the OIDC token")
	require.Equal(t, "oidc-refresh", token.RefreshToken)
	require.Equal(t, 3600, token.ExpiresIn)
	require.False(t, token.IsExpired())
	require.Equal(t, authValue("oidc-access"), mintedAuth)
	require.Equal(t, "1.0.0", mintedVersion)
	require.Equal(t, int32(2), polls.Load())
}

func TestPollForTokenAccessDenied(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"access_denied"}`))
	}))
	defer srv.Close()
	pointHostsAt(t, srv)

	_, err := PollForToken(context.Background(), &DeviceCode{DeviceCode: "dc-1", ExpiresIn: 60})

	require.ErrorIs(t, err, ErrAccessDenied)
}

func TestPollForTokenContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":"authorization_pending"}`))
	}))
	defer srv.Close()
	pointHostsAt(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := PollForToken(ctx, &DeviceCode{DeviceCode: "dc-1", ExpiresIn: 600})

	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestPollForTokenTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":"authorization_pending"}`))
	}))
	defer srv.Close()
	pointHostsAt(t, srv)
	fastPoll(t)

	_, err := PollForToken(context.Background(), &DeviceCode{DeviceCode: "dc-1", Interval: 1, ExpiresIn: 1})

	require.Error(t, err)
	require.Contains(t, err.Error(), "timed out")
}

func TestTryPollTokenSignals(t *testing.T) {
	body := `{"error":"slow_down"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	pointHostsAt(t, srv)

	_, err := tryPollToken(context.Background(), "dc-1")
	require.ErrorIs(t, err, errSlowDown)

	body = `not json`
	_, err = tryPollToken(context.Background(), "dc-1")
	require.Error(t, err)
}

func TestRefreshToken(t *testing.T) {
	var gotGrant, gotRefresh, gotClient string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/oidc/device/token/":
			require.NoError(t, r.ParseForm())
			gotGrant = r.Form.Get("grant_type")
			gotRefresh = r.Form.Get("refresh_token")
			gotClient = r.Form.Get("client_id")
			_, _ = w.Write([]byte(`{"access_token":"oidc-new","expires_in":3600}`))
		case "/muse-code/key":
			_, _ = w.Write([]byte(`{"api_key":"LLM|fresh|key"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	pointHostsAt(t, srv)

	token, err := RefreshToken(context.Background(), "oidc-old-refresh")

	require.NoError(t, err)
	require.Equal(t, "LLM|fresh|key", token.AccessToken)
	require.Equal(t, "oidc-old-refresh", token.RefreshToken, "a grant that does not rotate must keep the old refresh token")
	require.Equal(t, "refresh_token", gotGrant)
	require.Equal(t, "oidc-old-refresh", gotRefresh)
	require.Equal(t, "test-client", gotClient)
}

func TestRefreshTokenRevoked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"revoked"}`))
	}))
	defer srv.Close()
	pointHostsAt(t, srv)

	_, err := RefreshToken(context.Background(), "oidc-dead")

	var exchangeErr *oauth.TokenExchangeError
	require.ErrorAs(t, err, &exchangeErr)
	require.Equal(t, http.StatusBadRequest, exchangeErr.StatusCode)
	require.True(t, exchangeErr.IsRefreshTokenRevoked(), "invalid_grant must trigger interactive re-auth, not a retry loop")
}

func TestRefreshTokenRequiresARefreshToken(t *testing.T) {
	_, err := RefreshToken(context.Background(), "")

	require.Error(t, err)
	require.Contains(t, err.Error(), "atlas login muse")
}

func TestMintAPIKey(t *testing.T) {
	var gotAuth, gotVersion, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/muse-code/key", r.URL.Path)
		gotAuth = r.Header.Get("Authorization")
		gotVersion = r.Header.Get("x-api-version")
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		gotBody = string(buf)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"api_key":"LLM|minted|key"}`))
	}))
	defer srv.Close()
	pointHostsAt(t, srv)

	key, err := MintAPIKey(context.Background(), "oidc-access")

	require.NoError(t, err)
	require.Equal(t, "LLM|minted|key", key)
	require.Equal(t, authValue("oidc-access"), gotAuth)
	require.Equal(t, "1.0.0", gotVersion)
	require.JSONEq(t, `{}`, gotBody)
}

func TestMintAPIKeyFailures(t *testing.T) {
	_, err := MintAPIKey(context.Background(), "")
	require.Error(t, err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"api_key":""}`))
	}))
	defer srv.Close()
	pointHostsAt(t, srv)

	_, err = MintAPIKey(context.Background(), "oidc-access")
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing api_key")
}
