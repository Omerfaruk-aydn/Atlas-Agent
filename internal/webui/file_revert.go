package webui

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// maxRevertBytes bounds the text of one file the browser may ask to put back.
const maxRevertBytes = 4 << 20

// revertFileRequest asks to undo what the agent did to one file. NewContent
// is what the agent left there; OldContent is what was there before. An
// empty OldContent means the agent created the file, so undoing removes it.
type revertFileRequest struct {
	Path       string `json:"path"`
	OldContent string `json:"old_content"`
	NewContent string `json:"new_content"`
}

func (g *Gateway) registerFileRevert(api *http.ServeMux) {
	api.HandleFunc("POST /api/workspaces/{id}/files/revert", g.withHub(g.handleFileRevert))
}

// handleFileRevert puts one file back as it was before the agent's edit. It
// only acts when the file still holds exactly what the agent wrote, so a
// change the person made since is never overwritten, and only inside the
// workspace folder.
func (g *Gateway) handleFileRevert(w http.ResponseWriter, r *http.Request, h *hub) {
	var req revertFileRequest
	if !decodeJSON(w, r, 2*maxRevertBytes+(64<<10), &req) {
		return
	}
	if len(req.OldContent) > maxRevertBytes || len(req.NewContent) > maxRevertBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "the file is too large to undo here")
		return
	}
	target, err := workspaceFile(h.path, req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	current, err := os.ReadFile(target)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// Already gone: undoing a creation is done, anything else is not.
			if req.OldContent == "" {
				writeJSON(w, http.StatusOK, map[string]string{"result": "removed"})
				return
			}
			writeError(w, http.StatusConflict, "the file no longer exists")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if normalizeLines(string(current)) != normalizeLines(req.NewContent) {
		writeError(w, http.StatusConflict, "the file has changed since the agent edited it")
		return
	}
	if req.OldContent == "" {
		if err := os.Remove(target); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"result": "removed"})
		return
	}
	// Keep the line endings the file has now.
	content := req.OldContent
	if strings.Contains(string(current), "\r\n") {
		content = strings.ReplaceAll(normalizeLines(content), "\n", "\r\n")
	}
	mode := os.FileMode(0o644)
	if info, err := os.Stat(target); err == nil {
		mode = info.Mode().Perm()
	}
	if err := os.WriteFile(target, []byte(content), mode); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"result": "restored"})
}

func normalizeLines(s string) string { return strings.ReplaceAll(s, "\r\n", "\n") }

// workspaceFile resolves a path the browser sent to a file inside root. It
// refuses anything outside, including through symbolic links.
func workspaceFile(root, rel string) (string, error) {
	if root == "" || strings.TrimSpace(rel) == "" {
		return "", errors.New("a path is required")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if realRoot, err := filepath.EvalSymlinks(absRoot); err == nil {
		absRoot = realRoot
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
