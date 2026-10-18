package tools

import (
	"context"
	"net"
	"net/url"
	"strings"
	"time"
)

// loopbackProbeTimeout bounds the check that a local page has a server.
const loopbackProbeTimeout = 700 * time.Millisecond

// loopbackRefused reports whether rawURL points at this machine and nothing
// accepts connections on its port. The browser then starts blank instead of
// on that page: starting on a page that never loads waits out the whole
// launch timeout and reads as "failed to start browser", when the real
// problem is that the dev server is not running.
func loopbackRefused(ctx context.Context, rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	host := u.Hostname()
	if !isLoopbackHost(host) {
		return false
	}
	port := u.Port()
	if port == "" {
		port = "80"
		if u.Scheme == "https" {
			port = "443"
		}
	}
	ctx, cancel := context.WithTimeout(ctx, loopbackProbeTimeout)
	defer cancel()
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, port))
	if err != nil {
		return true
	}
	_ = conn.Close()
	return false
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") || strings.HasSuffix(strings.ToLower(host), ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
