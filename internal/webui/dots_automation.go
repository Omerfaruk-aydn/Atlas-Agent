package webui

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/dots"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/proto"
)

// registerDotsAutomation adds the trigger, run history and budget API. It
// is a no-op without an automation store.
func (g *Gateway) registerDotsAutomation(api *http.ServeMux) {
	if g.opts.Automation == nil {
		return
	}
	api.HandleFunc("GET /api/dots/{dot}/triggers", g.dotRoute(g.handleListTriggers))
	api.HandleFunc("POST /api/dots/{dot}/triggers", g.dotRoute(g.handleCreateTrigger))
	api.HandleFunc("PATCH /api/dots/{dot}/triggers/{tid}", g.dotRoute(g.handleUpdateTrigger))
	api.HandleFunc("DELETE /api/dots/{dot}/triggers/{tid}", g.dotRoute(g.handleDeleteTrigger))
	api.HandleFunc("POST /api/dots/{dot}/triggers/{tid}/run", g.dotRoute(g.handleRunTrigger))
	api.HandleFunc("GET /api/dots/{dot}/runs", g.dotRoute(g.handleListRuns))
	api.HandleFunc("POST /api/dots/{dot}/runs", g.dotRoute(g.handleStartRun))
	api.HandleFunc("POST /api/dots/{dot}/runs/{rid}/cancel", g.dotRoute(g.handleCancelRun))
	api.HandleFunc("GET /api/dots/{dot}/budget", g.dotRoute(g.handleGetBudget))
	api.HandleFunc("PUT /api/dots/{dot}/budget", g.dotRoute(g.handleSetBudget))
}

// registerDotHook adds the webhook endpoint, which is outside the cookie
// session because its caller is another program. The unguessable token in
// the path is the credential.
func (g *Gateway) registerDotHook(root *http.ServeMux) {
	if g.opts.Automation == nil || g.opts.Dots == nil {
		return
	}
	root.HandleFunc("POST /hooks/dots/{token}", g.handleDotHook)
}

func (g *Gateway) dotRoute(next func(http.ResponseWriter, *http.Request, dots.Dot)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d, err := g.opts.Dots.Get(r.PathValue("dot"))
		if err != nil {
			writeDotError(w, err)
			return
		}
		next(w, r, d)
	}
}

func (g *Gateway) handleListTriggers(w http.ResponseWriter, _ *http.Request, d dots.Dot) {
	writeJSON(w, http.StatusOK, g.opts.Automation.TriggersFor(d.ID))
}

func (g *Gateway) handleCreateTrigger(w http.ResponseWriter, r *http.Request, d dots.Dot) {
	var in dots.TriggerInput
	if !decodeJSON(w, r, maxJSONBody, &in) {
		return
	}
	t, err := g.opts.Automation.CreateTrigger(d.ID, in)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

func (g *Gateway) handleUpdateTrigger(w http.ResponseWriter, r *http.Request, d dots.Dot) {
	var in dots.TriggerInput
	if !decodeJSON(w, r, maxJSONBody, &in) {
		return
	}
	t, err := g.opts.Automation.UpdateTrigger(d.ID, r.PathValue("tid"), in)
	if err != nil {
		writeAutomationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (g *Gateway) handleDeleteTrigger(w http.ResponseWriter, r *http.Request, d dots.Dot) {
	if err := g.opts.Automation.DeleteTrigger(d.ID, r.PathValue("tid")); err != nil {
		writeAutomationError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleRunTrigger fires a trigger now, whatever its schedule says.
func (g *Gateway) handleRunTrigger(w http.ResponseWriter, r *http.Request, d dots.Dot) {
	t, err := g.opts.Automation.Trigger(d.ID, r.PathValue("tid"))
	if err != nil {
		writeAutomationError(w, err)
		return
	}
	run, err := g.runner.Fire(d, t, dots.SourceManual, "")
	if err != nil {
		writeAutomationError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, run)
}

func (g *Gateway) handleListRuns(w http.ResponseWriter, _ *http.Request, d dots.Dot) {
	writeJSON(w, http.StatusOK, g.opts.Automation.RunsFor(d.ID, 100))
}

// handleStartRun wakes a dot with a task typed by the owner.
func (g *Gateway) handleStartRun(w http.ResponseWriter, r *http.Request, d dots.Dot) {
	var body struct {
		Prompt string `json:"prompt"`
	}
	if !decodeJSON(w, r, maxJSONBody, &body) {
		return
	}
	if strings.TrimSpace(body.Prompt) == "" {
		writeError(w, http.StatusBadRequest, "tell the dot what to do")
		return
	}
	run, err := g.runner.RunNow(d, truncateRunes(body.Prompt, 4000))
	if err != nil {
		writeAutomationError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, run)
}

func (g *Gateway) handleCancelRun(w http.ResponseWriter, r *http.Request, _ dots.Dot) {
	if !g.runner.Cancel(r.PathValue("rid")) {
		writeError(w, http.StatusNotFound, "that run is not in progress")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (g *Gateway) handleGetBudget(w http.ResponseWriter, r *http.Request, d dots.Dot) {
	writeJSON(w, http.StatusOK, g.dotBudget(r.Context(), d))
}

func (g *Gateway) handleSetBudget(w http.ResponseWriter, r *http.Request, d dots.Dot) {
	var body struct {
		LimitUSD float64 `json:"limit_usd"`
	}
	if !decodeJSON(w, r, maxJSONBody, &body) {
		return
	}
	if err := g.opts.Automation.SetLimit(d.ID, body.LimitUSD); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, g.dotBudget(r.Context(), d))
}

// dotBudget reports a dot's budget. A dot whose workspace is not open has
// spent nothing the gateway can see.
func (g *Gateway) dotBudget(ctx context.Context, d dots.Dot) dots.Budget {
	a := g.opts.Automation
	if d.SessionID != "" {
		if h := g.hubForPath(d.Workspace); h != nil {
			cctx, cancel := context.WithTimeout(ctx, upstreamTimeout)
			defer cancel()
			var sess proto.Session
			if g.getJSON(cctx, "/workspaces/"+url.PathEscape(h.workspaceID())+"/sessions/"+url.PathEscape(d.SessionID), nil, &sess) == nil {
				return a.Spend(d.ID, d.SessionID, sess.Cost)
			}
		}
	}
	return dots.Budget{LimitUSD: a.Limit(d.ID), Day: time.Now().Format("2006-01-02")}
}

// handleDotHook wakes a dot because another program called its webhook.
// The body, if any, is passed to the dot as untrusted data.
func (g *Gateway) handleDotHook(w http.ResponseWriter, r *http.Request) {
	t, ok := g.opts.Automation.WebhookTrigger(r.PathValue("token"))
	if !ok {
		writeError(w, http.StatusNotFound, "unknown webhook")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, dots.MaxHookBody))
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not read the request body")
		return
	}
	d, err := g.opts.Dots.Get(t.DotID)
	if err != nil {
		writeError(w, http.StatusNotFound, "unknown webhook")
		return
	}
	g.opts.Automation.MarkFired(t.DotID, t.ID, time.Now())
	run, err := g.runner.Fire(d, t, dots.SourceWebhook, string(body))
	if err != nil {
		writeAutomationError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"run_id": run.ID, "status": run.Status})
}

func writeAutomationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, dots.ErrTriggerNotFound), errors.Is(err, dots.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrDotBusy):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}
