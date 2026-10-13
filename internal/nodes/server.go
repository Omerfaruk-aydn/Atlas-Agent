package nodes

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/procown"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/taskstate"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/transfer"
)

const (
	maxJSONBody      = 1 << 20
	maxOutputBytes   = 256 << 10
	defaultExecLimit = 10 * time.Minute
	maxExecLimit     = time.Hour
	observationTTL   = 2 * time.Minute
)

// ServerOptions configures a node.
type ServerOptions struct {
	// StateDir holds the certificate, device list and journal.
	StateDir string
	// Root is the only directory commands and transfers may use.
	Root         string
	Name         string
	Capabilities []Capability
	Isolated     bool
	MaxFileBytes int64
	// Desktop is the interactive session backend, nil when unavailable.
	Desktop       computer.Backend
	DesktopReason string
	Hosts         []string
	Logger        *slog.Logger
	// Access, when set, replaces paired-device tokens with session-scoped
	// grants (remote support). Pairing is then disabled on this server.
	Access Access
}

// Server is one node's HTTPS API.
type Server struct {
	opts    ServerOptions
	id      Identity
	auth    *Auth
	journal *Journal

	mu          sync.Mutex
	procs       map[string]*procown.Proc
	execWorkers sync.WaitGroup

	desktopMu sync.Mutex
	obs       *observationState
	// consumed is the observation the last pointer input used up. It
	// still authorizes keyboard input (which aims at the focused window,
	// not a point) until a newer observation replaces it.
	consumed *observationState

	limitMu  sync.Mutex
	failures map[string][]time.Time

	uploads *transfer.Uploads
}

type observationState struct {
	id         string
	size       computer.Size
	at         time.Time
	session    string
	generation uint64
}

// NewServer prepares a node: identity, device list and journal.
func NewServer(opts ServerOptions) (*Server, error) {
	if opts.StateDir == "" || !filepath.IsAbs(opts.Root) {
		return nil, errors.New("node needs a state directory and an absolute root")
	}
	if info, err := os.Stat(opts.Root); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("node root %s is not a directory", opts.Root)
	}
	if opts.MaxFileBytes <= 0 {
		opts.MaxFileBytes = transfer.DefaultMaxBytes
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Name == "" {
		opts.Name, _ = os.Hostname()
	}
	id, err := LoadOrCreateIdentity(opts.StateDir, opts.Hosts)
	if err != nil {
		return nil, err
	}
	auth, err := LoadAuth(opts.StateDir)
	if err != nil {
		return nil, err
	}
	journal, err := OpenJournal(filepath.Join(opts.StateDir, "ops"))
	if err != nil {
		return nil, err
	}
	uploads, err := transfer.OpenUploads(filepath.Join(opts.StateDir, "uploads"))
	if err != nil {
		return nil, err
	}
	return &Server{opts: opts, id: id, auth: auth, journal: journal, uploads: uploads, procs: map[string]*procown.Proc{}, failures: map[string][]time.Time{}}, nil
}

// Journal exposes the operation journal for owner-side reconciliation.
func (s *Server) Journal() *Journal { return s.journal }

// Identity returns the node certificate identity.
func (s *Server) Identity() Identity { return s.id }

// Auth exposes pairing and revocation for the CLI.
func (s *Server) Auth() *Auth { return s.auth }

// TLSConfig serves the node certificate.
func (s *Server) TLSConfig() *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{s.id.Certificate}}
}

func (s *Server) has(c Capability) bool { return slices.Contains(s.opts.Capabilities, c) }

