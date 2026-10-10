package mobile

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/question"
)

//go:embed web
var webFS embed.FS

const cookieName = "__Host-atlas_phone"

type deviceKey struct{}

func (g *Gateway) handler() http.Handler {
	return g.handlerForHost("")
}

func (g *Gateway) handlerForHost(relayHost string) http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(webFS, "web")
	files := http.FileServerFS(static)
	mux.Handle("GET /", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && !slices.Contains([]string{"/app.js", "/app.css", "/sw.js", "/manifest.webmanifest", "/icon.svg", "/apple-touch-icon.png", "/icon-192.png", "/icon-512.png"}, r.URL.Path) {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/sw.js" {
			w.Header().Set("Service-Worker-Allowed", "/")
		}
		files.ServeHTTP(w, r)
	}))
	mux.HandleFunc("POST /api/pair", g.handlePair)
	mux.HandleFunc("POST /api/pair/poll", g.handlePoll)
	mux.HandleFunc("GET /api/me", g.authed(g.handleMe))
	mux.HandleFunc("POST /api/logout", g.authed(g.mutating(g.handleLogout)))
	mux.HandleFunc("GET /api/sessions", g.authed(g.handleSessions))
	mux.HandleFunc("GET /api/sessions/{id}", g.authed(g.handleSession))
	mux.HandleFunc("POST /api/sessions/{id}/prompt", g.authed(g.mutating(g.handlePrompt)))
	mux.HandleFunc("POST /api/sessions/{id}/control", g.authed(g.mutating(g.handleControl)))
	mux.HandleFunc("POST /api/sessions/{id}/attachments", g.authed(g.mutating(g.handleUpload)))
	mux.HandleFunc("POST /api/permissions/{id}", g.authed(g.mutating(g.handlePermission)))
	mux.HandleFunc("POST /api/questions/{id}", g.authed(g.mutating(g.handleQuestion)))
	mux.HandleFunc("GET /api/events", g.authed(g.handleEvents))
	mux.HandleFunc("GET /api/preview", g.authed(g.handlePreview))
	return g.secureForHost(mux, relayHost)
}

// secure applies host validation and security headers to every response.
func (g *Gateway) secure(next http.Handler) http.Handler {
	return g.secureForHost(next, "")
}

func (g *Gateway) secureForHost(next http.Handler, relayHost string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if (relayHost != "" && r.Host != relayHost) || (relayHost == "" && !g.allowedHost(r.Host)) {
			http.Error(w, "unknown host", http.StatusMisdirectedRequest)
			return
		}
		h := w.Header()
		h.Set("Strict-Transport-Security", "max-age=31536000")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' blob: data:; style-src 'self'; script-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}

// sameOrigin requires the browser-supplied Origin to be this gateway.
func (g *Gateway) sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	return origin != "" && origin == "https://"+r.Host
}

func (g *Gateway) authed(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(cookieName)
		if err != nil {
			apiError(w, http.StatusUnauthorized, "not_paired", "pair this phone from Atlas first")
			return
		}
		d, err := g.Auth.Authenticate(c.Value)
		if err != nil {
			apiError(w, http.StatusUnauthorized, "not_paired", err.Error())
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), deviceKey{}, d)))
	}
}

// mutating adds Origin, CSRF and rate checks to state-changing requests.
func (g *Gateway) mutating(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := r.Context().Value(deviceKey{}).(Device)
		if !g.sameOrigin(r) {
			apiError(w, http.StatusForbidden, "bad_origin", "request origin does not match this gateway")
			return
		}
		if r.Header.Get("X-CSRF-Token") == "" || r.Header.Get("X-CSRF-Token") != d.CSRF {
			apiError(w, http.StatusForbidden, "bad_csrf", "missing or wrong CSRF token; reload the page")
			return
		}
		if g.rateLimited("dev:"+d.ID, 60) {
			apiError(w, http.StatusTooManyRequests, "rate_limited", "too many actions; wait a moment")
			return
		}
		next(w, r)
	}
}

func device(r *http.Request) Device { return r.Context().Value(deviceKey{}).(Device) }

