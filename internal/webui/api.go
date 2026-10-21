package webui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/proto"
	"github.com/google/uuid"
)

// Request size limits. Prompts may carry attachments, which arrive base64
// encoded, so their limit is larger than the encoded total allowed below.
const (
	maxJSONBody          = 1 << 20
	maxPromptBody        = 48 << 20
	maxPromptChars       = 1 << 20
	maxAttachments       = 10
	maxAttachmentBytes   = 10 << 20
	maxAttachmentsTotal  = 25 << 20
	upstreamTimeout      = 30 * time.Second
	heartbeatInterval    = 15 * time.Second
	defaultFileReadBytes = 512 << 10
)

// Handler returns the gateway's HTTP handler.
func (g *Gateway) Handler() http.Handler {
	api := http.NewServeMux()
	api.HandleFunc("GET /api/bootstrap", g.handleBootstrap)
	api.HandleFunc("POST /api/auth/signout", g.handleSignOut)
	api.HandleFunc("GET /api/projects", g.handleProjects)
	api.HandleFunc("GET /api/workspaces", g.handleListWorkspaces)
	api.HandleFunc("POST /api/workspaces", g.handleOpenWorkspace)
	api.HandleFunc("GET /api/workspaces/{id}", g.withHub(g.handleGetWorkspace))
	api.HandleFunc("DELETE /api/workspaces/{id}", g.handleCloseWorkspace)
	api.HandleFunc("GET /api/workspaces/{id}/events", g.withHub(g.handleEvents))
	api.HandleFunc("GET /api/workspaces/{id}/status", g.withHub(g.handleStatus))
	api.HandleFunc("GET /api/workspaces/{id}/sessions", g.withHub(g.handleListSessions))
	api.HandleFunc("POST /api/workspaces/{id}/sessions", g.withHub(g.handleCreateSession))
	api.HandleFunc("GET /api/workspaces/{id}/sessions/{sid}", g.withHub(g.relayGet("/sessions/{sid}")))
	api.HandleFunc("PATCH /api/workspaces/{id}/sessions/{sid}", g.withHub(g.handleRenameSession))
	api.HandleFunc("DELETE /api/workspaces/{id}/sessions/{sid}", g.withHub(g.relayDelete("/sessions/{sid}")))
	api.HandleFunc("GET /api/workspaces/{id}/sessions/{sid}/messages", g.withHub(g.relayGet("/sessions/{sid}/messages")))
	api.HandleFunc("GET /api/workspaces/{id}/sessions/{sid}/history", g.withHub(g.relayGet("/sessions/{sid}/history")))
	api.HandleFunc("GET /api/workspaces/{id}/sessions/{sid}/agents", g.withHub(g.relayGet("/sessions/{sid}/agenthub")))
	api.HandleFunc("GET /api/workspaces/{id}/commands", g.withHub(g.handleListCommands))
	api.HandleFunc("POST /api/workspaces/{id}/commands/expand", g.withHub(g.handleExpandCommand))
	api.HandleFunc("GET /api/workspaces/{id}/sessions/{sid}/workflow", g.withHub(g.handleWorkflow))
	api.HandleFunc("POST /api/workspaces/{id}/sessions/{sid}/prompt", g.withHub(g.handlePrompt))
	api.HandleFunc("POST /api/workspaces/{id}/sessions/{sid}/cancel", g.withHub(g.relayPost("/agent/sessions/{sid}/cancel")))
	api.HandleFunc("GET /api/workspaces/{id}/sessions/{sid}/queue", g.withHub(g.relayGet("/agent/sessions/{sid}/prompts/list")))
	api.HandleFunc("DELETE /api/workspaces/{id}/sessions/{sid}/queue", g.withHub(g.relayPost("/agent/sessions/{sid}/prompts/clear")))
	api.HandleFunc("POST /api/workspaces/{id}/sessions/{sid}/shell", g.withHub(g.handleShell))
	api.HandleFunc("POST /api/workspaces/{id}/sessions/{sid}/summarize", g.withHub(g.relayPost("/agent/sessions/{sid}/summarize")))
	api.HandleFunc("GET /api/workspaces/{id}/pending", g.withHub(g.handlePending))
	api.HandleFunc("POST /api/workspaces/{id}/permissions/{pid}", g.withHub(g.handlePermission))
	api.HandleFunc("POST /api/workspaces/{id}/questions/{qid}/answer", g.withHub(g.handleAnswer))
	api.HandleFunc("POST /api/workspaces/{id}/questions/{qid}/cancel", g.withHub(g.handleCancelQuestion))
	api.HandleFunc("GET /api/workspaces/{id}/models", g.withHub(g.handleModels))
	api.HandleFunc("POST /api/workspaces/{id}/models", g.withHub(g.handleSelectModel))
	api.HandleFunc("GET /api/workspaces/{id}/settings", g.withHub(g.handleSettings))
	api.HandleFunc("POST /api/workspaces/{id}/permission-mode", g.withHub(g.handlePermissionMode))
	api.HandleFunc("POST /api/workspaces/{id}/compaction", g.withHub(g.handleCompaction))
	api.HandleFunc("POST /api/workspaces/{id}/session-mode", g.withHub(g.handleSessionMode))
	api.HandleFunc("GET /api/workspaces/{id}/files", g.withHub(g.relayGet("/files")))
	api.HandleFunc("GET /api/workspaces/{id}/files/content", g.withHub(g.handleFileContent))
	api.HandleFunc("GET /api/workspaces/{id}/jobs", g.withHub(g.relayGet("/jobs")))
	api.HandleFunc("GET /api/workspaces/{id}/jobs/{jid}/output", g.withHub(g.handleJobOutput))
	api.HandleFunc("DELETE /api/workspaces/{id}/jobs/{jid}", g.withHub(g.relayDelete("/jobs/{jid}")))
	g.registerSessionActions(api)
	api.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "unknown API endpoint")
	})

	root := http.NewServeMux()
	root.Handle("/api/", g.requireSession(api))
	root.HandleFunc("GET /auth/launch", g.handleLaunch)
	root.HandleFunc("POST /control/launch", g.handleControlLaunch)
	root.HandleFunc("GET /control/ping", g.handleControlPing)
	root.Handle("/", g.staticHandler())
	return g.secure(root)
}

