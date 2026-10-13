package mobile

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/interaction"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/vault"
)

// resumeCommand is the only slash command the phone composer routes to a
// control action. Everything else is an ordinary message to the agent.
const resumeCommand = "/resume-use"

// credentialView is what the phone sees about a credential card. It never
// carries the secret or the bound page target.
func credentialView(r vault.CredentialRequest) map[string]any {
	return map[string]any{
		"id": r.ID, "session_id": r.SessionID, "service": r.Service, "origin": r.Origin,
		"account": r.Account, "state": string(r.State), "expires": r.ExpiresAt.UnixMilli(),
		"can_persist": r.Account != "" && strings.HasPrefix(r.Origin, "https://"),
	}
}

// interactionView reports a handed-off browser/desktop control without the
// workflow internals.
func interactionView(snap engineering.WorkflowSnapshot) map[string]any {
	return map[string]any{"paused": snap.Interactions.Paused, "reason": vault.Redact(snap.SessionID, snap.Interactions.Reason), "canceled": snap.Interactions.Canceled}
}

// handleCredential receives a secret typed into a credential card, or the
// user's cancellation. The value goes straight into the in-memory store
// bound to the task, origin and this device. It is never echoed, logged or
// written to the conversation.
func (g *Gateway) handleCredential(w http.ResponseWriter, r *http.Request) {
	d := device(r)
	id := r.PathValue("id")
	if g.rateLimited("cred:"+d.ID, 12) {
		apiError(w, http.StatusTooManyRequests, "rate_limited", "too many credential attempts; wait a moment")
		return
	}
	var req struct {
		SessionID string `json:"session_id"`
		Secret    string `json:"secret"`
		Persist   bool   `json:"persist"`
		Cancel    bool   `json:"cancel"`
	}
	if !strictJSON(w, r, &req) {
		return
	}
	secret := []byte(req.Secret)
	req.Secret = ""
	defer clear(secret)
	g.idempotent(w, r, d, func() (int, any) {
		if req.Cancel {
			view, err := vault.Ephemeral.Cancel(id, req.SessionID)
			if err != nil {
				return http.StatusConflict, map[string]any{"error": map[string]string{"code": "stale_request", "message": "this credential card is no longer open"}}
			}
			g.ring.add("credential_resolved", view.SessionID, map[string]any{"id": id, "state": string(view.State), "by": d.Name})
			return http.StatusOK, map[string]any{"ok": true, "state": string(view.State)}
		}
		view, err := vault.Ephemeral.Provide(id, req.SessionID, d.ID, secret, req.Persist)
		switch {
		case errors.Is(err, vault.ErrCredentialExpired):
			return http.StatusGone, map[string]any{"error": map[string]string{"code": "expired", "message": "this credential card expired; Atlas will ask again if it still needs it"}}
		case errors.Is(err, vault.ErrCredentialUsed):
			return http.StatusConflict, map[string]any{"error": map[string]string{"code": "already_provided", "message": "this card was already answered"}}
		case err != nil && strings.Contains(err.Error(), "bytes"):
			return http.StatusBadRequest, map[string]any{"error": map[string]string{"code": "invalid_secret", "message": "enter the password (at most 4096 bytes)"}}
		case err != nil:
			return http.StatusConflict, map[string]any{"error": map[string]string{"code": "stale_request", "message": "this credential card is no longer open"}}
		}
		g.ring.add("credential_resolved", view.SessionID, map[string]any{"id": id, "state": string(view.State), "by": d.Name})
		return http.StatusOK, map[string]any{"ok": true, "state": string(view.State)}
	})
}

