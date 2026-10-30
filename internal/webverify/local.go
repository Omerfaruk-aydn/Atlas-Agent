package webverify

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// CheckLocal accepts only http(s) addresses that point at the developer's
// own machine: localhost, 127.0.0.0/8, ::1, *.localhost and *.test. The
// verifier drives a real browser and evaluates scripts in the page, so it
// must never be pointed at someone else's site.
func CheckLocal(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("only http and https addresses can be verified, got %q", u.Scheme)
	}
	if u.User != nil {
		return nil, errors.New("addresses with credentials are not verified")
	}
	if !localHost(u.Hostname()) {
		return nil, fmt.Errorf("%q is not a local address; product verification only runs against localhost, 127.0.0.1, ::1, *.localhost and *.test", u.Hostname())
	}
	return u, nil
}

func localHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	switch {
	case host == "":
		return false
	case host == "localhost", strings.HasSuffix(host, ".localhost"), strings.HasSuffix(host, ".test"):
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Resolve joins a page path to the base address, refusing anything that
// leaves the local machine.
func Resolve(base, ref string) (string, error) {
	b, err := CheckLocal(base)
	if err != nil {
		return "", err
	}
	r, err := url.Parse(strings.TrimSpace(ref))
	if err != nil {
		return "", fmt.Errorf("invalid page %q: %w", ref, err)
	}
	full := b.ResolveReference(r)
	if _, err := CheckLocal(full.String()); err != nil {
		return "", err
	}
	return full.String(), nil
}

// Reachable reports whether something answers at the address. Any HTTP
// response counts: the page itself is judged later. Redirects away from the
// local machine are not followed.
func Reachable(ctx context.Context, raw string) bool {
	if _, err := CheckLocal(raw); err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return false
	}
	client := &http.Client{CheckRedirect: func(r *http.Request, _ []*http.Request) error {
		_, err := CheckLocal(r.URL.String())
		return err
	}}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return true
}
