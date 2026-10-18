package support

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/tsfunnel"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/tsfunnel/fakecli"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func supportFunnelManager(t *testing.T, cli *fakecli.Daemon) *tsfunnel.Manager {
	t.Helper()
	return &tsfunnel.Manager{CLI: cli, StateFile: filepath.Join(t.TempDir(), "support-funnel.json"), Poll: 5 * time.Millisecond, ConfirmTimeout: time.Second, CleanupTimeout: time.Second}
}

func TestSupportFunnelOfferOwnershipAndCleanup(t *testing.T) {
	t.Parallel()
	cli := fakecli.New()
	cli.AddForeign("rc-owned", 443, "http://127.0.0.1:12345")
	h := startHost(t, nil)
	mgr := supportFunnelManager(t, cli)
	info, err := h.offerFunnel(t.Context(), OfferOptions{Capabilities: Presets["diagnose"]}, mgr, tsfunnel.Inspect(t.Context(), cli, nil))
	require.NoError(t, err)
	target, err := ParseTarget(info.Link)
	require.NoError(t, err)
	require.Equal(t, "https://atlas-pc.tail1234.ts.net:8443", target.Funnel)
	require.Empty(t, info.LANAddresses)
	require.Empty(t, info.RelayURL)
	require.FileExists(t, mgr.StateFile)
	h.CancelOffer()
	require.Eventually(t, func() bool {
		h.funnelMu.Lock()
		defer h.funnelMu.Unlock()
		return h.funnel == nil
	}, 3*time.Second, 20*time.Millisecond)
	require.Eventually(t, func() bool { return !containsSupportSession(cli.ServeJSON()) }, 3*time.Second, 20*time.Millisecond)
	require.Contains(t, string(cli.ServeJSON()), "rc-owned")
	require.NoFileExists(t, mgr.StateFile)
}

func containsSupportSession(data []byte) bool {
	return strings.Contains(string(data), "8443")
}

func TestSupportFunnelFailureDoesNotCreateOffer(t *testing.T) {
	t.Parallel()
	for _, mode := range []fakecli.FunnelMode{fakecli.FunnelFail, fakecli.FunnelApprovalExit} {
		cli := fakecli.New()
		cli.Funnel = mode
		h := startHost(t, nil)
		_, err := h.offerFunnel(t.Context(), OfferOptions{Capabilities: Presets["diagnose"]}, supportFunnelManager(t, cli), tsfunnel.Inspect(t.Context(), cli, nil))
		require.Error(t, err)
		require.Nil(t, h.Status().Offer)
		require.Equal(t, "null", strings.TrimSpace(string(cli.ServeJSON())))
	}
}

func TestFunnelTargetValidation(t *testing.T) {
	t.Parallel()
	code, err := NewPairingCode()
	require.NoError(t, err)
	for _, address := range []string{"https://owner.tail123.ts.net", "https://owner.tail123.ts.net:8443", "https://owner.tail123.ts.net:10000"} {
		q := url.Values{"code": {code.String()}, "funnel": {address}}
		target, err := ParseTarget("atlas-support://join?" + q.Encode())
		require.NoError(t, err)
		require.Equal(t, address, target.Funnel)
	}
	for _, address := range []string{"http://owner.tail123.ts.net", "https://example.com", "https://owner.tail123.ts.net.evil.com", "https://owner.tail123.ts.net:80", "https://user@owner.tail123.ts.net", "https://owner.tail123.ts.net/other", "https://owner.tail123.ts.net?q=x", "https://owner.tail123.ts.net#x", "https://127.0.0.1"} {
		_, err := funnelEndpoint(address)
		require.Error(t, err, address)
	}
	q := url.Values{"code": {code.String()}, "funnel": {"https://owner.tail123.ts.net"}, "addr": {"127.0.0.1:80"}}
	_, err = ParseTarget("atlas-support://join?" + q.Encode())
	require.Error(t, err)
}

func TestFunnelTunnelPreservesPairingAcceptanceAndReconnect(t *testing.T) {
	t.Parallel()
	h := startHost(t, nil)
	info, err := h.Offer(OfferOptions{Capabilities: Presets["diagnose"], Funnel: true})
	require.NoError(t, err)
	server := httptest.NewTLSServer(h.funnelHandler("owner.tail123.ts.net"))
	t.Cleanup(server.Close)
	pool := x509.NewCertPool()
	pool.AddCert(server.Certificate())
	dialer := &websocket.Dialer{
		TLSClientConfig: &tls.Config{RootCAs: pool, ServerName: "example.com", MinVersion: tls.VersionTLS12},
		NetDialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "tcp", server.Listener.Addr().String())
		},
	}
	c := startController(t, func(opts *ControllerOptions) { opts.FunnelDialer = dialer })
	q := url.Values{"code": {info.Code}, "funnel": {"https://owner.tail123.ts.net"}}
	joining := startJoin(c, "atlas-support://join?"+q.Encode())
	pending := waitPending(t, h)
	_, _, err = c.Target()
	require.Error(t, err, "no access before owner acceptance")
	require.Equal(t, "funnel", pending.Network)
	require.NoError(t, h.Decide(pending.ID, true, nil))
	result := <-joining
	require.NoError(t, result.err)
	require.Equal(t, pending.Verification, result.prompt.Verification)
	require.Equal(t, "funnel", result.remote.Network)
	_, err = c.Refresh(t.Context())
	require.NoError(t, err)
	c.ForceReconnect()
	require.Eventually(t, func() bool {
		st := c.Status()
		return st.State == StateConnected && st.Remote != nil && st.Remote.Generation > result.remote.Generation
	}, 10*time.Second, 20*time.Millisecond)
	_, err = c.Refresh(t.Context())
	require.NoError(t, err)
	h.Disconnect("owner ended session")
	require.Eventually(t, func() bool { return c.Status().State.Terminal() }, 10*time.Second, 20*time.Millisecond)
	_, _, err = c.Target()
	require.Error(t, err)
}

func TestFunnelHandlerRejectsWrongHostPathAndOrigin(t *testing.T) {
	t.Parallel()
	h := startHost(t, nil)
	for _, test := range []struct{ host, path, origin string }{
		{"evil.test", supportFunnelPath, ""},
		{"owner.tail123.ts.net", "/other", ""},
		{"owner.tail123.ts.net", supportFunnelPath, "https://evil.test"},
	} {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+test.host+test.path, nil)
		req.Header.Set("Origin", test.origin)
		req.Header.Set("Connection", "upgrade")
		req.Header.Set("Upgrade", "websocket")
		req.Header.Set("Sec-Websocket-Version", "13")
		req.Header.Set("Sec-Websocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
		w := httptest.NewRecorder()
		h.funnelHandler("owner.tail123.ts.net").ServeHTTP(w, req)
		require.GreaterOrEqual(t, w.Code, 400)
		require.Empty(t, h.handshakes)
	}
}
