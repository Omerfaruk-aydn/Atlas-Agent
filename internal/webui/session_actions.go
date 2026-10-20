package webui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/proto"
)

// registerSessionActions adds the routes behind the composer's "/rewind",
// "/files" and "/agents" commands. They relay to the Atlas server, which
// does the work; the gateway only adds the workspace and session IDs.
func (g *Gateway) registerSessionActions(api *http.ServeMux) {
	api.HandleFunc("POST /api/workspaces/{id}/sessions/{sid}/rewind/preview", g.withHub(g.handleRewind("/rewind/preview")))
	api.HandleFunc("POST /api/workspaces/{id}/sessions/{sid}/rewind", g.withHub(g.handleRewind("/rewind")))
	api.HandleFunc("GET /api/workspaces/{id}/sessions/{sid}/user-messages", g.withHub(g.relayGet("/sessions/{sid}/messages/user")))
	api.HandleFunc("GET /api/workspaces/{id}/sessions/{sid}/files", g.withHub(g.relayGet("/sessions/{sid}/filetracker/files")))
	api.HandleFunc("GET /api/workspaces/{id}/sessions/{sid}/subagents", g.withHub(g.relayGet("/sessions/{sid}/subagents")))
	api.HandleFunc("GET /api/workspaces/{id}/sessions/{sid}/goal", g.withHub(g.relayGet("/agent/sessions/{sid}/goal")))
	api.HandleFunc("POST /api/workspaces/{id}/sessions/{sid}/goal", g.withHub(g.handleSetGoal))
}

// maxGoalLen bounds a goal. It is text the user typed, kept in the session.
const maxGoalLen = 4000

// handleSetGoal starts the session's autonomous run towards a goal, or ends
// it when the goal is empty.
func (g *Gateway) handleSetGoal(w http.ResponseWriter, r *http.Request, h *hub) {
	var req proto.AgentGoalRequest
	if !decodeJSON(w, r, maxJSONBody, &req) {
		return
	}
	if len(req.Goal) > maxGoalLen {
		writeError(w, http.StatusBadRequest, "the goal is too long")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), upstreamTimeout)
	defer cancel()
	path := "/workspaces/" + url.PathEscape(h.workspaceID()) + "/agent/sessions/" + url.PathEscape(r.PathValue("sid")) + "/goal"
	if err := g.sendJSON(ctx, http.MethodPost, path, nil, req, nil); err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct{}{})
}

// handleRewind forks a session at a user message, or, for the preview,
// reports how many files that would write and delete.
func (g *Gateway) handleRewind(suffix string) func(http.ResponseWriter, *http.Request, *hub) {
	return func(w http.ResponseWriter, r *http.Request, h *hub) {
		var req proto.RewindRequest
		if !decodeJSON(w, r, maxJSONBody, &req) {
			return
		}
		if req.UpToMessageID == "" {
			writeError(w, http.StatusBadRequest, "up_to_message_id is required")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*upstreamTimeout)
		defer cancel()
		var out json.RawMessage
		path := "/workspaces/" + url.PathEscape(h.workspaceID()) + "/sessions/" + url.PathEscape(r.PathValue("sid")) + suffix
		if err := g.sendJSON(ctx, http.MethodPost, path, nil, req, &out); err != nil {
			writeUpstreamError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(out)
	}
}
