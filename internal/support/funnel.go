package support

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/mobilerelay"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/tsfunnel"
	"github.com/gorilla/websocket"
)

const supportFunnelPath = "/v1/support/tunnel"

type supportFunnel struct {
	server *http.Server
	link   *tsfunnel.Link
}

// funnelEndpoint accepts only public Tailscale HTTPS routes, never an
// arbitrary URL embedded in a pairing link.
func funnelEndpoint(address string) (string, error) {
	u, err := url.Parse(address)
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", errors.New("funnel requires a Tailscale HTTPS address")
	}
	host := strings.ToLower(u.Hostname())
	labels := strings.Split(host, ".")
	if len(labels) != 4 || labels[2] != "ts" || labels[3] != "net" {
		return "", errors.New("funnel requires a device.tailnet.ts.net address")
	}
	for _, label := range labels[:2] {
		if label == "" || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return "", errors.New("invalid Funnel hostname")
		}
		for _, r := range label {
			letter := r >= 'a' && r <= 'z'
			digit := r >= '0' && r <= '9'
			if !letter && !digit && r != '-' {
				return "", errors.New("invalid Funnel hostname")
			}
		}
	}
	if port := u.Port(); port != "" && port != "443" && port != "8443" && port != "10000" {
		return "", errors.New("unsupported Funnel port")
	}
	u.Scheme, u.Path = "wss", supportFunnelPath
	return u.String(), nil
}

func dialFunnel(ctx context.Context, address string, dialer *websocket.Dialer) (net.Conn, error) {
	endpoint, err := funnelEndpoint(address)
	if err != nil {
		return nil, err
	}
	d := *websocket.DefaultDialer
	if dialer != nil {
		d = *dialer
	}
	d.HandshakeTimeout = 15 * time.Second
	ws, resp, err := d.DialContext(ctx, endpoint, nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		return nil, Errorf(CodeRemoteOffline, "cannot reach the Tailscale support route")
	}
	ws.SetReadLimit(2 << 20)
	return mobilerelay.NewStreamConn(ws), nil
}

// funnelHandler tunnels the existing end-to-end TLS support protocol.
// Funnel terminates only the outer HTTPS connection, not support identity,
// pairing proofs, owner acceptance or the authenticated HTTP/2 session.
func (h *Host) funnelHandler(host string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != supportFunnelPath || r.Host != host {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}
		select {
		case h.handshakes <- struct{}{}:
		default:
			http.Error(w, "Busy", http.StatusTooManyRequests)
			return
		}
		upgrader := websocket.Upgrader{HandshakeTimeout: 10 * time.Second}
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			<-h.handshakes
			return
		}
		ws.SetReadLimit(2 << 20)
		conn := mobilerelay.NewStreamConn(ws)
		if !h.goWork(func() {
			defer func() { <-h.handshakes; _ = conn.Close() }()
			h.serveConn(conn, "funnel")
		}) {
			<-h.handshakes
			_ = conn.Close()
		}
	})
}

// OfferFunnel creates a support offer through the installed Tailscale.
// The supporter needs only the resulting link, not Tailscale or a relay.
func (h *Host) OfferFunnel(ctx context.Context, opts OfferOptions) (OfferInfo, error) {
	cli := tsfunnel.CLI{}
	ready := tsfunnel.Inspect(ctx, cli, tsfunnel.SystemServiceState)
	mgr := &tsfunnel.Manager{CLI: cli, StateFile: filepath.Join(h.opts.StateDir, "support-funnel.json")}
	return h.offerFunnel(ctx, opts, mgr, ready)
}

func (h *Host) offerFunnel(ctx context.Context, opts OfferOptions, mgr *tsfunnel.Manager, ready tsfunnel.Readiness) (OfferInfo, error) {
	h.funnelMu.Lock()
	defer h.funnelMu.Unlock()
	if h.funnel != nil {
		return OfferInfo{}, errors.New("a support Funnel is already running; end it first")
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	stop := context.AfterFunc(h.ctx, cancel)
	defer stop()
	if !ready.Ready() {
		return OfferInfo{}, fmt.Errorf("tailscale is not ready (%s): install it and sign in using /rc first", ready.Problem)
	}
	plan, err := mgr.Prepare(ctx, ready)
	if err != nil {
		return OfferInfo{}, err
	}
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return OfferInfo{}, err
	}
	srv := &http.Server{Handler: h.funnelHandler(plan.HostHeader()), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	progress := tsfunnel.NewProgress()
	link, err := mgr.Start(ctx, plan, ln.Addr().(*net.TCPAddr).Port, progress)
	if err != nil {
		_ = srv.Close()
		var approval *tsfunnel.ApprovalRequiredError
		if errors.As(err, &approval) {
			return OfferInfo{}, fmt.Errorf("approve Funnel at %s, then retry: %w", approval.URL, err)
		}
		if _, approvalURL, _ := progress.Snapshot(); approvalURL != "" {
			return OfferInfo{}, fmt.Errorf("approve Funnel at %s, then retry: %w", approvalURL, err)
		}
		return OfferInfo{}, err
	}
	opts.LAN, opts.Internet, opts.Funnel = false, false, true
	info, err := h.Offer(opts)
	if err != nil {
		_ = link.Close()
		_ = srv.Close()
		return OfferInfo{}, err
	}
	q := url.Values{"code": {info.Code}, "funnel": {link.URL()}}
	info.Link = "atlas-support://join?" + q.Encode()
	h.mu.Lock()
	if h.offerInfo == nil || h.offerInfo.Code != info.Code {
		h.mu.Unlock()
		_ = link.Close()
		_ = srv.Close()
		return OfferInfo{}, errors.New("support offer was withdrawn during setup")
	}
	h.offerInfo.Link = info.Link
	h.mu.Unlock()
	f := &supportFunnel{server: srv, link: link}
	h.funnel = f
	h.goWork(func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-h.ctx.Done():
				return
			case <-ticker.C:
				st := h.Status()
				if !link.Alive() {
					h.Disconnect("Tailscale support route ended")
					h.CancelOffer()
				}
				if !link.Alive() || st.State.Terminal() || st.Offer != nil && st.Offer.Code != info.Code || st.Session == nil && st.Pending == nil && (st.Offer == nil || time.Now().After(st.Offer.ExpiresAt)) {
					h.stopFunnel()
					return
				}
			}
		}
	})
	h.notify()
	return info, nil
}

func (h *Host) stopFunnel() {
	h.funnelMu.Lock()
	f := h.funnel
	h.funnel = nil
	h.funnelMu.Unlock()
	if f != nil {
		_ = f.server.Close()
		if err := f.link.Close(); err != nil {
			h.logger.Warn("Failed to stop support Funnel", "error", err)
		}
	}
}
