package twilio_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/voice"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/voice/providers/mock"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/voice/transport/twilio"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

const (
	authToken = "test-auth-token"
	owner     = "+905551112233"
)

func TestSignatureMatchesTwilioExample(t *testing.T) {
	t.Parallel()
	// Example from Twilio's webhook security documentation.
	params := url.Values{
		"CallSid": {"CA1234567890ABCDE"},
		"Caller":  {"+12349013030"},
		"Digits":  {"1234"},
		"From":    {"+12349013030"},
		"To":      {"+18005551212"},
	}
	sig := twilio.Signature("12345", "https://mycompany.com/myapp.php?foo=1&bar=2", params)
	require.Equal(t, "0/KCTR6DLpKmkAf8muzZqo1nDgQ=", sig)
	require.True(t, twilio.ValidSignature("12345", "https://mycompany.com/myapp.php?foo=1&bar=2", params, sig))
	params.Set("Digits", "1235")
	require.False(t, twilio.ValidSignature("12345", "https://mycompany.com/myapp.php?foo=1&bar=2", params, sig))
	require.False(t, twilio.ValidSignature("", "https://x", nil, ""))
}

type runner struct {
	mu   sync.Mutex
	runs []string
}

func (r *runner) NewSession(context.Context, string) (string, error) { return "s1", nil }

func (r *runner) RunTurn(_ context.Context, req voice.TurnRequest) (voice.TurnResult, error) {
	r.mu.Lock()
	r.runs = append(r.runs, req.Text)
	r.mu.Unlock()
	return voice.TurnResult{Text: "Son e-postan Ayşe'den geldi."}, nil
}

// fakeREST records Twilio REST calls.
type fakeREST struct {
	mu    sync.Mutex
	forms []url.Values
	paths []string
	auth  []string
}

func (f *fakeREST) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	user, pass, _ := r.BasicAuth()
	f.mu.Lock()
	f.forms = append(f.forms, r.PostForm)
	f.paths = append(f.paths, r.URL.Path)
	f.auth = append(f.auth, user+":"+pass)
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if strings.HasSuffix(r.URL.Path, "/Calls.json") {
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"sid":"CAoutbound1","status":"queued"}`)
		return
	}
	_, _ = io.WriteString(w, `{"sid":"x","status":"completed"}`)
}

type env struct {
	t       *testing.T
	gw      *voice.Gateway
	tr      *twilio.Transport
	srv     *httptest.Server
	store   *voice.Store
	rest    *fakeREST
	runner  *runner
	baseURL string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	rest := &fakeREST{}
	restSrv := httptest.NewServer(rest)
	t.Cleanup(restSrv.Close)

	cfg := voice.Config{Enabled: true, Owner: voice.OwnerConfig{PhoneNumber: owner}, SilenceTimeoutSeconds: 30, MaxCallSeconds: 120}
	store := voice.NewMemoryStore()
	prov := mock.New()
	rn := &runner{}
	gw, err := voice.NewGateway(voice.GatewayOptions{Config: cfg, Store: store, Recognizer: prov.Recognizer, Synthesizer: prov.Synthesizer, Runner: rn, ThinkingDelay: time.Hour})
	require.NoError(t, err)

	e := &env{t: t, gw: gw, store: store, rest: rest, runner: rn}
	// The public base URL must be known before the server starts, so the
	// handler is swapped in after.
	var handler http.Handler
	e.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
	e.baseURL = "https://" + strings.TrimPrefix(e.srv.URL, "http://")
	client := &twilio.Client{AccountSID: voice.NewSecret("AC1"), AuthToken: voice.NewSecret(authToken), BaseURL: restSrv.URL}
	e.tr = twilio.New(twilio.Options{
		Config:    voice.TwilioConfig{PhoneNumber: "+15550001111", PublicBaseURL: e.baseURL},
		AuthToken: voice.NewSecret(authToken), Client: client, Gateway: gw,
	})
	handler = e.tr.Handler()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		gw.Shutdown(ctx)
		e.srv.Close()
	})
	return e
}

// post sends a webhook signed for the public URL and returns the status
// and body.
func (e *env) post(path string, form url.Values, sign bool) (int, string) {
	e.t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, e.srv.URL+path, strings.NewReader(form.Encode()))
	require.NoError(e.t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if sign {
		req.Header.Set("X-Twilio-Signature", twilio.Signature(authToken, e.baseURL+path, form))
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(e.t, err)
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	require.NoError(e.t, err)
	return resp.StatusCode, string(b)
}

// body returns just the response body of post.
func body(_ int, b string) string { return b }

func streamParams(t *testing.T, twiml string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, name := range []string{"call_id", "token"} {
		marker := `<Parameter name="` + name + `" value="`
		i := strings.Index(twiml, marker)
		require.GreaterOrEqual(t, i, 0, twiml)
		rest := twiml[i+len(marker):]
		out[name] = rest[:strings.Index(rest, `"`)]
	}
	return out
}

