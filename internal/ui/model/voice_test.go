package model

import (
	"context"
	"testing"

	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/key"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/textarea"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/i18n"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/speech"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/common"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/dialog"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/styles"
	"github.com/stretchr/testify/require"
)

func voiceTestUI() *UI {
	sty := styles.AtlasPantera()
	sty.Locale = i18n.New("en")
	m := &UI{com: &common.Common{Styles: &sty}, dialog: dialog.NewOverlay(), keyMap: DefaultKeyMap(), focus: uiFocusEditor, state: uiLanding}
	m.textarea = textarea.New()
	m.textarea.SetWidth(100)
	m.textarea.CharLimit = -1
	m.status = NewStatus(m.com, m)
	return m
}

func TestVoiceUsesMenuLanguageUnlessDictationLanguageIsExplicit(t *testing.T) {
	t.Parallel()
	for _, language := range []string{"tr", "en", "it", "fr"} {
		require.Equal(t, language, dictationOptionsForLanguage(speech.DictationOptions{}, language).Language)
	}
	explicit := speech.DictationOptions{Language: "fr-FR", ModelDir: "models", MaxSeconds: 45}
	require.Equal(t, explicit, dictationOptionsForLanguage(explicit, "tr"))
	windows := speech.DictationOptions{Backend: "windows"}
	require.Equal(t, windows, dictationOptionsForLanguage(windows, "tr"))
}

func TestVoiceResultPreservesDraftAndCurrentCursor(t *testing.T) {
	t.Parallel()
	m := voiceTestUI()
	m.textarea.SetValue("Önce\nsonra")
	m.textarea.MoveToEnd()
	m.textarea.SetCursorColumn(0)
	m.voice = voiceState{gen: 3, phase: voiceTranscribing}
	cmd := m.handleVoice(voiceMsg{gen: 3, text: "merhaba dünya", phase: voiceIdle})
	require.NotNil(t, cmd)
	require.Equal(t, "Önce\nmerhaba dünya sonra", m.textarea.Value())
	require.Equal(t, voiceIdle, m.voice.phase)
}

func TestVoiceCanceledAndStaleResultsNeverChangePrompt(t *testing.T) {
	t.Parallel()
	m := voiceTestUI()
	m.textarea.SetValue("my draft")
	ctx, cancel := context.WithCancel(t.Context())
	m.voice = voiceState{gen: 4, phase: voiceRecording, cancel: cancel}
	m.cancelVoice()
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	require.Nil(t, m.handleVoice(voiceMsg{gen: 4, text: "old speech"}))
	require.Equal(t, "my draft", m.textarea.Value())
	m.voice = voiceState{gen: 7, phase: voiceTranscribing}
	m.handleVoice(voiceMsg{gen: 7})
	require.Equal(t, "my draft", m.textarea.Value())
}

func TestVoiceShortcutStopsOnceAndEscapeCancels(t *testing.T) {
	t.Parallel()
	m := voiceTestUI()
	stop := make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())
	m.voice = voiceState{gen: 2, phase: voiceRecording, stop: stop, cancel: cancel}
	k := tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl}
	require.True(t, key.Matches(k, m.keyMap.Editor.Dictate))
	m.handleKeyPressMsg(k)
	select {
	case <-stop:
	default:
		t.Fatal("Ctrl+K did not stop recording")
	}
	require.Equal(t, voiceTranscribing, m.voice.phase)
	m.handleKeyPressMsg(k)
	m.handleKeyPressMsg(tea.KeyPressMsg{Code: tea.KeyEscape})
	require.Equal(t, voiceIdle, m.voice.phase)
	require.ErrorIs(t, ctx.Err(), context.Canceled)
}

func TestVoiceSendIsBlockedWhileTranscribing(t *testing.T) {
	t.Parallel()
	m := voiceTestUI()
	m.textarea.SetValue("keep me")
	m.voice = voiceState{gen: 1, phase: voiceTranscribing}
	require.NotNil(t, m.handleKeyPressMsg(tea.KeyPressMsg{Code: tea.KeyEnter}))
	require.Equal(t, "keep me", m.textarea.Value())
}
