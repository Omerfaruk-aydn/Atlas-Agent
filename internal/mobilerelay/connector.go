package mobilerelay

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// ConnectorOptions binds an outbound connection to one in-process handler.
// Dialer is optional and supports trusted private certificate authorities.
type ConnectorOptions struct {
	URL     string
	Token   string
	Handler http.Handler
	Dialer  *websocket.Dialer
}

// Connector reconnects its control channel, never an HTTP operation.
type Connector struct {
	opts      ConnectorOptions
	origin    *url.URL
	cancel    context.CancelFunc
	done      chan struct{}
	connected atomic.Bool
	slots     chan struct{}
}

// Connect requires the first authenticated TLS handshake to succeed.
func Connect(ctx context.Context, opts ConnectorOptions) (*Connector, error) {
	origin, err := Origin(opts.URL)
	if err != nil {
		return nil, err
	}
	key, err := base64.RawURLEncoding.DecodeString(opts.Token)
	if err != nil || len(key) != 32 || len(opts.Token) != 43 || opts.Handler == nil {
		return nil, fmt.Errorf("relay connector requires a private key and a phone handler")
	}
	if opts.Dialer == nil {
		dialer := *websocket.DefaultDialer
		dialer.HandshakeTimeout = 10 * time.Second
		opts.Dialer = &dialer
	}
	ctx, cancel := context.WithCancel(ctx)
	c := &Connector{opts: opts, origin: origin, cancel: cancel, done: make(chan struct{}), slots: make(chan struct{}, maxStreams)}
	peer, err := c.dialControl(ctx)
	if err != nil {
		cancel()
		return nil, err
	}
	c.connected.Store(true)
	go c.run(ctx, peer)
	return c, nil
}

// Connected reports the current control connection state.
func (c *Connector) Connected() bool { return c.connected.Load() }

// Close cancels all active streams and waits for the reconnect loop to stop.
func (c *Connector) Close() error {
	c.cancel()
	<-c.done
	return nil
}

func (c *Connector) dial(ctx context.Context, path string, query url.Values) (*websocket.Conn, error) {
	u := *c.origin
	u.Scheme, u.Path, u.RawQuery = "wss", path, query.Encode()
	conn, response, err := c.opts.Dialer.DialContext(ctx, u.String(), http.Header{"Authorization": {"Bearer " + c.opts.Token}})
	if err != nil && response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if err != nil {
		// Do not include the key, query string or server response in errors.
		return nil, fmt.Errorf("mobile relay TLS connection failed; verify its address, certificate and connector key")
	}
	return conn, nil
}

func (c *Connector) dialControl(ctx context.Context) (*control, error) {
	conn, err := c.dial(ctx, connectPath, nil)
	if err != nil {
		return nil, err
	}
	return newControl(conn), nil
}

func (c *Connector) run(ctx context.Context, peer *control) {
	defer close(c.done)
	defer c.connected.Store(false)
	for {
		c.serve(ctx, peer)
		c.connected.Store(false)
		for delay := time.Second; ; delay = min(delay*2, 30*time.Second) {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			var err error
			peer, err = c.dialControl(ctx)
			if err == nil {
				c.connected.Store(true)
				break
			}
		}
	}
}

func (c *Connector) serve(parent context.Context, peer *control) {
	ctx, cancel := context.WithCancel(parent)
	stop := context.AfterFunc(ctx, peer.close)
	var workers sync.WaitGroup
	defer func() {
		cancel()
		peer.close()
		stop()
		workers.Wait()
	}()
	go peer.heartbeat(ctx)
	for {
		var invite invitation
		if err := peer.conn.ReadJSON(&invite); err != nil {
			return
		}
		id, err := base64.RawURLEncoding.DecodeString(invite.ID)
		if err != nil || len(id) != 24 || len(invite.ID) != 32 {
			return
		}
		select {
		case c.slots <- struct{}{}:
			workers.Go(func() {
				defer func() { <-c.slots }()
				c.serveStream(ctx, invite.ID)
			})
		default:
			// Fail closed rather than allowing an unbounded request queue.
			return
		}
	}
}

func (c *Connector) serveStream(ctx context.Context, id string) {
	ws, err := c.dial(ctx, streamPath, url.Values{"id": {id}})
	if err != nil {
		return
	}
	conn := newStream(ws)
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	defer conn.Close()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != c.origin.Host || !allowedRequest(r) {
			http.Error(w, "invalid phone route", http.StatusMisdirectedRequest)
			return
		}
		if host, _, err := net.SplitHostPort(r.Header.Get("X-Atlas-Relay-Client")); err == nil && net.ParseIP(host) != nil {
			r.RemoteAddr = r.Header.Get("X-Atlas-Relay-Client")
		}
		r.Header.Del("X-Atlas-Relay-Client")
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		c.opts.Handler.ServeHTTP(w, r)
	})
	srv := &http.Server{
		Handler: handler, ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 30 * time.Second, IdleTimeout: 30 * time.Second,
		MaxHeaderBytes: 64 << 10, ErrorLog: log.New(io.Discard, "", 0),
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	_ = srv.Serve(&oneListener{conn: conn})
}
