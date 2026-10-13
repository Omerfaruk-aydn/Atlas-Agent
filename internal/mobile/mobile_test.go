package mobile

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/pubsub"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/question"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/stretchr/testify/require"
)

type fakeBackend struct {
	mu        sync.Mutex
	pending   map[string]bool
	questions map[string]bool
	runs      atomic.Int32
	controls  []string
	revision  string
	paused    bool
	// Handed-off browser/desktop control, as interaction.State reports it.
	interPaused   bool
	interCanceled bool
	interReason   string
	prompts       []string
}

func (f *fakeBackend) ListSessions(context.Context) ([]session.Session, error) {
	return []session.Session{{ID: "s1", Title: "Fix login", CreatedAt: 1, UpdatedAt: 2}, {ID: "child", ParentSessionID: "s1"}}, nil
}

func (f *fakeBackend) GetSession(_ context.Context, id string) (session.Session, error) {
	if id == "s1" {
		return session.Session{ID: "s1", Title: "Fix login"}, nil
	}
	return session.Session{}, fmt.Errorf("not found")
}

func (f *fakeBackend) ListMessages(context.Context, string) ([]message.Message, error) {
	m := message.Message{ID: "m1", Role: message.Assistant, SessionID: "s1"}
	m.AppendContent("working on it")
	return []message.Message{m}, nil
}

func (f *fakeBackend) AgentRun(_ context.Context, _ string, prompt string, _ ...message.Attachment) error {
	f.mu.Lock()
	f.prompts = append(f.prompts, prompt)
	f.mu.Unlock()
	f.runs.Add(1)
	return nil
}
func (f *fakeBackend) AgentIsSessionBusy(string) bool { return false }

func (f *fakeBackend) resolve(p permission.PermissionRequest) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.pending[p.ID] {
		return false
	}
	delete(f.pending, p.ID)
	return true
}

func (f *fakeBackend) PermissionGrant(p permission.PermissionRequest) bool { return f.resolve(p) }

func (f *fakeBackend) PermissionGrantPersistent(p permission.PermissionRequest) bool {
	return f.resolve(p)
}

func (f *fakeBackend) PermissionDeny(p permission.PermissionRequest) bool { return f.resolve(p) }

func (f *fakeBackend) QuestionAnswerRequest(id string, _ []question.Answer) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	ok := f.questions[id]
	delete(f.questions, id)
	return ok
}

func (f *fakeBackend) QuestionCancelRequest(id string) bool { return f.QuestionAnswerRequest(id, nil) }

func (f *fakeBackend) WorkflowSnapshot(context.Context, string) (engineering.WorkflowSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	snap := engineering.WorkflowSnapshot{SessionID: "s1", Revision: f.revision, Paused: f.paused}
	snap.Interactions.Paused, snap.Interactions.Canceled, snap.Interactions.Reason = f.interPaused, f.interCanceled, f.interReason
	return snap, nil
}

func (f *fakeBackend) WorkflowControl(_ context.Context, _ string, c engineering.WorkflowControl) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c.ExpectedRevision != f.revision {
		return fmt.Errorf("revision changed")
	}
	f.controls = append(f.controls, c.Action)
	if c.Action == "interaction_resume" {
		f.interPaused, f.interCanceled, f.interReason = false, false, ""
	} else {
		f.paused = c.Action == "pause" || c.Action == "stop"
	}
	f.revision += "+"
	return nil
}

type phone struct {
	t      *testing.T
	g      *Gateway
	base   string
	client *http.Client
	csrf   string
}

func newGateway(t *testing.T) (*Gateway, *fakeBackend) {
	t.Helper()
	b := &fakeBackend{pending: map[string]bool{}, questions: map[string]bool{}, revision: "r1"}
	g, err := Start(t.Context(), Options{StateDir: t.TempDir(), Listen: "127.0.0.1:0", Backend: b, Language: "tr"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = g.Close() })
	return g, b
}

