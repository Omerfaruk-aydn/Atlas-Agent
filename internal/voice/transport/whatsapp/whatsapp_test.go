package whatsapp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/voice"
)

const (
	testPhoneID   = "104560001"
	testAppSecret = "app-secret"
	testVerify    = "verify-me"
	testToken     = "access-token"
	waitFor       = 20 * time.Second
)

// fakeGateway records what the transport reports and holds an attached
// call open until its leg ends.
type fakeGateway struct {
	allow string

	mu       sync.Mutex
	statuses []voice.CallStatus
	attached chan attachInfo
}

type attachInfo struct {
	callID, token string
	leg           voice.CallLeg
}

func newFakeGateway(allow string) *fakeGateway {
	return &fakeGateway{allow: allow, attached: make(chan attachInfo, 4)}
}

func (g *fakeGateway) AdmitInbound(_, from string) (string, string, error) {
	if from != g.allow {
		return "", "", voice.ErrNotAllowed
	}
	return "call_in", "tok_in", nil
}

func (g *fakeGateway) Attach(_ context.Context, callID, token string, l voice.CallLeg) error {
	g.attached <- attachInfo{callID, token, l}
	<-l.Done()
	return nil
}

func (g *fakeGateway) HandleStatus(_ string, s voice.CallStatus) {
	g.mu.Lock()
	g.statuses = append(g.statuses, s)
	g.mu.Unlock()
}

func (g *fakeGateway) seen(s voice.CallStatus) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, x := range g.statuses {
		if x == s {
			return true
		}
	}
	return false
}

// graphAction is one request the fake Graph API received.
type graphAction struct {
	Action  string `json:"action"`
	CallID  string `json:"call_id"`
	To      string `json:"to"`
	Auth    string `json:"-"`
	Session struct {
		SDP string `json:"sdp"`
	} `json:"session"`
}

// fakeGraph is a Graph API stand-in. onAction may answer an action; a nil
// reply means a plain success.
type fakeGraph struct {
	srv      *httptest.Server
	mu       sync.Mutex
	actions  []graphAction
	onAction func(a graphAction) (status int, body string)
}

func newFakeGraph(t *testing.T) *fakeGraph {
	t.Helper()
	g := &fakeGraph{}
	g.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var a graphAction
		_ = json.NewDecoder(r.Body).Decode(&a)
		a.Auth = r.Header.Get("Authorization")
		g.mu.Lock()
		g.actions = append(g.actions, a)
		g.mu.Unlock()
		status, body := http.StatusOK, `{"messaging_product":"whatsapp","success":true}`
		if g.onAction != nil {
			if s, b := g.onAction(a); s != 0 {
				status, body = s, b
			}
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(g.srv.Close)
	return g
}

func (g *fakeGraph) names() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []string
	for _, a := range g.actions {
		out = append(out, a.Action)
	}
	return out
}

func (g *fakeGraph) client() *Client {
	return &Client{AccessToken: voice.NewSecret(testToken), PhoneNumberID: testPhoneID, BaseURL: g.srv.URL, Version: "v23.0"}
}

func newTransport(t *testing.T, g *fakeGraph, gw Gateway) *Transport {
	t.Helper()
	tr := New(Options{
		Config:    voice.WhatsAppConfig{PhoneNumberID: testPhoneID},
		AppSecret: voice.NewSecret(testAppSecret), VerifyToken: voice.NewSecret(testVerify),
		Client: g.client(), Gateway: gw, Loopback: true,
	})
	t.Cleanup(tr.Wait)
	return tr
}

func callsPayload(call map[string]any) map[string]any {
	return map[string]any{
		"object": "whatsapp_business_account",
		"entry": []any{map[string]any{"id": "waba", "changes": []any{map[string]any{
			"field": "calls",
			"value": map[string]any{
				"messaging_product": "whatsapp",
				"metadata":          map[string]any{"phone_number_id": testPhoneID},
				"calls":             []any{call},
			},
		}}}},
	}
}

func statusPayload(id, status string) map[string]any {
	return map[string]any{
		"object": "whatsapp_business_account",
		"entry": []any{map[string]any{"id": "waba", "changes": []any{map[string]any{
			"field": "calls",
			"value": map[string]any{
				"messaging_product": "whatsapp",
				"metadata":          map[string]any{"phone_number_id": testPhoneID},
				"statuses":          []any{map[string]any{"id": id, "type": "call", "status": status}},
			},
		}}}},
	}
}

// deliver posts a signed webhook straight to the handler.
func deliver(t *testing.T, tr *Transport, payload map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, PathWebhook, bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", Sign(testAppSecret, body))
	rec := httptest.NewRecorder()
	tr.Handler().ServeHTTP(rec, req)
	return rec
}

