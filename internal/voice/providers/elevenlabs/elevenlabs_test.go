package elevenlabs

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/voice"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func testProvider(base string) voice.Provider {
	return New(Options{
		APIKey: voice.NewSecret("el-key"), STTModel: "scribe_v2_realtime", TTSModel: "eleven_flash_v2_5", VoiceID: "v1",
		BaseURL: base, StreamURL: "ws" + strings.TrimPrefix(base, "http"),
	})
}

type sttServer struct {
	mu     sync.Mutex
	query  map[string][]string
	key    string
	audio  [][]byte
	script func(conn *websocket.Conn)
}

func (s *sttServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != sttPath {
		http.NotFound(w, r)
		return
	}
	s.mu.Lock()
	s.query = r.URL.Query()
	s.key = r.Header.Get("xi-api-key")
	s.mu.Unlock()
	if s.key != "el-key" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	up := websocket.Upgrader{}
	conn, err := up.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	_ = conn.WriteJSON(map[string]string{"message_type": "session_started", "session_id": "s1"})
	go func() {
		for {
			var msg sttAudioMessage
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			data, _ := base64.StdEncoding.DecodeString(msg.Audio)
			s.mu.Lock()
			s.audio = append(s.audio, data)
			s.mu.Unlock()
		}
	}()
	s.script(conn)
}

func collect(t *testing.T, events <-chan voice.Event, n int) []voice.Event {
	t.Helper()
	var out []voice.Event
	timeout := time.After(5 * time.Second)
	for len(out) < n {
		select {
		case ev, ok := <-events:
			if !ok {
				return out
			}
			out = append(out, ev)
		case <-timeout:
			t.Fatalf("got %d of %d events", len(out), n)
		}
	}
	return out
}

func TestRealtimeRecognition(t *testing.T) {
	t.Parallel()
	ready := make(chan struct{})
	srv := &sttServer{script: func(conn *websocket.Conn) {
		<-ready
		for _, m := range []map[string]string{
			{"message_type": "partial_transcript", "text": ""},
			{"message_type": "partial_transcript", "text": "son e"},
			{"message_type": "partial_transcript", "text": "son e-postamı"},
			{"message_type": "committed_transcript", "text": "Son e-postamı özetle."},
			{"message_type": "committed_transcript_with_timestamps", "text": "Son e-postamı özetle."},
			{"message_type": "committed_transcript", "text": "  "},
		} {
			_ = conn.WriteJSON(m)
		}
		time.Sleep(time.Second)
	}}
	hs := httptest.NewServer(srv)
	t.Cleanup(hs.Close)
	rec := testProvider(hs.URL).Recognizer
	stream, err := rec.StartRecognition(context.Background(), voice.RecognitionConfig{Language: "tr-TR", Encoding: voice.EncodingMulaw8k, Keyterms: []string{"ATLAS", "Twilio"}})
	require.NoError(t, err)
	defer stream.Close()
	require.NoError(t, stream.SendAudio(context.Background(), voice.AudioFrame{Encoding: voice.EncodingMulaw8k, Data: []byte{1, 2, 3}}))
	close(ready)

	events := collect(t, stream.Events(), 6)
	types := make([]voice.EventType, len(events))
	for i, ev := range events {
		types[i] = ev.Type
	}
	require.Equal(t, []voice.EventType{
		voice.EventSpeechStarted, voice.EventTranscriptPartial, voice.EventTranscriptPartial,
		voice.EventSpeechEnded, voice.EventTranscriptFinal, voice.EventSpeechEnded,
	}, types, "empty partials and empty commits produce no transcript")
	require.Equal(t, "Son e-postamı özetle.", events[4].Text)
	require.True(t, events[4].Final)

	require.Eventually(t, func() bool {
		srv.mu.Lock()
		defer srv.mu.Unlock()
		return len(srv.audio) == 1
	}, 5*time.Second, 5*time.Millisecond)
	srv.mu.Lock()
	defer srv.mu.Unlock()
	require.Equal(t, "scribe_v2_realtime", srv.query["model_id"][0])
	require.Equal(t, "ulaw_8000", srv.query["audio_format"][0])
	require.Equal(t, "tr", srv.query["language_code"][0])
	require.Equal(t, "vad", srv.query["commit_strategy"][0])
	require.Equal(t, []string{"ATLAS", "Twilio"}, srv.query["keyterms"])
	require.Equal(t, [][]byte{{1, 2, 3}}, srv.audio)
}

