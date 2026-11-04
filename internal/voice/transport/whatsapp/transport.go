package whatsapp

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/voice"
)

// PathWebhook is the single path Meta calls for verification and events.
const PathWebhook = "/voice/whatsapp/webhook"

const (
	maxWebhookBody = 1 << 20
	maxEarly       = 64
)

// Gateway is what the transport needs from the voice gateway.
type Gateway interface {
	AdmitInbound(providerCallID, from string) (callID, token string, err error)
	Attach(ctx context.Context, callID, token string, leg voice.CallLeg) error
	HandleStatus(providerCallID string, status voice.CallStatus)
}

// Options configures a [Transport].
type Options struct {
	Config      voice.WhatsAppConfig
	AppSecret   voice.Secret
	VerifyToken voice.Secret
	Client      *Client
	Gateway     Gateway
	// Loopback lets peers in one process connect; tests only.
	Loopback bool
}

// Transport serves the WhatsApp webhook and runs call media. It implements
// voice.Dialer.
type Transport struct {
	opts Options

	mu    sync.Mutex
	calls map[string]*call  // by WhatsApp call id
	early map[string]string // answer SDP that beat the dial response
	// inbound holds WhatsApp call ids being answered, so a repeated webhook
	// does not start a second peer.
	inbound map[string]struct{}
	wg      sync.WaitGroup
}

type call struct {
	leg    *leg
	callID string // gateway call id
	token  string
}

// New builds a transport.
func New(opts Options) *Transport {
	return &Transport{opts: opts, calls: map[string]*call{}, early: map[string]string{}, inbound: map[string]struct{}{}}
}

// Wait blocks until every call handler has returned.
func (t *Transport) Wait() { t.wg.Wait() }

// Handler returns the public webhook handler.
func (t *Transport) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+PathWebhook, t.verify)
	mux.HandleFunc("POST "+PathWebhook, t.receive)
	return mux
}

func (t *Transport) legOptions() legOptions {
	return legOptions{stun: t.opts.Config.STUNServers, loopback: t.opts.Loopback}
}

// verify answers Meta's subscription handshake.
func (t *Transport) verify(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	want := t.opts.VerifyToken.Reveal()
	got := q.Get("hub.verify_token")
	if q.Get("hub.mode") != "subscribe" || want == "" || subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
		slog.Warn("Rejected WhatsApp webhook verification")
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	_, _ = w.Write([]byte(q.Get("hub.challenge")))
}

// flexString reads a JSON string, or the first element of an array of
// strings, which Meta's samples use for call status.
type flexString string

func (f *flexString) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		*f = flexString(s)
		return nil
	}
	var a []string
	if json.Unmarshal(b, &a) == nil && len(a) > 0 {
		*f = flexString(a[0])
	}
	return nil
}

type callEvent struct {
	ID        string          `json:"id"`
	From      string          `json:"from"`
	Event     string          `json:"event"`
	Direction string          `json:"direction"`
	Status    flexString      `json:"status"`
	StartTime json.RawMessage `json:"start_time"`
	Session   *struct {
		SDP string `json:"sdp"`
	} `json:"session"`
}

type callStatus struct {
	ID     string     `json:"id"`
	Type   string     `json:"type"`
	Status flexString `json:"status"`
}

type webhook struct {
	Entry []struct {
		Changes []struct {
			Field string `json:"field"`
			Value struct {
				Metadata struct {
					PhoneNumberID string `json:"phone_number_id"`
				} `json:"metadata"`
				Calls    []callEvent  `json:"calls"`
				Statuses []callStatus `json:"statuses"`
			} `json:"value"`
		} `json:"changes"`
	} `json:"entry"`
}

// receive checks the signature, acknowledges at once and handles events in
// the background; Meta retries a slow acknowledgement.
func (t *Transport) receive(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBody))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !ValidSignature(t.opts.AppSecret.Reveal(), body, r.Header.Get("X-Hub-Signature-256")) {
		slog.Warn("Rejected WhatsApp webhook with an invalid signature")
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var wh webhook
	if err := json.Unmarshal(body, &wh); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
	for _, e := range wh.Entry {
		for _, c := range e.Changes {
			if c.Field != "calls" || c.Value.Metadata.PhoneNumberID != t.opts.Client.PhoneNumberID {
				continue
			}
			for _, ev := range c.Value.Calls {
				t.dispatch(ev)
			}
			for _, st := range c.Value.Statuses {
				if st.Type == "call" {
					t.status(st)
				}
			}
		}
	}
}

