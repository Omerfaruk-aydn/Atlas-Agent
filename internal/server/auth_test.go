package server

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTokenAuthRejectsMissingAndIncorrectCredentials(t *testing.T) {
	t.Setenv("ATLAS_SERVER_TOKEN", "fixture-token")
	handler := TokenAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	for _, authorization := range []string{"", "Bearer wrong", "Basic fixture-token", "Bearer fixture-token"} {
		request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/config", nil)
		request.Header.Set("Authorization", authorization)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if authorization == "Bearer fixture-token" {
			require.Equal(t, http.StatusNoContent, response.Code)
		} else {
			require.Equal(t, http.StatusUnauthorized, response.Code)
		}
	}
}

func TestServerAutomaticallyAuthenticatesWildcard(t *testing.T) {
	t.Setenv("ATLAS_SERVER_TOKEN", "")
	t.Setenv("ATLAS_AGENT_GLOBAL_DATA", t.TempDir())
	ln, err := net.Listen("tcp", "0.0.0.0:0") //nolint:noctx
	require.NoError(t, err)
	s := NewServer(nil, "tcp", ln.Addr().String())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ln) }()
	t.Cleanup(func() { _ = s.Close(); <-done })
	_, port, err := net.SplitHostPort(ln.Addr().String())
	require.NoError(t, err)
	address := net.JoinHostPort("127.0.0.1", port)
	token, err := LocalToken("tcp", address)
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+address+"/v1/health", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestServerAutomaticallyAuthenticatesLoopback(t *testing.T) {
	t.Setenv("ATLAS_SERVER_TOKEN", "")
	t.Setenv("ATLAS_AGENT_GLOBAL_DATA", t.TempDir())
	ln, err := net.Listen("tcp", "127.0.0.1:0") //nolint:noctx
	require.NoError(t, err)
	s := NewServer(nil, "tcp", ln.Addr().String())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ln) }()
	t.Cleanup(func() { _ = s.Close(); <-done })
	token, err := LocalToken("tcp", ln.Addr().String())
	require.NoError(t, err)
	require.Len(t, token, 64)
	url := "http://" + ln.Addr().String() + "/v1/health"
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	req, err = http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestServerAcceptsTCPWithToken(t *testing.T) {
	t.Setenv("ATLAS_SERVER_TOKEN", "test-token")
	ln, err := net.Listen("tcp", "127.0.0.1:0") //nolint:noctx
	require.NoError(t, err)
	s := NewServer(nil, "tcp", ln.Addr().String())
	// The listener decision must match the token captured by the handler.
	t.Setenv("ATLAS_SERVER_TOKEN", "")
	done := make(chan error, 1)
	go func() { done <- s.Serve(ln) }()
	t.Cleanup(func() {
		_ = s.Close()
		<-done
	})
	url := "http://" + ln.Addr().String() + "/v1/health"
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	req, err = http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer test-token")
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusOK, resp.StatusCode)
}
