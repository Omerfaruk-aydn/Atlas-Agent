// Package elevenlabs implements voice recognition and synthesis on the
// ElevenLabs APIs: Scribe Realtime over a WebSocket for speech-to-text and
// the streaming text-to-speech endpoint for replies. ElevenLabs is only a
// speech provider here; the conversation is run by the Atlas agent.
package elevenlabs

import (
	"cmp"
	"net/http"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/voice"
)

const (
	defaultBaseURL   = "https://api.elevenlabs.io"
	defaultStreamURL = "wss://api.elevenlabs.io"
)

// Options configures the provider.
type Options struct {
	APIKey   voice.Secret
	STTModel string
	TTSModel string
	VoiceID  string
	// BaseURL is the REST origin, StreamURL the WebSocket origin. Both
	// default to the public API.
	BaseURL   string
	StreamURL string
	HTTP      *http.Client
}

// New returns the provider with both capabilities.
func New(opts Options) voice.Provider {
	opts.BaseURL = cmp.Or(opts.BaseURL, defaultBaseURL)
	opts.StreamURL = cmp.Or(opts.StreamURL, defaultStreamURL)
	if opts.HTTP == nil {
		// No overall timeout: a synthesis response streams for as long as
		// the sentence lasts. Requests are bounded by their contexts.
		opts.HTTP = &http.Client{Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			ResponseHeaderTimeout: 15 * time.Second,
			IdleConnTimeout:       90 * time.Second,
			MaxIdleConnsPerHost:   4,
		}}
	}
	return voice.Provider{
		Name:        "elevenlabs",
		Recognizer:  &Recognizer{opts: opts},
		Synthesizer: &Synthesizer{opts: opts},
	}
}

// FromConfig builds the provider from configuration, resolving the key.
func FromConfig(cfg voice.ElevenLabsConfig, env func(string) string) (voice.Provider, error) {
	key, err := cfg.APIKey.Resolve(env)
	if err != nil {
		return voice.Provider{}, voice.Errorf(voice.CodeNotConfigured, "elevenlabs", err)
	}
	return New(Options{
		APIKey: key, STTModel: cfg.STTModel, TTSModel: cfg.TTSModel, VoiceID: cfg.VoiceID,
		BaseURL: cfg.BaseURL, StreamURL: cfg.StreamURL,
	}), nil
}

// languageCode turns tr-TR into the ISO 639-1 code the APIs expect.
func languageCode(lang string) string {
	if len(lang) >= 2 {
		return lang[:2]
	}
	return lang
}

// codeForStatus maps an HTTP status to a safe error code.
func codeForStatus(status int) voice.Code {
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return voice.CodeProviderAuth
	case status == http.StatusTooManyRequests || status == http.StatusPaymentRequired:
		return voice.CodeProviderQuota
	case status >= 500:
		return voice.CodeProviderUnavailable
	default:
		return voice.CodeProviderProtocol
	}
}
