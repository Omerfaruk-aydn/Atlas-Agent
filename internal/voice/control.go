package voice

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ControlState tells local agents where the running voice service listens.
// It lives in a 0600 file next to the call store.
type ControlState struct {
	Addr      string    `json:"addr"`
	Token     string    `json:"token"`
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`
}

// StateDir is where the voice service keeps its files under dataDir.
func StateDir(dataDir string) string { return filepath.Join(dataDir, "voice") }

// ControlStatePath is the control state file under dataDir.
func ControlStatePath(dataDir string) string { return filepath.Join(StateDir(dataDir), "control.json") }

// WriteControlState stores st privately.
func WriteControlState(path string, st ControlState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ReadControlState loads the control state.
func ReadControlState(path string) (ControlState, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return ControlState{}, err
	}
	var st ControlState
	if err := json.Unmarshal(b, &st); err != nil {
		return ControlState{}, err
	}
	if st.Addr == "" || st.Token == "" {
		return ControlState{}, errors.New("voice control state is incomplete")
	}
	return st, nil
}

// StatusView summarizes the running service.
type StatusView struct {
	Enabled          bool        `json:"enabled"`
	ActiveCalls      []string    `json:"active_calls"`
	RemainingMinutes int         `json:"remaining_minutes"`
	Sites            []SiteState `json:"sites,omitempty"`
}

// ControlServer is the loopback API agents use to reach the voice service.
type ControlServer struct {
	Config  Config
	Router  *Router
	Gateway *Gateway
	Store   *Store
	Budget  *Budget
	Monitor *Monitor
	Token   string
}

const maxControlBody = 64 << 10

// Handler returns the authenticated API.
func (s *ControlServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/voice/owner-calls", s.ownerCall)
	mux.HandleFunc("GET /v1/voice/calls", s.calls)
	mux.HandleFunc("GET /v1/voice/calls/{id}", s.call)
	mux.HandleFunc("GET /v1/voice/calls/{id}/transcript", s.transcript)
	mux.HandleFunc("POST /v1/voice/calls/{id}/cancel", s.cancel)
	mux.HandleFunc("POST /v1/voice/turns", s.turn)
	mux.HandleFunc("GET /v1/voice/status", s.status)
	mux.HandleFunc("POST /v1/voice/monitor/{site}/notify-on-recovery", s.notifyOnRecovery)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !loopbackHost(r.Host) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		auth := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if s.Token == "" || subtle.ConstantTimeCompare([]byte(auth), []byte(s.Token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxControlBody)
		mux.ServeHTTP(w, r)
	})
}

func loopbackHost(hostport string) bool {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		host = hostport
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

type apiError struct {
	Error string `json:"error"`
	Code  Code   `json:"code,omitempty"`
}

func writeErr(w http.ResponseWriter, err error) {
	status, code := http.StatusInternalServerError, CodeOf(err)
	switch {
	case errors.Is(err, ErrServiceDisabled):
		status, code = http.StatusServiceUnavailable, CodeNotConfigured
	case code == CodeInvalidInput:
		status = http.StatusBadRequest
	case code == CodeNotConfigured:
		status = http.StatusServiceUnavailable
	}
	// Only the code and a generic message leave the process.
	msg := string(code)
	var ve *Error
	if errors.As(err, &ve) && (ve.Code == CodeInvalidInput || ve.Code == CodeNotConfigured) && ve.Err != nil {
		msg = ve.Err.Error()
	}
	if errors.Is(err, ErrServiceDisabled) {
		msg = ErrServiceDisabled.Error()
	}
	writeJSON(w, status, apiError{Error: msg, Code: code})
}

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return Errorf(CodeInvalidInput, "decode request", fmt.Errorf("invalid JSON: %w", err))
	}
	return nil
}

func (s *ControlServer) ownerCall(w http.ResponseWriter, r *http.Request) {
	var req OwnerCallRequest
	if err := decode(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	if s.Router == nil {
		writeErr(w, ErrServiceDisabled)
		return
	}
	cfg := s.Config.WithDefaults()
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(cfg.Outbound.RingTimeoutSeconds*2+90)*time.Second)
	defer cancel()
	res, err := s.Router.CallOwner(ctx, req)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// PublicView strips transcript text from a record.
func PublicView(r CallRecord) CallRecord {
	r.Turns = nil
	return r
}

func (s *ControlServer) calls(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	limit = min(max(limit, 1), 50)
	recs := s.Store.List(limit)
	for i := range recs {
		recs[i] = PublicView(recs[i])
	}
	writeJSON(w, http.StatusOK, recs)
}

func (s *ControlServer) call(w http.ResponseWriter, r *http.Request) {
	rec, ok := s.Store.Get(r.PathValue("id"))
	if !ok {
		writeJSON(w, http.StatusNotFound, apiError{Error: "call not found"})
		return
	}
	writeJSON(w, http.StatusOK, PublicView(rec))
}

func (s *ControlServer) transcript(w http.ResponseWriter, r *http.Request) {
	rec, ok := s.Store.Get(r.PathValue("id"))
	if !ok {
		writeJSON(w, http.StatusNotFound, apiError{Error: "call not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"call_id": rec.ID, "stored": s.Config.KeepTranscripts(), "turns": rec.Turns, "summary": rec.Summary})
}

func (s *ControlServer) cancel(w http.ResponseWriter, r *http.Request) {
	if s.Gateway == nil {
		writeErr(w, ErrServiceDisabled)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"cancelled": s.Gateway.Cancel(r.PathValue("id"))})
}

func (s *ControlServer) turn(w http.ResponseWriter, r *http.Request) {
	var req TextTurnRequest
	if err := decode(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	if s.Gateway == nil {
		writeErr(w, ErrServiceDisabled)
		return
	}
	text, err := s.Gateway.RunTextTurn(r.Context(), req)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"run_id": req.RunID, "text": text})
}

func (s *ControlServer) status(w http.ResponseWriter, _ *http.Request) {
	v := StatusView{Enabled: s.Config.Enabled, ActiveCalls: []string{}}
	if s.Gateway != nil {
		v.ActiveCalls = s.Gateway.ActiveCalls()
	}
	if s.Budget != nil {
		v.RemainingMinutes = int(s.Budget.RemainingToday() / time.Minute)
	}
	v.Sites = s.Store.Sites()
	for i := range v.Sites {
		v.Sites[i].Checks = nil
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *ControlServer) notifyOnRecovery(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	if s.Monitor == nil {
		writeErr(w, Errorf(CodeNotConfigured, "monitor", errors.New("no sites are monitored")))
		return
	}
	st, err := s.Monitor.SetNotifyOnRecovery(r.PathValue("site"), req.Enabled)
	if err != nil {
		writeErr(w, Errorf(CodeInvalidInput, "monitor", err))
		return
	}
	st.Checks = nil
	writeJSON(w, http.StatusOK, st)
}

// Client talks to a running voice service over its loopback API.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// ErrNotRunning reports that no voice service is running.
var ErrNotRunning = errors.New("the voice service is not running; start it with `atlas phone serve`")

// DialControl builds a client from the control state under dataDir.
func DialControl(dataDir string) (*Client, error) {
	st, err := ReadControlState(ControlStatePath(dataDir))
	if err != nil {
		return nil, ErrNotRunning
	}
	return &Client{BaseURL: "http://" + st.Addr, Token: st.Token, HTTP: &http.Client{Timeout: 5 * time.Minute}}, nil
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = strings.NewReader(string(b))
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		var opErr *net.OpError
		if errors.As(err, &opErr) && opErr.Op == "dial" {
			return ErrNotRunning
		}
		return fmt.Errorf("voice service request failed: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		var e apiError
		if json.Unmarshal(data, &e) == nil && e.Error != "" {
			return Errorf(cmpCode(e.Code, CodeAgentFailed), "voice service", errors.New(e.Error))
		}
		return fmt.Errorf("voice service returned %s", resp.Status)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(data, out)
}

func cmpCode(a, b Code) Code {
	if a != "" {
		return a
	}
	return b
}

// CallOwner asks the service to call the owner.
func (c *Client) CallOwner(ctx context.Context, req OwnerCallRequest) (OwnerCallResult, error) {
	var out OwnerCallResult
	err := c.do(ctx, http.MethodPost, "/v1/voice/owner-calls", req, &out)
	return out, err
}

// Call returns one call without transcript text.
func (c *Client) Call(ctx context.Context, id string) (CallRecord, error) {
	var out CallRecord
	err := c.do(ctx, http.MethodGet, "/v1/voice/calls/"+pathEscape(id), nil, &out)
	return out, err
}

// Calls lists recent calls without transcript text.
func (c *Client) Calls(ctx context.Context, limit int) ([]CallRecord, error) {
	var out []CallRecord
	err := c.do(ctx, http.MethodGet, "/v1/voice/calls?limit="+strconv.Itoa(limit), nil, &out)
	return out, err
}

// Transcript is a call's stored transcript.
type Transcript struct {
	CallID  string       `json:"call_id"`
	Stored  bool         `json:"stored"`
	Turns   []TurnRecord `json:"turns"`
	Summary string       `json:"summary"`
}

// Transcript returns a call's transcript.
func (c *Client) Transcript(ctx context.Context, id string) (Transcript, error) {
	var out Transcript
	err := c.do(ctx, http.MethodGet, "/v1/voice/calls/"+pathEscape(id)+"/transcript", nil, &out)
	return out, err
}

// Status returns the service status.
func (c *Client) Status(ctx context.Context) (StatusView, error) {
	var out StatusView
	err := c.do(ctx, http.MethodGet, "/v1/voice/status", nil, &out)
	return out, err
}

// SetNotifyOnRecovery arms a recovery call for a monitored site.
func (c *Client) SetNotifyOnRecovery(ctx context.Context, site string, on bool) (SiteState, error) {
	var out SiteState
	err := c.do(ctx, http.MethodPost, "/v1/voice/monitor/"+pathEscape(site)+"/notify-on-recovery", map[string]bool{"enabled": on}, &out)
	return out, err
}

func pathEscape(s string) string { return url.PathEscape(s) }

// ServeControl serves the control API on addr until ctx ends. It refuses
// non-loopback addresses.
func ServeControl(ctx context.Context, addr string, h http.Handler) (net.Addr, <-chan error, error) {
	if !loopbackAddr(addr) {
		return nil, nil, errors.New("voice control API must listen on a loopback address")
	}
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", addr)
	if err != nil {
		return nil, nil, fmt.Errorf("listen on voice control address: %w", err)
	}
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute}
	errc := make(chan error, 1)
	go func() {
		err := srv.Serve(ln)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errc <- err
	}()
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(sctx); err != nil {
			slog.Warn("Voice control server shutdown failed", "error", err)
		}
	}()
	return ln.Addr(), errc, nil
}