func (g *Gateway) handlePair(w http.ResponseWriter, r *http.Request) {
	if !g.sameOrigin(r) {
		apiError(w, http.StatusForbidden, "bad_origin", "request origin does not match this gateway")
		return
	}
	if g.rateLimited("pair:"+clientIP(r), 10) {
		apiError(w, http.StatusTooManyRequests, "rate_limited", "too many pairing attempts")
		return
	}
	var req struct {
		Nonce string `json:"nonce"`
		Name  string `json:"name"`
	}
	if !strictJSON(w, r, &req) {
		return
	}
	p, poll, err := g.Auth.Redeem(req.Nonce, strings.TrimSpace(req.Name), r.UserAgent(), clientIP(r))
	if err != nil {
		apiError(w, http.StatusForbidden, "pairing_invalid", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"pending_id": p.ID, "code": p.Code, "poll": poll, "expires": p.Expires.UnixMilli(), "fingerprint": g.Fingerprint()})
}

func (g *Gateway) handlePoll(w http.ResponseWriter, r *http.Request) {
	if !g.sameOrigin(r) {
		apiError(w, http.StatusForbidden, "bad_origin", "request origin does not match this gateway")
		return
	}
	if g.rateLimited("poll:"+clientIP(r), 120) {
		apiError(w, http.StatusTooManyRequests, "rate_limited", "slow down")
		return
	}
	var req struct {
		PendingID string `json:"pending_id"`
		Poll      string `json:"poll"`
	}
	if !strictJSON(w, r, &req) {
		return
	}
	state, sess, d, err := g.Auth.Poll(req.PendingID, req.Poll)
	if err != nil {
		apiError(w, http.StatusNotFound, "not_pending", err.Error())
		return
	}
	if sess != "" {
		http.SetCookie(w, &http.Cookie{Name: cookieName, Value: sess, Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode, MaxAge: int(SessionAbsolute.Seconds())})
		writeJSON(w, http.StatusOK, map[string]any{"state": state, "csrf": d.CSRF, "device": map[string]string{"id": d.ID, "name": d.Name}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"state": state})
}

func (g *Gateway) handleMe(w http.ResponseWriter, r *http.Request) {
	d := device(r)
	_, _, last := g.ring.since(0)
	writeJSON(w, http.StatusOK, map[string]any{"device": map[string]any{"id": d.ID, "name": d.Name, "previews": d.Previews}, "csrf": d.CSRF, "language": g.opts.Language, "version": g.opts.Version, "fingerprint": g.Fingerprint(), "seq": last, "preview_available": g.opts.Preview != nil})
}

func (g *Gateway) handleLogout(w http.ResponseWriter, r *http.Request) {
	d := device(r)
	if err := g.Auth.Revoke(d.ID); err != nil {
		apiError(w, http.StatusInternalServerError, "revoke_failed", err.Error())
		return
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	writeJSON(w, http.StatusOK, map[string]any{"revoked": d.ID})
}

func (g *Gateway) pendingFor(sessionID string) (perms []map[string]any, qs []question.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, p := range g.permissions {
		if sessionID == "" || p.SessionID == sessionID {
			perms = append(perms, permissionView(p))
		}
	}
	for _, q := range g.questions {
		if sessionID == "" || q.SessionID == sessionID {
			qs = append(qs, q)
		}
	}
	return perms, qs
}

func (g *Gateway) handleSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := g.opts.Backend.ListSessions(r.Context())
	if err != nil {
		apiError(w, http.StatusInternalServerError, "list_failed", err.Error())
		return
	}
	var out []map[string]any
	for _, s := range sessions {
		if s.ParentSessionID != "" {
			continue
		}
		sum := sessionSummary(s, g.opts.Backend.AgentIsSessionBusy(s.ID))
		perms, qs := g.pendingFor(s.ID)
		sum["pending_permissions"], sum["pending_questions"] = len(perms), len(qs)
		out = append(out, sum)
		if len(out) >= 50 {
			break
		}
	}
	_, _, last := g.ring.since(0)
	writeJSON(w, http.StatusOK, map[string]any{"sessions": out, "seq": last})
}

func (g *Gateway) handleSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s, err := g.opts.Backend.GetSession(r.Context(), id)
	if err != nil || s.ParentSessionID != "" {
		apiError(w, http.StatusNotFound, "not_found", "no such task")
		return
	}
	msgs, err := g.opts.Backend.ListMessages(r.Context(), id)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "messages_failed", err.Error())
		return
	}
	if len(msgs) > 60 {
		msgs = msgs[len(msgs)-60:]
	}
	var views []map[string]any
	for _, m := range msgs {
		if m.Role == message.Tool {
			continue
		}
		views = append(views, messageView(m))
	}
	workflow := map[string]any{"available": false}
	if snap, err := g.opts.Backend.WorkflowSnapshot(r.Context(), id); err == nil {
		workflow = map[string]any{"available": true, "revision": snap.Revision, "paused": snap.Paused, "busy": snap.Busy}
	}
	perms, qs := g.pendingFor(id)
	_, _, last := g.ring.since(0)
	writeJSON(w, http.StatusOK, map[string]any{"session": sessionSummary(s, g.opts.Backend.AgentIsSessionBusy(id)), "messages": views, "workflow": workflow, "permissions": perms, "questions": qs, "seq": last})
}

