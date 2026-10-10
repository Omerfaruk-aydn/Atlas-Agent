package nodes

import (
	"crypto/x509"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIdentityRenewalCertifiesChangedLANAddresses(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	first, err := LoadOrCreateIdentity(dir, []string{"localhost", "127.0.0.1", "192.168.1.10"})
	require.NoError(t, err)
	hosts := []string{"localhost", "127.0.0.1", "192.168.1.20"}
	renewed, previous, err := EnsureIdentity(dir, hosts, hosts)
	require.NoError(t, err)
	require.Equal(t, first.Fingerprint, previous)
	require.Equal(t, first.NodeID, renewed.NodeID)
	require.NotEqual(t, first.Fingerprint, renewed.Fingerprint)
	leaf, err := x509.ParseCertificate(renewed.Certificate.Certificate[0])
	require.NoError(t, err)
	for _, host := range hosts {
		require.NoError(t, leaf.VerifyHostname(host))
	}
	stable, previous, err := EnsureIdentity(dir, hosts, hosts)
	require.NoError(t, err)
	require.Empty(t, previous)
	require.Equal(t, renewed.Fingerprint, stable.Fingerprint)
}

func TestLegacyIPInDNSSANIsNotAcceptedAsValidIPIdentity(t *testing.T) {
	t.Parallel()
	legacy := &x509.Certificate{DNSNames: []string{"192.168.1.20"}}
	require.False(t, certNames(legacy, "192.168.1.20"))
}
