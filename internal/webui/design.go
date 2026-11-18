package webui

import (
	"net/http"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/designstudio"
)

// registerDesign serves the design tool's files: the project's design
// documents under .atlas/design/files, the kit, and images they reference.
func (g *Gateway) registerDesign(api *http.ServeMux) {
	for _, route := range designstudio.Routes() {
		method, path, _ := strings.Cut(route.Pattern, " ")
		handle := route.Handle
		api.HandleFunc(method+" /api/workspaces/{id}"+path, g.withHub(func(w http.ResponseWriter, r *http.Request, h *hub) {
			handle(w, r, h.path)
		}))
	}
}