func TestIncomingWebhookSecurity(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	form := url.Values{"CallSid": {"CA100"}, "From": {owner}, "To": {"+15550001111"}}

	status, _ := e.post(twilio.PathIncoming, form, false)
	require.Equal(t, http.StatusForbidden, status, "unsigned webhooks are refused")

	stranger := url.Values{"CallSid": {"CA101"}, "From": {"+15550009999"}}
	got := body(e.post(twilio.PathIncoming, stranger, true))
	require.Contains(t, got, "<Reject", "callers outside the allowlist are rejected before answering")
	require.NotContains(t, got, "<Stream")

	got = body(e.post(twilio.PathIncoming, form, true))
	require.Contains(t, got, `<Stream url="wss://`+strings.TrimPrefix(e.baseURL, "https://")+twilio.PathStream+`">`)
	params := streamParams(t, got)
	require.NotEmpty(t, params["token"])
	require.NotContains(t, got, "?token", "tokens never go into a URL")

	again := body(e.post(twilio.PathIncoming, form, true))
	require.Equal(t, params, streamParams(t, again), "a retried webhook maps to the same call")

	big := url.Values{"CallSid": {"CA102"}, "From": {owner}, "Junk": {strings.Repeat("x", 40<<10)}}
	status, _ = e.post(twilio.PathIncoming, big, true)
	require.Equal(t, http.StatusBadRequest, status, "oversized bodies are refused")
}

// twilioStream simulates Twilio's side of a media stream.
type twilioStream struct {
	t      *testing.T
	conn   *websocket.Conn
	mu     sync.Mutex
	in     []map[string]any
	closed atomic.Bool
}

func dialStream(t *testing.T, e *env, params map[string]string, callSid string) *twilioStream {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(e.srv.URL, "http") + twilio.PathStream
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	resp.Body.Close()
	ts := &twilioStream{t: t, conn: conn}
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(`{"event":"connected","protocol":"Call","version":"1.0.0"}`)))
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, twilio.MarshalStart("MZ1", callSid, params)))
	go func() {
		for {
			var msg map[string]any
			if err := conn.ReadJSON(&msg); err != nil {
				ts.closed.Store(true)
				return
			}
			ts.mu.Lock()
			ts.in = append(ts.in, msg)
			ts.mu.Unlock()
			// Twilio echoes marks once playback reaches them.
			if msg["event"] == "mark" {
				name := msg["mark"].(map[string]any)["name"]
				b, _ := json.Marshal(map[string]any{"event": "mark", "streamSid": "MZ1", "mark": map[string]any{"name": name}})
				ts.mu.Lock()
				_ = conn.WriteMessage(websocket.TextMessage, b)
				ts.mu.Unlock()
			}
		}
	}()
	return ts
}

func (s *twilioStream) say(text string) {
	b, _ := json.Marshal(map[string]any{"event": "media", "streamSid": "MZ1", "media": map[string]any{"track": "inbound", "payload": base64.StdEncoding.EncodeToString([]byte("say:" + text))}})
	s.mu.Lock()
	defer s.mu.Unlock()
	require.NoError(s.t, s.conn.WriteMessage(websocket.TextMessage, b))
}

