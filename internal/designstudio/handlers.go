// Package designstudio serves the design editor's files: the project's design
// documents under .atlas/design/files, the design kit and the images a
// design references. The desktop gateway mounts these handlers per
// workspace; the agent's studio server mounts them for one project, so the
// agent can open the same editor in a browser and work it by mouse and
// keyboard.
package designstudio

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

// kitTheme is a theme as the library panel shows it.
type kitTheme struct {
	ID       string               `json:"id"`
	Name     string               `json:"name"`
	Platform string               `json:"platform"`
	Note     string               `json:"note"`
	Source   string               `json:"source"`
	DarkOnly bool                 `json:"dark_only,omitempty"`
	Fonts    design.ThemeFonts    `json:"fonts"`
	Colors   map[string][2]string `json:"colors"`
}

// DesignKit lists the design kit: themes, accent hues and blocks.
func DesignKit(w http.ResponseWriter, r *http.Request, root string) {
	var themes []kitTheme
	for _, t := range design.Themes() {
		full, err := design.LookupTheme(t.ID, "")
		if err != nil {
			continue
		}
		themes = append(themes, kitTheme{ID: t.ID, Name: t.Name, Platform: t.Platform, Note: t.Note, Source: t.Source, DarkOnly: t.DarkOnly, Fonts: t.Fonts, Colors: full.Colors})
	}
	type accent struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	var accents []accent
	for _, name := range design.Accents() {
		if t, err := design.LookupTheme("radix", name); err == nil {
			accents = append(accents, accent{Name: name, Color: t.Colors["accent"][0]})
		}
	}
	w.Header().Set("Cache-Control", "max-age=300")
	WriteJSON(w, http.StatusOK, map[string]any{"themes": themes, "accents": accents, "blocks": design.Blocks(""), "icons": design.IconCount()})
}

// DesignKitIcons searches the icon set; no query lists common ones.
func DesignKitIcons(w http.ResponseWriter, r *http.Request, root string) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	var found []design.Icon
	if q == "" {
		for _, name := range commonIcons {
			if ic, err := design.LookupIcon(name); err == nil {
				found = append(found, ic)
			}
		}
	} else {
		found = design.SearchIcons(q, 96)
	}
	WriteJSON(w, http.StatusOK, map[string]any{"icons": found})
}

var commonIcons = []string{
	"house", "search", "bell", "settings", "user", "users", "mail", "calendar", "heart", "star", "bookmark", "share-2",
	"plus", "minus", "x", "check", "chevron-right", "chevron-left", "chevron-down", "arrow-right", "arrow-left", "arrow-up-right", "menu", "ellipsis",
	"camera", "image", "play", "pencil", "trash", "download", "upload", "link", "lock", "globe", "map-pin", "phone",
	"message-circle", "send", "shopping-cart", "credit-card", "wallet", "chart-column", "trending-up", "layers", "zap", "sparkles", "shield-check", "info",
}

// DesignKitPreview draws one block in a theme as SVG, for the library's thumbnails.
func DesignKitPreview(w http.ResponseWriter, r *http.Request, root string) {
	q := r.URL.Query()
	block := q.Get("block")
	theme := q.Get("theme")
	if theme == "" {
		for _, b := range design.Blocks("") {
			if b.ID == block {
				theme = design.DefaultTheme(b.Platform)
			}
		}
	}
	doc, _, err := design.Apply(design.New("preview"), "", []design.Op{{Op: "set_theme", Name: theme, Accent: q.Get("accent"), Mode: q.Get("mode")}, {Op: "insert", Block: block, ID: "b", X: new(float64), Y: new(float64)}})
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	svg, err := design.SVG(doc, doc.Pages[0], []string{"b"})
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	hd := w.Header()
	hd.Set("Content-Type", "image/svg+xml")
	hd.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	hd.Set("Cache-Control", "max-age=300")
	_, _ = w.Write([]byte(svg))
}

// DesignKitApply runs kit ops (set_theme, insert, and plain ops) on the
// document the design tool holds and returns the result; the tool commits it
// as one undoable step and saves it as usual.
func DesignKitApply(w http.ResponseWriter, r *http.Request, root string) {
	var req struct {
		Doc  json.RawMessage `json:"doc"`
		Page string          `json:"page"`
		Ops  []design.Op     `json:"ops"`
	}
	if !DecodeJSON(w, r, design.MaxFileBytes+(1<<20), &req) {
		return
	}
	doc, err := design.Parse(req.Doc)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(req.Ops) == 0 || len(req.Ops) > 100 {
		WriteError(w, http.StatusBadRequest, "send between 1 and 100 ops")
		return
	}
	next, res, err := design.Apply(doc, req.Page, req.Ops)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	created := make([]string, 0, len(res.Created))
	for i := range req.Ops {
		if id, ok := res.Created[i]; ok {
			created = append(created, id)
		}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"doc": next, "created": created, "notes": res.Notes})
}

