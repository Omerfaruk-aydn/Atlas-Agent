package mobilerelay

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	connectPath = "/_atlas/connect"
	streamPath  = "/_atlas/stream"
	maxStreams  = 32
	maxBody     = 9 << 20
)

type pendingStream struct {
	ctx  context.Context
	peer *control
	conn chan *streamConn
}

type requestPeerKey struct{}

// relayResponseConnected rejects even a complete HTTP response if the
// request's original control channel disconnected before it arrived. A
// canceled handler can otherwise flush an implicit empty HTTP 200.
func relayResponseConnected(response *http.Response) error {
	peer, _ := response.Request.Context().Value(requestPeerKey{}).(*control)
	if peer == nil {
		return fmt.Errorf("request has no bound desktop connector")
	}
	select {
	case <-peer.done:
		return fmt.Errorf("request's desktop connector disconnected")
	default:
		return nil
	}
}

// Server forwards only the mobile surface to one authenticated connector.
// HTTPS terminates at this trusted server or its loopback reverse proxy.
type Server struct {
	origin    *url.URL
	tokenHash [32]byte
	proxy     *httputil.ReverseProxy
	mu        sync.Mutex
	peer      *control
	pending   map[string]pendingStream
	streams   map[*streamConn]*control
	closed    bool
	slots     chan struct{}
}

// NewServer validates the public origin and an independently generated key.
func NewServer(publicURL, token string) (*Server, error) {
	origin, err := Origin(publicURL)
	if err != nil {
		return nil, err
	}
	key, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(key) != 32 || len(token) != 43 {
		return nil, fmt.Errorf("invalid relay connector key")
	}
	s := &Server{origin: origin, tokenHash: sha256.Sum256([]byte(token)), pending: map[string]pendingStream{}, streams: map[*streamConn]*control{}, slots: make(chan struct{}, maxStreams)}
	s.proxy = &httputil.ReverseProxy{
		Rewrite: func(p *httputil.ProxyRequest) {
			p.Out.URL.Scheme, p.Out.URL.Host = "http", "atlas-mobile.internal"
			p.Out.Host = origin.Host
			for _, header := range []string{"Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto", "Authorization", "Proxy-Authorization"} {
				p.Out.Header.Del(header)
			}
			p.Out.Header.Set("X-Atlas-Relay-Client", p.In.RemoteAddr)
		},
		Transport: &http.Transport{
			DialContext: s.dialStream, DisableKeepAlives: true,
			ResponseHeaderTimeout: 30 * time.Second, MaxResponseHeaderBytes: 64 << 10,
		},
		FlushInterval:  -1,
		ModifyResponse: relayResponseConnected,
		ErrorLog:       log.New(io.Discard, "", 0),
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			// Never retry an uncertain mutation, including one with an
			// idempotency header. A reconnect only permits new requests.
			http.Error(w, "Atlas disconnected; refresh to inspect the current state before retrying", http.StatusBadGateway)
		},
	}
	return s, nil
}

// Close disconnects the connector and all active requests.
func (s *Server) Close() error {
	s.mu.Lock()
	s.closed = true
	if s.peer != nil {
		s.peer.close()
	}
	for conn := range s.streams {
		_ = conn.Close()
	}
	s.mu.Unlock()
	return nil
}