func TestValidSignature(t *testing.T) {
	t.Parallel()
	body := []byte(`{"a":1}`)
	good := Sign("s3cret", body)
	require.True(t, ValidSignature("s3cret", body, good))
	require.False(t, ValidSignature("other", body, good))
	require.False(t, ValidSignature("s3cret", []byte(`{"a":2}`), good))
	require.False(t, ValidSignature("s3cret", body, "sha256=zz"))
	require.False(t, ValidSignature("s3cret", body, "md5=abcd"))
	require.False(t, ValidSignature("", body, good))
}

func TestWebhookVerification(t *testing.T) {
	t.Parallel()
	tr := newTransport(t, newFakeGraph(t), newFakeGateway(""))
	get := func(query string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		tr.Handler().ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, PathWebhook+"?"+query, nil))
		return rec
	}
	ok := get("hub.mode=subscribe&hub.verify_token=" + testVerify + "&hub.challenge=12345")
	require.Equal(t, http.StatusOK, ok.Code)
	require.Equal(t, "12345", ok.Body.String())
	require.Equal(t, "text/plain; charset=utf-8", ok.Header().Get("Content-Type"))
	require.Equal(t, "nosniff", ok.Header().Get("X-Content-Type-Options"))
	markup := get("hub.mode=subscribe&hub.verify_token=" + testVerify + "&hub.challenge=%3Cscript%3Ealert(1)%3C%2Fscript%3E")
	require.Equal(t, "<script>alert(1)</script>", markup.Body.String())
	require.Equal(t, "nosniff", markup.Header().Get("X-Content-Type-Options"))
	require.Equal(t, http.StatusForbidden, get("hub.mode=subscribe&hub.verify_token=wrong&hub.challenge=1").Code)
	require.Equal(t, http.StatusForbidden, get("hub.mode=subscribe&hub.challenge=1").Code)
}

func TestWebhookRejectsBadSignature(t *testing.T) {
	t.Parallel()
	gw := newFakeGateway("")
	tr := newTransport(t, newFakeGraph(t), gw)
	body, _ := json.Marshal(statusPayload("wacid.x", "RINGING"))
	for _, sig := range []string{"", "sha256=00", Sign("wrong", body)} {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, PathWebhook, bytes.NewReader(body))
		req.Header.Set("X-Hub-Signature-256", sig)
		rec := httptest.NewRecorder()
		tr.Handler().ServeHTTP(rec, req)
		require.Equal(t, http.StatusForbidden, rec.Code)
	}
	require.False(t, gw.seen(voice.StatusRinging))
}

func TestWebhookIgnoresOtherNumbers(t *testing.T) {
	t.Parallel()
	gw := newFakeGateway("")
	tr := newTransport(t, newFakeGraph(t), gw)
	p := statusPayload("wacid.x", "RINGING")
	p["entry"].([]any)[0].(map[string]any)["changes"].([]any)[0].(map[string]any)["value"].(map[string]any)["metadata"] = map[string]any{"phone_number_id": "999"}
	require.Equal(t, http.StatusOK, deliver(t, tr, p).Code)
	require.False(t, gw.seen(voice.StatusRinging))
}

func TestStatusMapping(t *testing.T) {
	t.Parallel()
	gw := newFakeGateway("")
	tr := newTransport(t, newFakeGraph(t), gw)
	for _, s := range []string{"RINGING", "ACCEPTED", "REJECTED"} {
		require.Equal(t, http.StatusOK, deliver(t, tr, statusPayload("wacid.x", s)).Code)
	}
	require.True(t, gw.seen(voice.StatusRinging))
	require.True(t, gw.seen(voice.StatusInProgress))
	require.True(t, gw.seen(voice.StatusNoAnswer))
}

