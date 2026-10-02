package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/jsonstrict"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/proto"
)

// Workflow snapshots require an attached client, like presence mutations.
// @Summary Read coordinated workflow state
// @Tags workflow
// @Produce json
// @Param id path string true "Workspace ID"
// @Param sid path string true "Session ID"
// @Param client_id query string true "Attached client ID"
// @Success 200 {object} proto.WorkflowSnapshot
// @Router /workspaces/{id}/sessions/{sid}/workflow [get]
func (c *controllerV1) handleGetWorkflow(w http.ResponseWriter, r *http.Request) {
	client, ok := c.requireClientID(w, r)
	if !ok {
		return
	}
	if err := c.backend.AuthorizeWorkflow(r.PathValue("id"), client); err != nil {
		jsonError(w, http.StatusForbidden, "Attached client required")
		return
	}
	snapshot, err := c.backend.WorkflowSnapshot(r.Context(), r.PathValue("id"), r.PathValue("sid"))
	if err != nil {
		c.handleError(w, r, err)
		return
	}
	jsonEncode(w, snapshot)
}

// @Summary Control coordinated workflow with revision checking
// @Tags workflow
// @Accept json
// @Produce json
// @Param id path string true "Workspace ID"
// @Param sid path string true "Session ID"
// @Param client_id query string true "Attached client ID"
// @Param request body proto.WorkflowControl true "Revision-checked control"
// @Success 200 {object} proto.WorkflowSnapshot
// @Router /workspaces/{id}/sessions/{sid}/workflow [post]
func (c *controllerV1) handlePostWorkflow(w http.ResponseWriter, r *http.Request) {
	client, ok := c.requireClientID(w, r)
	if !ok {
		return
	}
	if err := c.backend.AuthorizeWorkflow(r.PathValue("id"), client); err != nil {
		jsonError(w, http.StatusForbidden, "Attached client required")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	data, err := io.ReadAll(r.Body)
	if err != nil || jsonstrict.Validate(r.Context(), data) != nil {
		jsonError(w, http.StatusBadRequest, "Invalid workflow control")
		return
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var action proto.WorkflowControl
	if err := decoder.Decode(&action); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid workflow control")
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		jsonError(w, http.StatusBadRequest, "Invalid workflow control")
		return
	}
	if err := c.backend.WorkflowControl(r.Context(), r.PathValue("id"), r.PathValue("sid"), action); err != nil {
		jsonError(w, http.StatusConflict, err.Error())
		return
	}
	snapshot, err := c.backend.WorkflowSnapshot(r.Context(), r.PathValue("id"), r.PathValue("sid"))
	if err != nil {
		c.handleError(w, r, err)
		return
	}
	jsonEncode(w, snapshot)
}