func newPhone(t *testing.T, g *Gateway) *phone {
	jar, _ := cookiejar.New(nil)
	return &phone{t: t, g: g, base: g.URLs()[1], client: &http.Client{Jar: jar, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}} //nolint:gosec // Test client for a self-signed gateway.
}

// reply is the part of a response the tests inspect; the body is already
// decoded and closed.
type reply struct {
	StatusCode int
	Header     http.Header
}

func (p *phone) do(method, path string, body any, hdr map[string]string) (reply, map[string]any) {
	var r io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		r = bytes.NewReader(data)
	}
	req, _ := http.NewRequestWithContext(p.t.Context(), method, p.base+path, r)
	req.Header.Set("Origin", p.base)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if p.csrf != "" {
		req.Header.Set("X-CSRF-Token", p.csrf)
	}
	for k, v := range hdr {
		if v == "" {
			req.Header.Del(k)
		} else {
			req.Header.Set(k, v)
		}
	}
	resp, err := p.client.Do(req)
	require.NoError(p.t, err)
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return reply{StatusCode: resp.StatusCode, Header: resp.Header}, out
}

func code(out map[string]any) string {
	if e, ok := out["error"].(map[string]any); ok {
		return e["code"].(string)
	}
	return ""
}

func (p *phone) pair(accept bool) string {
	u, _ := p.g.PairingURL()
	nonce := strings.TrimPrefix(u[strings.Index(u, "#"):], "#pair=")
	resp, out := p.do("POST", "/api/pair", map[string]string{"nonce": nonce, "name": "Pixel"}, nil)
	require.Equal(p.t, 200, resp.StatusCode, out)
	var mine *Pending
	for _, pp := range p.g.Auth.PendingList() {
		if pp.ID == out["pending_id"] {
			mine = &pp
		}
	}
	require.NotNil(p.t, mine)
	require.Equal(p.t, out["code"], mine.Code, "phone and desktop must show the same code")
	_, err := p.g.Auth.Decide(mine.ID, accept)
	require.NoError(p.t, err)
	resp, poll := p.do("POST", "/api/pair/poll", map[string]string{"pending_id": out["pending_id"].(string), "poll": out["poll"].(string)}, nil)
	require.Equal(p.t, 200, resp.StatusCode)
	if accept {
		p.csrf = poll["csrf"].(string)
		require.Contains(p.t, resp.Header.Get("Set-Cookie"), "HttpOnly")
		require.Contains(p.t, resp.Header.Get("Set-Cookie"), "Secure")
		require.Contains(p.t, resp.Header.Get("Set-Cookie"), "SameSite=Strict")
	}
	return poll["state"].(string)
}

func TestPairingRequiresDesktopAcceptanceAndSingleUseNonce(t *testing.T) {
	g, _ := newGateway(t)
	p := newPhone(t, g)
	resp, out := p.do("GET", "/api/me", nil, nil)
	require.Equal(t, 401, resp.StatusCode, out)

	require.Equal(t, "rejected", p.pair(false))
	resp, _ = p.do("GET", "/api/me", nil, nil)
	require.Equal(t, 401, resp.StatusCode, "a rejected phone gets no session")

	// A nonce cannot be redeemed twice, and an older QR stops working.
	u1, _ := g.PairingURL()
	u2, _ := g.PairingURL()
	n1 := u1[strings.Index(u1, "=")+1:]
	n2 := u2[strings.Index(u2, "=")+1:]
	_, out = p.do("POST", "/api/pair", map[string]string{"nonce": n1, "name": "x"}, nil)
	require.Equal(t, "pairing_invalid", code(out))
	resp, _ = p.do("POST", "/api/pair", map[string]string{"nonce": n2, "name": "x"}, nil)
	require.Equal(t, 200, resp.StatusCode)
	_, out = p.do("POST", "/api/pair", map[string]string{"nonce": n2, "name": "x"}, nil)
	require.Equal(t, "pairing_invalid", code(out))

	// Expired nonce.
	u3, _ := g.PairingURL()
	g.Auth.now = func() time.Time { return time.Now().Add(NonceTTL + time.Second) }
	_, out = p.do("POST", "/api/pair", map[string]string{"nonce": u3[strings.Index(u3, "=")+1:], "name": "x"}, nil)
	require.Equal(t, "pairing_invalid", code(out))
	g.Auth.now = time.Now

	_, out = p.do("POST", "/api/pair", map[string]string{"nonce": "garbage", "name": "x"}, nil)
	require.Equal(t, "pairing_invalid", code(out))

	p2 := newPhone(t, g)
	require.Equal(t, "accepted", p2.pair(true))
	resp, out = p2.do("GET", "/api/me", nil, nil)
	require.Equal(t, 200, resp.StatusCode)
	require.Equal(t, "tr", out["language"])
}