func TestClientErrors(t *testing.T) {
	t.Parallel()
	g := newFakeGraph(t)
	c := g.client()

	g.onAction = func(graphAction) (int, string) {
		return http.StatusBadRequest, `{"error":{"message":"no permission","code":138006}}`
	}
	_, err := c.Connect(t.Context(), "905551112233", "v=0")
	var de *voice.DialError
	require.ErrorAs(t, err, &de)
	require.True(t, de.NotPlaced)
	require.Equal(t, voice.CodePermissionRequired, de.Code)

	g.onAction = func(graphAction) (int, string) { return http.StatusInternalServerError, `{}` }
	_, err = c.Connect(t.Context(), "905551112233", "v=0")
	require.ErrorAs(t, err, &de)
	require.False(t, de.NotPlaced, "a server error may have created the call")

	g.onAction = func(graphAction) (int, string) { return http.StatusUnauthorized, `{}` }
	_, err = c.Connect(t.Context(), "905551112233", "v=0")
	require.ErrorAs(t, err, &de)
	require.Equal(t, voice.CodeProviderAuth, de.Code)

	g.onAction = func(graphAction) (int, string) { return http.StatusOK, `{"calls":[]}` }
	_, err = c.Connect(t.Context(), "905551112233", "v=0")
	require.ErrorAs(t, err, &de)
	require.Equal(t, voice.CodeProviderProtocol, de.Code)

	// The token goes in the header and never into an error message.
	g.onAction = func(graphAction) (int, string) { return http.StatusBadRequest, `{"error":{"code":100}}` }
	_, err = c.Connect(t.Context(), "905551112233", "v=0")
	require.NotContains(t, err.Error(), testToken)
	g.mu.Lock()
	require.Equal(t, "Bearer "+testToken, g.actions[len(g.actions)-1].Auth)
	g.mu.Unlock()
}

func TestLegPlaybackQueue(t *testing.T) {
	t.Parallel()
	l, err := newLeg(legOptions{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Hangup(context.Background()) })
	ctx := context.Background()

	data := bytes.Repeat([]byte{0x11}, 200)
	require.NoError(t, l.Send(ctx, voice.AudioFrame{Encoding: voice.EncodingMulaw8k, Data: data}))
	require.NoError(t, l.Mark(ctx, "done"))

	first := l.next()
	require.Equal(t, bytes.Repeat([]byte{0x11}, 160), first)
	select {
	case <-l.Marks():
		t.Fatal("mark fired before its audio was sent")
	default:
	}
	second := l.next()
	require.Equal(t, bytes.Repeat([]byte{0x11}, 40), second[:40])
	require.Equal(t, bytes.Repeat([]byte{mulawSilence}, 120), second[40:], "the rest of the frame is silence")
	require.Equal(t, "done", <-l.Marks())

	// Idle frames are silence.
	require.Equal(t, bytes.Repeat([]byte{mulawSilence}, frameBytes), l.next())

	// Clear drops audio and reports pending marks at once.
	require.NoError(t, l.Send(ctx, voice.AudioFrame{Encoding: voice.EncodingMulaw8k, Data: data}))
	require.NoError(t, l.Mark(ctx, "cleared"))
	require.NoError(t, l.Clear(ctx))
	require.Equal(t, "cleared", <-l.Marks())
	require.Equal(t, bytes.Repeat([]byte{mulawSilence}, frameBytes), l.next())

	// Wrong encodings and oversized backlogs are refused.
	require.Error(t, l.Send(ctx, voice.AudioFrame{Encoding: voice.EncodingPCM16k, Data: []byte{0, 0}}))
	big := voice.AudioFrame{Encoding: voice.EncodingMulaw8k, Data: make([]byte, 8000*2)}
	for range 200 {
		if err := l.Send(ctx, big); err != nil {
			require.Equal(t, voice.CodeTransportFailed, voice.CodeOf(err))
			return
		}
	}
	t.Fatal("the playback backlog never filled")
}

// newPhone plays the WhatsApp user's phone: a second WebRTC peer.
func newPhone(t *testing.T) *leg {
	t.Helper()
	p, err := newLeg(legOptions{loopback: true})
	require.NoError(t, err)
	t.Cleanup(func() { p.remoteHangup() })
	return p
}

// loud waits for a frame carrying the byte want in ch.
func loud(t *testing.T, ch <-chan voice.AudioFrame, want byte) {
	t.Helper()
	deadline := time.After(waitFor)
	for {
		select {
		case f, ok := <-ch:
			require.True(t, ok, "audio ended before the expected frame")
			if bytes.Contains(f.Data, []byte{want, want, want, want}) {
				return
			}
		case <-deadline:
			t.Fatalf("no frame of 0x%02x arrived", want)
		}
	}
}

