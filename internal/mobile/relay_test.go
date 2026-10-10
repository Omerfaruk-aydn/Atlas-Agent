package mobile

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/mobilerelay"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func TestPhoneRelayPreservesPairingAuthorizationAndTaskControls(t *testing.T) {
	t.Parallel()
	g, backend := newGateway(t)
	path := filepath.Join(t.TempDir(), "connector.key")
	require.NoError(t, mobilerelay.GenerateKey(path))
	token, err := mobilerelay.ReadToken(path)
	require.NoError(t, err)
	srv := httptest.NewUnstartedServer(nil)
	origin := "https://" + srv.Listener.Addr().String()
	relay, err := mobilerelay.NewServer(origin, token)
	require.NoError(t, err)
	srv.Config.Handler = relay
	srv.StartTLS()
	root := x509.NewCertPool()
	root.AddCert(srv.Certificate())
	conn, err := mobilerelay.Connect(t.Context(), mobilerelay.ConnectorOptions{
		URL: origin, Token: token, Handler: g.handlerForHost(srv.Listener.Addr().String()),
		Dialer: &websocket.Dialer{TLSClientConfig: &tls.Config{RootCAs: root, MinVersion: tls.VersionTLS12}},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close(); _ = relay.Close(); srv.Close() })
	require.Eventually(t, relay.Connected, time.Second, 10*time.Millisecond)
	client := srv.Client()
	client.Jar, err = cookiejar.New(nil)
	require.NoError(t, err)
	p := &phone{t: t, g: g, base: origin, client: client}
	response, _ := p.do("GET", "/api/sessions", nil, nil)
	require.Equal(t, http.StatusUnauthorized, response.StatusCode)
	require.Equal(t, "accepted", p.pair(true))
	response, payload := p.do("GET", "/api/sessions", nil, nil)
	require.Equal(t, http.StatusOK, response.StatusCode, payload)
	require.Len(t, payload["sessions"], 1)
	response, payload = p.do("POST", "/api/sessions/s1/prompt", map[string]string{"text": "hello from outside"}, map[string]string{"Idempotency-Key": "relay-once"})
	require.Equal(t, http.StatusAccepted, response.StatusCode, payload)
	response, payload = p.do("POST", "/api/sessions/s1/prompt", map[string]string{"text": "hello from outside"}, map[string]string{"Idempotency-Key": "relay-once"})
	require.Equal(t, http.StatusAccepted, response.StatusCode, payload)
	require.Eventually(t, func() bool { return backend.runs.Load() == 1 }, time.Second, 10*time.Millisecond)
	response, payload = p.do("POST", "/api/sessions/s1/control", map[string]string{"action": "pause", "revision": "r1"}, map[string]string{"Idempotency-Key": "pause", "Origin": "https://evil.example"})
	require.Equal(t, http.StatusForbidden, response.StatusCode, payload)
	require.Equal(t, "bad_origin", code(payload))
	response, payload = p.do("POST", "/api/sessions/s1/control", map[string]string{"action": "pause", "revision": "r1"}, map[string]string{"Idempotency-Key": "pause", "X-CSRF-Token": "wrong"})
	require.Equal(t, http.StatusForbidden, response.StatusCode, payload)
	response, payload = p.do("POST", "/api/sessions/s1/control", map[string]string{"action": "pause", "revision": "r1"}, map[string]string{"Idempotency-Key": "pause"})
	require.Equal(t, http.StatusOK, response.StatusCode, payload)
	backend.mu.Lock()
	require.Equal(t, []string{"pause"}, backend.controls)
	backend.mu.Unlock()
	response, _ = p.do("POST", "/api/logout", map[string]string{}, map[string]string{"Idempotency-Key": "logout"})
	require.Equal(t, http.StatusOK, response.StatusCode)
	response, _ = p.do("GET", "/api/sessions", nil, nil)
	require.Equal(t, http.StatusUnauthorized, response.StatusCode)
}

func TestPhoneRelayConfigurationRejectsPartialSetupAndLeaksNoConnectorKey(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "connector.key")
	require.NoError(t, mobilerelay.GenerateKey(path))
	for _, options := range []Options{
		{RelayURL: "https://atlas.example.com"},
		{RelayTokenFile: path},
		{RelayURL: "http://atlas.example.com", RelayTokenFile: path},
		{RelayURL: "https://atlas.example.com/path", RelayTokenFile: path},
	} {
		_, _, err := relayConfiguration(options)
		require.Error(t, err)
	}
	options := Options{RelayURL: "https://atlas.example.com/", RelayTokenFile: path}
	origin, token, err := relayConfiguration(options)
	require.NoError(t, err)
	require.Equal(t, "https://atlas.example.com", origin.String())
	auth, err := LoadAuth(t.TempDir())
	require.NoError(t, err)
	g := &Gateway{opts: options, Auth: auth}
	link, _ := g.PairingURL()
	require.True(t, strings.HasPrefix(link, "https://atlas.example.com/#pair="))
	require.NotContains(t, link, token)
	require.NotContains(t, link, "token=")
	// A relay Host is not accepted by the separate local listener.
	g.hosts = []string{"127.0.0.1:7777"}
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "https://atlas.example.com/api/me", nil)
	w := httptest.NewRecorder()
	g.handler().ServeHTTP(w, r)
	require.Equal(t, http.StatusMisdirectedRequest, w.Code)
}
