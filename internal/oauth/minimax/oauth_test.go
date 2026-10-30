package minimax

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
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
	t.Setenv("MINIMAX_OAUTH_HOST", srv.URL)
	oldPoll, oldBackoff := minPollIntervalSecs, retryBackoff
	minPollIntervalSecs = 0
	retryBackoff = time.Millisecond
	t.Cleanup(func() { minPollIntervalSecs, retryBackoff = oldPoll, oldBackoff })
}

func TestAuthHostByRegion(t *testing.T) {
	t.Setenv("MINIMAX_OAUTH_HOST", "")
	require.Equal(t, "https://account.minimax.io", AuthHost(Global))
	require.Equal(t, "https://account.minimaxi.com", AuthHost(China))
	t.Setenv("MINIMAX_OAUTH_HOST", "http://x/")
	require.Equal(t, "http://x", AuthHost(China))
}

func TestParseRegion(t *testing.T) {
	for in, want := range map[string]Region{"": Global, "global": Global, "CN": China, "china": China, "minimaxi": China} {
		got, err := ParseRegion(in)
		require.NoError(t, err, in)
		require.Equal(t, want, got, in)
	}
	_, err := ParseRegion("mars")
	require.Error(t, err)
}

func TestPKCEChallengeMatchesVerifier(t *testing.T) {
	v, c, err := newPKCE()
	require.NoError(t, err)
	raw, err := base64.RawURLEncoding.DecodeString(v)
	require.NoError(t, err)
	require.Len(t, raw, 32)
	sum := sha256.Sum256([]byte(v))
	require.Equal(t, base64.RawURLEncoding.EncodeToString(sum[:]), c)
	require.NotContains(t, v, "=")
	// RFC 7636 appendix B test vector.
	require.Equal(t, "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM",
		challengeFor("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"))
}

func TestDeviceFlowSendsPKCEAndVerifier(t *testing.T) {
	var challenge string
	setup(t, func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		switch r.URL.Path {
		case "/oauth2/device/code":
			require.Equal(t, clientID, r.Form.Get("client_id"))
			require.Equal(t, "openid profile coding_plan", r.Form.Get("scope"))
			require.Equal(t, "S256", r.Form.Get("code_challenge_method"))
			require.Contains(t, r.Header.Get("Content-Type"), "application/x-www-form-urlencoded")
			challenge = r.Form.Get("code_challenge")
			require.NotEmpty(t, challenge)
			_, _ = w.Write([]byte(`{"device_code":"dc","user_code":"AB-CD","verification_uri":"https://v","verification_uri_complete":"https://v?c=AB-CD","expires_in":600,"interval":1}`))
		case "/oauth2/token":
			require.Equal(t, deviceGrantType, r.Form.Get("grant_type"))
			require.Equal(t, "dc", r.Form.Get("device_code"))
			require.Equal(t, clientID, r.Form.Get("client_id"))
			require.Equal(t, challenge, challengeFor(r.Form.Get("code_verifier")), "verifier must hash to the challenge")
			_, _ = w.Write([]byte(`{"access_token":"at","refresh_token":"rt","expires_in":3600}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	dc, err := RequestDeviceCode(context.Background(), Global)
	require.NoError(t, err)
	require.Equal(t, "https://v?c=AB-CD", dc.VerificationURL())
	tok, err := PollForToken(context.Background(), Global, dc)
	require.NoError(t, err)
	require.Equal(t, "at", tok.AccessToken)
	require.Equal(t, "rt", tok.RefreshToken)
	require.NotZero(t, tok.ExpiresAt)
}

func TestRequestDeviceCodeErrors(t *testing.T) {
	setup(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotFound) })
	_, err := RequestDeviceCode(context.Background(), Global)
	require.ErrorContains(t, err, "404")
	require.ErrorContains(t, err, "atlas-agent login minimax-coding")

	setup(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusInternalServerError) })
	_, err = RequestDeviceCode(context.Background(), Global)
	require.ErrorContains(t, err, "status 500")

	setup(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"user_code":"x"}`)) })
	_, err = RequestDeviceCode(context.Background(), Global)
	require.ErrorContains(t, err, "missing device_code")
}

func TestPollPendingSlowDownThenSuccess(t *testing.T) {
	var calls atomic.Int32
	setup(t, func(w http.ResponseWriter, r *http.Request) {
		switch calls.Add(1) {
		case 1:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"authorization_pending"}`))
		case 2:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"slow_down"}`))
		default:
			_, _ = w.Write([]byte(`{"access_token":"at","expires_in":60}`))
		}
	})
	// slow_down adds 5s to the interval; keep the test fast by making the
	// context short only for the pending path.
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	tok, err := PollForToken(ctx, Global, &DeviceCode{DeviceCode: "dc", ExpiresIn: 60})
	require.NoError(t, err)
	require.Equal(t, "at", tok.AccessToken)
	require.GreaterOrEqual(t, time.Since(start), 5*time.Second, "slow_down must lengthen the interval")
}

func TestPollTerminalErrors(t *testing.T) {
	cases := map[string]func(*testing.T, error){
		`{"error":"access_denied"}`: func(t *testing.T, err error) { require.ErrorIs(t, err, ErrAccessDenied) },
		`{"error":"expired_token"}`: func(t *testing.T, err error) { require.ErrorContains(t, err, "expired") },
		`{"error":"invalid_client"}`: func(t *testing.T, err error) {
			require.ErrorContains(t, err, "invalid_client")
		},
	}
	for payload, check := range cases {
		setup(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(payload))
		})
		_, err := PollForToken(context.Background(), Global, &DeviceCode{DeviceCode: "dc"})
		check(t, err)
	}
}

func TestPollContextCancel(t *testing.T) {
	setup(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"authorization_pending"}`))
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := PollForToken(ctx, Global, &DeviceCode{DeviceCode: "dc"})
	require.Error(t, err)
}

func TestRefreshToken(t *testing.T) {
	setup(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/oauth2/token", r.URL.Path)
		require.NoError(t, r.ParseForm())
		require.Equal(t, "refresh_token", r.Form.Get("grant_type"))
		require.Equal(t, "old", r.Form.Get("refresh_token"))
		require.Equal(t, clientID, r.Form.Get("client_id"))
		_, _ = w.Write([]byte(`{"access_token":"new","expires_in":100}`))
	})
	tok, err := RefreshToken(context.Background(), China, "old")
	require.NoError(t, err)
	require.Equal(t, "new", tok.AccessToken)
	require.Equal(t, "old", tok.RefreshToken, "unrotated refresh token is kept")
}

func TestRefreshTokenRejected(t *testing.T) {
	for _, code := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden} {
		setup(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
		})
		_, err := RefreshToken(context.Background(), Global, "old")
		var ex *oauth.TokenExchangeError
		require.ErrorAs(t, err, &ex)
		require.Equal(t, code, ex.StatusCode)
		require.True(t, ex.IsRefreshTokenRevoked())
	}
	_, err := RefreshToken(context.Background(), Global, "")
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
	tok, err := RefreshToken(context.Background(), Global, "old")
	require.NoError(t, err)
	require.Equal(t, "r2", tok.RefreshToken)
	require.EqualValues(t, 3, calls.Load())
}

func TestRefreshTokenTransientGivesUpAsPlainError(t *testing.T) {
	setup(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTooManyRequests) })
	_, err := RefreshToken(context.Background(), Global, "old")
	require.Error(t, err)
	var ex *oauth.TokenExchangeError
	require.NotErrorAs(t, err, &ex)
}