// Handler returns the routed API.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/node/hello", s.handleHello)
	mux.HandleFunc("POST /v1/node/pair", s.handlePair)
	mux.HandleFunc("GET /v1/node/info", s.guard(NeedInfo, s.handleInfo))
	mux.HandleFunc("POST /v1/node/revoke", s.guard(NeedInfo, s.handleRevoke))
	mux.HandleFunc("GET /v1/node/sysinfo", s.guard(NeedSystemInfo, s.handleSysinfo))
	mux.HandleFunc("POST /v1/node/exec", s.guard(NeedExec, s.handleExec))
	mux.HandleFunc("GET /v1/node/ops/{id}", s.guard(NeedOps, s.handleGetOp))
	mux.HandleFunc("POST /v1/node/ops/{id}/cancel", s.guard(NeedOps, s.handleCancelOp))
	mux.HandleFunc("PUT /v1/node/files", s.guard(NeedFileWrite, s.handlePutFile))
	mux.HandleFunc("GET /v1/node/files", s.guard(NeedFileRead, s.handleGetFile))
	mux.HandleFunc("GET /v1/node/files/list", s.guard(NeedFileList, s.handleListFiles))
	mux.HandleFunc("POST /v1/node/uploads", s.guard(NeedFileWrite, s.handleCreateUpload))
	mux.HandleFunc("GET /v1/node/uploads/{id}", s.guard(NeedFileWrite, s.handleUploadStatus))
	mux.HandleFunc("PUT /v1/node/uploads/{id}", s.guard(NeedFileWrite, s.handleUploadChunk))
	mux.HandleFunc("POST /v1/node/uploads/{id}/complete", s.guard(NeedFileWrite, s.handleUploadComplete))
	mux.HandleFunc("DELETE /v1/node/uploads/{id}", s.guard(NeedFileWrite, s.handleUploadAbort))
	mux.HandleFunc("GET /v1/node/desktop/observe", s.guard(NeedDesktopView, s.handleObserve))
	mux.HandleFunc("GET /v1/node/desktop/preview", s.guard(NeedDesktopView, s.handlePreview))
	mux.HandleFunc("POST /v1/node/desktop/input", s.guard(NeedDesktopInput, s.handleInput))
	mux.HandleFunc("POST /v1/node/desktop/automation", s.guard(NeedDesktopView, s.handleAutomation))
	mux.HandleFunc("POST /v1/node/desktop/launch", s.guard(NeedDesktopLaunch, s.handleLaunch))
	return mux
}

func deviceFrom(r *http.Request) Device { return grantFrom(r).Device }

func remoteHost(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// limited reports whether host exceeded 20 auth failures in a minute.
func (s *Server) limited(host string) bool {
	s.limitMu.Lock()
	defer s.limitMu.Unlock()
	cutoff := time.Now().Add(-time.Minute)
	kept := s.failures[host][:0]
	for _, t := range s.failures[host] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	s.failures[host] = kept
	return len(kept) >= 20
}

func (s *Server) recordFailure(host string) {
	s.limitMu.Lock()
	defer s.limitMu.Unlock()
	s.failures[host] = append(s.failures[host], time.Now())
}

// guard authenticates the request and checks need before next runs. With
// an Access the decision is entirely the Access's; otherwise the paired
// device token and the node's coarse capabilities apply.
func (s *Server) guard(need Need, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var g Grant
		if s.opts.Access != nil {
			grant, apiErr, status := s.opts.Access.Authorize(r, need)
			if apiErr != nil {
				if status == 0 {
					status = http.StatusForbidden
				}
				writeJSON(w, status, apiErr)
				return
			}
			g = grant
		} else {
			host := remoteHost(r)
			if s.limited(host) {
				writeError(w, http.StatusTooManyRequests, "rate_limited", "too many failed authentications; wait a minute", false, "none")
				return
			}
			token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !ok {
				s.recordFailure(host)
				writeError(w, http.StatusUnauthorized, "unauthorized", ErrUnauthorized.Error(), false, "none")
				return
			}
			d, err := s.auth.Authenticate(token)
			if err != nil {
				s.recordFailure(host)
				writeError(w, http.StatusUnauthorized, "unauthorized", err.Error(), false, "none")
				return
			}
			if c := legacyCapability(need); c != "" && !s.has(c) {
				writeError(w, http.StatusForbidden, "capability_denied", fmt.Sprintf("this node does not grant %q; the node operator must restart it with --allow including %s", c, c), false, "none")
				return
			}
			g = Grant{Device: d}
		}
		if (need == NeedDesktopView || need == NeedDesktopInput || need == NeedDesktopLaunch) && s.opts.Desktop == nil {
			writeError(w, http.StatusConflict, "desktop_unavailable", "desktop control is not available on this node: "+s.opts.DesktopReason, false, "none")
			return
		}
		next(w, withGrant(r, g))
	}
}

