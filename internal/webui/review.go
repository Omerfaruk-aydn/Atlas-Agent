package webui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/proto"
)

const (
	maxReviewPaths    = 60
	maxReviewTask     = 6000
	maxReviewPathSize = 500
)

// registerReviews adds the routes behind the "Review" button on a turn's
// changed files. The review runs on the Atlas server as a job; the page starts
// it, polls it, and may cancel it.
func (g *Gateway) registerReviews(api *http.ServeMux) {
	api.HandleFunc("POST /api/workspaces/{id}/sessions/{sid}/reviews", g.withHub(g.handleStartReview))
	api.HandleFunc("GET /api/workspaces/{id}/reviews/{rid}", g.withHub(g.relayGet("/reviews/{rid}")))
	api.HandleFunc("DELETE /api/workspaces/{id}/reviews/{rid}", g.withHub(func(w http.ResponseWriter, r *http.Request, h *hub) {
		g.relay(w, r, http.MethodDelete, upstreamPath(r, h, "/reviews/{rid}"), nil, nil)
	}))
}

// handleStartReview checks a review request and hands it to the server. The
// server bounds it again; checking here keeps a malformed page from reaching it.
func (g *Gateway) handleStartReview(w http.ResponseWriter, r *http.Request, h *hub) {
	var req proto.ReviewRequest
	if !decodeJSON(w, r, 256<<10, &req) {
		return
	}
	if len(req.Paths) == 0 || len(req.Paths) > maxReviewPaths {
		writeError(w, http.StatusBadRequest, "a review takes between 1 and 60 files")
		return
	}
	for _, p := range req.Paths {
		if strings.TrimSpace(p) == "" || len(p) > maxReviewPathSize || strings.ContainsRune(p, 0) {
			writeError(w, http.StatusBadRequest, "a path is not valid")
			return
		}
	}
	if len(req.Task) > maxReviewTask {
		req.Task = req.Task[:maxReviewTask]
		// Do not leave half a character at the cut.
		for len(req.Task) > 0 && !utf8.ValidString(req.Task) {
			req.Task = req.Task[:len(req.Task)-1]
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), upstreamTimeout)
	defer cancel()
	var out json.RawMessage
	path := "/workspaces/" + url.PathEscape(h.workspaceID()) + "/sessions/" + url.PathEscape(r.PathValue("sid")) + "/reviews"
	if err := g.sendJSON(ctx, http.MethodPost, path, nil, req, &out); err != nil {
		if routeMissing(err) {
			writeError(w, http.StatusNotImplemented, "the running Atlas server is an older version")
			return
		}
		writeUpstreamError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write(out)
}