// secure applies the checks every request passes, signed in or not.
func (g *Gateway) secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !g.hostAllowed(r.Host) {
			writeError(w, http.StatusMisdirectedRequest, "unrecognized Host header")
			return
		}
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		next.ServeHTTP(w, r)
	})
}

// contentSecurityPolicy keeps the client to its own origin. Inline styles
// are allowed for syntax highlighting; scripts are not. connect-src admits
// blob: and data: because attached images are read back from those URLs;
// neither reaches the network.
const contentSecurityPolicy = "default-src 'self'; script-src 'self' 'wasm-unsafe-eval'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: blob:; font-src 'self' data:; connect-src 'self' blob: data:; worker-src 'self' blob:; " +
	"object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

// withHub resolves the {id} path value to an open workspace and rewrites
// it to the workspace's current server ID.
func (g *Gateway) withHub(next func(http.ResponseWriter, *http.Request, *hub)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h := g.hubFor(r.PathValue("id"))
		if h == nil {
			writeError(w, http.StatusNotFound, "workspace is not open in this web session")
			return
		}
		next(w, r, h)
	}
}

// upstreamPath expands {name} placeholders from the request's path values
// under the workspace prefix.
func upstreamPath(r *http.Request, h *hub, suffix string) string {
	out := "/workspaces/" + url.PathEscape(h.workspaceID()) + suffix
	for _, name := range []string{"sid", "jid"} {
		out = strings.ReplaceAll(out, "{"+name+"}", url.PathEscape(r.PathValue(name)))
	}
	return out
}