func (s *twilioStream) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.conn.WriteMessage(websocket.TextMessage, []byte(`{"event":"stop","streamSid":"MZ1"}`))
}

// heard decodes every media payload sent to the caller.
func (s *twilioStream) heard() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var b strings.Builder
	for _, m := range s.in {
		if m["event"] == "media" {
			p := m["media"].(map[string]any)["payload"].(string)
			raw, _ := base64.StdEncoding.DecodeString(p)
			b.Write(raw)
		}
	}
	return b.String()
}

func (s *twilioStream) count(event string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, m := range s.in {
		if m["event"] == event {
			n++
		}
	}
	return n
}

func TestInboundCallOverMediaStream(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	form := url.Values{"CallSid": {"CA200"}, "From": {owner}}
	params := streamParams(t, body(e.post(twilio.PathIncoming, form, true)))

	bad := dialStream(t, e, map[string]string{"call_id": params["call_id"], "token": "forged"}, "CA200")
	require.Eventually(t, bad.closed.Load, 5*time.Second, 10*time.Millisecond, "a forged token closes the stream")

	ts := dialStream(t, e, params, "CA200")
	require.Eventually(t, func() bool { return strings.Contains(ts.heard(), "Merhaba, ben Atlas") }, 5*time.Second, 10*time.Millisecond)
	ts.say("Son e-postamı özetle")
	require.Eventually(t, func() bool { return strings.Contains(ts.heard(), "Son e-postan Ayşe'den geldi.") }, 5*time.Second, 10*time.Millisecond)
	require.GreaterOrEqual(t, ts.count("mark"), 2)

	ts.stop()
	require.Eventually(t, func() bool { return len(e.gw.ActiveCalls()) == 0 }, 5*time.Second, 10*time.Millisecond)
	rec, ok := e.store.ByProviderCallID("CA200")
	require.True(t, ok)
	require.Equal(t, voice.StateEnded, rec.State)
	require.Equal(t, 1, rec.Metrics.Turns)
	require.Equal(t, []string{"Son e-postamı özetle"}, e.runner.runs)

	// The duplicate status callback after the call is harmless.
	for range 2 {
		status, _ := e.post(twilio.PathStatus, url.Values{"CallSid": {"CA200"}, "CallStatus": {"completed"}}, true)
		require.Equal(t, http.StatusNoContent, status)
	}
	e.rest.mu.Lock()
	defer e.rest.mu.Unlock()
	require.Contains(t, e.rest.paths, "/2010-04-01/Accounts/AC1/Calls/CA200.json", "hangup ends the call at Twilio too")
}

func TestOutboundDialAndMachineDetection(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	callID, token, outcome, err := e.gw.PrepareOutbound(voice.OutboundRequest{Reason: "Test", EventID: "e1"})
	require.NoError(t, err)
	pid, err := e.tr.Dial(context.Background(), voice.DialRequest{CallID: callID, To: owner, RingTimeout: 20, StreamToken: token})
	require.NoError(t, err)
	require.Equal(t, "CAoutbound1", pid)
	e.gw.BindProvider(callID, pid)

	e.rest.mu.Lock()
	form := e.rest.forms[0]
	auth := e.rest.auth[0]
	e.rest.mu.Unlock()
	require.Equal(t, owner, form.Get("To"))
	require.Equal(t, "+15550001111", form.Get("From"))
	require.Equal(t, e.baseURL+twilio.PathOutbound+callID, form.Get("Url"))
	require.Equal(t, e.baseURL+twilio.PathStatus, form.Get("StatusCallback"))
	require.Equal(t, "20", form.Get("Timeout"))
	require.NotContains(t, form.Encode(), token, "the stream token is not sent to the REST API")
	require.Equal(t, "AC1:"+authToken, auth)

	status, _ := e.post(twilio.PathOutbound+callID, url.Values{"CallSid": {pid}}, false)
	require.Equal(t, http.StatusForbidden, status)

	got := body(e.post(twilio.PathOutbound+callID, url.Values{"CallSid": {pid}, "AnsweredBy": {"machine_start"}}, true))
	require.Contains(t, got, "<Hangup/>", "voicemail is not talked to")
	select {
	case o := <-outcome:
		require.Equal(t, voice.StateNoAnswer, o.State)
	case <-time.After(5 * time.Second):
		t.Fatal("no outcome")
	}
	got = body(e.post(twilio.PathOutbound+"unknown", url.Values{"CallSid": {"CAx"}}, true))
	require.Contains(t, got, "<Hangup/>")
}

