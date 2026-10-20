package webui

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/client"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/proto"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/server"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/webui/webtest"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// harness runs the real stack a browser talks to: an Atlas server with
// its backend and agent engine, a gateway in front of it, and a scripted
// model behind it. A second client.Client plays the terminal UI in
// client/server mode.
type harness struct {
	t        *testing.T
	llm      *webtest.FakeLLM
	server   *httptest.Server
	srv      *server.Server
	gw       *Gateway
	base     string
	upstream *client.Client
	project  string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	llm := webtest.NewFakeLLM()
	llm.SlowDelay = 60 * time.Millisecond
	t.Cleanup(llm.Close)
	env, err := webtest.NewEnvironment(t.TempDir(), llm.URL)
	require.NoError(t, err)
	for k, v := range env.Env {
		t.Setenv(k, v)
	}

	srv := server.NewServer(nil, "tcp", "127.0.0.1:0")
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(hs.Close)

	project := t.TempDir()
	upstream, err := client.NewClient(project, "tcp", hs.Listener.Addr().String())
	require.NoError(t, err)

	gw, err := New(Options{
		Upstream:      upstream,
		Assets:        fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>Atlas</title>")}, "assets/app.js": {Data: []byte("console.log(1)")}},
		ServerAddress: "tcp://" + hs.Listener.Addr().String(),
		Version:       "test",
	})
	require.NoError(t, err)
	ln, err := gw.Listen("127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = gw.Serve(ln) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = gw.Shutdown(ctx)
	})
	return &harness{t: t, llm: llm, server: hs, srv: srv, gw: gw, base: "http://127.0.0.1:" + gw.Port(), upstream: upstream, project: project}
}

// cli returns a client that behaves like the terminal UI: its own client
// ID, joined to the same workspace, holding its own event stream.
func (h *harness) cli(ctx context.Context, path string) (*client.Client, *proto.Workspace, <-chan any) {
	h.t.Helper()
	c, err := client.NewClient(path, "tcp", h.server.Listener.Addr().String())
	require.NoError(h.t, err)
	ws, err := c.CreateWorkspace(ctx, proto.Workspace{Path: path, ClientID: c.ClientID()})
	require.NoError(h.t, err)
	events, err := c.SubscribeEvents(ctx, ws.ID)
	require.NoError(h.t, err)
	h.t.Cleanup(func() { _ = c.RetireClient(context.Background()) })
	return c, ws, events
}

// browser is a signed-in browser tab: cookie jar, CSRF token, Origin.
type browser struct {
	t    *testing.T
	h    *harness
	http *http.Client
	csrf string
}

func (h *harness) signIn() *browser {
	h.t.Helper()
	jar, err := cookiejar.New(nil)
	require.NoError(h.t, err)
	b := &browser{t: h.t, h: h, http: &http.Client{Jar: jar, Timeout: 2 * time.Minute}}
	rsp, err := b.http.Get(h.gw.LaunchURL())
	require.NoError(h.t, err)
	rsp.Body.Close()
	require.Equal(h.t, http.StatusOK, rsp.StatusCode)
	var boot struct {
		CSRF string `json:"csrf"`
	}
	require.Equal(h.t, http.StatusOK, b.json(http.MethodGet, "/api/bootstrap", nil, &boot))
	require.NotEmpty(h.t, boot.CSRF)
	b.csrf = boot.CSRF
	return b
}

func (b *browser) request(method, path string, body any) *http.Response {
	b.t.Helper()
	var r io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(b.t, err)
		r = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, b.h.base+path, r)
	require.NoError(b.t, err)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if method != http.MethodGet {
		req.Header.Set("Origin", b.h.base)
		req.Header.Set(csrfHeader, b.csrf)
	}
	rsp, err := b.http.Do(req)
	require.NoError(b.t, err)
	return rsp
}

// json performs a request and decodes the answer into out, returning the
// status code.
func (b *browser) json(method, path string, in, out any) int {
	b.t.Helper()
	rsp := b.request(method, path, in)
	defer rsp.Body.Close()
	raw, err := io.ReadAll(rsp.Body)
	require.NoError(b.t, err)
	if out != nil && rsp.StatusCode < 300 && len(raw) > 0 {
		require.NoError(b.t, json.Unmarshal(raw, out), string(raw))
	}
	return rsp.StatusCode
}

func (b *browser) raw(method, path string, in any) (int, string) {
	b.t.Helper()
	rsp := b.request(method, path, in)
	defer rsp.Body.Close()
	raw, _ := io.ReadAll(rsp.Body)
	return rsp.StatusCode, string(raw)
}

func (b *browser) open(path string) workspaceView {
	b.t.Helper()
	var ws workspaceView
	require.Equal(b.t, http.StatusOK, b.json(http.MethodPost, "/api/workspaces", map[string]string{"path": path}, &ws))
	require.NotEmpty(b.t, ws.ID)
	return ws
}

func (b *browser) newSession(wsID string) proto.Session {
	b.t.Helper()
	var s proto.Session
	require.Equal(b.t, http.StatusOK, b.json(http.MethodPost, "/api/workspaces/"+wsID+"/sessions", map[string]string{"title": "Web session"}, &s))
	return s
}

func (b *browser) prompt(wsID, sid, text string) string {
	b.t.Helper()
	runID := uuid.NewString()
	var out map[string]any
	require.Equal(b.t, http.StatusAccepted, b.json(http.MethodPost, "/api/workspaces/"+wsID+"/sessions/"+sid+"/prompt", map[string]any{"prompt": text, "run_id": runID}, &out))
	return runID
}

