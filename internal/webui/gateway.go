// Package webui serves the Atlas web client and the browser-facing API
// it talks to.
//
// The gateway is a client of the shared Atlas server, exactly like the
// terminal UI in client/server mode: it never runs an agent, holds a
// provider connection, or opens the session database itself. Every
// operation is relayed to the server's /v1 API, so a session started in
// the browser runs in the same agent, under the same permission and
// question services, as one started in the terminal.
//
// What the gateway adds is what a browser needs and the server API was
// not built for: a loopback listener with cookie sessions, CSRF and
// Host/Origin checks; responses stripped of credentials and environment
// variables; and an event stream with sequence numbers and replay.
package webui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/proto"
)

// Upstream is the Atlas server connection the gateway relays to.
// *client.Client implements it.
type Upstream interface {
	Do(ctx context.Context, method, path string, query url.Values, body io.Reader, headers http.Header) (*http.Response, error)
	ClientID() string
}

// Project is a directory the user has worked in before.
type Project struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	LastUsed int64  `json:"last_used,omitempty"`
}

// Options configures a [Gateway].
type Options struct {
	Upstream Upstream
	// Assets is the built web client. Nil uses the embedded build.
	Assets fs.FS
	// ServerAddress describes the Atlas server for display.
	ServerAddress string
	// Version is this binary's version.
	Version string
	// DevOrigins are extra origins allowed to call the API, such as a
	// Vite development server proxying to the gateway.
	DevOrigins []string
	// Env is passed to workspaces the gateway creates, the way the
	// terminal client passes its own environment.
	Env []string
	// RecentProjects lists recently used project directories.
	RecentProjects func() []Project
	// Reconnect, when set, is called after the server connection fails so
	// the caller can bring a stopped server back.
	Reconnect func(ctx context.Context) error
	Logger    *slog.Logger
}

// Gateway serves the web client.
type Gateway struct {
	opts      Options
	auth      *auth
	assets    fs.FS
	logger    *slog.Logger
	instance  string
	startedAt time.Time

	ctx    context.Context
	cancel context.CancelFunc

	mu      sync.Mutex
	hubs    map[string]*hub
	aliases map[string]string
	opening map[string]*openCall
	hosts   []string
	port    string
	srv     *http.Server

	controlSecret string
}

type openCall struct {
	done chan struct{}
	ws   *hub
	err  error
}

// ErrAssetsMissing is returned when the binary was built without the web
// client. It is never papered over with an empty page.
var ErrAssetsMissing = errors.New("the web client is not built into this binary: run `task web:build` (or `npm --prefix web run build`) and rebuild")

// New creates a gateway. It fails with [ErrAssetsMissing] when no web
// client build is available.
func New(opts Options) (*Gateway, error) {
	if opts.Upstream == nil {
		return nil, errors.New("webui: an upstream server connection is required")
	}
	assets := opts.Assets
	if assets == nil {
		var err error
		assets, err = EmbeddedAssets()
		if err != nil {
			return nil, err
		}
	}
	if _, err := fs.Stat(assets, "index.html"); err != nil {
		return nil, ErrAssetsMissing
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Gateway{
		opts:      opts,
		auth:      newAuth(),
		assets:    assets,
		logger:    logger,
		instance:  randomToken(9),
		startedAt: time.Now(),
		ctx:       ctx,
		cancel:    cancel,
		hubs:      make(map[string]*hub),
		aliases:   make(map[string]string),
		opening:   make(map[string]*openCall),
	}, nil
}

// Listen binds a loopback address. Anything else is refused: exposing the
// gateway beyond this computer is not something it does by default, and
// remote access already has its own path (/rc).
func (g *Gateway) Listen(addr string) (net.Listener, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, fmt.Errorf("refusing to listen on %s: the web UI only listens on loopback addresses", addr)
	}
	var lc net.ListenConfig
	ln, err := lc.Listen(g.ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	g.setListener(ln)
	return ln, nil
}

// setListener records the bound port and the Host values requests may
// carry.
func (g *Gateway) setListener(ln net.Listener) {
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	g.mu.Lock()
	defer g.mu.Unlock()
	g.port = port
	g.hosts = []string{"127.0.0.1:" + port, "localhost:" + port, "[::1]:" + port}
}

// Port returns the bound port.
func (g *Gateway) Port() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.port
}

