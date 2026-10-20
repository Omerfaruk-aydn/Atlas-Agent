package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/proto"
)

// handleGetWorkspacePermissionsPending lists permission requests still
// awaiting a decision.
//
//	@Summary		List pending permission requests
//	@Tags			permissions
//	@Produce		json
//	@Param			id	path		string	true	"Workspace ID"
//	@Success		200	{array}		proto.PermissionRequest
//	@Failure		404	{object}	proto.Error
//	@Router			/workspaces/{id}/permissions/pending [get]
func (c *controllerV1) handleGetWorkspacePermissionsPending(w http.ResponseWriter, r *http.Request) {
	pending, err := c.backend.PendingPermissions(r.PathValue("id"))
	if err != nil {
		c.handleError(w, r, err)
		return
	}
	out := make([]proto.PermissionRequest, 0, len(pending))
	for _, p := range pending {
		out = append(out, proto.PermissionRequest{
			ID:          p.ID,
			SessionID:   p.SessionID,
			ToolCallID:  p.ToolCallID,
			ToolName:    p.ToolName,
			Description: p.Description,
			Action:      p.Action,
			Path:        p.Path,
			Params:      p.Params,
		})
	}
	jsonEncode(w, out)
}

// handleGetWorkspaceQuestionsPending lists question batches still
// awaiting an answer, oldest first.
//
//	@Summary		List pending question batches
//	@Tags			questions
//	@Produce		json
//	@Param			id	path		string	true	"Workspace ID"
//	@Success		200	{array}		proto.QuestionRequest
//	@Failure		404	{object}	proto.Error
//	@Router			/workspaces/{id}/questions/pending [get]
func (c *controllerV1) handleGetWorkspaceQuestionsPending(w http.ResponseWriter, r *http.Request) {
	pending, err := c.backend.PendingQuestions(r.PathValue("id"))
	if err != nil {
		c.handleError(w, r, err)
		return
	}
	out := make([]proto.QuestionRequest, 0, len(pending))
	for _, q := range pending {
		out = append(out, proto.QuestionRequest{
			ID:                 q.ID,
			SessionID:          q.SessionID,
			ToolCallID:         q.ToolCallID,
			Questions:          questionsToProto(q.Questions),
			ConfirmTitle:       q.ConfirmTitle,
			ConfirmDescription: q.ConfirmDescription,
		})
	}
	jsonEncode(w, out)
}

// handleGetWorkspacePermissionsMode returns the permission mode.
//
//	@Summary		Get permission mode
//	@Tags			permissions
//	@Produce		json
//	@Param			id	path		string	true	"Workspace ID"
//	@Success		200	{object}	proto.PermissionModeRequest
//	@Failure		404	{object}	proto.Error
//	@Router			/workspaces/{id}/permissions/mode [get]
func (c *controllerV1) handleGetWorkspacePermissionsMode(w http.ResponseWriter, r *http.Request) {
	mode, err := c.backend.GetPermissionMode(r.PathValue("id"))
	if err != nil {
		c.handleError(w, r, err)
		return
	}
	jsonEncode(w, proto.PermissionModeRequest{Mode: string(mode)})
}

// handlePostWorkspacePermissionsMode sets the permission mode.
//
//	@Summary		Set permission mode
//	@Tags			permissions
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string						true	"Workspace ID"
//	@Param			request	body		proto.PermissionModeRequest	true	"manual, auto_accept_edits, plan or bypass"
//	@Success		200		{object}	proto.PermissionModeRequest
//	@Failure		400		{object}	proto.Error
//	@Failure		404		{object}	proto.Error
//	@Router			/workspaces/{id}/permissions/mode [post]
func (c *controllerV1) handlePostWorkspacePermissionsMode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req proto.PermissionModeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		c.server.logError(r, "Failed to decode request", "error", err)
		jsonError(w, http.StatusBadRequest, "failed to decode request")
		return
	}
	if err := c.backend.SetPermissionMode(id, permission.PermissionMode(req.Mode)); err != nil {
		c.handleError(w, r, err)
		return
	}
	mode, err := c.backend.GetPermissionMode(id)
	if err != nil {
		c.handleError(w, r, err)
		return
	}
	jsonEncode(w, proto.PermissionModeRequest{Mode: string(mode)})
}

