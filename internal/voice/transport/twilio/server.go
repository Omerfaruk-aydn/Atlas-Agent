package twilio

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/voice"
	"github.com/gorilla/websocket"
)

// Paths served on the public listener.
const (
	PathIncoming = "/voice/twilio/incoming"
	PathOutbound = "/voice/twilio/outbound/"
	PathStatus   = "/voice/twilio/status"
	PathStream   = "/voice/twilio/stream"
)

const (
	maxWebhookBody   = 32 << 10
	maxStreamMessage = 256 << 10
	startTimeout     = 10 * time.Second
	streamWriteWait  = 10 * time.Second
	audioBuffer      = 256
)

// Gateway is what the transport needs from the voice gateway.
type Gateway interface {
	AdmitInbound(providerCallID, from string) (callID, token string, err error)
	Attach(ctx context.Context, callID, token string, leg voice.CallLeg) error
	HandleStatus(providerCallID string, status voice.CallStatus)
}

// Options configures a [Transport].
type Options struct {
	Config    voice.TwilioConfig
	AuthToken voice.Secret
	Client    *Client
	Gateway   Gateway
	// Limiter bounds webhook requests per remote address; nil uses 60 per
	// minute.
	Limiter *RateLimiter
}

// Transport serves Twilio webhooks and media streams and places outbound
// calls. It implements voice.Dialer.
type Transport struct {
	opts     Options
	base     string
	upgrader websocket.Upgrader

	mu      sync.Mutex
	tokens  map[string]pendingToken // outbound calls by call id
	streams sync.WaitGroup
}

// New builds a transport.
func New(opts Options) *Transport {
	if opts.Limiter == nil {
		opts.Limiter = NewRateLimiter(60, time.Minute)
	}
	return &Transport{
		opts: opts,
		base: strings.TrimRight(opts.Config.PublicBaseURL, "/"),
		upgrader: websocket.Upgrader{
			ReadBufferSize: 4096, WriteBufferSize: 4096,
			// Media streams come from Twilio, not browsers.
			CheckOrigin: func(r *http.Request) bool { return r.Header.Get("Origin") == "" },
		},
		tokens: map[string]pendingToken{},
	}
}

type pendingToken struct {
	token string
	at    time.Time
}

// tokenTTL bounds how long an outbound call may take to be answered.
const tokenTTL = 10 * time.Minute

// Handler returns the public webhook and media stream handler.
func (t *Transport) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+PathIncoming, t.incoming)
	mux.HandleFunc("POST "+PathOutbound+"{call_id}", t.outbound)
	mux.HandleFunc("POST "+PathStatus, t.status)
	mux.HandleFunc("GET "+PathStream, t.stream)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !t.opts.Limiter.Allow(remoteHost(r)) {
			http.Error(w, "too many requests", http.StatusTooManyRequests)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

// Wait blocks until every media stream handler has returned.
func (t *Transport) Wait() { t.streams.Wait() }

func remoteHost(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// publicURL is the URL Twilio requested, which is what it signed.
func (t *Transport) publicURL(r *http.Request) string { return t.base + r.URL.RequestURI() }

// verify checks the body size and the request signature and returns the
// form.
func (t *Transport) verify(w http.ResponseWriter, r *http.Request) (url.Values, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxWebhookBody)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return nil, false
	}
	if !ValidSignature(t.opts.AuthToken.Reveal(), t.publicURL(r), r.PostForm, r.Header.Get("X-Twilio-Signature")) {
		slog.Warn("Rejected Twilio webhook with an invalid signature", "path", r.URL.Path)
		http.Error(w, "forbidden", http.StatusForbidden)
		return nil, false
	}
	return r.PostForm, true
}

func writeTwiML(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "text/xml")
	_, _ = w.Write([]byte(xml.Header + "<Response>" + body + "</Response>"))
}

