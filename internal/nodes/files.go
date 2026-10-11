package nodes

import (
	"errors"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/taskstate"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/transfer"
)

// ListResult is one directory listing under an allowed folder.
type ListResult struct {
	Path      string           `json:"path"`
	Entries   []transfer.Entry `json:"entries"`
	Truncated bool             `json:"truncated,omitempty"`
	// Roots lists the allowed folder labels when path is empty.
	Roots []string `json:"roots,omitempty"`
}

// UploadRequest starts or resumes a chunked upload.
type UploadRequest struct {
	OperationID string `json:"operation_id"`
	Path        string `json:"path"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
	// Conflict is error (default), rename or overwrite.
	Conflict string `json:"conflict,omitempty"`
}

// UploadResult is an upload's state as the controller sees it. Path is
// relative to the allowed folder, never a host path outside it.
type UploadResult struct {
	transfer.UploadStatus
	Path     string `json:"path"`
	Replayed bool   `json:"replayed,omitempty"`
}

func (s *Server) handleListFiles(w http.ResponseWriter, r *http.Request) {
	g := grantFrom(r)
	rel := r.URL.Query().Get("path")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if rel == "" && len(g.Roots) > 0 {
		out := ListResult{}
		for _, root := range sortedRoots(g.Roots) {
			out.Roots = append(out.Roots, root.Label)
			out.Entries = append(out.Entries, transfer.Entry{Name: root.Label, Dir: true})
		}
		writeJSON(w, http.StatusOK, out)
		return
	}
	root, dir, err := s.resolveFor(g, orDot(rel))
	if err != nil {
		writeError(w, http.StatusBadRequest, transfer.Code(err), err.Error(), false, "none")
		return
	}
	inner, _ := filepath.Rel(root, dir)
	entries, truncated, err := transfer.List(root, filepath.ToSlash(inner), min(max(limit, 1), 2000))
	if err != nil {
		status := http.StatusBadRequest
		if transfer.Code(err) == "not_found" {
			status = http.StatusNotFound
		}
		writeError(w, status, transfer.Code(err), err.Error(), false, "none")
		return
	}
	writeJSON(w, http.StatusOK, ListResult{Path: rel, Entries: entries, Truncated: truncated})
}

func orDot(rel string) string {
	if rel == "" {
		return "."
	}
	return rel
}

