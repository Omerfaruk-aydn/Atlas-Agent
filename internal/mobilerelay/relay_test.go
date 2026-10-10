package mobilerelay

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

type fixture struct {
	relay  *Server
	server *httptest.Server
	client *Connector
	token  string
	dialer *websocket.Dialer
}

func newFixture(t *testing.T, handler http.Handler) *fixture {
	t.Helper()
	path := filepath.Join(t.TempDir(), "connector.key")
	require.NoError(t, GenerateKey(path))
	token, err := ReadToken(path)
	require.NoError(t, err)
	srv := httptest.NewUnstartedServer(nil)
	relay, err := NewServer("https://"+srv.Listener.Addr().String(), token)
	require.NoError(t, err)
	srv.Config.Handler = relay
	srv.StartTLS()
	srv.Client().Timeout = 10 * time.Second
	root := x509.NewCertPool()
	root.AddCert(srv.Certificate())
	dialer := &websocket.Dialer{TLSClientConfig: &tls.Config{RootCAs: root, MinVersion: tls.VersionTLS12}, HandshakeTimeout: 3 * time.Second}
	client, err := Connect(t.Context(), ConnectorOptions{URL: srv.URL, Token: token, Handler: handler, Dialer: dialer})
	require.NoError(t, err)
	f := &fixture{relay: relay, server: srv, client: client, token: token, dialer: dialer}
	t.Cleanup(func() { _ = client.Close(); _ = relay.Close(); srv.Close() })
	require.Eventually(t, relay.Connected, time.Second, 10*time.Millisecond)
	return f
}

type fetched struct {
	StatusCode int
	data       []byte
	cookies    []*http.Cookie
}

func fetch(t *testing.T, f *fixture, method, path, body string) fetched {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, f.server.URL+path, strings.NewReader(body))
	require.NoError(t, err)
	resp, err := f.server.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return fetched{StatusCode: resp.StatusCode, data: data, cookies: resp.Cookies()}
}

func TestRelayTransfersHTTPAndSSEWithoutBuffering(t *testing.T) {
	t.Parallel()
	var seen atomic.Int32
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NotContains(t, r.Host, "atlas-mobile.internal")
		if r.URL.Path == "/api/events" {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: ready\n\n")
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		seen.Add(1)
		w.Header().Add("Set-Cookie", "one=1; Secure; HttpOnly")
		w.Header().Add("Set-Cookie", "two=2; Secure; HttpOnly")
		data, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(data)
	}))
	resp := fetch(t, f, http.MethodPost, "/api/message", strings.Repeat("payload ", 24000))
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	require.Len(t, resp.cookies, 2)
	require.Equal(t, strings.Repeat("payload ", 24000), string(resp.data))
	require.EqualValues(t, 1, seen.Load())
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.server.URL+"/api/events", nil)
	require.NoError(t, err)
	streamResponse, err := f.server.Client().Do(req)
	require.NoError(t, err)
	defer streamResponse.Body.Close()
	line, err := bufio.NewReader(streamResponse.Body).ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "data: ready\n", line)
	// An open stream must not block concurrent task/permission requests.
	other := fetch(t, f, http.MethodPost, "/api/message", "parallel")
	require.Equal(t, http.StatusCreated, other.StatusCode)
	require.EqualValues(t, 2, seen.Load())
}

func TestRelayRejectsWrongHostConnectorAndOversizedInput(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(http.StatusOK) }))
	for _, path := range []string{"/_atlas/connect", "/_atlas/stream?id=made-up"} {
		resp := fetch(t, f, http.MethodGet, path, "")
		require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	}
	wsURL := "wss" + strings.TrimPrefix(f.server.URL, "https")
	for _, tc := range []struct {
		path, token, origin string
		status              int
	}{
		{connectPath, "wrong", "", http.StatusUnauthorized},
		{connectPath, f.token, "https://evil.example", http.StatusUnauthorized},
		{connectPath, f.token, "", http.StatusConflict},
		{streamPath + "?id=unknown", f.token, "", http.StatusNotFound},
	} {
		conn, resp, err := f.dialer.DialContext(t.Context(), wsURL+tc.path, http.Header{"Authorization": {"Bearer " + tc.token}, "Origin": {tc.origin}})
		require.Error(t, err)
		require.Nil(t, conn)
		require.NotNil(t, resp)
		require.Equal(t, tc.status, resp.StatusCode)
		_ = resp.Body.Close()
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, f.server.URL+"/api/me", nil)
	require.NoError(t, err)
	req.Host = "evil.example"
	resp, err := f.server.Client().Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusMisdirectedRequest, resp.StatusCode)
	_ = resp.Body.Close()
	tooLarge := fetch(t, f, http.MethodPost, "/api/message", strings.Repeat("x", maxBody+1))
	require.Equal(t, http.StatusRequestEntityTooLarge, tooLarge.StatusCode)
	unknown := fetch(t, f, http.MethodGet, "/private/secret", "")
	require.Equal(t, http.StatusNotFound, unknown.StatusCode)
	require.Zero(t, calls.Load())
}

