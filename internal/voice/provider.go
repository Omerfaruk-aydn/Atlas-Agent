package voice

import (
	"context"
	"fmt"
	"time"
)

// EventType names a voice event.
type EventType string

// Event types shared by every provider and the gateway.
const (
	EventSpeechStarted     EventType = "speech_started"
	EventTranscriptPartial EventType = "transcript_partial"
	EventTranscriptFinal   EventType = "transcript_final"
	EventSpeechEnded       EventType = "speech_ended"
	EventResponseAudio     EventType = "response_audio"
	EventToolCall          EventType = "tool_call"
	EventProviderError     EventType = "provider_error"
	EventCallEnded         EventType = "call_ended"
)

// Event is one thing that happened on a call. Providers fill Type, Text,
// Final and Code; the gateway stamps CallID, TurnID and Seq.
type Event struct {
	Type   EventType `json:"type"`
	CallID string    `json:"call_id,omitempty"`
	TurnID string    `json:"turn_id,omitempty"`
	Seq    uint64    `json:"seq"`
	At     time.Time `json:"at"`
	Text   string    `json:"text,omitempty"`
	Final  bool      `json:"final,omitempty"`
	Code   Code      `json:"code,omitempty"`
	// Retryable marks a provider error after which the stream may be
	// reopened.
	Retryable bool `json:"retryable,omitempty"`
}

// RecognitionConfig configures one streaming recognition session.
type RecognitionConfig struct {
	CallID   string
	Language string
	Encoding Encoding
	// Keyterms biases recognition towards names and jargon.
	Keyterms []string
}

// Recognizer is a streaming speech-to-text capability.
type Recognizer interface {
	// InputEncodings lists the audio formats the recognizer accepts, best
	// first.
	InputEncodings() []Encoding
	StartRecognition(ctx context.Context, cfg RecognitionConfig) (RecognitionStream, error)
}

// RecognitionStream is one open recognition session. Events is closed when
// the stream ends; Close is idempotent and releases every resource.
type RecognitionStream interface {
	SendAudio(ctx context.Context, frame AudioFrame) error
	Events() <-chan Event
	Close() error
}

// SynthesisRequest is one segment of text to speak.
type SynthesisRequest struct {
	Text     string
	Language string
	Encoding Encoding
	TurnID   string
}

// Synthesizer is a streaming text-to-speech capability. Synthesize streams
// audio for one segment through emit as it arrives and returns when the
// segment is done, emit fails or ctx ends.
type Synthesizer interface {
	OutputEncodings() []Encoding
	Synthesize(ctx context.Context, req SynthesisRequest, emit func(AudioFrame) error) error
}

// Provider bundles the capabilities one vendor offers. A cascade provider
// has both a recognizer and a synthesizer; a provider may also supply only
// one of them and be combined with another (see Config.STTProvider).
type Provider struct {
	Name        string
	Recognizer  Recognizer
	Synthesizer Synthesizer
}

// Providers is the set of providers a gateway may use, by name.
type Providers map[string]Provider

// Resolve picks the recognizer and synthesizer the config asks for and
// checks they exist.
func (p Providers) Resolve(cfg Config) (Recognizer, Synthesizer, error) {
	stt, tts := cfg.STTProvider(), cfg.TTSProvider()
	sp, ok := p[stt]
	if !ok || sp.Recognizer == nil {
		return nil, nil, Errorf(CodeNotConfigured, "resolve provider", fmt.Errorf("provider %q offers no speech recognition", stt))
	}
	tp, ok := p[tts]
	if !ok || tp.Synthesizer == nil {
		return nil, nil, Errorf(CodeNotConfigured, "resolve provider", fmt.Errorf("provider %q offers no speech synthesis", tts))
	}
	return sp.Recognizer, tp.Synthesizer, nil
}

// pickEncoding returns prefer when have contains it, and otherwise the
// first entry of have, in which case the caller converts.
func pickEncoding(have []Encoding, prefer Encoding) (Encoding, bool) {
	for _, e := range have {
		if e == prefer {
			return e, true
		}
	}
	if len(have) == 0 {
		return "", false
	}
	return have[0], false
}
