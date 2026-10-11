package support

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/mobilerelay"
	"github.com/gorilla/websocket"
)

// The relay pairs exactly two outbound WebSocket connections that name the
// same slot id and then copies bytes between them. It sees slot ids, which
// identify an offer but not the code's secret part, and opaque end-to-end
// TLS traffic. It never logs slot ids, codes or payloads.
const (
	relayHostPath = "/v1/support/host"
	relayJoinPath = "/v1/support/join"
)

var slotPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// RelayConfig is how an endpoint reaches a relay you operate.
type RelayConfig struct {
	URL   string
	Token string
	// Dialer is optional; tests pass one trusting a private CA.
	Dialer *websocket.Dialer
}

// Configured reports whether internet mode is available.
func (c *RelayConfig) Configured() bool { return c != nil && c.URL != "" }

func (c *RelayConfig) endpoint(path, slot string) (string, error) {
	u, err := url.Parse(c.URL)
	if err != nil || u.Host == "" || u.User != nil {
		return "", Errorf(CodeRelayNotConfigured, "support relay URL %q is not a valid https:// URL", c.URL)
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		// Plain HTTP is only for a relay on this computer behind a proxy.
		if host := u.Hostname(); host != "localhost" && !net.ParseIP(host).IsLoopback() {
			return "", Errorf(CodeRelayNotConfigured, "support relay must use https:// unless it runs on this computer")
		}
		u.Scheme = "ws"
	default:
		return "", Errorf(CodeRelayNotConfigured, "support relay URL must start with https://")
	}
	u.Path = strings.TrimRight(u.Path, "/") + path
	u.RawQuery = url.Values{"slot": {slot}}.Encode()
	return u.String(), nil
}

func (c *RelayConfig) dial(ctx context.Context, path, slot string) (*websocket.Conn, error) {
	endpoint, err := c.endpoint(path, slot)
	if err != nil {
		return nil, err
	}
	dialer := c.Dialer
	if dialer == nil {
		d := *websocket.DefaultDialer
		d.HandshakeTimeout = 15 * time.Second
		dialer = &d
	}
	header := http.Header{}
	if c.Token != "" {
		header.Set("Authorization", "Bearer "+c.Token)
	}
	ws, resp, err := dialer.DialContext(ctx, endpoint, header)
	if err != nil {
		if resp != nil {
			defer resp.Body.Close()
			var body struct {
				Code string `json:"code"`
			}
			_ = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&body)
			switch {
			case resp.StatusCode == http.StatusNotFound && body.Code == "no_host":
				return nil, Errorf(CodePairingInvalid, "no computer is offering support with this code (it may be wrong or expired)")
			case resp.StatusCode == http.StatusTooManyRequests:
				return nil, Errorf(CodePairingRateLimited, "the relay is limiting attempts; wait a minute")
			case resp.StatusCode == http.StatusConflict:
				return nil, Errorf(CodeProtocol, "the relay already has an offer with this code")
			case resp.StatusCode == http.StatusUnauthorized:
				return nil, Errorf(CodeRelayNotConfigured, "the relay refused the access token; check support_relay_token_file")
			}
		}
		// Never echo the URL query (the slot) or the token.
		return nil, Errorf(CodeRemoteOffline, "cannot reach the support relay; check its address, certificate and your connection")
	}
	return ws, nil
}

type relayControl struct {
	Type string `json:"type"`
}

// relayHost registers slot and blocks until a peer joins. It returns the
// spliced byte stream to the joining endpoint.
func relayHost(ctx context.Context, cfg *RelayConfig, slot string) (net.Conn, error) {
	ws, err := cfg.dial(ctx, relayHostPath, slot)
	if err != nil {
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() { _ = ws.Close() })
	defer stop()
	ws.SetReadLimit(4096)
	for {
		var msg relayControl
		if err := ws.ReadJSON(&msg); err != nil {
			_ = ws.Close()
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, Errorf(CodeRemoteOffline, "support relay connection ended")
		}
		switch msg.Type {
		case "registered":
			continue
		case "paired":
			if err := ws.WriteJSON(relayControl{Type: "ready"}); err != nil {
				_ = ws.Close()
				return nil, Errorf(CodeRemoteOffline, "support relay connection ended")
			}
			return mobilerelay.NewStreamConn(ws), nil
		default:
			_ = ws.Close()
			return nil, Errorf(CodeProtocol, "unexpected relay message")
		}
	}
}

