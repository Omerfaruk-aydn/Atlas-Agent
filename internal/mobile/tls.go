package mobile

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"path/filepath"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/nodes"
)

// gatewayIdentity uses user-provided TLS files only when explicitly
// configured. Errors never fall back to a different certificate. Generated
// identities remain on disk unchanged when external certificates are used.
func gatewayIdentity(opts Options, hosts, required []string) (nodes.Identity, []string, string, error) {
	if opts.TLSCert == "" && opts.TLSKey == "" {
		id, previous, err := nodes.EnsureIdentity(opts.StateDir, hosts, required)
		return id, hosts, previous, err
	}
	if !filepath.IsAbs(opts.TLSCert) || !filepath.IsAbs(opts.TLSKey) {
		return nodes.Identity{}, nil, "", fmt.Errorf("phone TLS certificate and key must both be configured as absolute paths")
	}
	cert, err := tls.LoadX509KeyPair(opts.TLSCert, opts.TLSKey)
	if err != nil {
		return nodes.Identity{}, nil, "", fmt.Errorf("load phone TLS certificate: %w", err)
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return nodes.Identity{}, nil, "", fmt.Errorf("parse phone TLS certificate: %w", err)
	}
	now := time.Now()
	if now.Before(leaf.NotBefore) || !now.Before(leaf.NotAfter) {
		return nodes.Identity{}, nil, "", fmt.Errorf("phone TLS certificate is not currently valid; renew it before starting /mobile")
	}
	serverAuth := len(leaf.ExtKeyUsage) == 0
	for _, usage := range leaf.ExtKeyUsage {
		serverAuth = serverAuth || usage == x509.ExtKeyUsageServerAuth || usage == x509.ExtKeyUsageAny
	}
	if !serverAuth {
		return nodes.Identity{}, nil, "", fmt.Errorf("phone TLS certificate is not valid for server authentication")
	}
	for _, host := range opts.Hosts {
		if err := leaf.VerifyHostname(host); err != nil {
			return nodes.Identity{}, nil, "", fmt.Errorf("phone TLS certificate does not cover configured host %s: %w", host, err)
		}
	}
	var certified []string
	for _, host := range hosts {
		if leaf.VerifyHostname(host) == nil {
			certified = append(certified, host)
		}
	}
	if len(certified) == 0 {
		return nodes.Identity{}, nil, "", fmt.Errorf("phone TLS certificate covers no allowed host; configure its exact name in mobile_hosts")
	}
	cert.Leaf = leaf
	return nodes.Identity{Certificate: cert, Fingerprint: nodes.Fingerprint(cert.Certificate[0])}, certified, "", nil
}
