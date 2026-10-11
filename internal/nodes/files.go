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

// displayPath names a host path relative to the grant's folder it is in.
func (s *Server) displayPath(g Grant, abs string) string {
	if abs == "" {
		return ""
	}
	if len(g.Roots) == 0 {
		if rel, err := filepath.Rel(s.opts.Root, abs); err == nil {
			return filepath.ToSlash(rel)
		}
		return filepath.Base(abs)
	}
	for _, root := range g.Roots {
		real, err := filepath.EvalSymlinks(root.Path)
		if err != nil {
			real = root.Path
		}
		for _, base := range []string{real, root.Path} {
			if rel, err := filepath.Rel(base, abs); err == nil && !strings.HasPrefix(rel, "..") {
				return root.Label + "/" + filepath.ToSlash(rel)
			}
		}
	}
	return filepath.Base(abs)
}

func (s *Server) uploadResult(g Grant, st transfer.UploadStatus, replayed bool) UploadResult {
	out := UploadResult{UploadStatus: st, Replayed: replayed}
	out.Path = s.displayPath(g, st.Path)
	out.UploadStatus.Path = ""
	return out
}

func (s *Server) handleCreateUpload(w http.ResponseWriter, r *http.Request) {
	g := grantFrom(r)
	var req UploadRequest
	if !decode(w, r, &req) {
		return
	}
	req.SHA256 = strings.ToLower(req.SHA256)
	policy, err := transfer.ParseConflict(req.Conflict)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), false, "none")
		return
	}
	if policy == transfer.ConflictOverwrite && !s.allowModify(w, r) {
		return
	}
	root, inner, err := s.splitFor(g, req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, transfer.Code(err), err.Error(), false, "none")
		return
	}
	op, existing, err := s.begin(r, Operation{ID: req.OperationID, Kind: "upload", RequestHash: RequestHash([]any{req.Path, req.Size, req.SHA256, policy}), Path: req.Path, SHA256: req.SHA256, Size: req.Size})
	if err != nil {
		beginError(w, err)
		return
	}
	if existing && op.State.Terminal() {
		if op.State == taskstate.Completed {
			writeJSON(w, http.StatusOK, UploadResult{UploadStatus: transfer.UploadStatus{OperationID: op.ID, Size: op.Size, Received: op.Size, SHA256: op.SHA256, State: "complete"}, Path: op.Path, Replayed: true})
			return
		}
		writeError(w, http.StatusConflict, "operation_"+string(op.State), "this upload already ended: "+op.Error, false, op.Effect)
		return
	}
	st, err := s.uploads.Create(transfer.UploadSpec{Owner: g.Device.ID, Key: op.ID, Root: root, Rel: inner, Size: req.Size, SHA256: req.SHA256, Conflict: policy}, s.maxFile())
	if err != nil {
		code := transfer.Code(err)
		if errors.Is(err, transfer.ErrUploadConflict) {
			code = "operation_conflict"
		}
		// Nothing was written; the journaled operation ends without effect.
		_, _ = s.finish(g, op.ID, func(o *Operation) { o.State, o.Effect, o.Error = taskstate.Failed, "none", err.Error() })
		writeError(w, http.StatusBadRequest, code, err.Error(), false, "none")
		return
	}
	writeJSON(w, http.StatusOK, s.uploadResult(g, st, existing))
}

func (s *Server) handleUploadStatus(w http.ResponseWriter, r *http.Request) {
	g := grantFrom(r)
	st, err := s.uploads.Status(r.PathValue("id"), g.Device.ID)
	if err != nil {
		writeError(w, http.StatusNotFound, transfer.Code(err), err.Error(), false, "none")
		return
	}
	writeJSON(w, http.StatusOK, s.uploadResult(g, st, false))
}

func (s *Server) handleUploadChunk(w http.ResponseWriter, r *http.Request) {
	g := grantFrom(r)
	offset, err := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
	if err != nil || r.ContentLength <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "chunk needs an offset query and a Content-Length", false, "none")
		return
	}
	body := http.MaxBytesReader(w, r.Body, transfer.MaxChunkBytes)
	st, err := s.uploads.Write(r.PathValue("id"), g.Device.ID, offset, body, r.ContentLength, strings.ToLower(r.Header.Get("X-Atlas-Chunk-SHA256")))
	if err != nil {
		status := http.StatusBadRequest
		switch transfer.Code(err) {
		case "not_found":
			status = http.StatusNotFound
		case "offset_mismatch":
			status = http.StatusConflict
		}
		writeJSON(w, status, struct {
			APIError
			Upload UploadResult `json:"upload"`
		}{APIError{Code: transfer.Code(err), Message: err.Error(), Effect: "none"}, s.uploadResult(g, st, false)})
		return
	}
	writeJSON(w, http.StatusOK, s.uploadResult(g, st, false))
}

func (s *Server) handleUploadComplete(w http.ResponseWriter, r *http.Request) {
	g := grantFrom(r)
	st, err := s.uploads.Complete(r.PathValue("id"), g.Device.ID)
	if err != nil && st.ID == "" {
		writeError(w, http.StatusNotFound, transfer.Code(err), err.Error(), false, "none")
		return
	}
	if err != nil && st.State == "receiving" {
		// Not settled yet (incomplete or a transient disk error).
		writeError(w, http.StatusConflict, transfer.Code(err), err.Error(), false, "none")
		return
	}
	display := s.displayPath(g, st.Path)
	final, ferr := s.finish(g, st.OperationID, func(o *Operation) {
		if st.State == "complete" {
			o.State, o.Effect, o.Path = taskstate.Completed, "applied", display
			return
		}
		o.State, o.Effect, o.Error = taskstate.Failed, "none", st.Error
	})
	if ferr != nil {
		writeError(w, http.StatusInternalServerError, "persist_failed", ferr.Error(), true, "unknown")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, transfer.Code(err), err.Error(), false, "none")
		return
	}
	res := s.uploadResult(g, st, false)
	res.Path = final.Path
	writeJSON(w, http.StatusOK, res)
}