func xmlEscape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// streamTwiML connects the call to our media stream. The call id and its
// one-time token travel as stream parameters, never in a URL.
func (t *Transport) streamTwiML(callID, token string) string {
	wsURL := "wss://" + strings.TrimPrefix(strings.TrimPrefix(t.base, "https://"), "http://") + PathStream
	return `<Connect><Stream url="` + xmlEscape(wsURL) + `">` +
		`<Parameter name="call_id" value="` + xmlEscape(callID) + `"/>` +
		`<Parameter name="token" value="` + xmlEscape(token) + `"/>` +
		`</Stream></Connect>`
}

func (t *Transport) incoming(w http.ResponseWriter, r *http.Request) {
	form, ok := t.verify(w, r)
	if !ok {
		return
	}
	sid, from := form.Get("CallSid"), form.Get("From")
	if sid == "" {
		writeTwiML(w, `<Reject/>`)
		return
	}
	callID, token, err := t.opts.Gateway.AdmitInbound(sid, from)
	if err != nil {
		// Reject before answering, so an unknown caller costs nothing.
		writeTwiML(w, `<Reject reason="rejected"/>`)
		return
	}
	writeTwiML(w, t.streamTwiML(callID, token))
}

func (t *Transport) outbound(w http.ResponseWriter, r *http.Request) {
	form, ok := t.verify(w, r)
	if !ok {
		return
	}
	callID := r.PathValue("call_id")
	// The token stays until it expires: Twilio may repeat this request,
	// and the gateway accepts the stream only once anyway.
	t.mu.Lock()
	pt, known := t.tokens[callID]
	t.mu.Unlock()
	token := pt.token
	if !known {
		writeTwiML(w, `<Hangup/>`)
		return
	}
	if answeredBy := form.Get("AnsweredBy"); strings.HasPrefix(answeredBy, "machine") || answeredBy == "fax" {
		t.opts.Gateway.HandleStatus(form.Get("CallSid"), voice.StatusMachine)
		writeTwiML(w, `<Hangup/>`)
		return
	}
	writeTwiML(w, t.streamTwiML(callID, token))
}

func (t *Transport) status(w http.ResponseWriter, r *http.Request) {
	form, ok := t.verify(w, r)
	if !ok {
		return
	}
	status := voice.CallStatus(form.Get("CallStatus"))
	if answeredBy := form.Get("AnsweredBy"); strings.HasPrefix(answeredBy, "machine") || answeredBy == "fax" {
		status = voice.StatusMachine
	}
	t.opts.Gateway.HandleStatus(form.Get("CallSid"), status)
	w.WriteHeader(http.StatusNoContent)
}

// Dial implements voice.Dialer.
func (t *Transport) Dial(ctx context.Context, req voice.DialRequest) (string, error) {
	if t.opts.Client == nil {
		return "", &voice.DialError{NotPlaced: true, Code: voice.CodeNotConfigured, Err: errors.New("no Twilio client")}
	}
	t.mu.Lock()
	for id, pt := range t.tokens {
		if time.Since(pt.at) > tokenTTL {
			delete(t.tokens, id)
		}
	}
	t.tokens[req.CallID] = pendingToken{token: req.StreamToken, at: time.Now()}
	t.mu.Unlock()
	sid, err := t.opts.Client.CreateCall(ctx, CallRequest{
		To: req.To, From: t.opts.Config.PhoneNumber,
		URL:              t.base + PathOutbound + url.PathEscape(req.CallID),
		StatusCallback:   t.base + PathStatus,
		Timeout:          req.RingTimeout,
		MachineDetection: t.opts.Config.MachineDetection,
	})
	if err != nil {
		t.mu.Lock()
		delete(t.tokens, req.CallID)
		t.mu.Unlock()
		return "", err
	}
	return sid, nil
}

// Cancel implements voice.Dialer.
func (t *Transport) Cancel(ctx context.Context, sid string) error {
	if t.opts.Client == nil {
		return nil
	}
	return t.opts.Client.EndCall(ctx, sid)
}