func (g *Gateway) handlePrompt(w http.ResponseWriter, r *http.Request) {
	d := device(r)
	id := r.PathValue("id")
	var req struct {
		Text          string   `json:"text"`
		AttachmentIDs []string `json:"attachment_ids"`
	}
	if !strictJSON(w, r, &req) {
		return
	}
	text := strings.TrimSpace(req.Text)
	if text == "" || len(text) > 16000 {
		apiError(w, http.StatusBadRequest, "invalid_prompt", "prompt must be 1-16000 characters")
		return
	}
	g.idempotent(w, r, d, func() (int, any) {
		if _, err := g.opts.Backend.GetSession(r.Context(), id); err != nil {
			return http.StatusNotFound, map[string]any{"error": map[string]string{"code": "not_found", "message": "no such task"}}
		}
		var atts []message.Attachment
		g.mu.Lock()
		for _, aid := range req.AttachmentIDs {
			u, ok := g.uploads[aid]
			if !ok || u.device != d.ID {
				g.mu.Unlock()
				return http.StatusBadRequest, map[string]any{"error": map[string]string{"code": "unknown_attachment", "message": "attachment " + aid + " is unknown or expired"}}
			}
			atts = append(atts, u.att)
			delete(g.uploads, aid)
		}
		g.mu.Unlock()
		// The run outlives this request; it is tracked through events.
		go func() {
			if err := g.opts.Backend.AgentRun(context.Background(), id, text, atts...); err != nil {
				g.ring.add("run_error", id, map[string]string{"message": err.Error()})
			}
		}()
		g.ring.add("prompt_sent", id, map[string]any{"device": d.Name, "chars": len(text)})
		return http.StatusAccepted, map[string]any{"accepted": true, "queued": g.opts.Backend.AgentIsSessionBusy(id)}
	})
}

func (g *Gateway) handleControl(w http.ResponseWriter, r *http.Request) {
	d := device(r)
	id := r.PathValue("id")
	var req struct {
		Action   string `json:"action"`
		Revision string `json:"revision"`
	}
	if !strictJSON(w, r, &req) {
		return
	}
	if !slices.Contains([]string{"pause", "resume", "stop"}, req.Action) {
		apiError(w, http.StatusBadRequest, "invalid_action", "action must be pause, resume or stop")
		return
	}
	g.idempotent(w, r, d, func() (int, any) {
		snap, err := g.opts.Backend.WorkflowSnapshot(r.Context(), id)
		if err != nil {
			return http.StatusConflict, map[string]any{"error": map[string]string{"code": "unavailable", "message": err.Error()}}
		}
		if req.Revision == "" || req.Revision != snap.Revision {
			return http.StatusConflict, map[string]any{"error": map[string]string{"code": "stale_revision", "message": "the task changed since you loaded it; review the new state and try again"}, "revision": snap.Revision}
		}
		if err := g.opts.Backend.WorkflowControl(r.Context(), id, engineering.WorkflowControl{Action: req.Action, ExpectedRevision: req.Revision}); err != nil {
			return http.StatusConflict, map[string]any{"error": map[string]string{"code": "control_failed", "message": err.Error()}}
		}
		g.ring.add("control", id, map[string]any{"action": req.Action, "device": d.Name})
		return http.StatusOK, map[string]any{"ok": true, "action": req.Action}
	})
}