func TestRecognitionErrors(t *testing.T) {
	t.Parallel()
	t.Run("auth error is permanent", func(t *testing.T) {
		t.Parallel()
		srv := &sttServer{script: func(conn *websocket.Conn) {
			_ = conn.WriteJSON(map[string]string{"message_type": "auth_error", "error": "invalid key el-key"})
			time.Sleep(500 * time.Millisecond)
		}}
		hs := httptest.NewServer(srv)
		t.Cleanup(hs.Close)
		stream, err := testProvider(hs.URL).Recognizer.StartRecognition(context.Background(), voice.RecognitionConfig{Encoding: voice.EncodingMulaw8k})
		require.NoError(t, err)
		defer stream.Close()
		ev := collect(t, stream.Events(), 1)[0]
		require.Equal(t, voice.EventProviderError, ev.Type)
		require.Equal(t, voice.CodeProviderAuth, ev.Code)
		require.False(t, ev.Retryable)
		require.Empty(t, ev.Text, "provider messages are not passed on")
	})
	t.Run("dropped connection is retryable", func(t *testing.T) {
		t.Parallel()
		srv := &sttServer{script: func(conn *websocket.Conn) {}}
		hs := httptest.NewServer(srv)
		t.Cleanup(hs.Close)
		stream, err := testProvider(hs.URL).Recognizer.StartRecognition(context.Background(), voice.RecognitionConfig{Encoding: voice.EncodingMulaw8k})
		require.NoError(t, err)
		defer stream.Close()
		ev := collect(t, stream.Events(), 1)[0]
		require.Equal(t, voice.EventProviderError, ev.Type)
		require.True(t, ev.Retryable)
	})
	t.Run("rejected handshake", func(t *testing.T) {
		t.Parallel()
		hs := httptest.NewServer(&sttServer{})
		t.Cleanup(hs.Close)
		p := New(Options{APIKey: voice.NewSecret("wrong"), BaseURL: hs.URL, StreamURL: "ws" + strings.TrimPrefix(hs.URL, "http")})
		_, err := p.Recognizer.StartRecognition(context.Background(), voice.RecognitionConfig{Encoding: voice.EncodingMulaw8k})
		require.Equal(t, voice.CodeProviderAuth, voice.CodeOf(err))
		require.NotContains(t, err.Error(), "wrong")
	})
	t.Run("close ends events without error", func(t *testing.T) {
		t.Parallel()
		srv := &sttServer{script: func(conn *websocket.Conn) { time.Sleep(2 * time.Second) }}
		hs := httptest.NewServer(srv)
		t.Cleanup(hs.Close)
		stream, err := testProvider(hs.URL).Recognizer.StartRecognition(context.Background(), voice.RecognitionConfig{Encoding: voice.EncodingMulaw8k})
		require.NoError(t, err)
		require.NoError(t, stream.Close())
		require.NoError(t, stream.Close())
		for ev := range stream.Events() {
			t.Fatalf("unexpected event after close: %v", ev.Type)
		}
		require.ErrorIs(t, stream.SendAudio(context.Background(), voice.AudioFrame{Encoding: voice.EncodingMulaw8k, Data: []byte{1}}), voice.ErrCallEnded)
	})
}