type streamMessage struct {
	Event     string `json:"event"`
	StreamSid string `json:"streamSid"`
	Start     *struct {
		StreamSid        string            `json:"streamSid"`
		CallSid          string            `json:"callSid"`
		CustomParameters map[string]string `json:"customParameters"`
		MediaFormat      struct {
			Encoding   string `json:"encoding"`
			SampleRate int    `json:"sampleRate"`
		} `json:"mediaFormat"`
	} `json:"start,omitempty"`
	Media *struct {
		Track   string `json:"track"`
		Payload string `json:"payload"`
	} `json:"media,omitempty"`
	Mark *struct {
		Name string `json:"name"`
	} `json:"mark,omitempty"`
}

func (t *Transport) stream(w http.ResponseWriter, r *http.Request) {
	// The one-time token in the start message authenticates the stream:
	// it was handed out only in a response to a signed webhook. Twilio
	// also signs the handshake; a mismatch is logged rather than refused
	// because the signed URL form (https or wss) is not documented.
	if sig := r.Header.Get("X-Twilio-Signature"); sig != "" {
		wss := "wss://" + strings.TrimPrefix(t.publicURL(r), "https://")
		if !ValidSignature(t.opts.AuthToken.Reveal(), t.publicURL(r), nil, sig) && !ValidSignature(t.opts.AuthToken.Reveal(), wss, nil, sig) {
			slog.Warn("Twilio media stream handshake signature did not match; relying on the stream token")
		}
	}
	conn, err := t.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	t.streams.Add(1)
	defer t.streams.Done()
	conn.SetReadLimit(maxStreamMessage)
	leg := newLeg(conn, t)

	_ = conn.SetReadDeadline(time.Now().Add(startTimeout))
	var start *streamMessage
	for start == nil {
		var msg streamMessage
		if err := conn.ReadJSON(&msg); err != nil {
			_ = conn.Close()
			return
		}
		switch msg.Event {
		case "connected":
		case "start":
			if msg.Start == nil {
				_ = conn.Close()
				return
			}
			start = &msg
		default:
			_ = conn.Close()
			return
		}
	}
	_ = conn.SetReadDeadline(time.Time{})
	if f := start.Start.MediaFormat; f.Encoding != "" && (f.Encoding != "audio/x-mulaw" || f.SampleRate != 8000) {
		slog.Warn("Unsupported Twilio media format", "encoding", f.Encoding, "rate", f.SampleRate)
		_ = conn.Close()
		return
	}
	leg.streamSid, leg.callSid = start.Start.StreamSid, start.Start.CallSid
	params := start.Start.CustomParameters
	go leg.readLoop()
	if err := t.opts.Gateway.Attach(r.Context(), params["call_id"], params["token"], leg); err != nil {
		slog.Warn("Refused Twilio media stream", "error", err)
	}
	_ = leg.Hangup(context.Background())
	<-leg.readDone
}

// leg adapts a Twilio media stream to voice.CallLeg.
type leg struct {
	t         *Transport
	conn      *websocket.Conn
	streamSid string
	callSid   string

	audio    chan voice.AudioFrame
	marks    chan string
	done     chan struct{}
	readDone chan struct{}
	writeMu  sync.Mutex
	once     sync.Once
	closed   bool
}

func newLeg(conn *websocket.Conn, t *Transport) *leg {
	return &leg{
		t: t, conn: conn,
		audio: make(chan voice.AudioFrame, audioBuffer), marks: make(chan string, 32),
		done: make(chan struct{}), readDone: make(chan struct{}),
	}
}

func (l *leg) readLoop() {
	defer close(l.readDone)
	defer close(l.audio)
	defer l.end()
	for {
		var msg streamMessage
		if err := l.conn.ReadJSON(&msg); err != nil {
			return
		}
		switch msg.Event {
		case "media":
			if msg.Media == nil || (msg.Media.Track != "" && msg.Media.Track != "inbound") {
				continue
			}
			data, err := base64.StdEncoding.DecodeString(msg.Media.Payload)
			if err != nil || len(data) == 0 {
				continue
			}
			f := voice.AudioFrame{Encoding: voice.EncodingMulaw8k, Data: data}
			if f.Validate() != nil {
				continue
			}
			select {
			case l.audio <- f:
			default:
				// Recognition is behind; dropping audio beats growing
				// latency on a live call.
			}
		case "mark":
			if msg.Mark != nil {
				select {
				case l.marks <- msg.Mark.Name:
				default:
				}
			}
		case "stop":
			return
		}
	}
}