func (g *Gateway) relayGet(suffix string) func(http.ResponseWriter, *http.Request, *hub) {
	return func(w http.ResponseWriter, r *http.Request, h *hub) {
		q := url.Values{}
		if p := r.URL.Query().Get("path"); p != "" {
			q.Set("path", p)
		}
		g.relay(w, r, http.MethodGet, upstreamPath(r, h, suffix), q, nil)
	}
}

func (g *Gateway) relayPost(suffix string) func(http.ResponseWriter, *http.Request, *hub) {
	return func(w http.ResponseWriter, r *http.Request, h *hub) {
		g.relay(w, r, http.MethodPost, upstreamPath(r, h, suffix), nil, nil)
	}
}

func (g *Gateway) relayDelete(suffix string) func(http.ResponseWriter, *http.Request, *hub) {
	return func(w http.ResponseWriter, r *http.Request, h *hub) {
		g.relay(w, r, http.MethodDelete, upstreamPath(r, h, suffix), nil, nil)
	}
}

// relay forwards one request and streams the answer back. Only responses
// that carry no configuration or environment are relayed this way.
func (g *Gateway) relay(w http.ResponseWriter, r *http.Request, method, path string, q url.Values, body io.Reader) {
	ctx, cancel := context.WithTimeout(r.Context(), upstreamTimeout)
	defer cancel()
	headers := http.Header{}
	if body != nil {
		headers.Set("Content-Type", "application/json")
	}
	rsp, err := g.opts.Upstream.Do(ctx, method, path, q, body, headers)
	if err != nil {
		writeError(w, http.StatusBadGateway, "the Atlas server is unreachable")
		return
	}
	defer rsp.Body.Close()
	if rsp.StatusCode >= 400 {
		writeError(w, rsp.StatusCode, upstreamMessage(rsp))
		return
	}
	if ct := rsp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(rsp.StatusCode)
	_, _ = io.Copy(w, rsp.Body)
}

// writeUpstreamError reports a failed server call to the browser.
func writeUpstreamError(w http.ResponseWriter, err error) {
	var ue *upstreamError
	if errors.As(err, &ue) {
		status := ue.Status
		if status < 400 {
			status = http.StatusBadGateway
		}
		writeError(w, status, ue.Message)
		return
	}
	writeError(w, http.StatusBadGateway, err.Error())
}

// --- Workspaces ---

// workspaceView is the browser's view of a workspace: no configuration,
// credentials or environment variables, which the server's own workspace
// object carries.
type workspaceView struct {
	ID             string `json:"id"`
	Path           string `json:"path"`
	Name           string `json:"name"`
	Connected      bool   `json:"connected"`
	ServerVersion  string `json:"server_version,omitempty"`
	UsageProfile   string `json:"usage_profile,omitempty"`
	AgentReady     bool   `json:"agent_ready"`
	Configured     bool   `json:"configured"`
	PermissionMode string `json:"permission_mode,omitempty"`
}

func (g *Gateway) describe(ctx context.Context, h *hub) (workspaceView, error) {
	id := h.workspaceID()
	var ws proto.Workspace
	if err := g.getJSON(ctx, "/workspaces/"+url.PathEscape(id), nil, &ws); err != nil {
		return workspaceView{}, err
	}
	view := workspaceView{
		ID:            id,
		Path:          h.path,
		Name:          baseName(h.path),
		ServerVersion: ws.Version,
		UsageProfile:  ws.UsageProfile,
		Configured:    ws.Config != nil && ws.Config.IsConfigured(),
	}
	h.mu.Lock()
	view.Connected = h.connected
	h.mu.Unlock()
	var info proto.AgentInfo
	if err := g.getJSON(ctx, "/workspaces/"+url.PathEscape(id)+"/agent", nil, &info); err == nil {
		view.AgentReady = info.IsReady
	}
	var mode proto.PermissionModeRequest
	if err := g.getJSON(ctx, "/workspaces/"+url.PathEscape(id)+"/permissions/mode", nil, &mode); err == nil {
		view.PermissionMode = mode.Mode
	}
	return view, nil
}