func (g *Gateway) handlePermission(w http.ResponseWriter, r *http.Request) {
	d := device(r)
	id := r.PathValue("id")
	var req struct {
		Decision   string `json:"decision"`
		SessionID  string `json:"session_id"`
		ToolCallID string `json:"tool_call_id"`
	}
	if !strictJSON(w, r, &req) {
		return
	}
	if !slices.Contains([]string{"allow", "allow_session", "deny"}, req.Decision) {
		apiError(w, http.StatusBadRequest, "invalid_decision", "decision must be allow, allow_session or deny")
		return
	}
	g.idempotent(w, r, d, func() (int, any) {
		g.mu.Lock()
		p, ok := g.permissions[id]
		g.mu.Unlock()
		// The answer must name the exact pending request: id, session and
		// tool call. Anything else is a stale or misdirected answer.
		if !ok || p.SessionID != req.SessionID || p.ToolCallID != req.ToolCallID {
			return http.StatusConflict, map[string]any{"error": map[string]string{"code": "stale_request", "message": "this permission request is no longer pending (answered elsewhere or expired)"}}
		}
		var resolved bool
		switch req.Decision {
		case "allow":
			resolved = g.opts.Backend.PermissionGrant(p)
		case "allow_session":
			resolved = g.opts.Backend.PermissionGrantPersistent(p)
		default:
			resolved = g.opts.Backend.PermissionDeny(p)
		}
		if !resolved {
			return http.StatusConflict, map[string]any{"error": map[string]string{"code": "stale_request", "message": "this permission request was already resolved"}}
		}
		g.mu.Lock()
		delete(g.permissions, id)
		delete(g.byToolCall, p.ToolCallID)
		g.mu.Unlock()
		g.ring.add("permission_resolved", p.SessionID, map[string]any{"id": id, "granted": req.Decision != "deny", "by": d.Name})
		return http.StatusOK, map[string]any{"ok": true, "decision": req.Decision}
	})
}

func (g *Gateway) handleQuestion(w http.ResponseWriter, r *http.Request) {
	d := device(r)
	id := r.PathValue("id")
	var req struct {
		SessionID string            `json:"session_id"`
		Answers   []question.Answer `json:"answers"`
		Cancel    bool              `json:"cancel"`
	}
	if !strictJSON(w, r, &req) {
		return
	}
	g.idempotent(w, r, d, func() (int, any) {
		g.mu.Lock()
		q, ok := g.questions[id]
		g.mu.Unlock()
		if !ok || q.SessionID != req.SessionID {
			return http.StatusConflict, map[string]any{"error": map[string]string{"code": "stale_request", "message": "this question is no longer pending"}}
		}
		if !req.Cancel {
			valid := map[string]bool{}
			for _, qq := range q.Questions {
				valid[qq.ID] = true
			}
			for _, a := range req.Answers {
				if !valid[a.QuestionID] {
					return http.StatusBadRequest, map[string]any{"error": map[string]string{"code": "invalid_answer", "message": "answer for unknown question " + a.QuestionID}}
				}
			}
		}
		var resolved bool
		if req.Cancel {
			resolved = g.opts.Backend.QuestionCancelRequest(id)
		} else {
			resolved = g.opts.Backend.QuestionAnswerRequest(id, req.Answers)
		}
		if !resolved {
			return http.StatusConflict, map[string]any{"error": map[string]string{"code": "stale_request", "message": "this question was already answered"}}
		}
		g.mu.Lock()
		delete(g.questions, id)
		g.mu.Unlock()
		g.ring.add("question_resolved", q.SessionID, map[string]any{"id": id, "by": d.Name})
		return http.StatusOK, map[string]any{"ok": true}
	})
}

var uploadTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true, "text/plain; charset=utf-8": true, "application/pdf": true}

const maxUpload = 10 << 20

func (g *Gateway) handleUpload(w http.ResponseWriter, r *http.Request) {
	d := device(r)
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload+1<<20)
	if err := r.ParseMultipartForm(maxUpload); err != nil {
		apiError(w, http.StatusRequestEntityTooLarge, "too_large", "attachments are limited to 10 MiB")
		return
	}
	f, hdr, err := r.FormFile("file")
	if err != nil {
		apiError(w, http.StatusBadRequest, "invalid_upload", "missing file")
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxUpload+1))
	if err != nil || len(data) == 0 || len(data) > maxUpload {
		apiError(w, http.StatusRequestEntityTooLarge, "too_large", "attachments are limited to 10 MiB")
		return
	}
	// The detected type, not the client's claim, decides acceptance.
	detected := http.DetectContentType(data)
	if !uploadTypes[detected] {
		apiError(w, http.StatusUnsupportedMediaType, "unsupported_type", "accepted: PNG, JPEG, GIF, WebP, plain text, PDF (detected "+detected+")")
		return
	}
	name := strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == 0 || r < 32 {
			return '_'
		}
		return r
	}, hdr.Filename)
	if len(name) > 100 {
		name = name[:100]
	}
	mime := strings.TrimSuffix(detected, "; charset=utf-8")
	aid := "up-" + random(9)
	g.mu.Lock()
	for k, u := range g.uploads {
		if time.Since(u.at) > 30*time.Minute {
			delete(g.uploads, k)
		}
	}
	g.uploads[aid] = upload{device: d.ID, at: time.Now(), att: message.Attachment{FileName: name, MimeType: mime, Content: data}}
	g.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"attachment_id": aid, "name": name, "type": mime, "size": len(data), "sha256": hash(string(data))})
}

func (g *Gateway) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		apiError(w, http.StatusInternalServerError, "no_stream", "streaming unsupported")
		return
	}
	var since uint64
	if v := r.Header.Get("Last-Event-ID"); v != "" {
		since, _ = strconv.ParseUint(v, 10, 64)
	} else if v := r.URL.Query().Get("since"); v != "" {
		since, _ = strconv.ParseUint(v, 10, 64)
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	d := device(r)
	for {
		events, gap, last := g.ring.since(since)
		if gap {
			fmt.Fprintf(w, "event: resync\ndata: {\"seq\":%d}\n\n", last)
			since = last
			flusher.Flush()
			continue
		}
		for _, e := range events {
			data, _ := json.Marshal(e)
			fmt.Fprintf(w, "id: %d\nevent: update\ndata: %s\n\n", e.Seq, data)
			since = e.Seq
		}
		flusher.Flush()
		if r.Context().Err() != nil {
			return
		}
		// Revoked devices lose the stream at the next wake-up.
		if c, err := r.Cookie(cookieName); err != nil || func() bool { _, err := g.Auth.Authenticate(c.Value); return err != nil }() {
			fmt.Fprint(w, "event: revoked\ndata: {}\n\n")
			flusher.Flush()
			return
		}
		g.ring.wait(since, 15*time.Second)
		if r.Context().Err() != nil {
			return
		}
		if _, _, l := g.ring.since(since); l == since {
			fmt.Fprintf(w, ": keepalive %s\n\n", d.ID)
			flusher.Flush()
		}
	}
}

func (g *Gateway) handlePreview(w http.ResponseWriter, r *http.Request) {
	d := device(r)
	if g.opts.Preview == nil || !d.Previews {
		apiError(w, http.StatusForbidden, "previews_off", "screen previews are off for this phone; enable them for this device in Atlas (/mobile)")
		return
	}
	if g.rateLimited("preview:"+d.ID, 20) {
		apiError(w, http.StatusTooManyRequests, "rate_limited", "previews are limited to 20 per minute")
		return
	}
	img, err := g.opts.Preview()
	if err != nil {
		apiError(w, http.StatusConflict, "preview_failed", err.Error())
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(img)
}

var _ = permission.PermissionRequest{}