// begin journals an operation stamped with the request's grant and tells
// an observer. finish records the outcome the same way.
func (s *Server) begin(r *http.Request, op Operation) (*Operation, bool, error) {
	g := grantFrom(r)
	op.DeviceID = g.Device.ID
	op.SessionID, op.Generation = g.SessionID, g.Generation
	stored, existing, err := s.journal.Begin(op)
	if err == nil && !existing {
		if obs, ok := s.opts.Access.(OperationObserver); ok {
			obs.OperationStarted(g, *stored)
		}
	}
	return stored, existing, err
}

func (s *Server) finish(g Grant, id string, update func(*Operation)) (Operation, error) {
	final, err := s.journal.Finish(id, update)
	if obs, ok := s.opts.Access.(OperationObserver); ok && err == nil {
		obs.OperationFinished(g, final)
	}
	return final, err
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string, inputSent bool, effect string) {
	writeJSON(w, status, APIError{Code: code, Message: message, InputSent: inputSent, Effect: effect})
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), false, "none")
		return false
	}
	return true
}

func (s *Server) hello() Hello {
	return Hello{Protocol: ProtocolVersion, NodeID: s.id.NodeID, Name: s.opts.Name, Fingerprint: s.id.Fingerprint}
}

func (s *Server) info(g Grant) Info {
	host, _ := os.Hostname()
	info := Info{Hello: s.hello(), OS: runtime.GOOS, Arch: runtime.GOARCH, Hostname: host, Root: s.opts.Root, Capabilities: s.opts.Capabilities, Desktop: s.has(CapDesktop) && s.opts.Desktop != nil, DesktopReason: s.opts.DesktopReason, Isolated: s.opts.Isolated, DeviceID: g.Device.ID, MaxFileBytes: s.opts.MaxFileBytes}
	if s.opts.Access != nil {
		// A session grant decides availability, not the node's coarse list.
		info.Root, info.Capabilities = "", nil
		info.Desktop = s.opts.Desktop != nil
		info.SessionID, info.Generation, info.Permissions = g.SessionID, g.Generation, g.Permissions
		for _, r := range sortedRoots(g.Roots) {
			info.Roots = append(info.Roots, r.Label)
		}
	}
	return info
}

func (s *Server) handleHello(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.hello())
}

func (s *Server) handlePair(w http.ResponseWriter, r *http.Request) {
	if s.opts.Access != nil {
		writeError(w, http.StatusNotFound, "not_found", "this node does not accept device pairing", false, "none")
		return
	}
	host := remoteHost(r)
	if s.limited(host) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many failed attempts; wait a minute", false, "none")
		return
	}
	var req PairRequest
	if !decode(w, r, &req) {
		return
	}
	d, token, err := s.auth.Pair(req.Code, req.DeviceName)
	if err != nil {
		s.recordFailure(host)
		code := "pairing_invalid"
		if errors.Is(err, ErrPairingLocked) {
			code = "pairing_locked"
		}
		writeError(w, http.StatusForbidden, code, err.Error(), false, "none")
		return
	}
	s.opts.Logger.Info("Node paired with controller", "device", d.ID, "name", d.Name, "remote", host)
	writeJSON(w, http.StatusOK, PairResponse{DeviceID: d.ID, Token: token, Info: s.info(Grant{Device: d})})
}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.info(grantFrom(r)))
}

func (s *Server) handleRevoke(w http.ResponseWriter, r *http.Request) {
	if s.opts.Access != nil {
		writeError(w, http.StatusNotFound, "not_found", "session access ends through the session owner, not device revocation", false, "none")
		return
	}
	d := deviceFrom(r)
	if err := s.auth.Revoke(d.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "persist_failed", err.Error(), false, "unknown")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"revoked": d.ID})
}

