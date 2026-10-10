package remotecontrol

import (
	"crypto/tls"
	"crypto/x509"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/mobilerelay"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

// relayFixture is the real relay server behind real TLS. The verifier
// trusts only the fixture's certificate; verification is never disabled.
func relayFixture(t *testing.T) (host, token string, v Verifier, relay *mobilerelay.Server) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "connector.key")
	require.NoError(t, mobilerelay.GenerateKey(path))
	token, err := mobilerelay.ReadToken(path)
	require.NoError(t, err)
	srv := httptest.NewUnstartedServer(nil)
	host = srv.Listener.Addr().String()
	relay, err = mobilerelay.NewServer("https://"+host, token)
	require.NoError(t, err)
	srv.Config.Handler = relay
	srv.StartTLS()
	t.Cleanup(func() { _ = relay.Close(); srv.Close() })
	roots := x509.NewCertPool()
	roots.AddCert(srv.Certificate())
	tlsCfg := &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	v = Verifier{
		HTTP: &http.Client{Transport: &http.Transport{TLSClientConfig: tlsCfg}},
		WS:   &websocket.Dialer{TLSClientConfig: tlsCfg},
	}
	return host, token, v, relay
}

func TestVerifierSeparatesHTTPSFromTheRelayLink(t *testing.T) {
	t.Parallel()
	host, token, v, relay := relayFixture(t)
	https := v.HTTPS(t.Context(), host)
	require.Equal(t, CheckOK, https.State, https.Detail.String())
	require.Contains(t, https.Detail.String(), "not attached")

	link := v.Link(t.Context(), host, token, false)
	require.Equal(t, CheckOK, link.State, link.Detail.String())
	require.Eventually(t, func() bool { return !relay.Connected() }, 5*time.Second, 10*time.Millisecond, "the probe connector detaches afterwards")

	wrong := v.Link(t.Context(), host, "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", false)
	require.Equal(t, CheckFailed, wrong.State, "a relay holding a different key refuses this computer")

	untrusted := Verifier{HTTP: &http.Client{}}.HTTPS(t.Context(), host)
	require.Equal(t, CheckFailed, untrusted.State)
	require.Contains(t, untrusted.Detail.String(), "certificate")
}

func TestVerifierRejectsOtherHTTPSServices(t *testing.T) {
	t.Parallel()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "<html>parked domain</html>")
	}))
	t.Cleanup(srv.Close)
	v := Verifier{HTTP: srv.Client()}
	check := v.HTTPS(t.Context(), srv.Listener.Addr().String())
	require.Equal(t, CheckFailed, check.State, "a valid certificate on someone else's site is not the relay")

	check = v.Link(t.Context(), srv.Listener.Addr().String(), "", true)
	require.Equal(t, CheckFailed, check.State)
}
