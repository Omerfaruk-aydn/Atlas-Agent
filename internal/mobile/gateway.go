package mobile

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/mobilerelay"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/nodes"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/pubsub"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/question"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/vault"
)

// Backend is what the gateway needs from the running Atlas workspace. The
// interface lives here, with its only consumer.
type Backend interface {
	ListSessions(ctx context.Context) ([]session.Session, error)
	GetSession(ctx context.Context, id string) (session.Session, error)
	ListMessages(ctx context.Context, sessionID string) ([]message.Message, error)
	AgentRun(ctx context.Context, sessionID, prompt string, attachments ...message.Attachment) error
	AgentIsSessionBusy(sessionID string) bool
	PermissionGrant(permission.PermissionRequest) bool
	PermissionGrantPersistent(permission.PermissionRequest) bool
	PermissionDeny(permission.PermissionRequest) bool
	QuestionAnswerRequest(id string, answers []question.Answer) bool
	QuestionCancelRequest(id string) bool
	WorkflowSnapshot(ctx context.Context, sessionID string) (engineering.WorkflowSnapshot, error)
	WorkflowControl(ctx context.Context, sessionID string, c engineering.WorkflowControl) error
}

// Options configures the gateway.
type Options struct {
	StateDir string
	// Listen is host:port; loopback unless LAN is true.
	Listen string
	LAN    bool
	// Hosts are extra exact hostnames the phone may use, such as a
	// Tailscale MagicDNS name. They need LAN.
	Hosts          []string
	TLSCert        string
	TLSKey         string
	Language       string
	Version        string
	RelayURL       string
	RelayTokenFile string
	Backend        Backend
	// Events streams app event payloads (pubsub.Event[T] values).
	Events func(ctx context.Context) <-chan any
	// Preview returns a downscaled JPEG of the screen; nil disables it.
	Preview func() ([]byte, error)
	// Screen captures frames for the live screen stream; nil disables it.
	Screen ScreenSource
	Logger *slog.Logger
}

// Gateway is a running phone control server.
type Gateway struct {
	opts     Options
	Auth     *Auth
	id       nodes.Identity
	srv      *http.Server
	redirect *http.Server
	ln       net.Listener
	hosts    []string
	// renewedFrom is the previous certificate fingerprint when this start
	// renewed the certificate, so phones must trust it again.
	renewedFrom string
	ring        *ring
	cancel      context.CancelFunc
	relay       *mobilerelay.Connector
	// funnel is the loopback target of an Atlas-owned Tailscale Funnel.
	funnel atomic.Pointer[funnelTarget]
	// unsubscribeCredentials ends this gateway's role as a secure input
	// channel for credential cards.
	unsubscribeCredentials func()
	screen                 *screenHub

	mu          sync.Mutex
	permissions map[string]permission.PermissionRequest
	byToolCall  map[string]string
	questions   map[string]question.Request
	busy        map[string]bool
	idem        map[string]idemEntry
	limits      map[string][]time.Time
	uploads     map[string]upload
}

type idemEntry struct {
	at     time.Time
	status int
	body   []byte
}

type upload struct {
	device string
	at     time.Time
	att    message.Attachment
}