// boundedBuffer keeps the first limit bytes and notes truncation.
type boundedBuffer struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if room := b.limit - b.buf.Len(); room > 0 {
		if len(p) > room {
			b.buf.Write(p[:room])
			b.truncated = true
		} else {
			b.buf.Write(p)
		}
	} else if len(p) > 0 {
		b.truncated = true
	}
	return len(p), nil
}

func (b *boundedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (s *Server) resolveDir(g Grant, rel string) (string, error) {
	if rel == "" || rel == "." {
		return filepath.EvalSymlinks(s.defaultDir(g))
	}
	_, dir, err := s.resolveFor(g, rel)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("cwd %q is not a directory under an allowed folder", rel)
	}
	return dir, nil
}

// beginError writes the journal refusal for an operation that never ran.
func beginError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	if errors.Is(err, ErrOperationConflict) {
		status = http.StatusConflict
	}
	writeError(w, status, "operation_conflict", err.Error(), false, "none")
}

func (s *Server) handleExec(w http.ResponseWriter, r *http.Request) {
	var req ExecRequest
	if !decode(w, r, &req) {
		return
	}
	if len(req.Argv) == 0 || req.Argv[0] == "" || len(req.Argv) > 256 {
		writeError(w, http.StatusBadRequest, "invalid_request", "argv must contain 1-256 literal arguments", false, "none")
		return
	}
	limit := defaultExecLimit
	if req.TimeoutMS > 0 {
		limit = min(time.Duration(req.TimeoutMS)*time.Millisecond, maxExecLimit)
	}
	g := grantFrom(r)
	hashed := req
	hashed.Wait = nil
	// A repeated operation id returns its recorded result before any
	// precondition is re-evaluated, so a retry never runs twice.
	if replay, err := s.journal.Replay(req.OperationID, g.Device.ID, RequestHash(hashed), g.SessionID); err != nil {
		beginError(w, err)
		return
	} else if replay != nil {
		writeJSON(w, http.StatusOK, replay)
		return
	}
	if policy, ok := s.opts.Access.(ExecPolicy); ok {
		if apiErr := policy.CheckExec(g, "exec", req.Argv); apiErr != nil {
			writeJSON(w, http.StatusForbidden, apiErr)
			return
		}
	}
	dir, err := s.resolveDir(g, req.Cwd)
	if err != nil {
		writeError(w, http.StatusBadRequest, transferCode(err, "invalid_path"), err.Error(), false, "none")
		return
	}
	op, existing, err := s.begin(r, Operation{ID: req.OperationID, Kind: "exec", RequestHash: RequestHash(hashed), Argv: req.Argv, Cwd: req.Cwd})
	if err != nil {
		beginError(w, err)
		return
	}
	if existing {
		writeJSON(w, http.StatusOK, op)
		return
	}
	stdout, stderr := &boundedBuffer{limit: maxOutputBytes}, &boundedBuffer{limit: maxOutputBytes}
	proc, err := procown.Start(procown.Spec{Argv: req.Argv, Dir: dir, Stdout: stdout, Stderr: stderr})
	if err != nil {
		final, ferr := s.finish(g, op.ID, func(o *Operation) {
			o.State, o.Effect, o.Error = taskstate.Failed, "none", "start: "+err.Error()
		})
		if ferr != nil {
			writeError(w, http.StatusInternalServerError, "persist_failed", ferr.Error(), false, "none")
			return
		}
		writeJSON(w, http.StatusOK, final)
		return
	}
	s.mu.Lock()
	s.procs[op.ID] = proc
	s.mu.Unlock()
	if cur, ok := s.journal.Get(op.ID, g.Device.ID); ok && cur.State.Terminal() {
		// Canceled (for example by the owner ending the session) between
		// journaling and starting: stop at once.
		proc.Stop(3 * time.Second)
	}
	finished := make(chan Operation, 1)
	s.execWorkers.Add(1)
	go func() {
		defer s.execWorkers.Done()
		s.superviseExec(g, op.ID, proc, limit, stdout, stderr, finished)
	}()
	if req.Wait != nil && !*req.Wait {
		current, _ := s.journal.Get(op.ID, g.Device.ID)
		writeJSON(w, http.StatusAccepted, current)
		return
	}
	select {
	case final := <-finished:
		writeJSON(w, http.StatusOK, final)
	case <-r.Context().Done():
		// The controller went away; the operation keeps running and is
		// reconciled later through GET /v1/node/ops/{id}.
	}
}

