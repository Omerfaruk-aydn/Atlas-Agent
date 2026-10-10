package mobile

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHomeNetworkAddressBoundaries(t *testing.T) {
	t.Parallel()
	for _, address := range []string{"10.0.0.1", "172.16.0.1", "172.31.255.254", "192.168.1.135"} {
		require.True(t, IsHomeNetworkAddress(address), address)
	}
	for _, address := range []string{"", "localhost", "127.0.0.1", "0.0.0.0", "169.254.1.1", "100.64.0.1", "172.32.0.1", "8.8.8.8", "::1", "fd00::1", "::ffff:192.168.1.2", "192.168.1.2:7777"} {
		require.False(t, IsHomeNetworkAddress(address), address)
	}
}

func TestHomeNetworkGatewayOnPrivateInterface(t *testing.T) {
	t.Parallel()
	addresses, err := HomeNetworkAddresses()
	require.NoError(t, err)
	if len(addresses) == 0 {
		t.Skip("No private IPv4 interface is available")
	}
	address := addresses[0].Address
	g, err := Start(t.Context(), Options{
		StateDir: t.TempDir(), Listen: net.JoinHostPort(address, "0"),
		LAN: true, Backend: &fakeBackend{},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = g.Close() })
	pairURL, _ := g.PairingURL()
	u, err := url.Parse(pairURL)
	require.NoError(t, err)
	require.Equal(t, address, u.Hostname(), "QR must use the bound LAN interface")
	require.NotEqual(t, "0", u.Port())
	require.False(t, g.UsesRelay())
	cert, err := x509.ParseCertificate(g.id.Certificate.Certificate[0])
	require.NoError(t, err)
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	p := newPhone(t, g)
	p.base = "https://" + u.Host
	transport := p.client.Transport.(*http.Transport)
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: pool}
	p.client.Timeout = 5 * time.Second
	t.Cleanup(transport.CloseIdleConnections)
	resp, _ := p.do(http.MethodGet, "/api/me", nil, nil)
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	require.Equal(t, "accepted", p.pair(true))
	resp, _ = p.do(http.MethodGet, "/api/me", nil, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.NoError(t, g.Close())
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, p.base+"/api/me", nil)
	require.NoError(t, err)
	response, err := p.client.Do(req)
	if response != nil {
		_ = response.Body.Close()
	}
	require.Error(t, err, "stopped LAN gateway must reject subsequent requests")
	t.Logf("Verified LAN HTTPS, pairing and shutdown on %s (%s)", addresses[0].Interface, address)
}