// Start launches the gateway and its event pump.
func Start(ctx context.Context, opts Options) (*Gateway, error) {
	if opts.Backend == nil || opts.StateDir == "" {
		return nil, errors.New("phone gateway needs a backend and a state directory")
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Listen == "" {
		opts.Listen = "127.0.0.1:7777"
	}
	host, port, err := net.SplitHostPort(opts.Listen)
	if err != nil {
		return nil, err
	}
	if ip := net.ParseIP(host); !opts.LAN && (ip == nil || !ip.IsLoopback()) && host != "localhost" {
		return nil, fmt.Errorf("refusing to listen on %s without explicit local-network opt-in", opts.Listen)
	}
	names, err := validHostnames(opts.Hosts)
	if err != nil {
		return nil, err
	}
	if len(names) > 0 && !opts.LAN {
		return nil, errors.New("extra phone gateway hostnames need the local-network opt-in")
	}
	opts.Hosts = names
	relayOrigin, relayToken, err := relayConfiguration(opts)
	if err != nil {
		return nil, err
	}
	auth, err := LoadAuth(opts.StateDir)
	if err != nil {
		return nil, err
	}
	// Configured names come first, then tailnet, then LAN addresses: the
	// pairing QR uses the first non-loopback host, and a phone paired on a
	// name that also works away from home does not need a second pairing.
	hosts := []string{"localhost", "127.0.0.1"}
	var required []string
	if opts.LAN {
		lan, tailnet := interfaceAddresses()
		hosts = append(hosts, names...)
		hosts = append(hosts, tailnet...)
		hosts = append(hosts, lan...)
		// Every advertised address must be certified, including LAN addresses
		// added after enabling LAN or after a DHCP change.
		required = slices.Clone(hosts)
	}
	id, hosts, renewedFrom, err := gatewayIdentity(opts, hosts, required)
	if err != nil {
		return nil, err
	}
	if renewedFrom != "" {
		opts.Logger.Warn("Phone gateway certificate renewed; phones must accept the new certificate", "previous", renewedFrom, "fingerprint", id.Fingerprint)
	}
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", opts.Listen)
	if err != nil {
		return nil, err
	}
	realPort := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	if port == "0" {
		port = realPort
	}
	var allowed []string
	for _, h := range hosts {
		allowed = append(allowed, net.JoinHostPort(h, realPort))
	}
	g := &Gateway{opts: opts, Auth: auth, id: id, ln: ln, hosts: allowed, renewedFrom: renewedFrom, ring: newRing(512), permissions: map[string]permission.PermissionRequest{}, byToolCall: map[string]string{}, questions: map[string]question.Request{}, busy: map[string]bool{}, idem: map[string]idemEntry{}, limits: map[string][]time.Time{}, uploads: map[string]upload{}}
	// Waking every event stream makes a revoked phone lose its stream at
	// once; the others ignore this event type.
	auth.OnRevoke = func() { g.ring.add("device_revoked", "", map[string]any{}) }
	// A revoked phone's supplied passwords and live screen end at once.
	auth.OnRevokeDevices = func(ids []string) {
		for _, id := range ids {
			vault.Ephemeral.PurgeDevice(id)
		}
		g.screen.revoke(ids)
	}
	g.screen = newScreenHub(opts.Screen, opts.Logger)
	g.srv = &http.Server{Handler: g.handler(), TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{id.Certificate}}, ReadHeaderTimeout: 10 * time.Second}
	g.redirect = &http.Server{Handler: http.HandlerFunc(g.redirectToHTTPS), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 10 * time.Second, MaxHeaderBytes: 16 << 10}
	pumpCtx, cancel := context.WithCancel(ctx)
	g.cancel = cancel
	if relayOrigin != nil {
		// Only the outbound relay handler accepts this public Host. The
		// local listener retains its own exact host and certificate rules.
		g.relay, err = mobilerelay.Connect(pumpCtx, mobilerelay.ConnectorOptions{
			URL: relayOrigin.String(), Token: relayToken,
			Handler: g.handlerForHost(relayOrigin.Host),
		})
		if err != nil {
			cancel()
			_ = ln.Close()
			return nil, err
		}
	}
	if opts.Events != nil {
		go g.pump(pumpCtx, opts.Events(pumpCtx))
	}
	g.unsubscribeCredentials = g.subscribeCredentials()
	go g.watchBusy(pumpCtx)
	tlsListener, httpListener := splitProtocols(ln)
	go func() {
		if err := g.redirect.Serve(httpListener); err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
			opts.Logger.Error("Phone HTTPS redirect stopped", "error", err)
		}
	}()
	go func() {
		if err := g.srv.ServeTLS(tlsListener, "", ""); err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
			opts.Logger.Error("Phone gateway stopped", "error", err)
		}
	}()
	return g, nil
}