func (s *Server) superviseExec(g Grant, id string, proc *procown.Proc, limit time.Duration, stdout, stderr *boundedBuffer, finished chan<- Operation) {
	timer := time.NewTimer(limit)
	defer timer.Stop()
	timedOut := false
	select {
	case <-proc.Done():
	case <-timer.C:
		timedOut = true
		proc.Stop(3 * time.Second)
		<-proc.Done()
	}
	code, _ := proc.Result()
	s.mu.Lock()
	delete(s.procs, id)
	s.mu.Unlock()
	final, err := s.finish(g, id, func(o *Operation) {
		o.Stdout, o.Stderr = stdout.String(), stderr.String()
		o.Truncated = stdout.truncated || stderr.truncated
		o.ExitCode = &code
		o.Effect = "applied"
		switch {
		case timedOut:
			o.State, o.Error = taskstate.Failed, fmt.Sprintf("timed out after %s; process tree stopped", limit)
		case code == 0:
			o.State = taskstate.Completed
		default:
			o.State, o.Error = taskstate.Failed, "exit status "+strconv.Itoa(code)
		}
	})
	if err != nil {
		s.opts.Logger.Error("Failed to persist node operation result", "operation", id, "error", err)
		final.State, final.Error = taskstate.Failed, err.Error()
	}
	finished <- final
}

func (s *Server) handleGetOp(w http.ResponseWriter, r *http.Request) {
	op, ok := s.journal.Get(r.PathValue("id"), deviceFrom(r).ID, grantFrom(r).SessionID)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "no operation with that id for this device", false, "none")
		return
	}
	writeJSON(w, http.StatusOK, op)
}

func (s *Server) handleCancelOp(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	d := deviceFrom(r)
	op, ok := s.journal.Get(id, d.ID, grantFrom(r).SessionID)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "no operation with that id for this device", false, "none")
		return
	}
	if op.State.Terminal() {
		writeJSON(w, http.StatusOK, op)
		return
	}
	s.mu.Lock()
	proc := s.procs[id]
	s.mu.Unlock()
	if proc != nil {
		// Mark canceled first so the supervisor's later result cannot turn
		// the operation back into a success.
		final, err := s.finish(grantFrom(r), id, func(o *Operation) {
			o.State, o.Effect, o.Error = taskstate.Canceled, "unknown", "canceled by controller; partial effects possible"
		})
		proc.Stop(3 * time.Second)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "persist_failed", err.Error(), true, "unknown")
			return
		}
		writeJSON(w, http.StatusOK, final)
		return
	}
	writeJSON(w, http.StatusOK, op)
}