// handleGetWorkspaceFiles lists one level of a workspace directory.
//
//	@Summary		List workspace directory
//	@Tags			files
//	@Produce		json
//	@Param			id		path		string	true	"Workspace ID"
//	@Param			path	query		string	false	"Directory relative to the workspace root"
//	@Success		200		{object}	proto.DirectoryListing
//	@Failure		400		{object}	proto.Error
//	@Failure		403		{object}	proto.Error
//	@Failure		404		{object}	proto.Error
//	@Router			/workspaces/{id}/files [get]
func (c *controllerV1) handleGetWorkspaceFiles(w http.ResponseWriter, r *http.Request) {
	listing, err := c.backend.ListWorkspaceDirectory(r.PathValue("id"), r.URL.Query().Get("path"))
	if err != nil {
		c.handleError(w, r, err)
		return
	}
	jsonEncode(w, listing)
}

// handleGetWorkspaceFileContent reads a workspace file.
//
//	@Summary		Read workspace file
//	@Tags			files
//	@Produce		json
//	@Param			id			path		string	true	"Workspace ID"
//	@Param			path		query		string	true	"File relative to the workspace root"
//	@Param			max_bytes	query		int		false	"Maximum bytes of content to return"
//	@Success		200			{object}	proto.FileContent
//	@Failure		400			{object}	proto.Error
//	@Failure		403			{object}	proto.Error
//	@Failure		404			{object}	proto.Error
//	@Router			/workspaces/{id}/files/content [get]
func (c *controllerV1) handleGetWorkspaceFileContent(w http.ResponseWriter, r *http.Request) {
	var maxBytes int64
	if v := r.URL.Query().Get("max_bytes"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			jsonError(w, http.StatusBadRequest, "max_bytes must be a non-negative integer")
			return
		}
		maxBytes = n
	}
	content, err := c.backend.ReadWorkspaceFile(r.PathValue("id"), r.URL.Query().Get("path"), maxBytes)
	if err != nil {
		c.handleError(w, r, err)
		return
	}
	jsonEncode(w, content)
}

// handlePostWorkspaceSessionTitle renames a session without touching its
// usage counters, which a full PUT of the session would overwrite.
//
//	@Summary		Rename session
//	@Tags			sessions
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string				true	"Workspace ID"
//	@Param			sid		path		string				true	"Session ID"
//	@Param			request	body		proto.SessionRename	true	"New title"
//	@Success		200		{object}	proto.Session
//	@Failure		400		{object}	proto.Error
//	@Failure		404		{object}	proto.Error
//	@Router			/workspaces/{id}/sessions/{sid}/title [post]
func (c *controllerV1) handlePostWorkspaceSessionTitle(w http.ResponseWriter, r *http.Request) {
	id, sid := r.PathValue("id"), r.PathValue("sid")
	var req proto.SessionRename
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		c.server.logError(r, "Failed to decode request", "error", err)
		jsonError(w, http.StatusBadRequest, "failed to decode request")
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" || len(title) > 200 {
		jsonError(w, http.StatusBadRequest, "title must be 1 to 200 characters")
		return
	}
	if err := c.backend.RenameSession(r.Context(), id, sid, title); err != nil {
		c.handleError(w, r, err)
		return
	}
	sess, err := c.backend.GetSession(r.Context(), id, sid)
	if err != nil {
		c.handleError(w, r, err)
		return
	}
	ws, _ := c.backend.GetWorkspace(id)
	out := sessionToProto(sess)
	out.IsBusy = isSessionBusy(ws, sess.ID)
	out.AttachedClients = attachedClients(ws, sess.ID)
	jsonEncode(w, out)
}