// resumeInteraction returns handed-off control through the same
// revision-checked workflow control the desktop uses, then continues the
// task from a fresh observation unless the user canceled it meanwhile.
// expected is the revision the phone saw; "" uses the current one, which
// only the /resume-use message path does, for the session in the URL.
func (g *Gateway) resumeInteraction(ctx context.Context, id, expected, by string) (int, any) {
	if s, err := g.opts.Backend.GetSession(ctx, id); err != nil || s.ParentSessionID != "" {
		return http.StatusNotFound, map[string]any{"error": map[string]string{"code": "not_found", "message": "no such task"}}
	}
	snap, err := g.opts.Backend.WorkflowSnapshot(ctx, id)
	if err != nil {
		return http.StatusConflict, map[string]any{"error": map[string]string{"code": "unavailable", "message": err.Error()}}
	}
	if !snap.Interactions.Paused {
		return http.StatusConflict, map[string]any{"error": map[string]string{"code": "not_paused", "message": "Atlas already has browser and desktop control in this chat"}, "revision": snap.Revision}
	}
	if expected == "" {
		expected = snap.Revision
	}
	if expected != snap.Revision {
		return http.StatusConflict, map[string]any{"error": map[string]string{"code": "stale_revision", "message": "the task changed since you loaded it; review the new state and try again"}, "revision": snap.Revision}
	}
	if err := g.opts.Backend.WorkflowControl(ctx, id, engineering.WorkflowControl{Action: "interaction_resume", ExpectedRevision: expected}); err != nil {
		code := "control_failed"
		if strings.Contains(err.Error(), "revision") {
			code = "stale_revision"
		}
		return http.StatusConflict, map[string]any{"error": map[string]string{"code": code, "message": err.Error()}}
	}
	g.ring.add("control", id, map[string]any{"action": "interaction_resume", "device": by})
	if snap.Interactions.Canceled {
		// Returning control never revives a task the user canceled.
		return http.StatusOK, map[string]any{"ok": true, "action": "interaction_resume", "continued": false, "canceled": true}
	}
	prompt := interaction.ResumePrompt(nil, snap.Interactions.Reason)
	go func() {
		if err := g.opts.Backend.AgentRun(context.Background(), id, prompt); err != nil {
			g.ring.add("run_error", id, map[string]string{"message": err.Error()})
		}
	}()
	return http.StatusOK, map[string]any{"ok": true, "action": "interaction_resume", "continued": true}
}

// handleTransferCreate issues a code that moves this phone's access into
// its Home Screen app, whose cookie store is separate on iOS.
func (g *Gateway) handleTransferCreate(w http.ResponseWriter, r *http.Request) {
	d := device(r)
	if g.rateLimited("transfer:"+d.ID, 5) {
		apiError(w, http.StatusTooManyRequests, "rate_limited", "too many codes; wait a moment")
		return
	}
	code, expires, err := g.Auth.NewTransfer(d.ID)
	if err != nil {
		apiError(w, http.StatusUnauthorized, "not_paired", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"code": code[:4] + "-" + code[4:], "expires": expires.UnixMilli()})
}

// handleTransferRedeem pairs the Home Screen app with a transfer code from
// the same phone's paired browser.
func (g *Gateway) handleTransferRedeem(w http.ResponseWriter, r *http.Request) {
	if !g.sameOrigin(r) {
		apiError(w, http.StatusForbidden, "bad_origin", "request origin does not match this gateway")
		return
	}
	if g.rateLimited("pair:"+clientIP(r), 10) {
		apiError(w, http.StatusTooManyRequests, "rate_limited", "too many pairing attempts")
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if !strictJSON(w, r, &req) {
		return
	}
	d, session, err := g.Auth.RedeemTransfer(req.Code, r.UserAgent())
	if err != nil {
		apiError(w, http.StatusForbidden, "transfer_invalid", "the code is wrong, expired or already used; create a new one in the paired browser")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: session, Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode, MaxAge: int(SessionAbsolute.Seconds())})
	g.ring.add("device_paired", "", map[string]any{"device": d.Name})
	writeJSON(w, http.StatusOK, map[string]any{"state": "accepted", "csrf": d.CSRF, "device": map[string]string{"id": d.ID, "name": d.Name}})
}

// subscribeCredentials makes this gateway a secure input channel for
// credential cards while it runs.
func (g *Gateway) subscribeCredentials() func() {
	return vault.Ephemeral.Subscribe(func(ev vault.CredentialEvent) {
		kind := "credential"
		if ev.Request.State != vault.CredentialPending {
			kind = "credential_resolved"
		}
		g.ring.add(kind, ev.Request.SessionID, credentialView(ev.Request))
	})
}

func credentialCount(sessionID string) int { return len(vault.Ephemeral.Pending(sessionID)) }

// credentialViews lists open cards of a session, newest last.
func credentialViews(sessionID string) []map[string]any {
	reqs := vault.Ephemeral.Pending(sessionID)
	out := make([]map[string]any, 0, len(reqs))
	for _, r := range reqs {
		out = append(out, credentialView(r))
	}
	return out
}
