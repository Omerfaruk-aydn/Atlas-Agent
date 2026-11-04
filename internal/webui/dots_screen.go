package webui

import (
	"fmt"
	"net/http"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/dots"
)

// registerDotScreen adds the live screen of a dot. The picture is the
// dot's private browser. Cookie session covers the stream; state-changing
// calls also need the CSRF token, because they sit under /api/.
func (g *Gateway) registerDotScreen(api *http.ServeMux) {
	api.HandleFunc("GET /api/dots/{dot}/screen", g.dotRoute(g.handleScreenStatus))
	api.HandleFunc("POST /api/dots/{dot}/screen", g.dotRoute(g.handleScreenStart))
	api.HandleFunc("DELETE /api/dots/{dot}/screen", g.dotRoute(g.handleScreenStop))
	api.HandleFunc("GET /api/dots/{dot}/screen/frame", g.dotRoute(g.handleScreenFrame))
	api.HandleFunc("GET /api/dots/{dot}/screen/stream", g.dotRoute(g.handleScreenStream))
	api.HandleFunc("POST /api/dots/{dot}/screen/takeover", g.dotRoute(g.handleScreenTakeover))
	api.HandleFunc("POST /api/dots/{dot}/screen/input", g.dotRoute(g.handleScreenInput))
}

func (g *Gateway) handleScreenStatus(w http.ResponseWriter, r *http.Request, d dots.Dot) {
	writeJSON(w, http.StatusOK, dots.Status(r.Context(), d.Workspace))
}

func (g *Gateway) handleScreenStart(w http.ResponseWriter, r *http.Request, d dots.Dot) {
	if dots.SandboxCapable(r.Context()) {
		dots.BootSandbox(r.Context())
		writeJSON(w, http.StatusOK, dots.Status(r.Context(), d.Workspace))
		return
	}
	if _, err := dots.Ensure(r.Context(), d.Workspace); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, dots.Status(r.Context(), d.Workspace))
}

func (g *Gateway) handleScreenStop(w http.ResponseWriter, r *http.Request, d dots.Dot) {
	if err := dots.Stop(d.Workspace); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, dots.Status(r.Context(), d.Workspace))
}

func (g *Gateway) handleScreenTakeover(w http.ResponseWriter, r *http.Request, d dots.Dot) {
	var body struct {
		On bool `json:"on"`
	}
	if !decodeJSON(w, r, maxJSONBody, &body) {
		return
	}
	if _, err := dots.SetTakenOver(d.Workspace, body.On); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, dots.Status(r.Context(), d.Workspace))
}

func (g *Gateway) handleScreenInput(w http.ResponseWriter, r *http.Request, d dots.Dot) {
	var body struct {
		Kind   string   `json:"kind"`
		X      int      `json:"x"`
		Y      int      `json:"y"`
		EndX   int      `json:"end_x"`
		EndY   int      `json:"end_y"`
		Text   string   `json:"text"`
		Key    string   `json:"key"`
		Mods   []string `json:"mods"`
		Button string   `json:"button"`
	}
	if !decodeJSON(w, r, maxJSONBody, &body) {
		return
	}
	err := dots.Act(r.Context(), d.Workspace, dots.Pointer{
		Kind: body.Kind, X: body.X, Y: body.Y, DX: body.EndX, DY: body.EndY,
		Text: body.Text, Key: body.Key, Mods: body.Mods, Button: body.Button,
	}, true)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (g *Gateway) handleScreenFrame(w http.ResponseWriter, r *http.Request, d dots.Dot) {
	png, err := dots.Capture(r.Context(), d.Workspace)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(png)
}

func (g *Gateway) handleScreenStream(w http.ResponseWriter, r *http.Request, d dots.Dot) {
	status := dots.Status(r.Context(), d.Workspace)
	if status.Isolation != dots.IsolationSandbox {
		if _, err := dots.Ensure(r.Context(), d.Workspace); err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "this server cannot stream the screen")
		return
	}
	w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=frame")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	tick := time.NewTicker(400 * time.Millisecond)
	defer tick.Stop()
	for {
		png, err := dots.Capture(r.Context(), d.Workspace)
		if err != nil {
			st := dots.Status(r.Context(), d.Workspace)
			if st.Starting || (st.Running && st.Isolation == dots.IsolationSandbox) {
				select {
				case <-r.Context().Done():
					return
				case <-tick.C:
					continue
				}
			}
			return
		}
		if _, err := fmt.Fprintf(w, "--frame\r\nContent-Type: image/png\r\nContent-Length: %d\r\n\r\n", len(png)); err != nil {
			return
		}
		if _, err := w.Write(png); err != nil {
			return
		}
		if _, err := w.Write([]byte("\r\n")); err != nil {
			return
		}
		flusher.Flush()
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
		}
	}
}
