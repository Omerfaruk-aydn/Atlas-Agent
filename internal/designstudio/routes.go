package designstudio

import (
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
)

// Handler is one design endpoint for the project at root.
type Handler func(w http.ResponseWriter, r *http.Request, root string)

// Route is one design endpoint and the path under the workspace's API.
type Route struct {
	Pattern string // "GET /design/files"
	Handle  Handler
}

// Routes lists the design endpoints, relative to /api/workspaces/{id}.
func Routes() []Route {
	return []Route{
		{"GET /design/files", DesignFiles},
		{"GET /design/file", DesignFile},
		{"POST /design/file", SaveDesign},
		{"GET /design/asset", DesignAsset},
		{"GET /design/history", DesignHistory},
		{"GET /design/version", DesignVersion},
		{"POST /design/version", NameDesignVersion},
		{"DELETE /design/version", DeleteDesignVersion},
		{"GET /design/kit", DesignKit},
		{"GET /design/kit/icons", DesignKitIcons},
		{"GET /design/kit/preview", DesignKitPreview},
		{"POST /design/kit/apply", DesignKitApply},
	}
}

// WriteJSON answers with v as JSON.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteError answers with {"message": msg}, the gateway's error shape.
func WriteError(w http.ResponseWriter, status int, msg string) {
	WriteJSON(w, status, map[string]string{"message": msg})
}

// DecodeJSON reads a bounded JSON body, answering the request itself on
// failure.
func DecodeJSON(w http.ResponseWriter, r *http.Request, limit int64, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			WriteError(w, http.StatusRequestEntityTooLarge, "request body is too large")
			return false
		}
		WriteError(w, http.StatusBadRequest, "malformed JSON body")
		return false
	}
	return true
}

// InsideRoot resolves rel against root and refuses a path that leaves it,
// symbolic links included.
func InsideRoot(root, rel string) (string, error) {
	if root == "" || strings.TrimSpace(rel) == "" {
		return "", errors.New("a path is required")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if real, err := filepath.EvalSymlinks(absRoot); err == nil {
		absRoot = real
	}
	target := rel
	if !filepath.IsAbs(target) {
		target = filepath.Join(absRoot, target)
	}
	target = filepath.Clean(target)
	if real, err := filepath.EvalSymlinks(target); err == nil {
		target = real
	} else if real, err := filepath.EvalSymlinks(filepath.Dir(target)); err == nil {
		target = filepath.Join(real, filepath.Base(target))
	}
	inside, err := filepath.Rel(absRoot, target)
	if err != nil || inside == "." || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) || filepath.IsAbs(inside) {
		return "", errors.New("the path is outside the project")
	}
	return target, nil
}
