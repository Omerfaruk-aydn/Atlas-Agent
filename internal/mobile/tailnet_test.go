package mobile

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"net/http/cookiejar"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClassifyAddressSeparatesTailnetFromLANAndPublic(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		ip        string
		lan, tail bool
	}{
		{ip: "100.64.0.1", tail: true},
		{ip: "100.101.102.103", tail: true},
		{ip: "100.127.255.254", tail: true},
		{ip: "100.63.255.255"},
		{ip: "100.128.0.1"},
		{ip: "8.8.8.8"},
		{ip: "192.168.1.20", lan: true},
		{ip: "10.0.0.2", lan: true},
		{ip: "169.254.10.1", lan: true},
		{ip: "fd7a:115c:a1e0::1"},
	} {
		lan, tail := classifyAddress(net.ParseIP(tc.ip), nil, nil)
		require.Equal(t, tc.lan, len(lan) == 1, tc.ip)
		require.Equal(t, tc.tail, len(tail) == 1, tc.ip)
	}
}

func TestValidHostnamesAcceptsOnlyExactNames(t *testing.T) {
	t.Parallel()
	got, err := validHostnames([]string{" Home-PC.tail1234.ts.net. ", "home-pc.tail1234.ts.net", "ev-pc", "localhost"})
	require.NoError(t, err)
	require.Equal(t, []string{"home-pc.tail1234.ts.net", "ev-pc"}, got, "names are lowercased, deduplicated and localhost is implicit")

	for _, bad := range []string{
		"", "*.ts.net", "*", "home-pc.tail1234.ts.net:7777", "https://home-pc.ts.net",
		"home-pc.ts.net/path", "100.101.102.103", "::1", "-bad.ts.net", "bad-.ts.net",
		"a..b", "home pc.ts.net", "home_pc.ts.net",
	} {
		_, err := validHostnames([]string{bad})
		require.Error(t, err, "%q must be rejected", bad)
	}
}

func TestExtraHostnamesNeedLocalNetworkOptIn(t *testing.T) {
	t.Parallel()
	b := &fakeBackend{pending: map[string]bool{}, questions: map[string]bool{}}
	_, err := Start(t.Context(), Options{StateDir: t.TempDir(), Listen: "127.0.0.1:0", Backend: b, Hosts: []string{"home-pc.ts.net"}})
	require.ErrorContains(t, err, "local-network opt-in")
	_, err = Start(t.Context(), Options{StateDir: t.TempDir(), Listen: "127.0.0.1:0", LAN: true, Backend: b, Hosts: []string{"*.ts.net"}})
	require.ErrorContains(t, err, "invalid phone gateway hostname")
}

func TestEnablingLANCertifiesEveryAdvertisedAddress(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	opts := Options{StateDir: dir, Listen: "127.0.0.1:0", Backend: &fakeBackend{}}
	g, err := Start(t.Context(), opts)
	require.NoError(t, err)
	require.NoError(t, g.Close())
	opts.LAN = true
	g, err = Start(t.Context(), opts)
	require.NoError(t, err)
	t.Cleanup(func() { _ = g.Close() })
	leaf, err := x509.ParseCertificate(g.id.Certificate.Certificate[0])
	require.NoError(t, err)
	for _, host := range g.hosts {
		name, _, err := net.SplitHostPort(host)
		require.NoError(t, err)
		require.NoError(t, leaf.VerifyHostname(name))
	}
}

// phoneVia returns a phone that addresses the gateway as host but connects
// to its loopback listener, as a phone resolving a MagicDNS name would.
func phoneVia(t *testing.T, g *Gateway, host string) *phone {
	t.Helper()
	port := strconv.Itoa(g.ln.Addr().(*net.TCPAddr).Port)
	jar, _ := cookiejar.New(nil)
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // Test client for a self-signed gateway.
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, net.JoinHostPort("127.0.0.1", port))
		},
	}
	return &phone{t: t, g: g, base: "https://" + net.JoinHostPort(host, port), client: &http.Client{Jar: jar, Transport: tr}}
}

func TestConfiguredHostnameIsCertifiedAndMatchedExactly(t *testing.T) {
	t.Parallel()
	const name = "home-pc.tail1234.ts.net"
	b := &fakeBackend{pending: map[string]bool{}, questions: map[string]bool{}, revision: "r1"}
	g, err := Start(t.Context(), Options{StateDir: t.TempDir(), Listen: "127.0.0.1:0", LAN: true, Backend: b, Hosts: []string{"Home-PC.tail1234.ts.net"}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = g.Close() })

	leaf, err := x509.ParseCertificate(g.id.Certificate.Certificate[0])
	require.NoError(t, err)
	require.Contains(t, leaf.DNSNames, name)
	require.NoError(t, leaf.VerifyHostname(name))
	require.True(t, leaf.IPAddresses[0].IsLoopback(), "IP hosts are IP SANs, not DNS names")

	u, _ := g.PairingURL()
	p := phoneVia(t, g, name)
	require.Contains(t, u, p.base+"/#pair=", "the QR prefers the configured name")

	require.Equal(t, "accepted", p.pair(true))
	resp, _ := p.do("GET", "/api/me", nil, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Origin and CSRF are still enforced on the new name.
	key := map[string]string{"Idempotency-Key": "k1", "Origin": "https://127.0.0.1"}
	_, out := p.do("POST", "/api/sessions/s1/prompt", map[string]string{"text": "hi"}, key)
	require.Equal(t, "bad_origin", code(out))
	p.csrf = "wrong"
	_, out = p.do("POST", "/api/sessions/s1/prompt", map[string]string{"text": "hi"}, map[string]string{"Idempotency-Key": "k2"})
	require.Equal(t, "bad_csrf", code(out))
	require.Zero(t, b.runs.Load())

	// Names that only resemble the configured one are refused.
	for _, host := range []string{"evil.tail1234.ts.net", "x." + name, "tail1234.ts.net", "100.101.102.103"} {
		resp, _ := phoneVia(t, g, host).do("GET", "/", nil, nil)
		require.Equal(t, http.StatusMisdirectedRequest, resp.StatusCode, host)
	}
}

func TestCertificateRenewalKeepsPairingsAndReportsPreviousFingerprint(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	b := &fakeBackend{pending: map[string]bool{}, questions: map[string]bool{}, revision: "r1"}
	start := func(hosts ...string) *Gateway {
		g, err := Start(t.Context(), Options{StateDir: dir, Listen: "127.0.0.1:0", LAN: true, Backend: b, Hosts: hosts})
		require.NoError(t, err)
		return g
	}

	g := start()
	first := g.Fingerprint()
	p := newPhone(t, g)
	require.Equal(t, "accepted", p.pair(true))
	require.NoError(t, g.Close())

	g = start("home-pc.tail1234.ts.net")
	require.NotEqual(t, first, g.Fingerprint(), "a new name renews the certificate")
	require.Equal(t, first, g.RenewedFrom(), "the dialog can show which certificate was replaced")
	require.Len(t, g.Auth.Devices(), 1, "pairings do not depend on the certificate")
	// The phone that paired before still authenticates once its browser
	// accepts the new certificate.
	p.g, p.base = g, g.URLs()[1]
	resp, out := p.do("GET", "/api/me", nil, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, g.Fingerprint(), out["fingerprint"], "the phone app shows the new fingerprint to compare")
	require.NoError(t, g.Close())

	g = start("home-pc.tail1234.ts.net")
	t.Cleanup(func() { _ = g.Close() })
	require.Empty(t, g.RenewedFrom(), "an unchanged host list keeps the certificate")
}