func (s *Server) handlePutFile(w http.ResponseWriter, r *http.Request) {
	g := grantFrom(r)
	rel := r.URL.Query().Get("path")
	sha := strings.ToLower(r.Header.Get("X-Atlas-SHA256"))
	size, err := strconv.ParseInt(r.Header.Get("X-Atlas-Size"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "X-Atlas-Size header is required", false, "none")
		return
	}
	overwrite := r.URL.Query().Get("overwrite") == "1"
	if overwrite && !s.allowModify(w, r) {
		return
	}
	opID := r.Header.Get("X-Atlas-Operation")
	root, inner, err := s.splitFor(g, rel)
	if err != nil {
		writeError(w, http.StatusBadRequest, transfer.Code(err), err.Error(), false, "none")
		return
	}
	op, existing, err := s.begin(r, Operation{ID: opID, Kind: "push", RequestHash: RequestHash([]any{rel, sha, size, overwrite}), Path: rel, SHA256: sha, Size: size})
	if err != nil {
		beginError(w, err)
		return
	}
	if existing {
		if op.State == taskstate.Completed {
			writeJSON(w, http.StatusOK, FileResult{Path: op.Path, SHA256: op.SHA256, Size: op.Size, Replayed: true})
			return
		}
		writeError(w, http.StatusConflict, "operation_"+string(op.State), "this transfer already ran: "+op.Error, false, op.Effect)
		return
	}
	_, rerr := transfer.Receive(root, inner, r.Body, size, sha, s.maxFile(), overwrite)
	final, ferr := s.finish(g, op.ID, func(o *Operation) {
		if rerr != nil {
			o.State, o.Effect, o.Error = taskstate.Failed, "none", rerr.Error()
			return
		}
		o.State, o.Effect = taskstate.Completed, "applied"
	})
	if ferr != nil {
		writeError(w, http.StatusInternalServerError, "persist_failed", ferr.Error(), true, "unknown")
		return
	}
	if rerr != nil {
		writeError(w, http.StatusBadRequest, transfer.Code(rerr), rerr.Error(), false, "none")
		return
	}
	writeJSON(w, http.StatusOK, FileResult{Path: final.Path, SHA256: final.SHA256, Size: final.Size})
}

// allowModify checks the separate permission to replace existing files.
// Paired-device nodes grant it with files; session grants decide alone.
func (s *Server) allowModify(w http.ResponseWriter, r *http.Request) bool {
	if s.opts.Access == nil {
		return true
	}
	if _, apiErr, status := s.opts.Access.Authorize(r, NeedFileModify); apiErr != nil {
		if status == 0 {
			status = http.StatusForbidden
		}
		writeJSON(w, status, apiErr)
		return false
	}
	return true
}

func (s *Server) maxFile() int64 { return s.opts.MaxFileBytes }

func (s *Server) handleGetFile(w http.ResponseWriter, r *http.Request) {
	g := grantFrom(r)
	root, inner, err := s.splitFor(g, r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, transfer.Code(err), err.Error(), false, "none")
		return
	}
	f, size, sum, err := transfer.Open(root, inner, s.maxFile())
	if err != nil {
		status := http.StatusBadRequest
		if transfer.Code(err) == "not_found" {
			status = http.StatusNotFound
		}
		writeError(w, status, transfer.Code(err), err.Error(), false, "none")
		return
	}
	defer f.Close()
	// A resumed download names the content it expects; a changed file is
	// never appended to a partial copy of another one.
	if want := strings.ToLower(r.Header.Get("X-Atlas-If-SHA256")); want != "" && want != sum {
		writeError(w, http.StatusPreconditionFailed, "source_changed", "the file changed since the download started; restart it", false, "none")
		return
	}
	offset := int64(0)
	if spec, ok := strings.CutPrefix(r.Header.Get("Range"), "bytes="); ok {
		start, _, _ := strings.Cut(spec, "-")
		n, perr := strconv.ParseInt(start, 10, 64)
		if perr != nil || n < 0 || n > size {
			writeError(w, http.StatusRequestedRangeNotSatisfiable, "invalid_range", "range start must be within the file", false, "none")
			return
		}
		offset = n
	}
	if offset > 0 {
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			writeError(w, http.StatusInternalServerError, "io_error", err.Error(), false, "none")
			return
		}
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Atlas-SHA256", sum)
	w.Header().Set("X-Atlas-Size", strconv.FormatInt(size, 10))
	w.Header().Set("X-Atlas-Offset", strconv.FormatInt(offset, 10))
	w.Header().Set("Content-Length", strconv.FormatInt(size-offset, 10))
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, f)
}

// transferCode keeps classified transfer errors and falls back otherwise.
func transferCode(err error, fallback string) string {
	var te *transfer.Error
	if errors.As(err, &te) {
		return te.Code
	}
	return fallback
}