// Close stops the server.
func (g *Gateway) Close() error {
	var funnelErr error
	if ft := g.funnel.Load(); ft != nil {
		// Remove the public entry before the listener it points to.
		funnelErr = ft.close()
	}
	g.cancel()
	if g.unsubscribeCredentials != nil {
		g.unsubscribeCredentials()
	}
	g.screen.close()
	// Close the shared socket even if Serve has not registered it yet.
	_ = g.ln.Close()
	if g.relay != nil {
		_ = g.relay.Close()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return errors.Join(funnelErr, g.redirect.Close(), g.srv.Shutdown(ctx))
}

// Fingerprint is the TLS certificate fingerprint shown to the user.
func (g *Gateway) Fingerprint() string { return g.id.Fingerprint }

// RenewedFrom returns the previous certificate fingerprint when this start
// renewed the certificate, or "". Paired phones stay paired, because their
// session token does not depend on the certificate, but their browser shows
// a certificate warning again and should accept only Fingerprint().
func (g *Gateway) RenewedFrom() string { return g.renewedFrom }

// URLs lists the addresses a phone can open.
func (g *Gateway) URLs() []string {
	var out []string
	for _, h := range g.hosts {
		out = append(out, "https://"+h)
	}
	return out
}

// PairingURL returns a fresh pairing URL for the QR code. The nonce sits in
// the URL fragment, which browsers never send to the server or proxies.
func (g *Gateway) PairingURL() (string, time.Time) {
	nonce, expires := g.Auth.NewNonce()
	if u := g.FunnelURL(); u != "" {
		return u + "/#pair=" + nonce, expires
	}
	if g.opts.RelayURL != "" {
		origin, _ := mobilerelay.Origin(g.opts.RelayURL)
		return origin.String() + "/#pair=" + nonce, expires
	}
	base := g.URLs()[0]
	if len(g.opts.Hosts) == 0 {
		// Prefer the explicit bound interface over an unrelated virtual
		// adapter. Configured DNS names retain their documented priority.
		if host, _, err := net.SplitHostPort(g.opts.Listen); err == nil {
			if ip := net.ParseIP(host); ip != nil && !ip.IsLoopback() && !ip.IsUnspecified() {
				for _, address := range g.hosts {
					candidate, _, _ := net.SplitHostPort(address)
					if candidate == host {
						return "https://" + address + "/#pair=" + nonce, expires
					}
				}
			}
		}
	}
	for _, address := range g.hosts {
		host, _, _ := net.SplitHostPort(address)
		if host != "localhost" && (net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback()) {
			base = "https://" + address
			break
		}
	}
	return base + "/#pair=" + nonce, expires
}

// RelayConnected reports the outbound connection state for the pairing UI.
func (g *Gateway) RelayConnected() bool {
	return g.relay != nil && g.relay.Connected()
}

// UsesRelay reports whether this gateway advertises the public relay.
func (g *Gateway) UsesRelay() bool { return g.opts.RelayURL != "" }

// RelayHost is the public relay hostname, or "" without a relay.
func (g *Gateway) RelayHost() string {
	if g.opts.RelayURL == "" {
		return ""
	}
	origin, err := mobilerelay.Origin(g.opts.RelayURL)
	if err != nil {
		return ""
	}
	return origin.Hostname()
}

// tailnet is the shared address range (RFC 6598) Tailscale assigns from.
// An address in it is only an address of this computer that phones may
// name in the Host header; it says nothing about who connects, so it grants
// no trust. Pairing, cookies and CSRF still authenticate every phone.
var tailnet = netip.MustParsePrefix("100.64.0.0/10")

// interfaceAddresses returns this computer's own IPv4 addresses that a phone
// may use: private and link-local LAN addresses, and tailnet addresses.
func interfaceAddresses() (lan, tail []string) {
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok {
				lan, tail = classifyAddress(ipn.IP, lan, tail)
			}
		}
	}
	return lan, tail
}

func classifyAddress(ip net.IP, lan, tail []string) ([]string, []string) {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok || !addr.Unmap().Is4() {
		return lan, tail
	}
	addr = addr.Unmap()
	switch {
	case tailnet.Contains(addr):
		tail = append(tail, addr.String())
	case addr.IsPrivate() || addr.IsLinkLocalUnicast():
		lan = append(lan, addr.String())
	}
	return lan, tail
}

