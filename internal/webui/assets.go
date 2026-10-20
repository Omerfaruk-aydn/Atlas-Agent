package webui

import (
	"embed"
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// build holds the production web client. Only the placeholder is
// committed; `task web:build` fills it before a release build. A binary
// built without it still compiles and runs every other command -- `web`
// reports ErrAssetsMissing instead of serving an empty page.
//
//go:embed all:build
var buildFS embed.FS

// EmbeddedAssets returns the web client built into this binary.
func EmbeddedAssets() (fs.FS, error) {
	sub, err := fs.Sub(buildFS, "build")
	if err != nil {
		return nil, err
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil, ErrAssetsMissing
	}
	return sub, nil
}

// staticHandler serves the client. Hashed build assets are cached for
// good; everything else, index.html included, is revalidated so a new
// binary's client is picked up at once. Unknown paths fall back to
// index.html, which routes on the client.
func (g *Gateway) staticHandler() http.Handler {
	files := http.FileServerFS(g.assets)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "" || name == "index.html" {
			g.serveIndex(w, r)
			return
		}
		info, err := fs.Stat(g.assets, name)
		if err != nil || info.IsDir() {
			if errors.Is(err, fs.ErrNotExist) && path.Ext(name) != "" {
				http.NotFound(w, r)
				return
			}
			g.serveIndex(w, r)
			return
		}
		if strings.HasPrefix(name, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	})
}

func (g *Gateway) serveIndex(w http.ResponseWriter, r *http.Request) {
	raw, err := fs.ReadFile(g.assets, "index.html")
	if err != nil {
		writeError(w, http.StatusInternalServerError, ErrAssetsMissing.Error())
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(raw)
}