func TestQRDoesNotCarryACredential(t *testing.T) {
	g, _ := newGateway(t)
	u, _ := g.PairingURL()
	require.Contains(t, u, "#pair=", "the nonce lives in the fragment, which browsers never send")
	lines, err := QRLines(u)
	require.NoError(t, err)
	require.NotEmpty(t, lines)
	p := newPhone(t, g)
	p.pair(true)
	for _, d := range g.Auth.Devices() {
		require.NotContains(t, u, d.SessionHash)
		require.NotContains(t, u, d.CSRF)
	}
}

func TestOriginCSRFAndHostChecks(t *testing.T) {
	g, b := newGateway(t)
	p := newPhone(t, g)
	p.pair(true)
	key := map[string]string{"Idempotency-Key": "k1"}

	_, out := p.do("POST", "/api/sessions/s1/prompt", map[string]string{"text": "hi"}, map[string]string{"Idempotency-Key": "k1", "Origin": "https://evil.example"})
	require.Equal(t, "bad_origin", code(out))
	_, out = p.do("POST", "/api/sessions/s1/prompt", map[string]string{"text": "hi"}, map[string]string{"Idempotency-Key": "k1", "Origin": ""})
	require.Equal(t, "bad_origin", code(out))
	_, out = p.do("POST", "/api/sessions/s1/prompt", map[string]string{"text": "hi"}, map[string]string{"Idempotency-Key": "k1", "X-CSRF-Token": "wrong"})
	require.Equal(t, "bad_csrf", code(out))
	require.Zero(t, b.runs.Load(), "rejected requests must not run anything")

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, p.base+"/api/me", nil)
	req.Host = "attacker.example:443"
	resp, err := p.client.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusMisdirectedRequest, resp.StatusCode, "DNS-rebound hosts are refused")

	resp2, _ := p.do("POST", "/api/sessions/s1/prompt", map[string]string{"text": "hi"}, key)
	require.Equal(t, 202, resp2.StatusCode)
}

func TestDoubleSubmitIsReplayedNotRepeated(t *testing.T) {
	g, b := newGateway(t)
	p := newPhone(t, g)
	p.pair(true)
	h := map[string]string{"Idempotency-Key": "same-tap"}
	r1, _ := p.do("POST", "/api/sessions/s1/prompt", map[string]string{"text": "deploy"}, h)
	r2, _ := p.do("POST", "/api/sessions/s1/prompt", map[string]string{"text": "deploy"}, h)
	require.Equal(t, 202, r1.StatusCode)
	require.Equal(t, 202, r2.StatusCode)
	require.Equal(t, "true", r2.Header.Get("Idempotent-Replay"))
	require.Eventually(t, func() bool { return b.runs.Load() == 1 }, 2*time.Second, 20*time.Millisecond)
	time.Sleep(100 * time.Millisecond)
	require.EqualValues(t, 1, b.runs.Load(), "the prompt must run exactly once")
	_, out := p.do("POST", "/api/sessions/s1/prompt", map[string]string{"text": "x"}, map[string]string{"Idempotency-Key": ""})
	require.Equal(t, "idempotency_key_required", code(out))
}