// validHostnames normalizes configured hostnames and rejects anything that
// is not one exact DNS name: wildcards, ports, schemes, paths and IP
// literals. Each name is matched exactly against the Host header.
func validHostnames(in []string) ([]string, error) {
	var out []string
	for _, raw := range in {
		h := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(raw), "."))
		if err := checkHostname(h); err != nil {
			return nil, fmt.Errorf("invalid phone gateway hostname %q: %w", raw, err)
		}
		if h != "localhost" && !slices.Contains(out, h) {
			out = append(out, h)
		}
	}
	return out, nil
}

func checkHostname(h string) error {
	if h == "" || len(h) > 253 {
		return errors.New("must be 1-253 characters")
	}
	if _, err := netip.ParseAddr(h); err == nil {
		return errors.New("must be a hostname; this computer's own addresses are added automatically")
	}
	for label := range strings.SplitSeq(h, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return errors.New("each dot-separated label must be 1-63 characters and not start or end with '-'")
		}
		for _, c := range label {
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
				return fmt.Errorf("character %q is not allowed; wildcards, ports and URLs are not accepted", c)
			}
		}
	}
	return nil
}

// pump folds app events into pending state and the event ring.
func (g *Gateway) pump(ctx context.Context, events <-chan any) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			g.ingest(ev)
		}
	}
}

// Ingest is exported for tests and adapters feeding events directly.
func (g *Gateway) Ingest(payload any) { g.ingest(payload) }

func (g *Gateway) ingest(payload any) {
	switch e := payload.(type) {
	case pubsub.Event[permission.PermissionRequest]:
		g.mu.Lock()
		g.permissions[e.Payload.ID] = e.Payload
		g.byToolCall[e.Payload.ToolCallID] = e.Payload.ID
		g.mu.Unlock()
		g.ring.add("permission", e.Payload.SessionID, permissionView(e.Payload))
	case pubsub.Event[permission.PermissionNotification]:
		g.mu.Lock()
		id := g.byToolCall[e.Payload.ToolCallID]
		req, ok := g.permissions[id]
		if ok && (e.Payload.Granted || e.Payload.Denied) {
			delete(g.permissions, id)
			delete(g.byToolCall, e.Payload.ToolCallID)
		}
		g.mu.Unlock()
		if ok && (e.Payload.Granted || e.Payload.Denied) {
			g.ring.add("permission_resolved", req.SessionID, map[string]any{"id": id, "granted": e.Payload.Granted})
		}
	case pubsub.Event[question.Request]:
		g.mu.Lock()
		g.questions[e.Payload.ID] = e.Payload
		g.mu.Unlock()
		g.ring.add("question", e.Payload.SessionID, e.Payload)
	case pubsub.Event[question.Notification]:
		g.mu.Lock()
		req, ok := g.questions[e.Payload.BatchID]
		delete(g.questions, e.Payload.BatchID)
		g.mu.Unlock()
		if ok {
			g.ring.add("question_resolved", req.SessionID, map[string]any{"id": e.Payload.BatchID})
		}
	case pubsub.Event[session.Session]:
		if e.Payload.ParentSessionID == "" {
			g.ring.add("session", e.Payload.ID, sessionSummary(e.Payload, g.opts.Backend.AgentIsSessionBusy(e.Payload.ID)))
		}
	case pubsub.Event[message.Message]:
		m := e.Payload
		if m.Role == message.Tool && len(messageImages(m)) == 0 {
			return
		}
		g.ring.add("message", m.SessionID, messageView(m))
	}
}

// watchBusy turns run start/stop into events; the agent does not publish
// a busy flag, so it is sampled.
func (g *Gateway) watchBusy(ctx context.Context) {
	t := time.NewTicker(1500 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		sessions, err := g.opts.Backend.ListSessions(ctx)
		if err != nil {
			continue
		}
		for i, s := range sessions {
			if i >= 50 || s.ParentSessionID != "" {
				continue
			}
			busy := g.opts.Backend.AgentIsSessionBusy(s.ID)
			g.mu.Lock()
			prev, seen := g.busy[s.ID]
			g.busy[s.ID] = busy
			g.mu.Unlock()
			if !seen || prev != busy {
				g.ring.add("busy", s.ID, map[string]any{"busy": busy})
			}
		}
	}
}

