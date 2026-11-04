package webui

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/dots"
)

// maxFullAccess bounds a timed full access grant.
const maxFullAccess = 30 * 24 * time.Hour

type dotsList struct {
	Dots            []dots.Dot `json:"dots"`
	Shapes          []string   `json:"shapes"`
	Colors          []string   `json:"colors"`
	VoiceConfigured bool       `json:"voice_configured"`
}

// registerDots adds the Atlas Dots API. It is a no-op when the gateway
// was built without a dots store.
func (g *Gateway) registerDots(api *http.ServeMux) {
	if g.opts.Dots == nil {
		return
	}
	api.HandleFunc("GET /api/dots", g.handleListDots)
	api.HandleFunc("POST /api/dots", g.handleCreateDot)
	api.HandleFunc("GET /api/dots/voices", g.handleListVoices)
	api.HandleFunc("POST /api/dots/speak", g.handleSpeak)
	api.HandleFunc("GET /api/dots/{dot}", g.handleGetDot)
	api.HandleFunc("PATCH /api/dots/{dot}", g.handleUpdateDot)
	api.HandleFunc("DELETE /api/dots/{dot}", g.handleDeleteDot)
	api.HandleFunc("POST /api/dots/{dot}/full-access", g.handleGrantFullAccess)
	api.HandleFunc("DELETE /api/dots/{dot}/full-access", g.handleRevokeFullAccess)
	g.registerDotScreen(api)
	if err := g.opts.Dots.WriteMarkers(); err != nil {
		g.logger.Warn("Failed to write dot workspace markers", "error", err)
	}
}

func (g *Gateway) expireDots() {
	if err := g.opts.Dots.ExpireFullAccess(); err != nil {
		g.logger.Warn("Failed to expire dot full access", "error", err)
	}
}

func (g *Gateway) handleListDots(w http.ResponseWriter, _ *http.Request) {
	g.expireDots()
	list := g.opts.Dots.List()
	if list == nil {
		list = []dots.Dot{}
	}
	writeJSON(w, http.StatusOK, dotsList{
		Dots:            list,
		Shapes:          dots.Shapes,
		Colors:          dots.Colors,
		VoiceConfigured: g.opts.Voices != nil && g.opts.Voices.Configured(),
	})
}

func (g *Gateway) handleCreateDot(w http.ResponseWriter, r *http.Request) {
	var in dots.Input
	if !decodeJSON(w, r, maxJSONBody, &in) {
		return
	}
	d, err := g.opts.Dots.Create(in)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, d)
}

func (g *Gateway) handleGetDot(w http.ResponseWriter, r *http.Request) {
	g.expireDots()
	d, err := g.opts.Dots.Get(r.PathValue("dot"))
	if err != nil {
		writeDotError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (g *Gateway) handleUpdateDot(w http.ResponseWriter, r *http.Request) {
	var in dots.Input
	if !decodeJSON(w, r, maxJSONBody, &in) {
		return
	}
	d, err := g.opts.Dots.Update(r.PathValue("dot"), in)
	if err != nil {
		writeDotError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (g *Gateway) handleDeleteDot(w http.ResponseWriter, r *http.Request) {
	if err := g.opts.Dots.Delete(r.PathValue("dot")); err != nil {
		writeDotError(w, err)
		return
	}
	if g.opts.Automation != nil {
		if err := g.opts.Automation.DeleteDot(r.PathValue("dot")); err != nil {
			g.logger.Warn("Failed to clear dot automation", "error", err)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleGrantFullAccess turns on full access. The request must repeat the
// dot's name, which the client asks the user to type after reading the
// warning, so a stray request cannot grant it.
func (g *Gateway) handleGrantFullAccess(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Minutes     int    `json:"minutes"`
		Permanent   bool   `json:"permanent"`
		ConfirmName string `json:"confirm_name"`
		Accepted    bool   `json:"accepted"`
	}
	if !decodeJSON(w, r, maxJSONBody, &body) {
		return
	}
	d, err := g.opts.Dots.Get(r.PathValue("dot"))
	if err != nil {
		writeDotError(w, err)
		return
	}
	if !body.Accepted || !strings.EqualFold(strings.TrimSpace(body.ConfirmName), d.Name) {
		writeError(w, http.StatusBadRequest, "accept the warning and type the dot's name to turn on full access")
		return
	}
	dur := time.Duration(body.Minutes) * time.Minute
	if !body.Permanent && (dur <= 0 || dur > maxFullAccess) {
		writeError(w, http.StatusBadRequest, "choose a duration between 1 minute and 30 days, or permanent")
		return
	}
	d, err = g.opts.Dots.SetFullAccess(d.ID, dur, body.Permanent)
	if err != nil {
		writeDotError(w, err)
		return
	}
	g.logger.Info("Dot full access granted", "dot", d.ID, "permanent", body.Permanent, "minutes", body.Minutes)
	writeJSON(w, http.StatusOK, d)
}

func (g *Gateway) handleRevokeFullAccess(w http.ResponseWriter, r *http.Request) {
	d, err := g.opts.Dots.SetFullAccess(r.PathValue("dot"), 0, false)
	if err != nil {
		writeDotError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (g *Gateway) handleListVoices(w http.ResponseWriter, r *http.Request) {
	if g.opts.Voices == nil {
		writeError(w, http.StatusServiceUnavailable, dots.ErrNoVoiceKey.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), upstreamTimeout)
	defer cancel()
	list, err := g.opts.Voices.List(ctx)
	if err != nil {
		writeVoiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// handleSpeak streams text-to-speech audio for a voice, so the client can
// play a dot's reply and drive its mouth from the audio itself.
func (g *Gateway) handleSpeak(w http.ResponseWriter, r *http.Request) {
	if g.opts.Voices == nil {
		writeError(w, http.StatusServiceUnavailable, dots.ErrNoVoiceKey.Error())
		return
	}
	var body struct {
		VoiceID string `json:"voice_id"`
		Text    string `json:"text"`
	}
	if !decodeJSON(w, r, maxJSONBody, &body) {
		return
	}
	text := strings.TrimSpace(body.Text)
	if text == "" {
		writeError(w, http.StatusBadRequest, "nothing to say")
		return
	}
	if utf8.RuneCountInString(text) > dots.MaxSpeakRunes {
		text = truncateRunes(text, dots.MaxSpeakRunes)
	}
	audio, err := g.opts.Voices.Speak(r.Context(), body.VoiceID, text)
	if err != nil {
		writeVoiceError(w, err)
		return
	}
	defer audio.Close()
	w.Header().Set("Content-Type", "audio/mpeg")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(flushWriter{w}, audio); err != nil && !errors.Is(err, context.Canceled) {
		slog.Debug("Speech stream ended early", "error", err)
	}
}

// flushWriter flushes after every write so audio starts playing before
// the whole reply is synthesized.
type flushWriter struct{ w http.ResponseWriter }

func (f flushWriter) Write(p []byte) (int, error) {
	n, err := f.w.Write(p)
	if fl, ok := f.w.(http.Flusher); ok {
		fl.Flush()
	}
	return n, err
}

func writeDotError(w http.ResponseWriter, err error) {
	if errors.Is(err, dots.ErrNotFound) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeError(w, http.StatusBadRequest, err.Error())
}

func writeVoiceError(w http.ResponseWriter, err error) {
	if errors.Is(err, dots.ErrNoVoiceKey) {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeError(w, http.StatusBadGateway, err.Error())
}