func (l *leg) end() { l.once.Do(func() { close(l.done) }) }

func (l *leg) Encoding() voice.Encoding       { return voice.EncodingMulaw8k }
func (l *leg) Audio() <-chan voice.AudioFrame { return l.audio }
func (l *leg) Marks() <-chan string           { return l.marks }
func (l *leg) Done() <-chan struct{}          { return l.done }

func (l *leg) write(ctx context.Context, v any) error {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	if l.closed {
		return voice.ErrCallEnded
	}
	select {
	case <-l.done:
		return voice.ErrCallEnded
	default:
	}
	deadline := time.Now().Add(streamWriteWait)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = l.conn.SetWriteDeadline(deadline)
	if err := l.conn.WriteJSON(v); err != nil {
		return voice.Errorf(voice.CodeTransportFailed, "twilio stream", errors.New("write failed"))
	}
	return nil
}

func (l *leg) Send(ctx context.Context, f voice.AudioFrame) error {
	if f.Encoding != voice.EncodingMulaw8k {
		return voice.Errorf(voice.CodeInvalidInput, "twilio stream", errors.New("audio must be mu-law 8 kHz"))
	}
	return l.write(ctx, map[string]any{"event": "media", "streamSid": l.streamSid, "media": map[string]string{"payload": base64.StdEncoding.EncodeToString(f.Data)}})
}

func (l *leg) Clear(ctx context.Context) error {
	return l.write(ctx, map[string]string{"event": "clear", "streamSid": l.streamSid})
}

func (l *leg) Mark(ctx context.Context, name string) error {
	return l.write(ctx, map[string]any{"event": "mark", "streamSid": l.streamSid, "mark": map[string]string{"name": name}})
}

// Hangup closes the stream; with nothing after <Connect> in the TwiML,
// Twilio then ends the call. The REST call makes sure of it.
func (l *leg) Hangup(ctx context.Context) error {
	l.writeMu.Lock()
	already := l.closed
	l.closed = true
	if !already {
		_ = l.conn.SetWriteDeadline(time.Now().Add(time.Second))
		_ = l.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		_ = l.conn.Close()
	}
	l.writeMu.Unlock()
	l.end()
	if !already && l.callSid != "" && l.t.opts.Client != nil {
		cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if err := l.t.opts.Client.EndCall(cctx, l.callSid); err != nil {
			slog.Debug("Twilio call end request failed", "error", err)
		}
	}
	return nil
}

// RateLimiter is a fixed-window per-key request limiter.
type RateLimiter struct {
	limit  int
	window time.Duration
	mu     sync.Mutex
	counts map[string]*windowCount
}

type windowCount struct {
	start time.Time
	n     int
}

// NewRateLimiter allows limit requests per key per window.
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{limit: limit, window: window, counts: map[string]*windowCount{}}
}

// Allow records one request for key and reports whether it is allowed.
func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	if len(rl.counts) > 4096 {
		for k, c := range rl.counts {
			if now.Sub(c.start) > rl.window {
				delete(rl.counts, k)
			}
		}
	}
	c, ok := rl.counts[key]
	if !ok || now.Sub(c.start) > rl.window {
		rl.counts[key] = &windowCount{start: now, n: 1}
		return true
	}
	c.n++
	return c.n <= rl.limit
}

// MarshalStart builds a Twilio start message, for tests and simulators.
func MarshalStart(streamSid, callSid string, params map[string]string) []byte {
	b, _ := json.Marshal(map[string]any{
		"event": "start", "streamSid": streamSid,
		"start": map[string]any{
			"streamSid": streamSid, "callSid": callSid, "customParameters": params,
			"mediaFormat": map[string]any{"encoding": "audio/x-mulaw", "sampleRate": 8000, "channels": 1},
		},
	})
	return b
}