func sessionSummary(s session.Session, busy bool) map[string]any {
	done, total := 0, len(s.Todos)
	for _, t := range s.Todos {
		if t.Status == session.TodoStatusCompleted {
			done++
		}
	}
	return map[string]any{"id": s.ID, "title": s.Title, "busy": busy, "created_at": s.CreatedAt, "updated_at": s.UpdatedAt, "messages": s.MessageCount, "cost": s.Cost, "todos_done": done, "todos_total": total, "goal": vault.Redact(s.ID, s.Goal)}
}

func messageView(m message.Message) map[string]any {
	text := m.Content().Text
	text = vault.Redact(m.SessionID, text)
	if len(text) > 4000 {
		text = text[:4000] + "…"
	}
	var tools []string
	for _, tc := range m.ToolCalls() {
		tools = append(tools, tc.Name)
	}
	v := map[string]any{"id": m.ID, "role": string(m.Role), "text": text, "tools": tools, "tool_calls": toolCallViews(m), "created_at": m.CreatedAt, "updated_at": m.UpdatedAt, "finished": m.IsFinished()}
	v["images"] = messageImages(m)
	if m.Role == message.Tool {
		v["text"] = ""
	}
	if m.Role == message.Assistant && m.Model != "" {
		v["model"] = m.Model
	}
	if m.FinishReason() == message.FinishReasonError {
		v["error"] = true
	}
	return v
}

func permissionView(p permission.PermissionRequest) map[string]any {
	v := map[string]any{"id": p.ID, "session_id": p.SessionID, "tool_call_id": p.ToolCallID, "tool": p.ToolName, "action": p.Action, "description": permission.RedactForDisplay(p.Description), "path": p.Path}
	permissionDetails(p, v)
	return v
}

// allowedHost defends against DNS rebinding: requests must address one of
// the gateway's own host:port pairs.
func (g *Gateway) allowedHost(host string) bool { return slices.Contains(g.hosts, host) }

func (g *Gateway) rateLimited(key string, perMinute int) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	cutoff := time.Now().Add(-time.Minute)
	kept := g.limits[key][:0]
	for _, t := range g.limits[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= perMinute {
		g.limits[key] = kept
		return true
	}
	g.limits[key] = append(kept, time.Now())
	return false
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func apiError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": msg}})
}

func clientIP(r *http.Request) string {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return h
}

func strictJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		apiError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return false
	}
	return true
}

// idempotent replays the stored response for a repeated Idempotency-Key
// from the same device, so a double tap or a retry after a dropped
// connection never repeats a side effect.
func (g *Gateway) idempotent(w http.ResponseWriter, r *http.Request, d Device, fn func() (int, any)) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" || len(key) > 80 {
		apiError(w, http.StatusBadRequest, "idempotency_key_required", "state-changing requests need an Idempotency-Key header")
		return
	}
	full := d.ID + "|" + r.URL.Path + "|" + key
	g.mu.Lock()
	for k, e := range g.idem {
		if time.Since(e.at) > 10*time.Minute {
			delete(g.idem, k)
		}
	}
	if e, ok := g.idem[full]; ok {
		g.mu.Unlock()
		w.Header().Set("Idempotent-Replay", "true")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(e.status)
		_, _ = w.Write(e.body)
		return
	}
	// Reserve the key before running, so a concurrent duplicate waits on
	// the stored result instead of running the effect a second time.
	g.idem[full] = idemEntry{at: time.Now(), status: http.StatusConflict, body: []byte(`{"error":{"code":"in_progress","message":"the same request is already being processed"}}`)}
	g.mu.Unlock()
	status, body := fn()
	data, _ := json.Marshal(body)
	g.mu.Lock()
	g.idem[full] = idemEntry{at: time.Now(), status: status, body: data}
	g.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}
