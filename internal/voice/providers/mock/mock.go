// Package mock is a deterministic voice provider for tests. Its recognizer
// treats incoming audio bytes as text: a frame "say:hello" produces a
// partial and a final transcript "hello". Its synthesizer emits the text
// itself as audio bytes, so a test can read what the caller would hear.
package mock

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/voice"
)

// Provider is a mock provider with both capabilities.
type Provider struct {
	Recognizer  *Recognizer
	Synthesizer *Synthesizer
}

// New builds a mock provider.
func New() *Provider {
	return &Provider{Recognizer: &Recognizer{}, Synthesizer: &Synthesizer{}}
}

// Voice returns the provider as a voice.Provider.
func (p *Provider) Voice() voice.Provider {
	return voice.Provider{Name: "mock", Recognizer: p.Recognizer, Synthesizer: p.Synthesizer}
}

// Recognizer is the mock speech-to-text capability.
type Recognizer struct {
	// FailStarts makes the next n StartRecognition calls fail.
	FailStarts atomic.Int32
	// Encodings overrides InputEncodings.
	Encodings []voice.Encoding

	mu      sync.Mutex
	streams []*Stream
	opened  atomic.Int32
}

// InputEncodings implements voice.Recognizer.
func (r *Recognizer) InputEncodings() []voice.Encoding {
	if len(r.Encodings) > 0 {
		return r.Encodings
	}
	return []voice.Encoding{voice.EncodingMulaw8k}
}

// StartRecognition implements voice.Recognizer.
func (r *Recognizer) StartRecognition(ctx context.Context, cfg voice.RecognitionConfig) (voice.RecognitionStream, error) {
	if r.FailStarts.Load() > 0 {
		r.FailStarts.Add(-1)
		return nil, voice.Errorf(voice.CodeProviderUnavailable, "mock stt", errors.New("start failed"))
	}
	s := &Stream{events: make(chan voice.Event, 64), done: make(chan struct{}), cfg: cfg}
	r.mu.Lock()
	r.streams = append(r.streams, s)
	r.mu.Unlock()
	r.opened.Add(1)
	return s, nil
}

// Opened is the number of streams started.
func (r *Recognizer) Opened() int { return int(r.opened.Load()) }

// Latest returns the most recently opened stream.
func (r *Recognizer) Latest() *Stream {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.streams) == 0 {
		return nil
	}
	return r.streams[len(r.streams)-1]
}

// OpenStreams counts streams not yet closed.
func (r *Recognizer) OpenStreams() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, s := range r.streams {
		if !s.Closed() {
			n++
		}
	}
	return n
}

// Stream is one mock recognition session.
type Stream struct {
	cfg    voice.RecognitionConfig
	events chan voice.Event
	done   chan struct{}
	once   sync.Once
	closed atomic.Bool
	pushMu sync.Mutex
	mu     sync.Mutex
	audio  [][]byte
}

// SendAudio implements voice.RecognitionStream. Frames starting with
// "say:" become transcripts; "drop" simulates a dropped connection.
func (s *Stream) SendAudio(_ context.Context, f voice.AudioFrame) error {
	if s.closed.Load() {
		return voice.ErrCallEnded
	}
	s.mu.Lock()
	s.audio = append(s.audio, append([]byte(nil), f.Data...))
	s.mu.Unlock()
	text := string(f.Data)
	switch {
	case strings.HasPrefix(text, "say:"):
		said := strings.TrimPrefix(text, "say:")
		s.Push(voice.Event{Type: voice.EventSpeechStarted})
		s.Push(voice.Event{Type: voice.EventTranscriptPartial, Text: said})
		s.Push(voice.Event{Type: voice.EventTranscriptFinal, Text: said, Final: true})
	case strings.HasPrefix(text, "partial:"):
		s.Push(voice.Event{Type: voice.EventTranscriptPartial, Text: strings.TrimPrefix(text, "partial:")})
	case text == "drop":
		s.Push(voice.Event{Type: voice.EventProviderError, Code: voice.CodeProviderUnavailable, Retryable: true})
	case text == "fatal":
		s.Push(voice.Event{Type: voice.EventProviderError, Code: voice.CodeProviderAuth})
	}
	return nil
}

// Push delivers an event as if the provider sent it.
func (s *Stream) Push(ev voice.Event) {
	ev.At = time.Now()
	s.pushMu.Lock()
	defer s.pushMu.Unlock()
	if s.closed.Load() {
		return
	}
	select {
	case s.events <- ev:
	case <-time.After(time.Second):
	}
}

// Events implements voice.RecognitionStream.
func (s *Stream) Events() <-chan voice.Event { return s.events }

// Close implements voice.RecognitionStream.
func (s *Stream) Close() error {
	s.once.Do(func() {
		s.closed.Store(true)
		close(s.done)
		s.pushMu.Lock()
		close(s.events)
		s.pushMu.Unlock()
	})
	return nil
}

// Closed reports whether Close was called.
func (s *Stream) Closed() bool { return s.closed.Load() }

// Config returns the recognition config the stream was opened with.
func (s *Stream) Config() voice.RecognitionConfig { return s.cfg }