func TestOutboundAnsweredStreamsReason(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	callID, token, outcome, err := e.gw.PrepareOutbound(voice.OutboundRequest{Reason: "Sunucu yeniden çalışıyor.", EventID: "e2"})
	require.NoError(t, err)
	pid, err := e.tr.Dial(context.Background(), voice.DialRequest{CallID: callID, To: owner, StreamToken: token})
	require.NoError(t, err)
	e.gw.BindProvider(callID, pid)
	params := streamParams(t, body(e.post(twilio.PathOutbound+callID, url.Values{"CallSid": {pid}, "AnsweredBy": {"human"}}, true)))
	ts := dialStream(t, e, params, pid)
	select {
	case o := <-outcome:
		require.Equal(t, voice.StateActive, o.State)
	case <-time.After(5 * time.Second):
		t.Fatal("no outcome")
	}
	require.Eventually(t, func() bool { return strings.Contains(ts.heard(), "Sunucu yeniden çalışıyor.") }, 5*time.Second, 10*time.Millisecond)
	ts.stop()
	require.Eventually(t, func() bool { return len(e.gw.ActiveCalls()) == 0 }, 5*time.Second, 10*time.Millisecond)
}

func TestDialErrorsAreClassified(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		status    int
		notPlaced bool
	}{{http.StatusBadRequest, true}, {http.StatusUnauthorized, true}, {http.StatusInternalServerError, false}} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = io.WriteString(w, `{"code":21211,"message":"secret detail"}`)
		}))
		c := &twilio.Client{AccountSID: voice.NewSecret("AC1"), AuthToken: voice.NewSecret("tok"), BaseURL: srv.URL}
		_, err := c.CreateCall(context.Background(), twilio.CallRequest{To: owner, From: "+1"})
		srv.Close()
		require.Error(t, err)
		require.Equal(t, tc.notPlaced, voice.ProvablyNotPlaced(err), "status %d", tc.status)
		require.NotContains(t, err.Error(), "tok")
	}
	c := &twilio.Client{AccountSID: voice.NewSecret("AC1"), AuthToken: voice.NewSecret("tok"), BaseURL: "http://127.0.0.1:1"}
	_, err := c.CreateCall(context.Background(), twilio.CallRequest{To: owner})
	require.True(t, voice.ProvablyNotPlaced(err), "connection refused never reached Twilio")
}

func TestWhatsAppNotifierPrefixesNumbers(t *testing.T) {
	t.Parallel()
	rest := &fakeREST{}
	srv := httptest.NewServer(rest)
	t.Cleanup(srv.Close)
	n := &twilio.Notifier{Client: &twilio.Client{AccountSID: voice.NewSecret("AC1"), AuthToken: voice.NewSecret("t"), BaseURL: srv.URL}, Kind: voice.ChannelWhatsAppText, From: "+15550001111", OwnerTo: owner}
	require.NoError(t, n.Notify(context.Background(), "Atlas: test"))
	require.Equal(t, "whatsapp:"+owner, rest.forms[0].Get("To"))
	require.Equal(t, "whatsapp:+15550001111", rest.forms[0].Get("From"))
	require.Equal(t, "/2010-04-01/Accounts/AC1/Messages.json", rest.paths[0])
}

func TestRateLimiter(t *testing.T) {
	t.Parallel()
	rl := twilio.NewRateLimiter(2, time.Minute)
	require.True(t, rl.Allow("a"))
	require.True(t, rl.Allow("a"))
	require.False(t, rl.Allow("a"))
	require.True(t, rl.Allow("b"))
}
