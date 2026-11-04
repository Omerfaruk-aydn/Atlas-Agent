package server

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/connections"
)

func (c *controllerV1) handleGetWorkspaceConnections(w http.ResponseWriter, r *http.Request) {
	views, err := c.backend.ListConnections(r.PathValue("id"))
	if err != nil {
		c.handleError(w, r, err)
		return
	}
	jsonEncode(w, views)
}

func (c *controllerV1) handlePostWorkspaceConnection(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 32<<10))
	if err != nil {
		jsonError(w, http.StatusBadRequest, "invalid connection settings")
		return
	}
	req, err := connections.DecodeSave(data)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := c.backend.SaveConnection(r.PathValue("id"), r.PathValue("connection"), req); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	views, err := c.backend.ListConnections(r.PathValue("id"))
	if err != nil {
		c.handleError(w, r, err)
		return
	}
	jsonEncode(w, views)
}

func (c *controllerV1) handlePostWorkspaceConnectionTest(w http.ResponseWriter, r *http.Request) {
	result, err := c.backend.TestConnection(r.Context(), r.PathValue("id"), r.PathValue("connection"))
	if err != nil {
		jsonError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	jsonEncode(w, result)
}

func (c *controllerV1) handlePostWorkspaceConnectionOAuth(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 32<<10))
	if err != nil {
		jsonError(w, http.StatusBadRequest, "invalid OAuth request")
		return
	}
	var input struct {
		ReturnURL string `json:"return_url"`
		Reauth    bool   `json:"reauth"`
	}
	if len(data) > 0 && json.Unmarshal(data, &input) != nil {
		jsonError(w, http.StatusBadRequest, "invalid OAuth request")
		return
	}
	result, err := c.backend.StartConnectionOAuth(r.Context(), r.PathValue("id"), r.PathValue("connection"), input.ReturnURL, input.Reauth)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonEncode(w, result)
}
