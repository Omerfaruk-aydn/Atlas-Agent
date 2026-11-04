package dots

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrNoVoiceKey reports that no ElevenLabs credential is configured.
var ErrNoVoiceKey = errors.New("ElevenLabs is not connected; sign in from the dot's voice settings")

// Voice is one ElevenLabs voice a dot can use.
type Voice struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Category    string `json:"category,omitempty"`
	Description string `json:"description,omitempty"`
	Gender      string `json:"gender,omitempty"`
	Accent      string `json:"accent,omitempty"`
	PreviewURL  string `json:"preview_url,omitempty"`
}

// Voices talks to ElevenLabs with the user's API key, or with the
// access token from the voice settings sign-in.
type Voices struct {
	// Key returns the API key or access token, or "" when neither is set.
	Key     func() string
	BaseURL string
	HTTP    *http.Client
	// Model is the text-to-speech model; empty uses a low-latency default.
	Model string
}

// MaxSpeakRunes bounds the text sent to text-to-speech in one request.
const MaxSpeakRunes = 2500

func (v *Voices) base() string {
	if v.BaseURL != "" {
		return strings.TrimRight(v.BaseURL, "/")
	}
	return "https://api.elevenlabs.io"
}

func (v *Voices) client() *http.Client {
	if v.HTTP != nil {
		return v.HTTP
	}
	return &http.Client{Timeout: 60 * time.Second}
}

func (v *Voices) key() (string, error) {
	if v.Key == nil {
		return "", ErrNoVoiceKey
	}
	k := strings.TrimSpace(v.Key())
	if k == "" {
		return "", ErrNoVoiceKey
	}
	return k, nil
}

// Configured reports whether an API key is available.
func (v *Voices) Configured() bool {
	_, err := v.key()
	return err == nil
}

// List returns the voices in the user's ElevenLabs library.
func (v *Voices) List(ctx context.Context) ([]Voice, error) {
	key, err := v.key()
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.base()+"/v1/voices", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("xi-api-key", key)
	rsp, err := v.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("reach ElevenLabs: %w", err)
	}
	defer rsp.Body.Close()
	if rsp.StatusCode != http.StatusOK {
		return nil, upstreamError(rsp)
	}
	var body struct {
		Voices []struct {
			VoiceID     string            `json:"voice_id"`
			Name        string            `json:"name"`
			Category    string            `json:"category"`
			Description string            `json:"description"`
			PreviewURL  string            `json:"preview_url"`
			Labels      map[string]string `json:"labels"`
		} `json:"voices"`
	}
	if err := json.NewDecoder(io.LimitReader(rsp.Body, 8<<20)).Decode(&body); err != nil {
		return nil, fmt.Errorf("read ElevenLabs voices: %w", err)
	}
	out := make([]Voice, 0, len(body.Voices))
	for _, x := range body.Voices {
		out = append(out, Voice{
			ID:          x.VoiceID,
			Name:        x.Name,
			Category:    x.Category,
			Description: firstNonEmpty(x.Description, x.Labels["description"], x.Labels["descriptive"]),
			Gender:      x.Labels["gender"],
			Accent:      x.Labels["accent"],
			PreviewURL:  x.PreviewURL,
		})
	}
	return out, nil
}

// Speak streams MP3 audio of text in voiceID. The caller closes the
// returned reader.
func (v *Voices) Speak(ctx context.Context, voiceID, text string) (io.ReadCloser, error) {
	key, err := v.key()
	if err != nil {
		return nil, err
	}
	if voiceID == "" {
		return nil, errors.New("this dot has no voice yet")
	}
	model := v.Model
	if model == "" {
		model = "eleven_flash_v2_5"
	}
	payload, err := json.Marshal(map[string]any{
		"text":     text,
		"model_id": model,
		"voice_settings": map[string]any{
			"stability":        0.45,
			"similarity_boost": 0.8,
		},
	})
	if err != nil {
		return nil, err
	}
	u := v.base() + "/v1/text-to-speech/" + url.PathEscape(voiceID) + "/stream?output_format=mp3_44100_128"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(string(payload)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("xi-api-key", key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "audio/mpeg")
	rsp, err := v.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("reach ElevenLabs: %w", err)
	}
	if rsp.StatusCode != http.StatusOK {
		defer rsp.Body.Close()
		return nil, upstreamError(rsp)
	}
	return rsp.Body, nil
}

func upstreamError(rsp *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(rsp.Body, 4<<10))
	var body struct {
		Detail any `json:"detail"`
	}
	msg := ""
	if json.Unmarshal(raw, &body) == nil {
		switch d := body.Detail.(type) {
		case string:
			msg = d
		case map[string]any:
			if m, ok := d["message"].(string); ok {
				msg = m
			}
		}
	}
	if msg == "" {
		msg = http.StatusText(rsp.StatusCode)
	}
	return fmt.Errorf("ElevenLabs: %s", msg)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