// Serve serves the gateway on ln until Shutdown.
func (g *Gateway) Serve(ln net.Listener) error {
	if g.Port() == "" {
		g.setListener(ln)
	}
	srv := &http.Server{
		Handler:           g.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    64 << 10,
	}
	g.mu.Lock()
	g.srv = srv
	g.mu.Unlock()
	err := srv.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// LaunchURL mints a one-time sign-in link.
func (g *Gateway) LaunchURL() string {
	return fmt.Sprintf("http://127.0.0.1:%s/auth/launch?code=%s", g.Port(), url.QueryEscape(g.auth.newLaunchCode()))
}

// Shutdown stops serving and releases the gateway's claims on every
// workspace. Workspaces other clients still hold stay up with their runs;
// the server itself is never asked to stop.
func (g *Gateway) Shutdown(ctx context.Context) error {
	g.mu.Lock()
	srv := g.srv
	g.mu.Unlock()
	var err error
	if srv != nil {
		err = srv.Shutdown(ctx)
	}
	g.cancel()
	retireCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if rsp, rerr := g.opts.Upstream.Do(retireCtx, http.MethodDelete, "/clients/"+g.opts.Upstream.ClientID(), nil, nil, nil); rerr == nil {
		rsp.Body.Close()
	} else if !errors.Is(rerr, context.Canceled) {
		g.logger.Warn("Failed to release web workspaces", "error", rerr)
	}
	return err
}

// hubFor resolves a workspace ID the browser holds, following
// replacements, to its hub.
func (g *Gateway) hubFor(id string) *hub {
	g.mu.Lock()
	defer g.mu.Unlock()
	for range 8 {
		if h, ok := g.hubs[id]; ok {
			return h
		}
		next, ok := g.aliases[id]
		if !ok {
			return nil
		}
		id = next
	}
	return nil
}

func (g *Gateway) rekey(oldID, newID string, h *hub) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.hubs, oldID)
	g.hubs[newID] = h
	g.aliases[oldID] = newID
}

// openWorkspace opens (or reuses) the server workspace for path and keeps
// a stream claim on it. Concurrent opens of one path share a single call.
func (g *Gateway) openWorkspace(ctx context.Context, path string) (*hub, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("%s is not a directory on the computer running Atlas", abs)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is a file, not a project directory", abs)
	}

	g.mu.Lock()
	for _, h := range g.hubs {
		if samePath(h.path, abs) {
			g.mu.Unlock()
			return h, nil
		}
	}
	if call, ok := g.opening[abs]; ok {
		g.mu.Unlock()
		<-call.done
		return call.ws, call.err
	}
	call := &openCall{done: make(chan struct{})}
	g.opening[abs] = call
	g.mu.Unlock()

	call.ws, call.err = g.openWorkspaceSlow(ctx, abs)
	g.mu.Lock()
	delete(g.opening, abs)
	g.mu.Unlock()
	close(call.done)
	return call.ws, call.err
}

func (g *Gateway) openWorkspaceSlow(ctx context.Context, abs string) (*hub, error) {
	ws, err := g.createUpstream(ctx, abs)
	if err != nil {
		return nil, err
	}
	g.mu.Lock()
	if h, ok := g.hubs[ws.ID]; ok {
		g.mu.Unlock()
		return h, nil
	}
	h := newHub(g, ws.ID, abs)
	g.hubs[ws.ID] = h
	g.mu.Unlock()
	go h.run()

	// The create left a hold that lapses unless a stream attaches; wait
	// for ours so the workspace cannot slip away before the first tab
	// connects.
	select {
	case <-h.firstConn:
	case <-time.After(15 * time.Second):
		g.logger.Warn("Web workspace stream slow to attach", "workspace", ws.ID)
	case <-ctx.Done():
	}
	return h, nil
}

