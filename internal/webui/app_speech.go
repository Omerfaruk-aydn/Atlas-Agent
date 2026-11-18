package webui

import (
	"context"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/speech"
)

func speechOptions(r *http.Request) speech.DictationOptions {
	return speech.DictationOptions{Language: r.URL.Query().Get("language")}
}

func (g *Gateway) handleSpeechStatus(w http.ResponseWriter, r *http.Request, _ *hub) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	status, err := speech.Check(ctx, speechOptions(r))
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

// handleSpeechTranscribe accepts bounded WAV audio for local recognition.
// Temporary audio is removed on success, failure and request cancellation.
func (g *Gateway) handleSpeechTranscribe(w http.ResponseWriter, r *http.Request, _ *hub) {
	opts := speechOptions(r)
	if err := opts.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, speech.MaxAudioBytes)
	file, err := os.CreateTemp("", "atlas-dictation-*.wav")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create temporary audio")
		return
	}
	defer os.Remove(file.Name())
	_, copyErr := io.Copy(file, r.Body)
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		writeError(w, http.StatusBadRequest, "could not read audio; maximum size is 24 MB")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	text, err := speech.Transcribe(ctx, file.Name(), opts)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"text": text})
}