func DesignHistory(w http.ResponseWriter, r *http.Request, root string) {
	versions, err := design.NewStore(root).History(r.URL.Query().Get("name"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"versions": versions})
}

func DesignVersion(w http.ResponseWriter, r *http.Request, root string) {
	doc, err := design.NewStore(root).Version(r.URL.Query().Get("name"), r.URL.Query().Get("id"))
	if err != nil {
		WriteError(w, http.StatusNotFound, err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"doc": doc})
}

func NameDesignVersion(w http.ResponseWriter, r *http.Request, root string) {
	var req struct {
		Name  string `json:"name"`
		Label string `json:"label"`
	}
	if !DecodeJSON(w, r, 1<<16, &req) {
		return
	}
	if err := design.NewStore(root).SaveVersion(req.Name, req.Label); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func DeleteDesignVersion(w http.ResponseWriter, r *http.Request, root string) {
	if err := design.NewStore(root).DeleteVersion(r.URL.Query().Get("name"), r.URL.Query().Get("id")); err != nil {
		WriteError(w, http.StatusNotFound, err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func DesignFiles(w http.ResponseWriter, r *http.Request, root string) {
	files, err := design.NewStore(root).List()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"files": files})
}

// DesignFile returns a design. With rev set to the revision the
// caller has, an unchanged file answers without the document, so the
// design tool can watch a file for the agent's edits cheaply.
func DesignFile(w http.ResponseWriter, r *http.Request, root string) {
	name := r.URL.Query().Get("name")
	store := design.NewStore(root)
	if !design.ValidName(name) {
		WriteError(w, http.StatusBadRequest, "invalid design name")
		return
	}
	if have := r.URL.Query().Get("rev"); have != "" {
		rev, err := store.Stat(name)
		if err == nil && rev == have {
			WriteJSON(w, http.StatusOK, map[string]any{"name": name, "rev": rev, "unchanged": true})
			return
		}
	}
	doc, rev, err := store.Load(name)
	if errors.Is(err, os.ErrNotExist) {
		WriteError(w, http.StatusNotFound, "no design named "+name)
		return
	}
	if err != nil {
		WriteError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"name": name, "rev": rev, "doc": doc})
}

type saveDesignRequest struct {
	Name    string          `json:"name"`
	Doc     json.RawMessage `json:"doc"`
	BaseRev string          `json:"base_rev"`
	Create  bool            `json:"create"`
}

func SaveDesign(w http.ResponseWriter, r *http.Request, root string) {
	var req saveDesignRequest
	if !DecodeJSON(w, r, design.MaxFileBytes+(1<<20), &req) {
		return
	}
	if !design.ValidName(req.Name) {
		WriteError(w, http.StatusBadRequest, "invalid design name: use lower case letters, digits, '.', '-' and '_'")
		return
	}
	doc, err := design.Parse(req.Doc)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	store := design.NewStore(root)
	rev, err := store.Save(req.Name, doc, req.BaseRev, req.Create)
	if errors.Is(err, design.ErrConflict) {
		current, _ := store.Stat(req.Name)
		WriteJSON(w, http.StatusConflict, map[string]any{"error": err.Error(), "rev": current})
		return
	}
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"name": req.Name, "rev": rev})
}

// DesignAsset serves an image file of the project that a design
// places by path. Only image types, only inside the project, and never as
// a document that could run script on this origin.
func DesignAsset(w http.ResponseWriter, r *http.Request, root string) {
	rel := r.URL.Query().Get("path")
	kind, ok := designAssetTypes[strings.ToLower(filepath.Ext(rel))]
	if !ok {
		WriteError(w, http.StatusBadRequest, "not an image path")
		return
	}
	target, err := InsideRoot(root, rel)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	info, err := os.Stat(target)
	if err != nil || info.IsDir() {
		WriteError(w, http.StatusNotFound, "no such image")
		return
	}
	if info.Size() > maxDesignAssetBytes {
		WriteError(w, http.StatusRequestEntityTooLarge, "the image is too large")
		return
	}
	f, err := os.Open(target)
	if err != nil {
		WriteError(w, http.StatusNotFound, "no such image")
		return
	}
	defer f.Close()
	hd := w.Header()
	hd.Set("Content-Type", kind)
	hd.Set("Content-Security-Policy", "default-src 'none'; img-src data:; style-src 'unsafe-inline'; sandbox")
	hd.Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, "", info.ModTime(), f)
}