func (t *Transport) dispatch(ev callEvent) {
	switch {
	case ev.ID == "":
	case ev.Event == "connect" && ev.Direction == "USER_INITIATED":
		t.goRun(func() { t.handleIncoming(ev) })
	case ev.Event == "connect" && ev.Session != nil:
		t.answerArrived(ev.ID, ev.Session.SDP)
	case ev.Event == "terminate":
		t.terminated(ev)
	}
}

func (t *Transport) goRun(fn func()) {
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		fn()
	}()
}

func (t *Transport) status(st callStatus) {
	switch strings.ToUpper(string(st.Status)) {
	case "RINGING":
		t.opts.Gateway.HandleStatus(st.ID, voice.StatusRinging)
	case "ACCEPTED":
		t.opts.Gateway.HandleStatus(st.ID, voice.StatusInProgress)
	case "REJECTED":
		t.opts.Gateway.HandleStatus(st.ID, voice.StatusNoAnswer)
	}
}

// terminated handles a call WhatsApp reports as over, whoever ended it.
func (t *Transport) terminated(ev callEvent) {
	started := len(ev.StartTime) > 0 && string(ev.StartTime) != "null"
	status := voice.StatusCompleted
	if strings.EqualFold(string(ev.Status), "FAILED") {
		status = voice.StatusFailed
		if !started && ev.Direction == "BUSINESS_INITIATED" {
			// Nobody picked up.
			status = voice.StatusNoAnswer
		}
	}
	t.opts.Gateway.HandleStatus(ev.ID, status)
	if c := t.take(ev.ID); c != nil {
		c.leg.remoteHangup()
	}
}

func (t *Transport) take(id string) *call {
	t.mu.Lock()
	defer t.mu.Unlock()
	c := t.calls[id]
	delete(t.calls, id)
	return c
}

func (t *Transport) forget(id string) {
	t.mu.Lock()
	delete(t.calls, id)
	delete(t.inbound, id)
	t.mu.Unlock()
}

// newLeg builds a peer whose hangup tells WhatsApp the call is over.
func (t *Transport) newLeg(waID string) (*leg, error) {
	l, err := newLeg(t.legOptions())
	if err != nil {
		return nil, err
	}
	l.onHangup = func(ctx context.Context) {
		if err := t.opts.Client.Terminate(ctx, waID); err != nil {
			slog.Debug("WhatsApp terminate request failed", "error", err)
		}
	}
	return l, nil
}

// handleIncoming answers a call from a WhatsApp user: admit, answer with a
// pre-accept, wait for media, then accept and hand the call to the gateway.
func (t *Transport) handleIncoming(ev callEvent) {
	ctx := context.Background()
	t.mu.Lock()
	_, dup := t.inbound[ev.ID]
	t.inbound[ev.ID] = struct{}{}
	t.mu.Unlock()
	if dup {
		slog.Debug("Ignored a repeated WhatsApp call webhook")
		return
	}
	from := ""
	if d := digits(ev.From); d != "" {
		from = "+" + d
	}
	callID, token, err := t.opts.Gateway.AdmitInbound(ev.ID, from)
	if err != nil {
		// Reject before answering, so an unknown caller costs nothing.
		t.reject(ctx, ev.ID)
		return
	}
	fail := func(l *leg, why string, err error) {
		slog.Warn("Failed to answer a WhatsApp call", "step", why, "error", err)
		t.reject(ctx, ev.ID)
		if l != nil {
			l.remoteHangup()
		}
		t.forget(ev.ID)
		t.opts.Gateway.HandleStatus(ev.ID, voice.StatusFailed)
	}
	if ev.Session == nil || ev.Session.SDP == "" {
		fail(nil, "offer", errors.New("no SDP offer"))
		return
	}
	l, err := t.newLeg(ev.ID)
	if err != nil {
		fail(nil, "peer", err)
		return
	}
	answer, err := l.answer(ctx, ev.Session.SDP)
	if err != nil {
		fail(l, "answer", err)
		return
	}
	t.mu.Lock()
	t.calls[ev.ID] = &call{leg: l, callID: callID, token: token}
	t.mu.Unlock()
	if err := t.opts.Client.Answer(ctx, ev.ID, "pre_accept", answer); err != nil {
		fail(l, "pre_accept", err)
		return
	}
	if err := l.waitConnected(ctx, connectTimeout); err != nil {
		fail(l, "media", err)
		return
	}
	if err := t.opts.Client.Answer(ctx, ev.ID, "accept", answer); err != nil {
		fail(l, "accept", err)
		return
	}
	t.run(ctx, ev.ID, callID, token, l)
}