// Synthesizer is the mock text-to-speech capability.
type Synthesizer struct {
	// ChunkDelay is waited between emitted chunks, in nanoseconds.
	ChunkDelay atomic.Int64
	// ChunkSize is the bytes per emitted frame; 4 when zero.
	ChunkSize int
	// Fail makes every call fail.
	Fail atomic.Bool
	// Encodings overrides OutputEncodings.
	Encodings []voice.Encoding

	mu     sync.Mutex
	spoken []string
}

// OutputEncodings implements voice.Synthesizer.
func (s *Synthesizer) OutputEncodings() []voice.Encoding {
	if len(s.Encodings) > 0 {
		return s.Encodings
	}
	return []voice.Encoding{voice.EncodingMulaw8k}
}

// Synthesize implements voice.Synthesizer: the text bytes are the audio.
func (s *Synthesizer) Synthesize(ctx context.Context, req voice.SynthesisRequest, emit func(voice.AudioFrame) error) error {
	if s.Fail.Load() {
		return voice.Errorf(voice.CodeProviderUnavailable, "mock tts", errors.New("synthesis failed"))
	}
	s.mu.Lock()
	s.spoken = append(s.spoken, req.Text)
	s.mu.Unlock()
	size := s.ChunkSize
	if size <= 0 {
		size = 4
	}
	data := []byte(req.Text)
	enc := req.Encoding
	if enc == "" {
		enc = voice.EncodingMulaw8k
	}
	for len(data) > 0 {
		n := min(size, len(data))
		if enc.BytesPerSample() == 2 && n%2 == 1 {
			if n < len(data) {
				n++
			} else {
				data = append(data, ' ')
				n++
			}
		}
		if err := emit(voice.AudioFrame{Encoding: enc, Data: append([]byte(nil), data[:n]...), TurnID: req.TurnID}); err != nil {
			return err
		}
		data = data[n:]
		if d := time.Duration(s.ChunkDelay.Load()); d > 0 && len(data) > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(d):
			}
		}
	}
	return nil
}

// Spoken returns every segment synthesized so far.
func (s *Synthesizer) Spoken() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.spoken...)
}

// Leg is an in-memory call leg.
type Leg struct {
	Enc   voice.Encoding
	audio chan voice.AudioFrame
	marks chan string
	done  chan struct{}
	once  sync.Once

	mu      sync.Mutex
	sent    []voice.AudioFrame
	clears  int
	hangups int
	// AutoMark echoes every mark back immediately.
	AutoMark bool
}

// NewLeg builds a leg that echoes marks.
func NewLeg() *Leg {
	return &Leg{Enc: voice.EncodingMulaw8k, audio: make(chan voice.AudioFrame, 64), marks: make(chan string, 64), done: make(chan struct{}), AutoMark: true}
}

// Say queues caller audio "say:<text>".
func (l *Leg) Say(text string) {
	l.audio <- voice.AudioFrame{Encoding: l.Enc, Data: []byte("say:" + text)}
}

// Raw queues raw caller audio.
func (l *Leg) Raw(data string) { l.audio <- voice.AudioFrame{Encoding: l.Enc, Data: []byte(data)} }

// CallerHangup simulates the caller hanging up.
func (l *Leg) CallerHangup() { l.once.Do(func() { close(l.done) }) }

// Encoding implements voice.CallLeg.
func (l *Leg) Encoding() voice.Encoding { return l.Enc }

// Audio implements voice.CallLeg.
func (l *Leg) Audio() <-chan voice.AudioFrame { return l.audio }

// Send implements voice.CallLeg.
func (l *Leg) Send(_ context.Context, f voice.AudioFrame) error {
	select {
	case <-l.done:
		return voice.ErrCallEnded
	default:
	}
	l.mu.Lock()
	l.sent = append(l.sent, f)
	l.mu.Unlock()
	return nil
}

// Clear implements voice.CallLeg.
func (l *Leg) Clear(context.Context) error {
	l.mu.Lock()
	l.clears++
	l.mu.Unlock()
	return nil
}

// Mark implements voice.CallLeg.
func (l *Leg) Mark(_ context.Context, name string) error {
	if l.AutoMark {
		select {
		case l.marks <- name:
		default:
		}
	}
	return nil
}

// Marks implements voice.CallLeg.
func (l *Leg) Marks() <-chan string { return l.marks }

// Hangup implements voice.CallLeg.
func (l *Leg) Hangup(context.Context) error {
	l.mu.Lock()
	l.hangups++
	l.mu.Unlock()
	l.once.Do(func() { close(l.done) })
	return nil
}

// Done implements voice.CallLeg.
func (l *Leg) Done() <-chan struct{} { return l.done }

// Heard returns the concatenated audio sent to the caller.
func (l *Leg) Heard() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var b strings.Builder
	for _, f := range l.sent {
		b.Write(f.Data)
	}
	return b.String()
}

// SentFrames returns every frame sent to the caller.
func (l *Leg) SentFrames() []voice.AudioFrame {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]voice.AudioFrame(nil), l.sent...)
}

// Clears is how often playback was cleared.
func (l *Leg) Clears() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.clears
}

// Hangups is how often Hangup was called.
func (l *Leg) Hangups() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.hangups
}
