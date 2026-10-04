package model

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/speech"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/util"
)

type voicePhase uint8

const (
	voiceIdle voicePhase = iota
	voiceStarting
	voiceRecording
	voiceTranscribing
)

type voiceState struct {
	gen      uint64
	phase    voicePhase
	cancel   context.CancelFunc
	stop     chan struct{}
	done     chan struct{}
	language string
}

type voiceMsg struct {
	gen      uint64
	phase    voicePhase
	text     string
	language string
	err      error
	events   <-chan voiceMsg
}

func waitVoice(events <-chan voiceMsg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-events
		if !ok {
			return nil
		}
		msg.events = events
		return msg
	}
}

func (m *UI) toggleVoice() tea.Cmd {
	if m.voice.phase != voiceIdle {
		if m.voice.stop != nil {
			close(m.voice.stop)
			m.voice.stop = nil
			m.voice.phase = voiceTranscribing
			m.refreshVoiceStatus()
		}
		return nil
	}
	if m.bangMode {
		return util.ReportWarn(m.com.Text("Switch to the prompt editor before using the microphone."))
	}
	if m.completionsOpen {
		m.closeCompletions()
	}
	o := speech.DictationOptions{}
	if cfg := m.com.Config(); cfg != nil && cfg.Options != nil {
		o = cfg.Options.Voice
	}
	o = dictationOptionsForLanguage(o, m.Language())
	ctx, cancel := context.WithCancel(context.Background())
	m.voice.gen++
	gen := m.voice.gen
	stop := make(chan struct{})
	done := make(chan struct{})
	m.voice.done = done
	m.voice.cancel, m.voice.stop, m.voice.phase = cancel, stop, voiceStarting
	m.refreshVoiceStatus()
	events := make(chan voiceMsg, 4)
	return func() tea.Msg {
		go func() { defer close(done); runVoice(ctx, o, gen, stop, events) }()
		return waitVoice(events)()
	}
}

func dictationOptionsForLanguage(o speech.DictationOptions, language string) speech.DictationOptions {
	if o.Language == "" && o.Backend != "windows" {
		o.Language = language
	}
	return o
}

// runVoice owns all audio resources; it never reads or writes the UI model.
func runVoice(ctx context.Context, o speech.DictationOptions, gen uint64, stop <-chan struct{}, events chan<- voiceMsg) {
	defer close(events)
	finish := func(text string, err error) { events <- voiceMsg{gen: gen, text: text, err: err, phase: voiceIdle} }
	status, err := speech.Check(ctx, o)
	if err != nil {
		finish("", err)
		return
	}
	o.Language = status.Language
	dir, err := os.MkdirTemp("", "atlas-voice-")
	if err != nil {
		finish("", err)
		return
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "recording.wav")
	ready := make(chan error, 1)
	done := make(chan error, 1)
	go func() { done <- speech.Record(ctx, path, o, stop, ready) }()
	if err := <-ready; err != nil {
		<-done
		finish("", err)
		return
	}
	events <- voiceMsg{gen: gen, phase: voiceRecording, language: status.Language}
	if err := <-done; err != nil {
		finish("", err)
		return
	}
	if err := ctx.Err(); err != nil {
		finish("", err)
		return
	}
	events <- voiceMsg{gen: gen, phase: voiceTranscribing, language: status.Language}
	text, err := speech.Transcribe(ctx, path, o)
	finish(text, err)
}

func (m *UI) cancelVoice() {
	if m.voice.phase != voiceIdle && m.status != nil {
		m.status.ClearInfoMsg()
	}
	if m.voice.cancel != nil {
		m.voice.cancel()
		m.voice.cancel = nil
	}
	m.voice.gen++
	m.voice.phase = voiceIdle
	m.voice.stop = nil
}

// Close releases microphone and helper processes when the TUI terminates.
func (m *UI) Close() {
	done := m.voice.done
	m.cancelVoice()
	if done != nil {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	}
}

func (m *UI) handleVoice(msg voiceMsg) tea.Cmd {
	if msg.gen != m.voice.gen || m.voice.phase == voiceIdle {
		return nil
	}
	if msg.phase != voiceIdle {
		if msg.phase == voiceRecording && m.voice.stop == nil {
			msg.phase = voiceTranscribing
		}
		m.voice.phase, m.voice.language = msg.phase, msg.language
		m.refreshVoiceStatus()
		return waitVoice(msg.events)
	}
	m.cancelVoice()
	if msg.err != nil {
		if errors.Is(msg.err, context.Canceled) {
			return nil
		}
		if errors.Is(msg.err, speech.ErrNoRecognizer) {
			return util.ReportWarn(fmt.Sprintf(m.com.Text("Offline speech model is unavailable: %v"), msg.err))
		}
		if errors.Is(msg.err, speech.ErrUnsupported) {
			return util.ReportWarn(m.com.Text("Offline microphone dictation currently requires Windows."))
		}
		return util.ReportWarn(fmt.Sprintf(m.com.Text("Microphone dictation failed: %v"), msg.err))
	}
	text := strings.TrimSpace(msg.text)
	if text == "" {
		return util.ReportInfo(m.com.Text("No speech was recognized. The prompt was kept unchanged."))
	}
	prevHeight := m.textarea.Height()
	// Insert as plain text rather than a paste: a transcript is never interpreted
	// as an attachment path, command completion or automatic send.
	m.textarea.ClearSelection()
	value := m.textarea.Value()
	offset := m.textarea.Column()
	for _, line := range strings.Split(value, "\n")[:m.textarea.Line()] {
		offset += len([]rune(line)) + 1
	}
	m.textarea.InsertString(voiceInsertion(value, offset, text))
	return tea.Batch(m.handleTextareaHeightChange(prevHeight), util.ReportSuccess(m.com.Text("Speech added to the prompt. Review it before sending.")))
}

func voiceInsertion(value string, offset int, text string) string {
	runes := []rune(value)
	offset = max(0, min(offset, len(runes)))
	if offset > 0 && !unicode.IsSpace(runes[offset-1]) {
		text = " " + text
	}
	if offset < len(runes) && !unicode.IsSpace(runes[offset]) {
		text += " "
	}
	return text
}

func (m *UI) refreshVoiceStatus() {
	var text string
	switch m.voice.phase {
	case voiceStarting:
		text = m.com.Text("Preparing offline microphone dictation… Esc cancels.")
	case voiceRecording:
		text = fmt.Sprintf(m.com.Text("Recording (%s) · Ctrl+K finishes · Esc cancels"), m.voice.language)
	case voiceTranscribing:
		text = m.com.Text("Converting speech to text locally… Esc cancels.")
	default:
		return
	}
	m.status.SetInfoMsg(util.NewInfoMsg(text))
}