// Connected reports whether the desktop control channel is attached.
func (s *Server) Connected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.peer != nil && !s.closed
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if r.Host != s.origin.Host {
		http.Error(w, "unknown host", http.StatusMisdirectedRequest)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/_atlas/") {
		if r.Method != http.MethodGet || r.Header.Get("Origin") != "" || !s.authorized(r) {
			http.Error(w, "connector authentication required", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case connectPath:
			s.attach(w, r)
		case streamPath:
			s.stream(w, r)
		default:
			http.NotFound(w, r)
		}
		return
	}
	if !allowedRequest(r) {
		http.NotFound(w, r)
		return
	}
	s.mu.Lock()
	peer := s.peer
	connected := peer != nil && !s.closed
	s.mu.Unlock()
	if !connected {
		http.Error(w, "Atlas is offline; keep the computer and Atlas running", http.StatusServiceUnavailable)
		return
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		http.Error(w, "too many active phone requests", http.StatusTooManyRequests)
		return
	}
	if r.ContentLength > maxBody {
		http.Error(w, "phone request is too large", http.StatusRequestEntityTooLarge)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	r = r.WithContext(context.WithValue(r.Context(), requestPeerKey{}, peer))
	s.proxy.ServeHTTP(w, r)
}

func allowedRequest(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodPost {
		return false
	}
	return r.URL.Path == "/" || strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/app.js" || r.URL.Path == "/app.css" || r.URL.Path == "/sw.js" || r.URL.Path == "/manifest.webmanifest" || r.URL.Path == "/icon.svg" || r.URL.Path == "/apple-touch-icon.png" || r.URL.Path == "/icon-192.png" || r.URL.Path == "/icon-512.png"
}

func (s *Server) authorized(r *http.Request) bool {
	got := sha256.Sum256([]byte(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")))
	return strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") && subtle.ConstantTimeCompare(got[:], s.tokenHash[:]) == 1
}

func (s *Server) attach(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	if s.closed || s.peer != nil {
		s.mu.Unlock()
		http.Error(w, "a connector is already attached or the relay is stopped", http.StatusConflict)
		return
	}
	ws, err := (&websocket.Upgrader{HandshakeTimeout: 10 * time.Second}).Upgrade(w, r, nil)
	if err != nil {
		s.mu.Unlock()
		return
	}
	peer := newControl(ws)
	s.peer = peer
	s.mu.Unlock()
	defer func() {
		peer.close()
		s.mu.Lock()
		if s.peer == peer {
			s.peer = nil
		}
		for conn, owner := range s.streams {
			if owner == peer {
				_ = conn.Close()
				delete(s.streams, conn)
			}
		}
		s.mu.Unlock()
	}()
	go peer.heartbeat(r.Context())
	// Ping/pong is handled internally. Any data message or read error ends
	// this channel; the desktop does not send application data here.
	_, _, _ = ws.ReadMessage()
}

func (s *Server) dialStream(ctx context.Context, _, _ string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var idBytes [24]byte
	if _, err := rand.Read(idBytes[:]); err != nil {
		return nil, err
	}
	id := base64.RawURLEncoding.EncodeToString(idBytes[:])
	s.mu.Lock()
	peer := s.peer
	bound, _ := ctx.Value(requestPeerKey{}).(*control)
	if peer == nil || s.closed || (bound != nil && bound != peer) {
		s.mu.Unlock()
		return nil, fmt.Errorf("desktop is offline")
	}
	select {
	case <-peer.done:
		s.mu.Unlock()
		return nil, fmt.Errorf("desktop disconnected")
	default:
	}
	p := pendingStream{ctx: ctx, peer: peer, conn: make(chan *streamConn)}
	s.pending[id] = p
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.pending, id); s.mu.Unlock() }()
	if err := peer.send(invitation{ID: id}); err != nil {
		peer.close()
		return nil, err
	}
	select {
	case conn := <-p.conn:
		return conn, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-peer.done:
		return nil, fmt.Errorf("desktop disconnected")
	}
}

func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	s.mu.Lock()
	p, ok := s.pending[id]
	if ok {
		delete(s.pending, id)
	}
	valid := ok && !s.closed && p.peer == s.peer && p.ctx.Err() == nil
	s.mu.Unlock()
	if !valid {
		http.Error(w, "unknown or expired stream", http.StatusNotFound)
		return
	}
	ws, err := (&websocket.Upgrader{HandshakeTimeout: 10 * time.Second}).Upgrade(w, r, nil)
	if err != nil {
		return
	}
	conn := newStream(ws)
	s.mu.Lock()
	if s.closed || s.peer != p.peer {
		s.mu.Unlock()
		_ = conn.Close()
		return
	}
	s.streams[conn] = p.peer
	s.mu.Unlock()
	go func() {
		<-conn.done
		s.mu.Lock()
		delete(s.streams, conn)
		s.mu.Unlock()
	}()
	select {
	case p.conn <- conn:
	case <-p.ctx.Done():
		_ = conn.Close()
	case <-p.peer.done:
		_ = conn.Close()
	}
}
