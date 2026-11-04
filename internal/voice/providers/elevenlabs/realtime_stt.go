package elevenlabs

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/voice"
	"github.com/gorilla/websocket"
)

const (
	sttPath         = "/v1/speech-to-text/realtime"
	sttReadLimit    = 1 << 20
	sttWriteTimeout = 10 * time.Second
	sttDialTimeout  = 15 * time.Second
	sttEventBuffer  = 64
)

// Recognizer is the Scribe Realtime speech-to-text capability.
type Recognizer struct{ opts Options }

// InputEncodings implements voice.Recognizer. Telephone mu-law is sent as
// is, so no transcoding happens on a phone call.
func (r *Recognizer) InputEncodings() []voice.Encoding {
	return []voice.Encoding{voice.EncodingMulaw8k, voice.EncodingPCM16k, voice.EncodingPCM8k, voice.EncodingPCM24k}
}

// StartRecognition opens a realtime session. Transcripts are committed by
// the service's voice activity detection; only committed text becomes a
// final transcript.
func (r *Recognizer) StartRecognition(ctx context.Context, cfg voice.RecognitionConfig) (voice.RecognitionStream, error) {
	if !cfg.Encoding.Valid() {
		return nil, voice.Errorf(voice.CodeInvalidInput, "elevenlabs stt", fmt.Errorf("unsupported encoding %q", cfg.Encoding))
	}
	u, err := url.Parse(r.opts.StreamURL + sttPath)
	if err != nil {
		return nil, voice.Errorf(voice.CodeNotConfigured, "elevenlabs stt", err)
	}
	q := u.Query()
	q.Set("model_id", r.opts.STTModel)
	q.Set("audio_format", string(cfg.Encoding))
	q.Set("commit_strategy", "vad")
	if cfg.Language != "" {
		q.Set("language_code", languageCode(cfg.Language))
	}
	for _, k := range cfg.Keyterms {
		if k = strings.TrimSpace(k); k != "" {
			q.Add("keyterms", k)
		}
	}
	u.RawQuery = q.Encode()

	dialer := websocket.Dialer{HandshakeTimeout: sttDialTimeout, Proxy: http.ProxyFromEnvironment}
	header := http.Header{"xi-api-key": []string{r.opts.APIKey.Reveal()}}
	dctx, cancel := context.WithTimeout(ctx, sttDialTimeout)
	defer cancel()
	conn, resp, err := dialer.DialContext(dctx, u.String(), header)
	if err != nil {
		code := voice.CodeProviderUnavailable
		if resp != nil {
			code = codeForStatus(resp.StatusCode)
			resp.Body.Close()
		}
		// The dial error may echo the URL; report only the code.
		return nil, voice.Errorf(code, "elevenlabs stt", errors.New("connection failed"))
	}
	conn.SetReadLimit(sttReadLimit)
	s := &sttStream{conn: conn, events: make(chan voice.Event, sttEventBuffer), done: make(chan struct{}), rate: cfg.Encoding.SampleRate()}
	go s.readLoop()
	return s, nil
}

type sttStream struct {
	conn   *websocket.Conn
	rate   int
	events chan voice.Event
	done   chan struct{}

	writeMu sync.Mutex
	once    sync.Once
	closed  bool

	inSpeech bool
}

type sttAudioMessage struct {
	MessageType string `json:"message_type"`
	Audio       string `json:"audio_base_64"`
	Commit      bool   `json:"commit"`
	SampleRate  int    `json:"sample_rate"`
}

type sttServerMessage struct {
	MessageType string `json:"message_type"`
	Text        string `json:"text"`
	Error       string `json:"error"`
}

func (s *sttStream) SendAudio(ctx context.Context, f voice.AudioFrame) error {
	if err := f.Validate(); err != nil {
		return voice.Errorf(voice.CodeInvalidInput, "elevenlabs stt", err)
	}
	msg := sttAudioMessage{MessageType: "input_audio_chunk", Audio: base64.StdEncoding.EncodeToString(f.Data), SampleRate: s.rate}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if s.closed {
		return voice.ErrCallEnded
	}
	deadline := time.Now().Add(sttWriteTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = s.conn.SetWriteDeadline(deadline)
	if err := s.conn.WriteJSON(msg); err != nil {
		return voice.Errorf(voice.CodeProviderUnavailable, "elevenlabs stt", errors.New("write failed"))
	}
	return nil
}

func (s *sttStream) Events() <-chan voice.Event { return s.events }

func (s *sttStream) Close() error {
	s.once.Do(func() {
		// Mark the stream done first so the read loop treats the closed
		// connection as an orderly end, not a dropped one.
		close(s.done)
		s.writeMu.Lock()
		s.closed = true
		_ = s.conn.SetWriteDeadline(time.Now().Add(time.Second))
		_ = s.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		s.writeMu.Unlock()
		_ = s.conn.Close()
	})
	return nil
}

func (s *sttStream) emit(ev voice.Event) bool {
	ev.At = time.Now()
	select {
	case s.events <- ev:
		return true
	case <-s.done:
		return false
	}
}

func (s *sttStream) readLoop() {
	defer close(s.events)
	for {
		_, data, err := s.conn.ReadMessage()
		if err != nil {
			select {
			case <-s.done:
			default:
				// An unexpected drop is retryable; the gateway reconnects.
				s.emit(voice.Event{Type: voice.EventProviderError, Code: voice.CodeProviderUnavailable, Retryable: true})
			}
			return
		}
		var msg sttServerMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		if !s.handle(msg) {
			return
		}
	}
}

// handle maps one server message to events. It returns false once the
// stream should stop.
func (s *sttStream) handle(msg sttServerMessage) bool {
	switch msg.MessageType {
	case "session_started":
		return true
	case "partial_transcript":
		text := strings.TrimSpace(msg.Text)
		if text == "" {
			return true
		}
		if !s.inSpeech {
			s.inSpeech = true
			if !s.emit(voice.Event{Type: voice.EventSpeechStarted}) {
				return false
			}
		}
		return s.emit(voice.Event{Type: voice.EventTranscriptPartial, Text: text})
	case "committed_transcript", "committed_transcript_with_timestamps":
		if msg.MessageType == "committed_transcript_with_timestamps" {
			// The same text also arrives as committed_transcript.
			return true
		}
		s.inSpeech = false
		text := strings.TrimSpace(msg.Text)
		if !s.emit(voice.Event{Type: voice.EventSpeechEnded}) {
			return false
		}
		if text == "" {
			return true
		}
		return s.emit(voice.Event{Type: voice.EventTranscriptFinal, Text: text, Final: true})
	}
	if code, retryable, ok := classifyServerError(msg.MessageType); ok {
		s.emit(voice.Event{Type: voice.EventProviderError, Code: code, Retryable: retryable})
		return false
	}
	return true
}

// classifyServerError recognizes error message types. Authentication,
// quota and input errors are permanent for the call; anything else may be
// retried by reconnecting.
func classifyServerError(t string) (voice.Code, bool, bool) {
	switch {
	case strings.Contains(t, "auth"):
		return voice.CodeProviderAuth, false, true
	case strings.Contains(t, "quota") || strings.Contains(t, "insufficient"):
		return voice.CodeProviderQuota, false, true
	case strings.Contains(t, "input") || strings.Contains(t, "unaccepted") || strings.Contains(t, "invalid"):
		return voice.CodeProviderProtocol, false, true
	case strings.Contains(t, "error") || strings.Contains(t, "limit") || strings.Contains(t, "exceeded") || strings.Contains(t, "throttl"):
		return voice.CodeProviderUnavailable, true, true
	}
	return "", false, false
}
