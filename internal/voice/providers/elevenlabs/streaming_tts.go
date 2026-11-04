package elevenlabs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/voice"
)

const (
	maxSegmentRunes = 2000
	// ttsChunkMillis is the audio duration of one emitted frame.
	ttsChunkMillis = 80
)

// Synthesizer is the streaming text-to-speech capability.
type Synthesizer struct{ opts Options }

// OutputEncodings implements voice.Synthesizer. mu-law 8 kHz is first so a
// phone call needs no transcoding.
func (s *Synthesizer) OutputEncodings() []voice.Encoding {
	return []voice.Encoding{voice.EncodingMulaw8k, voice.EncodingPCM16k, voice.EncodingPCM24k}
}

type ttsRequest struct {
	Text         string `json:"text"`
	ModelID      string `json:"model_id"`
	LanguageCode string `json:"language_code,omitempty"`
}

// Synthesize streams one segment. Audio is emitted as it arrives, in
// sample-aligned chunks, so playback starts before synthesis finishes.
func (s *Synthesizer) Synthesize(ctx context.Context, req voice.SynthesisRequest, emit func(voice.AudioFrame) error) error {
	text := strings.TrimSpace(req.Text)
	if text == "" {
		return nil
	}
	if utf8.RuneCountInString(text) > maxSegmentRunes {
		return voice.Errorf(voice.CodeInvalidInput, "elevenlabs tts", errors.New("segment too long"))
	}
	if s.opts.VoiceID == "" {
		return voice.Errorf(voice.CodeNotConfigured, "elevenlabs tts", errors.New("voice_id is not set"))
	}
	enc := req.Encoding
	if enc == "" {
		enc = voice.EncodingMulaw8k
	}
	supported := false
	for _, e := range s.OutputEncodings() {
		supported = supported || e == enc
	}
	if !supported {
		return voice.Errorf(voice.CodeInvalidInput, "elevenlabs tts", fmt.Errorf("unsupported output encoding %q", enc))
	}

	body := ttsRequest{Text: text, ModelID: s.opts.TTSModel}
	if strings.Contains(s.opts.TTSModel, "v2_5") || strings.Contains(s.opts.TTSModel, "v3") {
		body.LanguageCode = languageCode(req.Language)
	}
	payload, _ := json.Marshal(body)
	endpoint := fmt.Sprintf("%s/v1/text-to-speech/%s/stream?output_format=%s", s.opts.BaseURL, url.PathEscape(s.opts.VoiceID), url.QueryEscape(string(enc)))
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(payload)))
	if err != nil {
		return voice.Errorf(voice.CodeNotConfigured, "elevenlabs tts", err)
	}
	hreq.Header.Set("xi-api-key", s.opts.APIKey.Reveal())
	hreq.Header.Set("Content-Type", "application/json")
	resp, err := s.opts.HTTP.Do(hreq)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return voice.Errorf(voice.CodeProviderUnavailable, "elevenlabs tts", errors.New("request failed"))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return voice.Errorf(codeForStatus(resp.StatusCode), "elevenlabs tts", fmt.Errorf("status %d", resp.StatusCode))
	}

	chunk := enc.SampleRate() * enc.BytesPerSample() * ttsChunkMillis / 1000
	buf := make([]byte, chunk)
	pending := 0
	for {
		n, err := resp.Body.Read(buf[pending:])
		pending += n
		if pending == chunk || (err != nil && pending > 0) {
			usable := pending - pending%enc.BytesPerSample()
			if usable > 0 {
				data := make([]byte, usable)
				copy(data, buf[:usable])
				if e := emit(voice.AudioFrame{Encoding: enc, Data: data, TurnID: req.TurnID}); e != nil {
					return e
				}
			}
			copy(buf, buf[usable:pending])
			pending -= usable
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return voice.Errorf(voice.CodeProviderUnavailable, "elevenlabs tts", errors.New("stream interrupted"))
		}
	}
}