// createUpstream creates or joins the server workspace for path and makes
// sure its agent is ready. The agent is initialized only when the server
// reports it is not: initializing replaces the coordinator, which would
// orphan a run another client has in flight.
func (g *Gateway) createUpstream(ctx context.Context, path string) (proto.Workspace, error) {
	var ws proto.Workspace
	req := proto.Workspace{
		Path:     path,
		ClientID: g.opts.Upstream.ClientID(),
		Version:  g.opts.Version,
		Env:      g.opts.Env,
	}
	if err := g.sendJSON(ctx, http.MethodPost, "/workspaces", nil, req, &ws); err != nil {
		return proto.Workspace{}, err
	}
	if ws.Config != nil && ws.Config.IsConfigured() {
		var info proto.AgentInfo
		if err := g.getJSON(ctx, "/workspaces/"+url.PathEscape(ws.ID)+"/agent", nil, &info); err == nil && !info.IsReady {
			if err := g.sendJSON(ctx, http.MethodPost, "/workspaces/"+url.PathEscape(ws.ID)+"/agent/init", nil, proto.AgentInitRequest{Interactive: true}, nil); err != nil {
				g.logger.Warn("Failed to initialize agent for web workspace", "workspace", ws.ID, "error", err)
			}
		}
	}
	return ws, nil
}

// closeWorkspace drops the gateway's claim on a workspace. Other clients'
// claims, and their runs, are unaffected.
func (g *Gateway) closeWorkspace(id string) bool {
	h := g.hubFor(id)
	if h == nil {
		return false
	}
	g.mu.Lock()
	for k, v := range g.hubs {
		if v == h {
			delete(g.hubs, k)
		}
	}
	g.mu.Unlock()
	h.cancel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	q := url.Values{"client_id": []string{g.opts.Upstream.ClientID()}}
	if rsp, err := g.opts.Upstream.Do(ctx, http.MethodDelete, "/workspaces/"+url.PathEscape(h.workspaceID()), q, nil, nil); err == nil {
		rsp.Body.Close()
	}
	return true
}

func (g *Gateway) openHubs() []*hub {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]*hub, 0, len(g.hubs))
	for _, h := range g.hubs {
		out = append(out, h)
	}
	return out
}

func samePath(a, b string) bool {
	ra, err := filepath.EvalSymlinks(a)
	if err != nil {
		ra = filepath.Clean(a)
	}
	rb, err := filepath.EvalSymlinks(b)
	if err != nil {
		rb = filepath.Clean(b)
	}
	if os.PathSeparator == '\\' {
		return equalFoldPath(ra, rb)
	}
	return ra == rb
}

// upstreamError is a non-2xx answer from the server.
type upstreamError struct {
	Status  int
	Message string
}

func (e *upstreamError) Error() string { return e.Message }

// getJSON and sendJSON call the server and decode its JSON answer.
func (g *Gateway) getJSON(ctx context.Context, path string, query url.Values, out any) error {
	return g.sendJSON(ctx, http.MethodGet, path, query, nil, out)
}

func (g *Gateway) sendJSON(ctx context.Context, method, path string, query url.Values, in, out any) error {
	var body io.Reader
	headers := http.Header{}
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytesReader(raw)
		headers.Set("Content-Type", "application/json")
	}
	rsp, err := g.opts.Upstream.Do(ctx, method, path, query, body, headers)
	if err != nil {
		return &upstreamError{Status: http.StatusBadGateway, Message: "the Atlas server is unreachable"}
	}
	defer rsp.Body.Close()
	if rsp.StatusCode < 200 || rsp.StatusCode > 299 {
		return &upstreamError{Status: rsp.StatusCode, Message: upstreamMessage(rsp)}
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, rsp.Body)
		return nil
	}
	return json.NewDecoder(rsp.Body).Decode(out)
}

func upstreamMessage(rsp *http.Response) string {
	raw, _ := io.ReadAll(io.LimitReader(rsp.Body, 64<<10))
	var e proto.Error
	if json.Unmarshal(raw, &e) == nil && e.Message != "" {
		return e.Message
	}
	if len(raw) > 0 && len(raw) < 300 {
		return string(raw)
	}
	return http.StatusText(rsp.StatusCode)
}

// OpenedWorkspace identifies a workspace the gateway holds open.
type OpenedWorkspace struct {
	ID   string
	Path string
}

// OpenWorkspace opens the project at path, as the browser's "open project"
// does, and keeps it open until Shutdown.
func (g *Gateway) OpenWorkspace(ctx context.Context, path string) (OpenedWorkspace, error) {
	h, err := g.openWorkspace(ctx, path)
	if err != nil {
		return OpenedWorkspace{}, err
	}
	return OpenedWorkspace{ID: h.workspaceID(), Path: h.path}, nil
}