func (b *browser) messages(wsID, sid string) []proto.Message {
	b.t.Helper()
	var msgs []proto.Message
	require.Equal(b.t, http.StatusOK, b.json(http.MethodGet, "/api/workspaces/"+wsID+"/sessions/"+sid+"/messages", nil, &msgs))
	return msgs
}

// stream is an open event stream of one tab.
type stream struct {
	cancel context.CancelFunc
	mu     sync.Mutex
	events []hubEvent
	hello  map[string]any
	notify chan struct{}
	done   chan struct{}
}

// events opens the tab's event stream, resuming after the given sequence.
func (b *browser) events(wsID string, after uint64) *stream {
	b.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.h.base+"/api/workspaces/"+wsID+"/events", nil)
	require.NoError(b.t, err)
	if after > 0 {
		req.Header.Set("Last-Event-ID", strconv.FormatUint(after, 10))
	}
	rsp, err := b.http.Do(req)
	require.NoError(b.t, err)
	require.Equal(b.t, http.StatusOK, rsp.StatusCode)
	s := &stream{cancel: cancel, notify: make(chan struct{}, 1), done: make(chan struct{})}
	go func() {
		defer close(s.done)
		defer rsp.Body.Close()
		r := bufio.NewReaderSize(rsp.Body, 1<<20)
		for {
			line, err := readLine(r, maxUpstreamEvent)
			if err != nil {
				return
			}
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var ev hubEvent
			if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev) != nil {
				continue
			}
			s.mu.Lock()
			if ev.Type == "hello" {
				_ = json.Unmarshal(ev.Payload, &s.hello)
			} else {
				s.events = append(s.events, ev)
			}
			s.mu.Unlock()
			select {
			case s.notify <- struct{}{}:
			default:
			}
		}
	}()
	b.t.Cleanup(s.close)
	return s
}

func (s *stream) close() {
	s.cancel()
	<-s.done
}

func (s *stream) snapshot() []hubEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]hubEvent(nil), s.events...)
}

// waitFor blocks until an event satisfies match and returns it.
func (s *stream) waitFor(t *testing.T, timeout time.Duration, match func(hubEvent) bool) hubEvent {
	t.Helper()
	deadline := time.After(timeout)
	seen := 0
	for {
		evs := s.snapshot()
		for ; seen < len(evs); seen++ {
			if match(evs[seen]) {
				return evs[seen]
			}
		}
		select {
		case <-s.notify:
		case <-deadline:
			types := make([]string, 0, len(evs))
			for _, e := range evs {
				types = append(types, e.Type)
			}
			t.Fatalf("event not seen within %s; got %v", timeout, types)
		}
	}
}

// innerPayload decodes the payload of a pubsub event relayed by the hub.
func innerPayload[T any](t *testing.T, ev hubEvent) T {
	t.Helper()
	var wrapped struct {
		Payload T `json:"payload"`
	}
	require.NoError(t, json.Unmarshal(ev.Payload, &wrapped))
	return wrapped.Payload
}

func isStatus(sid string, busy bool) func(hubEvent) bool {
	return func(ev hubEvent) bool {
		if ev.Type != "session_status" {
			return false
		}
		var st struct {
			SessionID string `json:"session_id"`
			Busy      bool   `json:"busy"`
		}
		return json.Unmarshal(ev.Payload, &st) == nil && st.SessionID == sid && st.Busy == busy
	}
}

// isRun matches the terminal event of one run.
func isRun(runID string) func(hubEvent) bool {
	return func(ev hubEvent) bool {
		if ev.Type != "run_complete" {
			return false
		}
		var wrapped struct {
			Payload proto.RunComplete `json:"payload"`
		}
		return json.Unmarshal(ev.Payload, &wrapped) == nil && wrapped.Payload.RunID == runID
	}
}

// run sends a prompt and waits until its run has finished and the server
// reports the session idle.
func (b *browser) run(ev *stream, wsID, sid, text string) string {
	b.t.Helper()
	runID := b.prompt(wsID, sid, text)
	ev.waitFor(b.t, 60*time.Second, isRun(runID))
	b.h.waitIdle(wsID, sid, 30*time.Second)
	return runID
}

func isRunComplete(sid string) func(hubEvent) bool {
	return func(ev hubEvent) bool {
		if ev.Type != "run_complete" {
			return false
		}
		var wrapped struct {
			Payload proto.RunComplete `json:"payload"`
		}
		return json.Unmarshal(ev.Payload, &wrapped) == nil && wrapped.Payload.SessionID == sid
	}
}

// waitIdle waits until the server reports the session idle.
func (h *harness) waitIdle(wsID, sid string, timeout time.Duration) {
	h.t.Helper()
	require.Eventually(h.t, func() bool {
		s, err := h.upstream.GetSession(context.Background(), wsID, sid)
		return err == nil && !s.IsBusy
	}, timeout, 50*time.Millisecond)
}

// assistantText joins the text parts of a session's assistant messages.
func assistantText(msgs []proto.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		if m.Role != proto.Assistant {
			continue
		}
		for _, p := range m.Parts {
			if tc, ok := p.(proto.TextContent); ok {
				b.WriteString(tc.Text)
			}
		}
	}
	return b.String()
}

// lastFinish is the finish reason of the newest assistant message.
func lastFinish(msgs []proto.Message) proto.FinishReason {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != proto.Assistant {
			continue
		}
		for _, p := range msgs[i].Parts {
			if f, ok := p.(proto.Finish); ok {
				return f.Reason
			}
		}
	}
	return ""
}

func wsPath(wsID string, rest ...string) string {
	return "/api/workspaces/" + url.PathEscape(wsID) + strings.Join(rest, "")
}
