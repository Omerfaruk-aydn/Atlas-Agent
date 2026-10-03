package server

import (
	"net/http"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/shell"
)

func (c *controllerV1) handleGetJobOutput(w http.ResponseWriter, r *http.Request) {
	client, ok := c.requireClientID(w, r)
	if !ok {
		return
	}
	wid := r.PathValue("id")
	if err := c.backend.AuthorizeWorkflow(wid, client); err != nil {
		jsonError(w, http.StatusForbidden, "Attached client required")
		return
	}
	ws, err := c.backend.GetWorkspace(wid)
	if err != nil {
		c.handleError(w, r, err)
		return
	}
	out, err := shell.GetBackgroundShellManager().OutputForRoot(ws.Cfg.WorkingDir(), r.PathValue("jid"))
	if err != nil {
		jsonError(w, http.StatusNotFound, err.Error())
		return
	}
	jsonEncode(w, out)
}
