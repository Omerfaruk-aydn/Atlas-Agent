package mobile

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// funnelRequestHeader is set by tailscaled on requests that reached this
// computer through Tailscale Funnel, that is from the public internet.
const funnelRequestHeader = "Tailscale-Funnel-Request"

// funnelTarget is the loopback listener that Tailscale Funnel forwards to.
//
// Funnel terminates TLS with a publicly trusted certificate for the
// device's ts.net name and proxies to this listener over loopback. The
// listener speaks plain HTTP so that tailscaled verifies nothing it cannot
// verify: an https+insecure:// target would disable certificate checks.
// The connection never leaves this computer.
//
// Every request still passes the same Host, cookie, Origin, CSRF, rate
// limit and idempotency checks as the local listener. Only the exact public
// Host is accepted, so a local program that connects directly gains
// nothing it could not already try against the local TLS listener.
type funnelTarget struct {
	host string
	ln   net.Listener
	srv  *http.Server
	// onClose removes the public Funnel entry; it runs before the
	// listener closes so phones never reach a closed port.
	onClose func() error
	once    sync.Once
	err     error
	// seen is the last request from the internet; verified is the last one
	// from a paired, signed-in phone, with its device name.
	seen     atomic.Int64
	verified atomic.Int64
	device   atomic.Pointer[string]
}

type funnelKey struct{}

// ServeFunnel opens the loopback listener for Tailscale Funnel and returns
// its port. host is the exact Host header phones send: the ts.net name,
// with the port when it is not 443. onClose runs when the gateway closes,
// before the listener stops; it should remove the Funnel entry.
func (g *Gateway) ServeFunnel(ctx context.Context, host string, onClose func() error) (int, error) {
	host, err := checkFunnelHost(host)
	if err != nil {
		return 0, err
	}
	if g.opts.RelayURL != "" {
		return 0, errors.New("phone gateway already uses a relay")
	}
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	ft := &funnelTarget{host: host, ln: ln}
	inner := g.handlerForHost(host)
	ft.srv = &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// tailscaled forwards the phone's address; it keys rate limits
			// and is shown with a pairing request.
			if ip := forwardedClient(r); ip != "" {
				r.RemoteAddr = net.JoinHostPort(ip, "0")
			}
			if r.Header.Get(funnelRequestHeader) != "" && r.Host == host {
				ft.seen.Store(time.Now().UnixMilli())
				r = r.WithContext(context.WithValue(r.Context(), funnelKey{}, ft))
			}
			inner.ServeHTTP(w, r)
		}),
		ReadHeaderTimeout: 10 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}
	if !g.funnel.CompareAndSwap(nil, ft) {
		_ = ln.Close()
		return 0, errors.New("phone gateway already serves a Funnel")
	}
	ft.onClose = onClose
	go func() {
		if err := ft.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
			g.opts.Logger.Error("Phone Funnel listener stopped", "error", err)
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

func (ft *funnelTarget) close() error {
	ft.once.Do(func() {
		if ft.onClose != nil {
			ft.err = ft.onClose()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = ft.srv.Shutdown(ctx)
		_ = ft.ln.Close()
	})
	return ft.err
}

// markVerified records an authenticated phone request through Funnel.
func (ft *funnelTarget) markVerified(d Device) {
	name := d.Name
	ft.device.Store(&name)
	ft.verified.Store(time.Now().UnixMilli())
}

// UsesFunnel reports whether phones reach this gateway through Funnel.
func (g *Gateway) UsesFunnel() bool { return g.funnel.Load() != nil }

// FunnelURL is the public Funnel address, or "".
func (g *Gateway) FunnelURL() string {
	if ft := g.funnel.Load(); ft != nil {
		return "https://" + ft.host
	}
	return ""
}

// FunnelLocalPort is the loopback port Funnel forwards to, or 0.
func (g *Gateway) FunnelLocalPort() int {
	if ft := g.funnel.Load(); ft != nil {
		return ft.ln.Addr().(*net.TCPAddr).Port
	}
	return 0
}

// FunnelSeen is the time of the last request that tailscaled marked as
// coming from the internet, or zero.
func (g *Gateway) FunnelSeen() time.Time {
	return funnelTime(g, func(ft *funnelTarget) int64 { return ft.seen.Load() })
}

// FunnelVerified is the time of the last request from a paired phone
// through Funnel, and that phone's name. Only this proves that a phone
// reached the computer over the internet.
func (g *Gateway) FunnelVerified() (time.Time, string) {
	ft := g.funnel.Load()
	if ft == nil {
		return time.Time{}, ""
	}
	at := funnelTime(g, func(ft *funnelTarget) int64 { return ft.verified.Load() })
	if p := ft.device.Load(); p != nil {
		return at, *p
	}
	return at, ""
}

func funnelTime(g *Gateway, field func(*funnelTarget) int64) time.Time {
	ft := g.funnel.Load()
	if ft == nil {
		return time.Time{}
	}
	if ms := field(ft); ms > 0 {
		return time.UnixMilli(ms)
	}
	return time.Time{}
}

// forwardedClient returns the last X-Forwarded-For address, which
// tailscaled sets to the connecting client.
func forwardedClient(r *http.Request) string {
	xff := r.Header.Get("X-Forwarded-For")
	if xff == "" {
		return ""
	}
	parts := strings.Split(xff, ",")
	ip := net.ParseIP(strings.TrimSpace(parts[len(parts)-1]))
	if ip == nil {
		return ""
	}
	return ip.String()
}

// checkFunnelHost accepts only a ts.net name, optionally with one of the
// ports Funnel serves. Funnel never serves any other domain.
func checkFunnelHost(host string) (string, error) {
	host = strings.ToLower(strings.TrimSpace(host))
	name := host
	if h, p, err := net.SplitHostPort(host); err == nil {
		n, perr := strconv.Atoi(p)
		if perr != nil || (n != 8443 && n != 10000) {
			return "", fmt.Errorf("funnel host %q has an unsupported port", host)
		}
		name = h
	}
	if !strings.HasSuffix(name, ".ts.net") || strings.Count(name, ".") < 3 {
		return "", fmt.Errorf("funnel host %q is not a ts.net device name", host)
	}
	if err := checkHostname(name); err != nil {
		return "", err
	}
	return host, nil
}