func (g *Gateway) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	s, _ := g.browserSessionFor(r)
	ctx, cancel := context.WithTimeout(r.Context(), upstreamTimeout)
	defer cancel()
	var server proto.VersionInfo
	serverErr := g.getJSON(ctx, "/version", nil, &server)
	workspaces := []workspaceView{}
	for _, h := range g.openHubs() {
		if v, err := g.describe(ctx, h); err == nil {
			workspaces = append(workspaces, v)
		}
	}
	out := map[string]any{
		"csrf":       s.csrf,
		"instance":   g.instance,
		"version":    g.opts.Version,
		"started_at": g.startedAt.UnixMilli(),
		"server": map[string]any{
			"address":   g.opts.ServerAddress,
			"reachable": serverErr == nil,
			"version":   server.Version,
			"platform":  server.Platform,
		},
		"workspaces": workspaces,
	}
	writeJSON(w, http.StatusOK, out)
}

func (g *Gateway) handleProjects(w http.ResponseWriter, _ *http.Request) {
	projects := []Project{}
	if g.opts.RecentProjects != nil {
		projects = append(projects, g.opts.RecentProjects()...)
	}
	writeJSON(w, http.StatusOK, projects)
}

func (g *Gateway) handleListWorkspaces(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), upstreamTimeout)
	defer cancel()
	out := []workspaceView{}
	for _, h := range g.openHubs() {
		if v, err := g.describe(ctx, h); err == nil {
			out = append(out, v)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (g *Gateway) handleOpenWorkspace(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if !decodeJSON(w, r, maxJSONBody, &req) {
		return
	}
	if strings.TrimSpace(req.Path) == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	h, err := g.openWorkspace(ctx, strings.TrimSpace(req.Path))
	if err != nil {
		var ue *upstreamError
		if errors.As(err, &ue) {
			writeUpstreamError(w, err)
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	view, err := g.describe(ctx, h)
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (g *Gateway) handleGetWorkspace(w http.ResponseWriter, r *http.Request, h *hub) {
	ctx, cancel := context.WithTimeout(r.Context(), upstreamTimeout)
	defer cancel()
	view, err := g.describe(ctx, h)
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (g *Gateway) handleCloseWorkspace(w http.ResponseWriter, r *http.Request) {
	if !g.closeWorkspace(r.PathValue("id")) {
		writeError(w, http.StatusNotFound, "workspace is not open in this web session")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Events ---

func (g *Gateway) handleStatus(w http.ResponseWriter, _ *http.Request, h *hub) {
	writeJSON(w, http.StatusOK, h.status())
}

func (g *Gateway) handleEvents(w http.ResponseWriter, r *http.Request, h *hub) {
	rc := http.NewResponseController(w)
	var after uint64
	if v := r.Header.Get("Last-Event-ID"); v != "" {
		after, _ = strconv.ParseUint(v, 10, 64)
	} else if v := r.URL.Query().Get("after"); v != "" {
		after, _ = strconv.ParseUint(v, 10, 64)
	}
	if inst := r.URL.Query().Get("instance"); inst != "" && inst != g.instance {
		// The tab's sequence numbers came from another gateway process.
		after = ^uint64(0)
	}
	sub, replay, resync, seq := h.subscribe(after)
	defer h.unsubscribe(sub)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	write := func(ev hubEvent, withID bool) bool {
		data, err := json.Marshal(ev)
		if err != nil {
			return true
		}
		var buf bytes.Buffer
		if withID {
			fmt.Fprintf(&buf, "id: %d\n", ev.Seq)
		}
		buf.WriteString("data: ")
		buf.Write(data)
		buf.WriteString("\n\n")
		if _, err := w.Write(buf.Bytes()); err != nil {
			return false
		}
		return rc.Flush() == nil
	}

	hello := hubEvent{Seq: seq, Type: "hello"}
	hello.Payload, _ = json.Marshal(map[string]any{"instance": g.instance, "seq": seq, "workspace_id": h.workspaceID(), "resync": resync})
	if !write(hello, false) {
		return
	}
	for _, ev := range replay {
		if !write(ev, true) {
			return
		}
	}

	heartbeat := time.NewTicker(heartbeatInterval)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-g.ctx.Done():
			return
		case <-sub.done:
			// Dropped for falling behind; the browser reconnects and
			// resumes from the ring.
			return
		case <-heartbeat.C:
			if _, err := io.WriteString(w, ": keep-alive\n\n"); err != nil || rc.Flush() != nil {
				return
			}
		case ev := <-sub.ch:
			if !write(ev, true) {
				return
			}
		}
	}
}

// --- Sessions ---

func (g *Gateway) handleListSessions(w http.ResponseWriter, r *http.Request, h *hub) {
	if q := strings.TrimSpace(r.URL.Query().Get("q")); q != "" {
		g.relay(w, r, http.MethodGet, upstreamPath(r, h, "/sessions/search"), url.Values{"q": []string{q}}, nil)
		return
	}
	g.relay(w, r, http.MethodGet, upstreamPath(r, h, "/sessions"), nil, nil)
}

func (g *Gateway) handleCreateSession(w http.ResponseWriter, r *http.Request, h *hub) {
	var req struct {
		Title string `json:"title"`
	}
	if !decodeJSON(w, r, maxJSONBody, &req) {
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = "New Session"
	}
	body, _ := json.Marshal(map[string]string{"title": truncateRunes(title, 200)})
	g.relay(w, r, http.MethodPost, upstreamPath(r, h, "/sessions"), nil, bytes.NewReader(body))
}

func (g *Gateway) handleRenameSession(w http.ResponseWriter, r *http.Request, h *hub) {
	var req proto.SessionRename
	if !decodeJSON(w, r, maxJSONBody, &req) {
		return
	}
	body, _ := json.Marshal(req)
	g.relay(w, r, http.MethodPost, upstreamPath(r, h, "/sessions/{sid}/title"), nil, bytes.NewReader(body))
}

func (g *Gateway) handleWorkflow(w http.ResponseWriter, r *http.Request, h *hub) {
	q := url.Values{"client_id": []string{g.opts.Upstream.ClientID()}}
	g.relay(w, r, http.MethodGet, upstreamPath(r, h, "/sessions/{sid}/workflow"), q, nil)
}

// promptRequest is a prompt from the browser. RunID makes it idempotent:
// a client that lost the acknowledgement retries with the same RunID and
// the server starts the run once.
type promptRequest struct {
	Prompt      string             `json:"prompt"`
	RunID       string             `json:"run_id"`
	Attachments []proto.Attachment `json:"attachments,omitempty"`
}

func (g *Gateway) handlePrompt(w http.ResponseWriter, r *http.Request, h *hub) {
	var req promptRequest
	if !decodeJSON(w, r, maxPromptBody, &req) {
		return
	}
	if strings.TrimSpace(req.Prompt) == "" {
		writeError(w, http.StatusBadRequest, "prompt is empty")
		return
	}
	if utf8.RuneCountInString(req.Prompt) > maxPromptChars {
		writeError(w, http.StatusRequestEntityTooLarge, "prompt is too long")
		return
	}
	if _, err := uuid.Parse(req.RunID); err != nil {
		writeError(w, http.StatusBadRequest, "run_id must be a UUID")
		return
	}
	if len(req.Attachments) > maxAttachments {
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("at most %d attachments per message", maxAttachments))
		return
	}
	total := 0
	for i, a := range req.Attachments {
		if len(a.Content) > maxAttachmentBytes {
			writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("%s is larger than %d MB", a.FileName, maxAttachmentBytes>>20))
			return
		}
		total += len(a.Content)
		if !allowedAttachmentType(a.MimeType) {
			writeError(w, http.StatusUnsupportedMediaType, fmt.Sprintf("%s: attachments must be images or text files", a.FileName))
			return
		}
		// The browser has no path on the computer running Atlas; the name
		// is all it can honestly supply.
		name := baseName(a.FileName)
		req.Attachments[i].FileName = name
		req.Attachments[i].FilePath = name
	}
	if total > maxAttachmentsTotal {
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("attachments exceed %d MB in total", maxAttachmentsTotal>>20))
		return
	}
	body, err := json.Marshal(proto.AgentMessage{
		SessionID:   r.PathValue("sid"),
		RunID:       req.RunID,
		Prompt:      req.Prompt,
		Attachments: req.Attachments,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), upstreamTimeout)
	defer cancel()
	rsp, err := g.opts.Upstream.Do(ctx, http.MethodPost, upstreamPath(r, h, "/agent"), nil, bytes.NewReader(body), http.Header{"Content-Type": []string{"application/json"}})
	if err != nil {
		writeError(w, http.StatusBadGateway, "the Atlas server is unreachable")
		return
	}
	defer rsp.Body.Close()
	if rsp.StatusCode != http.StatusAccepted {
		writeError(w, rsp.StatusCode, upstreamMessage(rsp))
		return
	}
	h.scheduleStatus(r.PathValue("sid"))
	writeJSON(w, http.StatusAccepted, map[string]any{
		"run_id":    req.RunID,
		"duplicate": rsp.Header.Get("X-Atlas-Run-Duplicate") == "1",
	})
}

func allowedAttachmentType(mime string) bool {
	mime = strings.ToLower(strings.TrimSpace(mime))
	if strings.HasPrefix(mime, "image/") {
		return mime != "image/svg+xml"
	}
	return strings.HasPrefix(mime, "text/") || mime == "application/json"
}

func (g *Gateway) handleShell(w http.ResponseWriter, r *http.Request, h *hub) {
	var req struct {
		Command string `json:"command"`
	}
	if !decodeJSON(w, r, maxJSONBody, &req) {
		return
	}
	if strings.TrimSpace(req.Command) == "" {
		writeError(w, http.StatusBadRequest, "command is empty")
		return
	}
	body, _ := json.Marshal(proto.ShellCommandRequest{Command: req.Command, TermWidth: 120})
	// A command may legitimately run for a while; it is bounded by the
	// server's own shell timeout rather than the usual relay timeout.
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Minute)
	defer cancel()
	rsp, err := g.opts.Upstream.Do(ctx, http.MethodPost, upstreamPath(r, h, "/agent/sessions/{sid}/shell"), nil, bytes.NewReader(body), http.Header{"Content-Type": []string{"application/json"}})
	if err != nil {
		writeError(w, http.StatusBadGateway, "the Atlas server is unreachable")
		return
	}
	defer rsp.Body.Close()
	if rsp.StatusCode >= 400 {
		writeError(w, rsp.StatusCode, upstreamMessage(rsp))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(rsp.StatusCode)
	_, _ = io.Copy(w, rsp.Body)
}

// --- Permissions and questions ---

func (g *Gateway) handlePending(w http.ResponseWriter, r *http.Request, h *hub) {
	ctx, cancel := context.WithTimeout(r.Context(), upstreamTimeout)
	defer cancel()
	id := url.PathEscape(h.workspaceID())
	permissions := []proto.PermissionRequest{}
	questions := []proto.QuestionRequest{}
	if err := g.getJSON(ctx, "/workspaces/"+id+"/permissions/pending", nil, &permissions); err != nil {
		writeUpstreamError(w, err)
		return
	}
	if err := g.getJSON(ctx, "/workspaces/"+id+"/questions/pending", nil, &questions); err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"permissions": permissions, "questions": questions})
}

// handlePermission resolves a pending permission request. The request is
// looked up on the server by ID instead of being taken from the browser,
// so the browser can only decide on what is actually pending.
func (g *Gateway) handlePermission(w http.ResponseWriter, r *http.Request, h *hub) {
	var req struct {
		Action string `json:"action"`
	}
	if !decodeJSON(w, r, maxJSONBody, &req) {
		return
	}
	action := proto.PermissionAction(req.Action)
	switch action {
	case proto.PermissionAllow, proto.PermissionAllowForSession, proto.PermissionDeny:
	default:
		writeError(w, http.StatusBadRequest, "action must be allow, allow_session or deny")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), upstreamTimeout)
	defer cancel()
	id := url.PathEscape(h.workspaceID())
	var pending []proto.PermissionRequest
	if err := g.getJSON(ctx, "/workspaces/"+id+"/permissions/pending", nil, &pending); err != nil {
		writeUpstreamError(w, err)
		return
	}
	var target *proto.PermissionRequest
	for i := range pending {
		if pending[i].ID == r.PathValue("pid") {
			target = &pending[i]
		}
	}
	if target == nil {
		// Answered elsewhere, or withdrawn because its run ended.
		writeJSON(w, http.StatusOK, map[string]any{"resolved": false})
		return
	}
	var out proto.PermissionGrantResponse
	if err := g.sendJSON(ctx, http.MethodPost, "/workspaces/"+id+"/permissions/grant", nil, proto.PermissionGrant{Permission: *target, Action: action}, &out); err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"resolved": out.Resolved})
}