func (t *Transport) reject(ctx context.Context, id string) {
	if err := t.opts.Client.Reject(ctx, id); err != nil {
		slog.Debug("WhatsApp reject request failed", "error", err)
	}
}

// run hands a connected leg to the gateway and cleans up when it ends.
func (t *Transport) run(ctx context.Context, waID, callID, token string, l *leg) {
	l.start()
	if err := t.opts.Gateway.Attach(ctx, callID, token, l); err != nil {
		slog.Warn("Refused WhatsApp call", "error", err)
	}
	_ = l.Hangup(context.Background())
	t.forget(waID)
}

// Dial implements voice.Dialer. The offer goes out with the connect
// action; the answer arrives later on the webhook.
func (t *Transport) Dial(ctx context.Context, req voice.DialRequest) (string, error) {
	notPlaced := func(err error) error {
		return &voice.DialError{NotPlaced: true, Code: voice.CodeTransportFailed, Err: err}
	}
	l, err := newLeg(t.legOptions())
	if err != nil {
		return "", notPlaced(err)
	}
	offer, err := l.offer(ctx)
	if err != nil {
		_ = l.pc.Close()
		return "", notPlaced(err)
	}
	waID, err := t.opts.Client.Connect(ctx, strings.TrimPrefix(req.To, "+"), offer)
	if err != nil {
		l.remoteEnded.Store(true)
		_ = l.Hangup(context.Background())
		return "", err
	}
	l.onHangup = func(ctx context.Context) {
		if err := t.opts.Client.Terminate(ctx, waID); err != nil {
			slog.Debug("WhatsApp terminate request failed", "error", err)
		}
	}
	t.mu.Lock()
	t.calls[waID] = &call{leg: l, callID: req.CallID, token: req.StreamToken}
	sdp, early := t.early[waID]
	delete(t.early, waID)
	t.mu.Unlock()
	if early {
		t.answerArrived(waID, sdp)
	}
	return waID, nil
}

// answerArrived applies the SDP answer to a call we placed and starts
// media. An answer that beats the dial response waits for it.
func (t *Transport) answerArrived(waID, sdp string) {
	t.mu.Lock()
	c, ok := t.calls[waID]
	if !ok {
		if len(t.early) < maxEarly {
			t.early[waID] = sdp
		}
		t.mu.Unlock()
		return
	}
	t.mu.Unlock()
	t.goRun(func() {
		ctx := context.Background()
		if err := c.leg.setAnswer(sdp); err != nil {
			t.endFailed(waID, c, "answer", err)
			return
		}
		if err := c.leg.waitConnected(ctx, connectTimeout); err != nil {
			t.endFailed(waID, c, "media", err)
			return
		}
		t.run(ctx, waID, c.callID, c.token, c.leg)
	})
}

func (t *Transport) endFailed(waID string, c *call, step string, err error) {
	slog.Warn("Failed to connect a WhatsApp call", "step", step, "error", err)
	_ = c.leg.Hangup(context.Background())
	t.forget(waID)
	t.opts.Gateway.HandleStatus(waID, voice.StatusFailed)
}

// Cancel implements voice.Dialer.
func (t *Transport) Cancel(ctx context.Context, waID string) error {
	t.mu.Lock()
	c := t.calls[waID]
	t.mu.Unlock()
	if c != nil {
		return c.leg.Hangup(ctx)
	}
	return t.opts.Client.Terminate(ctx, waID)
}

func digits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