func TestOutboundCallEndToEnd(t *testing.T) {
	t.Parallel()
	g := newFakeGraph(t)
	gw := newFakeGateway("")
	tr := newTransport(t, g, gw)
	phone := newPhone(t)

	g.onAction = func(a graphAction) (int, string) {
		if a.Action != "connect" {
			return 0, ""
		}
		if a.To != "905551112233" {
			t.Errorf("the number must go out without a plus sign, got %q", a.To)
		}
		answer, err := phone.answer(context.Background(), a.Session.SDP)
		if err != nil {
			t.Errorf("phone answer: %v", err)
			return http.StatusInternalServerError, `{}`
		}
		go func() {
			deliver(t, tr, callsPayload(map[string]any{
				"id": "wacid.out", "event": "connect", "direction": "BUSINESS_INITIATED",
				"session": map[string]any{"sdp_type": "answer", "sdp": answer},
			}))
		}()
		return http.StatusOK, `{"messaging_product":"whatsapp","calls":[{"id":"wacid.out"}]}`
	}

	waID, err := tr.Dial(t.Context(), voice.DialRequest{CallID: "call_out", To: "+905551112233", StreamToken: "tok_out"})
	require.NoError(t, err)
	require.Equal(t, "wacid.out", waID)

	var info attachInfo
	select {
	case info = <-gw.attached:
	case <-time.After(waitFor):
		t.Fatal("the call never reached the gateway")
	}
	require.Equal(t, "call_out", info.callID)
	require.Equal(t, "tok_out", info.token)
	phone.start()

	// Audio flows both ways over real WebRTC.
	require.NoError(t, phone.Send(t.Context(), voice.AudioFrame{Encoding: voice.EncodingMulaw8k, Data: bytes.Repeat([]byte{0x55}, 1600)}))
	loud(t, info.leg.Audio(), 0x55)

	require.NoError(t, info.leg.Send(t.Context(), voice.AudioFrame{Encoding: voice.EncodingMulaw8k, Data: bytes.Repeat([]byte{0x77}, 1600)}))
	require.NoError(t, info.leg.Mark(t.Context(), "spoken"))
	loud(t, phone.Audio(), 0x77)
	select {
	case name := <-info.leg.Marks():
		require.Equal(t, "spoken", name)
	case <-time.After(waitFor):
		t.Fatal("the mark was never reported")
	}

	// WhatsApp reports the end; the leg and gateway see it.
	deliver(t, tr, callsPayload(map[string]any{
		"id": "wacid.out", "event": "terminate", "direction": "BUSINESS_INITIATED",
		"status": "COMPLETED", "start_time": "1700000000",
	}))
	select {
	case <-info.leg.Done():
	case <-time.After(waitFor):
		t.Fatal("the leg did not end after the terminate webhook")
	}
	require.True(t, gw.seen(voice.StatusCompleted))
	require.NotContains(t, g.names(), "terminate", "a call WhatsApp ended needs no terminate request")
}

func TestOutboundAnswerBeforeDialResponse(t *testing.T) {
	t.Parallel()
	g := newFakeGraph(t)
	gw := newFakeGateway("")
	tr := newTransport(t, g, gw)
	phone := newPhone(t)

	g.onAction = func(a graphAction) (int, string) {
		if a.Action != "connect" {
			return 0, ""
		}
		answer, err := phone.answer(context.Background(), a.Session.SDP)
		if err != nil {
			t.Errorf("phone answer: %v", err)
			return http.StatusInternalServerError, `{}`
		}
		// The webhook arrives before the response does.
		deliver(t, tr, callsPayload(map[string]any{
			"id": "wacid.fast", "event": "connect", "direction": "BUSINESS_INITIATED",
			"session": map[string]any{"sdp_type": "answer", "sdp": answer},
		}))
		return http.StatusOK, `{"calls":[{"id":"wacid.fast"}]}`
	}
	_, err := tr.Dial(t.Context(), voice.DialRequest{CallID: "call_fast", To: "+905551112233", StreamToken: "tok"})
	require.NoError(t, err)
	select {
	case info := <-gw.attached:
		require.Equal(t, "call_fast", info.callID)
		require.NoError(t, info.leg.(*leg).Hangup(t.Context()))
	case <-time.After(waitFor):
		t.Fatal("an early answer was lost")
	}
	require.Contains(t, g.names(), "terminate", "our hangup tells WhatsApp")
}

func TestDialRefusedIsNotPlaced(t *testing.T) {
	t.Parallel()
	g := newFakeGraph(t)
	tr := newTransport(t, g, newFakeGateway(""))
	g.onAction = func(graphAction) (int, string) {
		return http.StatusBadRequest, `{"error":{"code":138006,"message":"no permission"}}`
	}
	_, err := tr.Dial(t.Context(), voice.DialRequest{CallID: "c", To: "+905551112233", StreamToken: "t"})
	require.Error(t, err)
	require.True(t, voice.ProvablyNotPlaced(err))
	var de *voice.DialError
	require.ErrorAs(t, err, &de)
	require.Equal(t, voice.CodePermissionRequired, de.Code)
	require.Empty(t, tr.calls, "a refused dial leaves nothing behind")
}