func (g *Gateway) handleAnswer(w http.ResponseWriter, r *http.Request, h *hub) {
	var req struct {
		Responses []proto.QuestionResponse `json:"responses"`
	}
	if !decodeJSON(w, r, maxJSONBody, &req) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), upstreamTimeout)
	defer cancel()
	var out proto.QuestionAnswerResponse
	answer := proto.QuestionAnswer{BatchRequestID: r.PathValue("qid"), Responses: req.Responses}
	if err := g.sendJSON(ctx, http.MethodPost, "/workspaces/"+url.PathEscape(h.workspaceID())+"/questions/answer", nil, answer, &out); err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"resolved": out.Resolved})
}

func (g *Gateway) handleCancelQuestion(w http.ResponseWriter, r *http.Request, h *hub) {
	ctx, cancel := context.WithTimeout(r.Context(), upstreamTimeout)
	defer cancel()
	var out proto.QuestionAnswerResponse
	body := proto.QuestionCancel{BatchRequestID: r.PathValue("qid")}
	if err := g.sendJSON(ctx, http.MethodPost, "/workspaces/"+url.PathEscape(h.workspaceID())+"/questions/cancel", nil, body, &out); err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"resolved": out.Resolved})
}

// handleJobOutput relays a background job's output. The server only shows
// it to an attached client, which the gateway is.
func (g *Gateway) handleJobOutput(w http.ResponseWriter, r *http.Request, h *hub) {
	q := url.Values{"client_id": []string{g.opts.Upstream.ClientID()}}
	g.relay(w, r, http.MethodGet, upstreamPath(r, h, "/jobs/{jid}/output"), q, nil)
}

// --- Files ---

func (g *Gateway) handleFileContent(w http.ResponseWriter, r *http.Request, h *hub) {
	q := url.Values{"path": []string{r.URL.Query().Get("path")}}
	maxBytes := defaultFileReadBytes
	if v := r.URL.Query().Get("max_bytes"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxBytes = n
		}
	}
	q.Set("max_bytes", strconv.Itoa(maxBytes))
	g.relay(w, r, http.MethodGet, upstreamPath(r, h, "/files/content"), q, nil)
}

// --- Helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, proto.Error{Message: msg})
}

// decodeJSON reads a bounded JSON body, answering the request itself on
// failure.
func decodeJSON(w http.ResponseWriter, r *http.Request, limit int64, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body is too large")
			return false
		}
		writeError(w, http.StatusBadRequest, "malformed JSON body")
		return false
	}
	return true
}

func bytesReader(b []byte) io.Reader { return bytes.NewReader(b) }

func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

func baseName(p string) string {
	p = strings.TrimRight(strings.ReplaceAll(p, `\`, "/"), "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

func equalFoldPath(a, b string) bool { return strings.EqualFold(a, b) }
