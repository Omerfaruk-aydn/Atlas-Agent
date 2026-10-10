// Package mobilerelay carries the phone HTTP surface over outbound TLS
// WebSockets. A relay serves one explicitly configured Atlas installation.
package mobilerelay

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Origin requires an HTTPS origin, without credentials, paths or queries.
func Origin(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || u.RawPath != "" || u.Opaque != "" || strings.ContainsAny(u.Host, "\\\r\n\t ") {
		return nil, fmt.Errorf("mobile relay URL must be an HTTPS origin, such as https://atlas.example.com")
	}
	u.Host = strings.ToLower(u.Host)
	u.Path = ""
	return u, nil
}

// ReadToken loads a 256-bit shared connector key, never a phone credential.
func ReadToken(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("mobile relay key file must have an absolute path")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("read mobile relay key: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("read mobile relay key: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() > 128 {
		return "", fmt.Errorf("mobile relay key must be a small regular file")
	}
	data, err := io.ReadAll(io.LimitReader(f, 129))
	if err != nil {
		return "", fmt.Errorf("read mobile relay key: %w", err)
	}
	token := strings.TrimSpace(string(data))
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(decoded) != 32 || len(token) != 43 {
		return "", fmt.Errorf("mobile relay key must be a 32-byte base64url key; use mobile-relay keygen")
	}
	return token, nil
}

// GenerateKey creates a private key file without replacing an existing file.
func GenerateKey(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("mobile relay key file must have an absolute path")
	}
	var key [32]byte
	if _, err := rand.Read(key[:]); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := f.WriteString(base64.RawURLEncoding.EncodeToString(key[:]) + "\n")
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}