func TestCancelTerminates(t *testing.T) {
	t.Parallel()
	g := newFakeGraph(t)
	tr := newTransport(t, g, newFakeGateway(""))
	require.NoError(t, tr.Cancel(t.Context(), "wacid.unknown"))
	require.Equal(t, []string{"terminate"}, g.names())
}

func TestInboundCallEndToEnd(t *testing.T) {
	t.Parallel()
	g := newFakeGraph(t)
	gw := newFakeGateway("+905551112233")
	tr := newTransport(t, g, gw)
	phone := newPhone(t)

	// The phone applies each answer WhatsApp is given, as the real
	// network would.
	g.onAction = func(a graphAction) (int, string) {
		if a.Action == "pre_accept" {
			if err := phone.setAnswer(a.Session.SDP); err != nil {
				t.Errorf("phone set answer: %v", err)
			}
		}
		return 0, ""
	}
	offer, err := phone.offer(t.Context())
	require.NoError(t, err)
	rec := deliver(t, tr, callsPayload(map[string]any{
		"id": "wacid.in", "from": "905551112233", "event": "connect", "direction": "USER_INITIATED",
		"session": map[string]any{"sdp_type": "offer", "sdp": offer},
	}))
	require.Equal(t, http.StatusOK, rec.Code)

	var info attachInfo
	select {
	case info = <-gw.attached:
	case <-time.After(waitFor):
		t.Fatalf("the call never reached the gateway; actions: %v", g.names())
	}
	require.Equal(t, "call_in", info.callID)
	require.Equal(t, "tok_in", info.token)
	require.Equal(t, []string{"pre_accept", "accept"}, g.names(), "media connects before the call is accepted")

	phone.start()
	require.NoError(t, phone.Send(t.Context(), voice.AudioFrame{Encoding: voice.EncodingMulaw8k, Data: bytes.Repeat([]byte{0x33}, 1600)}))
	loud(t, info.leg.Audio(), 0x33)

	// The user hangs up.
	deliver(t, tr, callsPayload(map[string]any{
		"id": "wacid.in", "event": "terminate", "direction": "USER_INITIATED", "status": "COMPLETED",
	}))
	select {
	case <-info.leg.Done():
	case <-time.After(waitFor):
		t.Fatal("the leg did not end")
	}
}

func TestRepeatedInboundWebhookAnswersOnce(t *testing.T) {
	t.Parallel()
	g := newFakeGraph(t)
	gw := newFakeGateway("+905551112233")
	tr := newTransport(t, g, gw)
	phone := newPhone(t)
	g.onAction = func(a graphAction) (int, string) {
		if a.Action == "pre_accept" {
			if err := phone.setAnswer(a.Session.SDP); err != nil {
				t.Errorf("phone set answer: %v", err)
			}
		}
		return 0, ""
	}
	offer, err := phone.offer(t.Context())
	require.NoError(t, err)
	ev := callsPayload(map[string]any{
		"id": "wacid.dup", "from": "905551112233", "event": "connect", "direction": "USER_INITIATED",
		"session": map[string]any{"sdp_type": "offer", "sdp": offer},
	})
	for range 3 {
		require.Equal(t, http.StatusOK, deliver(t, tr, ev).Code)
	}
	var info attachInfo
	select {
	case info = <-gw.attached:
	case <-time.After(waitFor):
		t.Fatal("the call never reached the gateway")
	}
	require.NoError(t, info.leg.Hangup(t.Context()))
	require.Equal(t, 1, countOf(g.names(), "pre_accept"), "a repeated webhook must not start a second peer")
	require.Equal(t, 1, countOf(g.names(), "accept"))
}

func countOf(names []string, want string) int {
	n := 0
	for _, s := range names {
		if s == want {
			n++
		}
	}
	return n
}

func TestInboundFromStrangerIsRejected(t *testing.T) {
	t.Parallel()
	g := newFakeGraph(t)
	gw := newFakeGateway("+905551112233")
	tr := newTransport(t, g, gw)
	phone := newPhone(t)
	offer, err := phone.offer(t.Context())
	require.NoError(t, err)
	deliver(t, tr, callsPayload(map[string]any{
		"id": "wacid.stranger", "from": "905559999999", "event": "connect", "direction": "USER_INITIATED",
		"session": map[string]any{"sdp_type": "offer", "sdp": offer},
	}))
	require.Eventually(t, func() bool { return len(g.names()) > 0 }, waitFor, 20*time.Millisecond)
	require.Equal(t, []string{"reject"}, g.names(), "an unknown caller is declined before anything is answered")
	select {
	case <-gw.attached:
		t.Fatal("a rejected caller reached the gateway")
	default:
	}
}
