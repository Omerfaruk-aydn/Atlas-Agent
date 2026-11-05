package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/lock"
)

// listenerToken creates the same per-port credential for wildcard listeners
// that loopback clients use. It does not expose that credential to clients
// connecting to a non-loopback address.
func listenerToken(network, address string) (string, error) {
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", err
	}
	return LocalToken(network, net.JoinHostPort("127.0.0.1", port))
}

// LocalToken returns a private per-port token for loopback TCP connections.
// Explicit tokens retain precedence and are also supported for remote hosts.
func LocalToken(network, address string) (string, error) {
	if token := os.Getenv("ATLAS_SERVER_TOKEN"); token != "" {
		return token, nil
	}
	if !strings.HasPrefix(network, "tcp") {
		return "", nil
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", err
	}
	ip := net.ParseIP(host)
	if !strings.EqualFold(host, "localhost") && (ip == nil || !ip.IsLoopback()) {
		return "", nil
	}
	dir := filepath.Join(config.GlobalWorkspaceDir(), "server-auth")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create local server credentials: %w", err)
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || (runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0) {
		return "", errors.New("local server credential directory is not private")
	}
	sum := sha256.Sum256([]byte(port))
	path := filepath.Join(dir, hex.EncodeToString(sum[:16])+".token")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	release, err := lock.File(ctx, path+".lock")
	if err != nil {
		return "", err
	}
	defer release()
	info, err = os.Lstat(path)
	if err == nil && (!info.Mode().IsRegular() || (runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0)) {
		return "", errors.New("local server credential file is not private")
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err == nil {
		if len(data) != 64 {
			return "", errors.New("invalid local server credential")
		}
		if _, err := hex.DecodeString(string(data)); err != nil {
			return "", errors.New("invalid local server credential")
		}
		return string(data), nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	token := hex.EncodeToString(random[:])
	tmp, err := os.CreateTemp(dir, ".token-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(token); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return "", err
	}
	return token, nil
}
