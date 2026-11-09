package webui

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/design"
)

// maxDesignAssetBytes bounds one image the design tool loads from the project.
const maxDesignAssetBytes = 32 << 20

var designAssetTypes = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".svg":  "image/svg+xml",
}

// registerDesign serves the design tool's files: the project's design
// documents under .atlas/design/files, and images they reference.
func (g *Gateway) registerDesign(api *http.ServeMux) {
	api.HandleFunc("GET /api/workspaces/{id}/design/files", g.withHub(g.handleDesignFiles))
	api.HandleFunc("GET /api/workspaces/{id}/design/file", g.withHub(g.handleDesignFile))
	api.HandleFunc("POST /api/workspaces/{id}/design/file", g.withHub(g.handleSaveDesign))
	api.HandleFunc("GET /api/workspaces/{id}/design/asset", g.withHub(g.handleDesignAsset))
	api.HandleFunc("GET /api/workspaces/{id}/design/history", g.withHub(g.handleDesignHistory))
	api.HandleFunc("GET /api/workspaces/{id}/design/version", g.withHub(g.handleDesignVersion))
	api.HandleFunc("POST /api/workspaces/{id}/design/version", g.withHub(g.handleNameDesignVersion))
	api.HandleFunc("DELETE /api/workspaces/{id}/design/version", g.withHub(g.handleDeleteDesignVersion))
}

func (g *Gateway) handleDesignHistory(w http.ResponseWriter, r *http.Request, h *hub) {
	versions, err := design.NewStore(h.path).History(r.URL.Query().Get("name"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"versions": versions})
}

func (g *Gateway) handleDesignVersion(w http.ResponseWriter, r *http.Request, h *hub) {
	doc, err := design.NewStore(h.path).Version(r.URL.Query().Get("name"), r.URL.Query().Get("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"doc": doc})
}

func (g *Gateway) handleNameDesignVersion(w http.ResponseWriter, r *http.Request, h *hub) {
	var req struct {
		Name  string `json:"name"`
		Label string `json:"label"`
	}
	if !decodeJSON(w, r, 1<<16, &req) {
		return
	}
	if err := design.NewStore(h.path).SaveVersion(req.Name, req.Label); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (g *Gateway) handleDeleteDesignVersion(w http.ResponseWriter, r *http.Request, h *hub) {
	if err := design.NewStore(h.path).DeleteVersion(r.URL.Query().Get("name"), r.URL.Query().Get("id")); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (g *Gateway) handleDesignFiles(w http.ResponseWriter, _ *http.Request, h *hub) {
	files, err := design.NewStore(h.path).List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": files})
}

// handleDesignFile returns a design. With rev set to the revision the
// caller has, an unchanged file answers without the document, so the
// design tool can watch a file for the agent's edits cheaply.
func (g *Gateway) handleDesignFile(w http.ResponseWriter, r *http.Request, h *hub) {
	name := r.URL.Query().Get("name")
	store := design.NewStore(h.path)
	if !design.ValidName(name) {
		writeError(w, http.StatusBadRequest, "invalid design name")
		return
	}
	if have := r.URL.Query().Get("rev"); have != "" {
		rev, err := store.Stat(name)
		if err == nil && rev == have {
			writeJSON(w, http.StatusOK, map[string]any{"name": name, "rev": rev, "unchanged": true})
			return
		}
	}
	doc, rev, err := store.Load(name)
	if errors.Is(err, os.ErrNotExist) {
		writeError(w, http.StatusNotFound, "no design named "+name)
		return
	}
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": name, "rev": rev, "doc": doc})
}

type saveDesignRequest struct {
	Name    string          `json:"name"`
	Doc     json.RawMessage `json:"doc"`
	BaseRev string          `json:"base_rev"`
	Create  bool            `json:"create"`
}

func (g *Gateway) handleSaveDesign(w http.ResponseWriter, r *http.Request, h *hub) {
	var req saveDesignRequest
	if !decodeJSON(w, r, design.MaxFileBytes+(1<<20), &req) {
		return
	}
	if !design.ValidName(req.Name) {
		writeError(w, http.StatusBadRequest, "invalid design name: use lower case letters, digits, '.', '-' and '_'")
		return
	}
	doc, err := design.Parse(req.Doc)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	store := design.NewStore(h.path)
	rev, err := store.Save(req.Name, doc, req.BaseRev, req.Create)
	if errors.Is(err, design.ErrConflict) {
		current, _ := store.Stat(req.Name)
		writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error(), "rev": current})
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": req.Name, "rev": rev})
}

// handleDesignAsset serves an image file of the project that a design
// places by path. Only image types, only inside the project, and never as
// a document that could run script on this origin.
func (g *Gateway) handleDesignAsset(w http.ResponseWriter, r *http.Request, h *hub) {
	rel := r.URL.Query().Get("path")
	kind, ok := designAssetTypes[strings.ToLower(filepath.Ext(rel))]
	if !ok {
		writeError(w, http.StatusBadRequest, "not an image path")
		return
	}
	target, err := workspaceFile(h.path, rel)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	info, err := os.Stat(target)
	if err != nil || info.IsDir() {
		writeError(w, http.StatusNotFound, "no such image")
		return
	}
	if info.Size() > maxDesignAssetBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "the image is too large")
		return
	}
	f, err := os.Open(target)
	if err != nil {
		writeError(w, http.StatusNotFound, "no such image")
		return
	}
	defer f.Close()
	hd := w.Header()
	hd.Set("Content-Type", kind)
	hd.Set("Content-Security-Policy", "default-src 'none'; img-src data:; style-src 'unsafe-inline'; sandbox")
	hd.Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, "", info.ModTime(), f)
}