func TestRelayReconnectNeverReplaysUncertainMutation(t *testing.T) {
	t.Parallel()
	entered := make(chan struct{})
	var calls atomic.Int32
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			calls.Add(1)
			close(entered)
			<-r.Context().Done()
			return
		}
		_, _ = io.WriteString(w, "fresh snapshot")
	}))
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.server.URL+"/api/mutate", strings.NewReader("execute once"))
	require.NoError(t, err)
	req.Header.Set("Idempotency-Key", "must-not-trigger-a-transport-retry")
	result := make(chan int, 1)
	go func() {
		resp, err := f.server.Client().Do(req)
		if err != nil {
			result <- 0
			return
		}
		_ = resp.Body.Close()
		result <- resp.StatusCode
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("mutation never arrived")
	}
	f.relay.mu.Lock()
	old := f.relay.peer
	old.close()
	f.relay.mu.Unlock()
	select {
	case status := <-result:
		require.Equal(t, http.StatusBadGateway, status)
	case <-ctx.Done():
		t.Fatal("disconnected mutation did not terminate")
	}
	require.Eventually(t, func() bool {
		f.relay.mu.Lock()
		defer f.relay.mu.Unlock()
		return f.relay.peer != nil && f.relay.peer != old
	}, 5*time.Second, 20*time.Millisecond)
	resp := fetch(t, f, http.MethodGet, "/api/snapshot", "")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.EqualValues(t, 1, calls.Load())
	require.NoError(t, f.client.Close())
	require.Eventually(t, func() bool { return !f.relay.Connected() }, time.Second, 10*time.Millisecond)
	resp = fetch(t, f, http.MethodGet, "/api/snapshot", "")
	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
}

func TestRelayRejectsSuccessFromDisconnectedRequestPeer(t *testing.T) {
	t.Parallel()
	peer := &control{done: make(chan struct{})}
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://relay.example/api/mutate", nil)
	req = req.WithContext(context.WithValue(req.Context(), requestPeerKey{}, peer))
	response := &http.Response{Request: req, StatusCode: http.StatusOK}
	require.NoError(t, relayResponseConnected(response))
	close(peer.done)
	require.Error(t, relayResponseConnected(response), "a canceled handler's implicit HTTP 200 must not be delivered")
	// A replacement connector cannot authorize the old request's response.
	replacement := &control{done: make(chan struct{})}
	server := &Server{peer: replacement}
	_, err := server.dialStream(req.Context(), "", "")
	require.Error(t, err)
	require.Nil(t, server.pending, "old request must not invite or retry through the new connector")
}

func TestRelayTLSAndCredentialConfiguration(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "connector.key")
	require.NoError(t, GenerateKey(path))
	token, err := ReadToken(path)
	require.NoError(t, err)
	require.Error(t, GenerateKey(path), "never overwrite an existing key")
	unchanged, err := ReadToken(path)
	require.NoError(t, err)
	require.Equal(t, token, unchanged)
	for _, raw := range []string{"http://atlas.example.com", "https://user:secret@atlas.example.com", "https://atlas.example.com/path", "https://atlas.example.com/?key=secret", "https://atlas.example.com/#token"} {
		_, err := Origin(raw)
		require.Error(t, err)
	}
	f := newFixture(t, http.NotFoundHandler())
	_, err = Connect(t.Context(), ConnectorOptions{URL: f.server.URL, Token: token, Handler: http.NotFoundHandler()})
	require.Error(t, err, "untrusted TLS must never silently fall back")
	require.NotContains(t, err.Error(), token)
	_, err = NewServer("https://atlas.example.com", "short")
	require.Error(t, err)
	u, err := Origin("https://ATLAS.example.com/")
	require.NoError(t, err)
	require.Equal(t, "https://atlas.example.com", u.String())
}
