package server

import (
	"encoding/json"
	"net/http"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/proto"
)

// maxReviewBody bounds a review request: a task of a few thousand bytes and
// up to sixty paths.
const maxReviewBody = 256 << 10

// handlePostWorkspaceSessionReview starts a review of the changes a turn made
// and answers at once with the running review.
//
//	@Summary		Review changes
//	@Tags			agent
//	@Accept			json
//	@Produce		json
//	@Param			id		path	string				true	"Workspace ID"
//	@Param			sid		path	string				true	"Session ID"
//	@Param			request	body	proto.ReviewRequest	true	"Files and task"
//	@Success		202		{object}	proto.Review
//	@Failure		400		{object}	proto.Error
//	@Failure		404		{object}	proto.Error
//	@Failure		429		{object}	proto.Error
//	@Router			/workspaces/{id}/sessions/{sid}/reviews [post]
func (c *controllerV1) handlePostWorkspaceSessionReview(w http.ResponseWriter, r *http.Request) {
	var req proto.ReviewRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxReviewBody)).Decode(&req); err != nil {
		c.server.logError(r, "Failed to decode review request", "error", err)
		jsonError(w, http.StatusBadRequest, "failed to decode request")
		return
	}
	review, err := c.backend.StartReview(r.PathValue("id"), r.PathValue("sid"), req)
	if err != nil {
		c.handleError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	jsonEncode(w, review)
}

// handleGetWorkspaceReview returns a review: running, or finished with its
// findings.
//
//	@Summary		Get a review
//	@Tags			agent
//	@Produce		json
//	@Param			id	path	string	true	"Workspace ID"
//	@Param			rid	path	string	true	"Review ID"
//	@Success		200	{object}	proto.Review
//	@Failure		404	{object}	proto.Error
//	@Router			/workspaces/{id}/reviews/{rid} [get]
func (c *controllerV1) handleGetWorkspaceReview(w http.ResponseWriter, r *http.Request) {
	review, err := c.backend.GetReview(r.PathValue("id"), r.PathValue("rid"))
	if err != nil {
		c.handleError(w, r, err)
		return
	}
	jsonEncode(w, review)
}

// handleDeleteWorkspaceReview stops a running review.
//
//	@Summary		Cancel a review
//	@Tags			agent
//	@Produce		json
//	@Param			id	path	string	true	"Workspace ID"
//	@Param			rid	path	string	true	"Review ID"
//	@Success		200	{object}	proto.Review
//	@Failure		404	{object}	proto.Error
//	@Router			/workspaces/{id}/reviews/{rid} [delete]
func (c *controllerV1) handleDeleteWorkspaceReview(w http.ResponseWriter, r *http.Request) {
	review, err := c.backend.CancelReview(r.PathValue("id"), r.PathValue("rid"))
	if err != nil {
		c.handleError(w, r, err)
		return
	}
	jsonEncode(w, review)
}