// relayJoin connects to the offer registered under slot.
func relayJoin(ctx context.Context, cfg *RelayConfig, slot string) (net.Conn, error) {
	ws, err := cfg.dial(ctx, relayJoinPath, slot)
	if err != nil {
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() { _ = ws.Close() })
	defer stop()
	ws.SetReadLimit(4096)
	_ = ws.SetReadDeadline(time.Now().Add(20 * time.Second))
	var msg relayControl
	if err := ws.ReadJSON(&msg); err != nil || msg.Type != "paired" {
		_ = ws.Close()
		return nil, Errorf(CodeRemoteOffline, "the support relay could not connect the two computers")
	}
	_ = ws.SetReadDeadline(time.Time{})
	return mobilerelay.NewStreamConn(ws), nil
}

// RelayServerOptions configures a relay.
type RelayServerOptions struct {
	// Token, when set, must be presented by both endpoints.
	Token string
	// MaxWaiting bounds registered offers.
	MaxWaiting int
	// WaitTimeout bounds how long an offer waits for a peer.
	WaitTimeout time.Duration
	Logger      *slog.Logger
}

// RelayServer is the rendezvous and byte relay.
type RelayServer struct {
	opts      RelayServerOptions
	tokenHash [32]byte
	mu        sync.Mutex
	waiting   map[string]*waitingHost
	limits    map[string][]time.Time
	upgrader  websocket.Upgrader
	pairs     atomic.Int64
	closed    bool
}

type waitingHost struct {
	join chan *websocket.Conn
	done chan struct{}
}

// NewRelayServer builds a relay.
func NewRelayServer(opts RelayServerOptions) *RelayServer {
	if opts.MaxWaiting <= 0 {
		opts.MaxWaiting = 1000
	}
	if opts.WaitTimeout <= 0 {
		opts.WaitTimeout = 35 * time.Minute
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &RelayServer{
		opts: opts, tokenHash: sha256.Sum256([]byte(opts.Token)),
		waiting: map[string]*waitingHost{}, limits: map[string][]time.Time{},
		upgrader: websocket.Upgrader{HandshakeTimeout: 10 * time.Second, CheckOrigin: func(r *http.Request) bool { return r.Header.Get("Origin") == "" }},
	}
}

// Pairs reports how many connections were paired, for monitoring.
func (s *RelayServer) Pairs() int64 { return s.pairs.Load() }

// Waiting reports how many offers wait for a peer.
func (s *RelayServer) Waiting() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.waiting)
}

func relayError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "message": message})
}