func TestPermissionAnswerMustMatchTheLivePendingRequest(t *testing.T) {
	g, b := newGateway(t)
	p := newPhone(t, g)
	p.pair(true)
	req := permission.PermissionRequest{ID: "perm-1", SessionID: "s1", ToolCallID: "call-1", ToolName: "bash", Action: "execute", Description: "rm -rf build"}
	b.pending["perm-1"] = true
	g.Ingest(pubsub.Event[permission.PermissionRequest]{Payload: req})

	_, out := p.do("POST", "/api/permissions/perm-1", map[string]string{"decision": "allow", "session_id": "other", "tool_call_id": "call-1"}, map[string]string{"Idempotency-Key": "a"})
	require.Equal(t, "stale_request", code(out), "a wrong session id must not resolve the request")

	resp, _ := p.do("POST", "/api/permissions/perm-1", map[string]string{"decision": "deny", "session_id": "s1", "tool_call_id": "call-1"}, map[string]string{"Idempotency-Key": "b"})
	require.Equal(t, 200, resp.StatusCode)
	_, out = p.do("POST", "/api/permissions/perm-1", map[string]string{"decision": "allow", "session_id": "s1", "tool_call_id": "call-1"}, map[string]string{"Idempotency-Key": "c"})
	require.Equal(t, "stale_request", code(out), "a second answer is refused")

	// Answered on the computer first: the phone's late answer is stale.
	req2 := req
	req2.ID, req2.ToolCallID = "perm-2", "call-2"
	b.pending["perm-2"] = true
	g.Ingest(pubsub.Event[permission.PermissionRequest]{Payload: req2})
	b.resolve(req2)
	g.Ingest(pubsub.Event[permission.PermissionNotification]{Payload: permission.PermissionNotification{ToolCallID: "call-2", Granted: true}})
	_, out = p.do("POST", "/api/permissions/perm-2", map[string]string{"decision": "allow", "session_id": "s1", "tool_call_id": "call-2"}, map[string]string{"Idempotency-Key": "d"})
	require.Equal(t, "stale_request", code(out))
}

func TestQuestionAnswerValidation(t *testing.T) {
	g, b := newGateway(t)
	p := newPhone(t, g)
	p.pair(true)
	b.questions["q1"] = true
	g.Ingest(pubsub.Event[question.Request]{Payload: question.Request{ID: "q1", SessionID: "s1", Questions: []question.Question{{ID: "a", Type: question.TypeFreeText, Text: "Name?", Description: "d"}}}})
	_, out := p.do("POST", "/api/questions/q1", map[string]any{"session_id": "s1", "answers": []map[string]string{{"question_id": "zzz"}}}, map[string]string{"Idempotency-Key": "q-a"})
	require.Equal(t, "invalid_answer", code(out))
	resp, _ := p.do("POST", "/api/questions/q1", map[string]any{"session_id": "s1", "answers": []map[string]string{{"question_id": "a", "fill_in_text": "Ada"}}}, map[string]string{"Idempotency-Key": "q-b"})
	require.Equal(t, 200, resp.StatusCode)
	_, out = p.do("POST", "/api/questions/q1", map[string]any{"session_id": "s1", "cancel": true}, map[string]string{"Idempotency-Key": "q-c"})
	require.Equal(t, "stale_request", code(out))
}

func TestControlRequiresCurrentRevision(t *testing.T) {
	g, b := newGateway(t)
	p := newPhone(t, g)
	p.pair(true)
	_, out := p.do("POST", "/api/sessions/s1/control", map[string]string{"action": "pause", "revision": "old"}, map[string]string{"Idempotency-Key": "c1"})
	require.Equal(t, "stale_revision", code(out))
	resp, _ := p.do("POST", "/api/sessions/s1/control", map[string]string{"action": "pause", "revision": "r1"}, map[string]string{"Idempotency-Key": "c2"})
	require.Equal(t, 200, resp.StatusCode)
	require.Equal(t, []string{"pause"}, b.controls)
}

