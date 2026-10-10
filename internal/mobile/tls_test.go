package mobile

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/nodes"
	"github.com/stretchr/testify/require"
)

func phoneTLSFixture(t *testing.T, host string, expires time.Time, usage x509.ExtKeyUsage) (string, string, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	root := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Test phone CA"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(1, 0, 0), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	rootDER, err := x509.CreateCertificate(rand.Reader, root, root, &key.PublicKey, key)
	require.NoError(t, err)
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), DNSNames: []string{host}, NotBefore: time.Now().Add(-time.Hour), NotAfter: expires, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{usage}}
	der, err := x509.CreateCertificate(rand.Reader, leaf, root, &key.PublicKey, key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "phone.crt"), filepath.Join(dir, "phone.key")
	chain := append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: rootDER})...)
	require.NoError(t, os.WriteFile(certFile, chain, 0o644))
	require.NoError(t, os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600))
	roots := x509.NewCertPool()
	require.True(t, roots.AppendCertsFromPEM(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: rootDER})))
	return certFile, keyFile, roots
}

func TestPhoneGatewayUsesTrustedCertificateAndOnlyAdvertisesCertifiedHosts(t *testing.T) {
	t.Parallel()
	name := "home-pc.tail1234.ts.net"
	certFile, keyFile, roots := phoneTLSFixture(t, name, time.Now().Add(time.Hour), x509.ExtKeyUsageServerAuth)
	dir := t.TempDir()
	legacy, err := nodes.LoadOrCreateIdentity(dir, []string{"localhost", "127.0.0.1"})
	require.NoError(t, err)
	g, err := Start(t.Context(), Options{StateDir: dir, Listen: "127.0.0.1:0", LAN: true, Hosts: []string{name}, TLSCert: certFile, TLSKey: keyFile, Backend: &fakeBackend{}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = g.Close() })
	require.Len(t, g.URLs(), 1)
	url, _ := g.PairingURL()
	require.Contains(t, url, "https://"+name+":")
	p := phoneVia(t, g, name)
	p.client.Transport.(*http.Transport).TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}
	p.pair(true)
	reply, _ := p.do("GET", "/api/me", nil, nil)
	require.Equal(t, http.StatusOK, reply.StatusCode, "TLS must verify with the provided trust root, without skipping verification")
	require.NotEqual(t, legacy.Fingerprint, g.Fingerprint())
	unchanged, err := nodes.LoadOrCreateIdentity(dir, []string{"localhost"})
	require.NoError(t, err)
	require.Equal(t, legacy.Fingerprint, unchanged.Fingerprint, "external TLS must not overwrite the generated identity")
	require.False(t, g.allowedHost("127.0.0.1:7777"))
}

func TestPhoneTLSRejectsInvalidConfigurationWithoutFallback(t *testing.T) {
	t.Parallel()
	name := "home-pc.tail1234.ts.net"
	for _, issue := range []string{"missing-key", "wrong-name", "expired", "wrong-usage"} {
		t.Run(issue, func(t *testing.T) {
			t.Parallel()
			expires, usage := time.Now().Add(time.Hour), x509.ExtKeyUsageServerAuth
			if issue == "expired" {
				expires = time.Now().Add(-time.Minute)
			}
			if issue == "wrong-usage" {
				usage = x509.ExtKeyUsageClientAuth
			}
			certFile, keyFile, _ := phoneTLSFixture(t, name, expires, usage)
			host := name
			if issue == "wrong-name" {
				host = "other.tail1234.ts.net"
			}
			if issue == "missing-key" {
				keyFile = ""
			}
			_, _, _, err := gatewayIdentity(Options{StateDir: t.TempDir(), Hosts: []string{host}, TLSCert: certFile, TLSKey: keyFile}, []string{"localhost", host}, nil)
			require.Error(t, err)
		})
	}
}