// allow is a sliding per-client limit of n events per minute.
func (s *RelayServer) allow(key string, n int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := time.Now().Add(-time.Minute)
	kept := s.limits[key][:0]
	for _, t := range s.limits[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= n {
		s.limits[key] = kept
		return false
	}
	s.limits[key] = append(kept, time.Now())
	return true
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *RelayServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/healthz" {
		relayError(w, http.StatusOK, "ok", "atlas support relay")
		return
	}
	if r.Method != http.MethodGet {
		relayError(w, http.StatusMethodNotAllowed, "method", "GET only")
		return
	}
	if s.opts.Token != "" {
		got := sha256.Sum256([]byte(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")))
		if subtle.ConstantTimeCompare(got[:], s.tokenHash[:]) != 1 {
			relayError(w, http.StatusUnauthorized, "unauthorized", "relay access token required")
			return
		}
	}
	slot := r.URL.Query().Get("slot")
	if !slotPattern.MatchString(slot) {
		relayError(w, http.StatusBadRequest, "invalid_slot", "invalid slot")
		return
	}
	switch r.URL.Path {
	case relayHostPath:
		s.serveHost(w, r, slot)
	case relayJoinPath:
		s.serveJoin(w, r, slot)
	default:
		relayError(w, http.StatusNotFound, "not_found", "unknown path")
	}
}

func (s *RelayServer) serveHost(w http.ResponseWriter, r *http.Request, slot string) {
	if !s.allow("host|"+clientIP(r), 60) {
		relayError(w, http.StatusTooManyRequests, "rate_limited", "too many offers")
		return
	}
	s.mu.Lock()
	if s.closed || len(s.waiting) >= s.opts.MaxWaiting {
		s.mu.Unlock()
		relayError(w, http.StatusServiceUnavailable, "busy", "relay is full")
		return
	}
	if _, taken := s.waiting[slot]; taken {
		s.mu.Unlock()
		relayError(w, http.StatusConflict, "slot_taken", "an offer with this slot is already waiting")
		return
	}
	wh := &waitingHost{join: make(chan *websocket.Conn), done: make(chan struct{})}
	s.waiting[slot] = wh
	s.mu.Unlock()
	release := func() {
		s.mu.Lock()
		if s.waiting[slot] == wh {
			delete(s.waiting, slot)
		}
		s.mu.Unlock()
	}
	ws, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		release()
		return
	}
	defer ws.Close()
	ws.SetReadLimit(4096)
	if err := ws.WriteJSON(relayControl{Type: "registered"}); err != nil {
		release()
		return
	}
	// The host sends nothing until paired; a read error means it left.
	gone := make(chan struct{})
	ready := make(chan struct{})
	go func() {
		defer close(gone)
		var msg relayControl
		if err := ws.ReadJSON(&msg); err == nil && msg.Type == "ready" {
			close(ready)
		}
	}()
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	timeout := time.NewTimer(s.opts.WaitTimeout)
	defer timeout.Stop()
	for {
		select {
		case <-gone:
			release()
			return
		case <-timeout.C:
			release()
			return
		case <-ping.C:
			_ = ws.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second))
		case peer := <-wh.join:
			release()
			defer peer.Close()
			if err := ws.WriteJSON(relayControl{Type: "paired"}); err != nil {
				return
			}
			// The reader exits after "ready" or an error; only "ready"
			// hands the connection over to the splice.
			select {
			case <-gone:
			case <-time.After(10 * time.Second):
				return
			}
			select {
			case <-ready:
			default:
				return
			}
			if err := peer.WriteJSON(relayControl{Type: "paired"}); err != nil {
				return
			}
			s.pairs.Add(1)
			splice(mobilerelay.NewStreamConn(ws), mobilerelay.NewStreamConn(peer))
			return
		}
	}
}

func (s *RelayServer) serveJoin(w http.ResponseWriter, r *http.Request, slot string) {
	if !s.allow("join|"+clientIP(r), 30) {
		relayError(w, http.StatusTooManyRequests, "rate_limited", "too many attempts")
		return
	}
	s.mu.Lock()
	wh := s.waiting[slot]
	if wh != nil {
		// One join per registration; the host registers again for another.
		delete(s.waiting, slot)
	}
	s.mu.Unlock()
	if wh == nil {
		relayError(w, http.StatusNotFound, "no_host", "no offer with this code")
		return
	}
	ws, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	ws.SetReadLimit(64 << 10)
	select {
	case wh.join <- ws:
	case <-time.After(5 * time.Second):
		_ = ws.Close()
	}
}

// splice copies both directions and closes both ends when either ends.
func splice(a, b net.Conn) {
	var once sync.Once
	closeBoth := func() { once.Do(func() { _ = a.Close(); _ = b.Close() }) }
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(a, b); closeBoth(); done <- struct{}{} }()
	go func() { _, _ = io.Copy(b, a); closeBoth(); done <- struct{}{} }()
	<-done
	<-done
}

// Close stops accepting offers.
func (s *RelayServer) Close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
}

// RelaySlotFor derives the relay slot used to resume a session. It is
// different from any code slot and only both endpoints can compute it.
func RelaySlotFor(sessionID, resumeToken string) string {
	sum := sha256.Sum256([]byte("atlas-support-resume-v1|" + sessionID + "|" + resumeToken))
	return hex.EncodeToString(sum[:16])
}