func TestRevokedPhoneLosesAccessImmediately(t *testing.T) {
	g, _ := newGateway(t)
	p := newPhone(t, g)
	p.pair(true)
	d := g.Auth.Devices()[0]
	require.NoError(t, g.Auth.Revoke(d.ID))
	resp, _ := p.do("GET", "/api/sessions", nil, nil)
	require.Equal(t, 401, resp.StatusCode)
}

func TestSessionLifetimes(t *testing.T) {
	g, _ := newGateway(t)
	p := newPhone(t, g)
	p.pair(true)
	g.Auth.now = func() time.Time { return time.Now().Add(SessionIdle + time.Hour) }
	resp, _ := p.do("GET", "/api/me", nil, nil)
	require.Equal(t, 401, resp.StatusCode, "an idle session expires")
}

func TestUploadsAreTypeCheckedByContent(t *testing.T) {
	g, _ := newGateway(t)
	p := newPhone(t, g)
	p.pair(true)
	send := func(name string, data []byte) map[string]any {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		fw, _ := mw.CreateFormFile("file", name)
		_, _ = fw.Write(data)
		mw.Close()
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, p.base+"/api/sessions/s1/attachments", &buf)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.Header.Set("Origin", p.base)
		req.Header.Set("X-CSRF-Token", p.csrf)
		req.Header.Set("Idempotency-Key", name)
		resp, err := p.client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return out
	}
	out := send("evil.png", []byte("MZ\x90\x00 executable pretending to be png"))
	require.Equal(t, "unsupported_type", code(out))
	out = send("note.txt", []byte("hello from the phone"))
	require.Equal(t, "text/plain", out["type"])
	_, prompt := p.do("POST", "/api/sessions/s1/prompt", map[string]any{"text": "see file", "attachment_ids": []string{"up-forged"}}, map[string]string{"Idempotency-Key": "u1"})
	require.Equal(t, "unknown_attachment", code(prompt))
}

func TestEventsReplayInOrderAndSignalGaps(t *testing.T) {
	t.Parallel()
	r := newRing(3)
	for i := range 5 {
		r.add("message", "s1", i)
	}
	events, gap, last := r.since(3)
	require.False(t, gap)
	require.EqualValues(t, 5, last)
	require.Len(t, events, 2)
	require.EqualValues(t, 4, events[0].Seq)
	_, gap, _ = r.since(0)
	require.True(t, gap, "evicted events force a resync instead of a silent hole")
	_, gap, _ = r.since(99)
	require.True(t, gap, "a sequence from an earlier gateway run forces a resync")
}

func TestSSEStreamDeliversEventsWithIDs(t *testing.T) {
	g, _ := newGateway(t)
	p := newPhone(t, g)
	p.pair(true)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", p.base+"/api/events?since=0", nil)
	resp, err := p.client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	g.Ingest(pubsub.Event[session.Session]{Payload: session.Session{ID: "s1", Title: "Fix login"}})
	sc := bufio.NewScanner(resp.Body)
	var id string
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "id: ") {
			id = strings.TrimPrefix(line, "id: ")
		}
		if strings.HasPrefix(line, "data: ") && id != "" {
			require.Contains(t, line, `"type":"session"`)
			break
		}
	}
	require.NotEmpty(t, id)
}

func TestNonLoopbackRequiresOptIn(t *testing.T) {
	t.Parallel()
	_, err := Start(t.Context(), Options{StateDir: t.TempDir(), Listen: "0.0.0.0:0", Backend: &fakeBackend{}})
	require.Error(t, err)
}

var _ = url.Values{}
