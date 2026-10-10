// Package nodes lets one Atlas task use several computers. A node is an
// Atlas process started with `atlas node serve` on a machine (another PC, a
// WSL distribution, a Windows Sandbox guest). It exposes a small,
// capability-scoped HTTPS API: command execution under a root directory,
// verified file transfer and, on an interactive Windows session, desktop
// observation and input. Controllers pair once with a one-time code and the
// node's pinned certificate fingerprint, then authenticate every request
// with a bearer token whose hash is all the node stores.
package nodes

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/taskstate"
)

// ProtocolVersion is bumped on incompatible API changes.
const ProtocolVersion = 1

// Identity is a node's TLS certificate and stable id.
type Identity struct {
	NodeID      string
	Certificate tls.Certificate
	Fingerprint string
}

// Fingerprint formats the SHA-256 of a DER certificate as "sha256:<hex>".
func Fingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// NormalizeFingerprint lowercases and strips separators so fingerprints
// copied with colons or spaces compare equal.
func NormalizeFingerprint(fp string) string {
	fp = strings.ToLower(strings.TrimSpace(fp))
	fp = strings.TrimPrefix(fp, "sha256:")
	fp = strings.NewReplacer(":", "", " ", "", "-", "").Replace(fp)
	return "sha256:" + fp
}

// LoadOrCreateIdentity reads the node key pair from dir or creates a new
// self-signed ECDSA P-256 certificate valid for ten years.
func LoadOrCreateIdentity(dir string, hosts []string) (Identity, error) {
	certPath, keyPath, idPath := filepath.Join(dir, "node-cert.pem"), filepath.Join(dir, "node-key.pem"), filepath.Join(dir, "node-id")
	if cert, err := tls.LoadX509KeyPair(certPath, keyPath); err == nil {
		id, err := os.ReadFile(idPath)
		if err != nil {
			return Identity{}, fmt.Errorf("node id missing next to certificate: %w", err)
		}
		return Identity{NodeID: strings.TrimSpace(string(id)), Certificate: cert, Fingerprint: Fingerprint(cert.Certificate[0])}, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return Identity{}, fmt.Errorf("load node certificate: %w", err)
	}
	return createIdentity(dir, "node-"+randomHex(8), hosts)
}

// EnsureIdentity loads the identity in dir like LoadOrCreateIdentity, and
// renews the certificate when it does not name every host in required. The
// node id is kept. A renewed certificate has a new fingerprint, so the old
// one is returned for the caller to announce; it is empty otherwise.
func EnsureIdentity(dir string, hosts, required []string) (Identity, string, error) {
	id, err := LoadOrCreateIdentity(dir, hosts)
	if err != nil {
		return Identity{}, "", err
	}
	leaf, err := x509.ParseCertificate(id.Certificate.Certificate[0])
	if err != nil {
		return Identity{}, "", fmt.Errorf("parse node certificate: %w", err)
	}
	for _, h := range required {
		if !certNames(leaf, h) {
			renewed, err := createIdentity(dir, id.NodeID, hosts)
			if err != nil {
				return Identity{}, "", fmt.Errorf("renew node certificate: %w", err)
			}
			return renewed, id.Fingerprint, nil
		}
	}
	return id, "", nil
}

// certNames uses TLS hostname rules. IP literals require IP SANs, even when
// an older certificate incorrectly stored them as DNS names.
func certNames(cert *x509.Certificate, host string) bool {
	return cert.VerifyHostname(host) == nil
}

func createIdentity(dir, nodeID string, hosts []string) (Identity, error) {
	certPath, keyPath, idPath := filepath.Join(dir, "node-cert.pem"), filepath.Join(dir, "node-key.pem"), filepath.Join(dir, "node-id")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Identity{}, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return Identity{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		return Identity{}, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "Atlas node " + nodeID},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(10, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
	}
	for _, h := range hosts {
		switch ip := net.ParseIP(h); {
		case ip != nil:
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		case h != "" && h != "localhost":
			tmpl.DNSNames = append(tmpl.DNSNames, h)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return Identity{}, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return Identity{}, err
	}
	if err := taskstate.WriteFileAtomic(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		return Identity{}, err
	}
	if err := taskstate.WriteFileAtomic(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		return Identity{}, err
	}
	if err := taskstate.WriteFileAtomic(idPath, []byte(nodeID), 0o644); err != nil {
		return Identity{}, err
	}
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return Identity{}, err
	}
	return Identity{NodeID: nodeID, Certificate: cert, Fingerprint: Fingerprint(der)}, nil
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// PinnedTLSConfig returns a client TLS config that accepts exactly the
// certificate with the given fingerprint. Hostname and CA checks are
// replaced by the pin, which is what makes a self-signed node safe to use.
func PinnedTLSConfig(fingerprint string) *tls.Config {
	want := NormalizeFingerprint(fingerprint)
	return &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: true, //nolint:gosec // Replaced by the fingerprint pin below.
		VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
			if len(raw) == 0 {
				return errors.New("node presented no certificate")
			}
			if got := Fingerprint(raw[0]); got != want {
				return &PinError{Want: want, Got: got}
			}
			return nil
		},
	}
}

// PinError reports a certificate that does not match the pinned node.
type PinError struct{ Want, Got string }

func (e *PinError) Error() string {
	return fmt.Sprintf("node certificate fingerprint %s does not match pinned %s; refusing to connect", e.Got, e.Want)
}