func TestStreamingSynthesis(t *testing.T) {
	t.Parallel()
	audio := bytes.Repeat([]byte{0x7F}, 1500)
	var got struct {
		path, format, key string
		body              ttsRequest
	}
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.path, got.format, got.key = r.URL.Path, r.URL.Query().Get("output_format"), r.Header.Get("xi-api-key")
		_ = json.NewDecoder(r.Body).Decode(&got.body)
		fl := w.(http.Flusher)
		for i := 0; i < len(audio); i += 333 {
			_, _ = w.Write(audio[i:min(i+333, len(audio))])
			fl.Flush()
		}
	}))
	t.Cleanup(hs.Close)
	syn := testProvider(hs.URL).Synthesizer
	var frames []voice.AudioFrame
	err := syn.Synthesize(context.Background(), voice.SynthesisRequest{Text: "Merhaba.", Language: "tr-TR", Encoding: voice.EncodingMulaw8k, TurnID: "t1"},
		func(f voice.AudioFrame) error { frames = append(frames, f); return nil })
	require.NoError(t, err)
	require.Equal(t, "/v1/text-to-speech/v1/stream", got.path)
	require.Equal(t, "ulaw_8000", got.format)
	require.Equal(t, "el-key", got.key)
	require.Equal(t, ttsRequest{Text: "Merhaba.", ModelID: "eleven_flash_v2_5", LanguageCode: "tr"}, got.body)
	var all []byte
	for _, f := range frames {
		require.Equal(t, "t1", f.TurnID)
		require.LessOrEqual(t, len(f.Data), 640, "80 ms frames")
		all = append(all, f.Data...)
	}
	require.Equal(t, audio, all)
	require.Greater(t, len(frames), 1, "audio streams in pieces")

	// PCM output stays sample aligned.
	pcm := 0
	err = syn.Synthesize(context.Background(), voice.SynthesisRequest{Text: "x", Encoding: voice.EncodingPCM16k}, func(f voice.AudioFrame) error {
		require.Zero(t, len(f.Data)%2)
		pcm += len(f.Data)
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 1500, pcm)

	require.Error(t, syn.Synthesize(context.Background(), voice.SynthesisRequest{Text: "x", Encoding: voice.EncodingPCM8k}, nil), "unsupported output format")
	require.NoError(t, syn.Synthesize(context.Background(), voice.SynthesisRequest{Text: "  "}, nil))
}

func TestSynthesisErrorsAndCancel(t *testing.T) {
	t.Parallel()
	for status, code := range map[int]voice.Code{
		http.StatusUnauthorized:        voice.CodeProviderAuth,
		http.StatusTooManyRequests:     voice.CodeProviderQuota,
		http.StatusInternalServerError: voice.CodeProviderUnavailable,
		http.StatusBadRequest:          voice.CodeProviderProtocol,
	} {
		hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = io.WriteString(w, `{"detail":"key el-key invalid"}`)
		}))
		err := testProvider(hs.URL).Synthesizer.Synthesize(context.Background(), voice.SynthesisRequest{Text: "x"}, func(voice.AudioFrame) error { return nil })
		hs.Close()
		require.Equal(t, code, voice.CodeOf(err), "status %d", status)
		require.NotContains(t, err.Error(), "el-key")
	}

	release := make(chan struct{})
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(bytes.Repeat([]byte{1}, 640))
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() { close(release); hs.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- testProvider(hs.URL).Synthesizer.Synthesize(ctx, voice.SynthesisRequest{Text: "uzun"}, func(voice.AudioFrame) error {
			cancel()
			return nil
		})
	}()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled, "barge-in cancels synthesis mid-stream")
	case <-time.After(5 * time.Second):
		t.Fatal("synthesis did not stop on cancel")
	}
}

func TestFromConfigNeedsKey(t *testing.T) {
	t.Parallel()
	_, err := FromConfig(voice.ElevenLabsConfig{APIKey: "$MISSING"}, func(string) string { return "" })
	require.Equal(t, voice.CodeNotConfigured, voice.CodeOf(err))
	p, err := FromConfig(voice.ElevenLabsConfig{APIKey: "$K", VoiceID: "v"}, func(string) string { return "k" })
	require.NoError(t, err)
	require.Equal(t, "elevenlabs", p.Name)
	require.Equal(t, voice.EncodingMulaw8k, p.Recognizer.InputEncodings()[0])
	require.Equal(t, voice.EncodingMulaw8k, p.Synthesizer.OutputEncodings()[0])
}
