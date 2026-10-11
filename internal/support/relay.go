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

