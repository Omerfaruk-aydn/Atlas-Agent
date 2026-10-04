//go:build windows

package speech

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVoskDownloadedLanguagesAreAvailable(t *testing.T) {
	home, err := filepath.Abs(filepath.Join("..", "..", ".atlas"))
	require.NoError(t, err)
	if _, err := os.Stat(filepath.Join(home, "speech-models", "vosk-model-small-tr-0.3", "final.mdl")); err != nil {
		t.Skip("Downloaded Vosk assets are required for this integration test")
	}
	t.Setenv("ATLAS_AGENT_SPEECH_HOME", home)
	for _, language := range []string{"tr-TR", "en-US", "it-IT", "fr-FR"} {
		status, err := Check(t.Context(), DictationOptions{Language: language})
		require.NoError(t, err, language)
		require.Equal(t, language, status.Language)
		require.Contains(t, status.Installed, language)
	}
	_, err = Check(t.Context(), DictationOptions{Language: "ar"})
	require.ErrorIs(t, err, ErrNoRecognizer)
}

func TestVoskTranscribesLocalEnglishFixtureAndCancels(t *testing.T) {
	home, err := filepath.Abs(filepath.Join("..", "..", ".atlas"))
	require.NoError(t, err)
	path := filepath.Join(home, "windows-speech-synthetic.wav")
	if _, err := os.Stat(path); err != nil {
		t.Skip("Local generated speech fixture is required")
	}
	t.Setenv("ATLAS_AGENT_SPEECH_HOME", home)
	text, err := Transcribe(t.Context(), path, DictationOptions{Language: "en-US"})
	require.NoError(t, err)
	require.Contains(t, text, "hello world")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = Transcribe(ctx, path, DictationOptions{Language: "en-US"})
	require.ErrorIs(t, err, context.Canceled)
}

func TestVoskRealLanguageRecordings(t *testing.T) {
	home, err := filepath.Abs(filepath.Join("..", "..", ".atlas"))
	require.NoError(t, err)
	fixtures := []struct {
		language, file string
		phrases        []string
	}{
		{"tr-TR", "turkish-sample.wav", []string{"bilgilere göre", "yol kenarına", "aracın yanına"}},
		{"fr-FR", "fr-FR-sample.wav", []string{"je souhaite changer mon adresse"}},
		{"it-IT", "it-IT-sample.wav", []string{"conto corrente"}},
	}
	for _, fixture := range fixtures {
		if _, err := os.Stat(filepath.Join(home, "voice-research", fixture.file)); err != nil {
			t.Skip("Local real-language speech recordings are required")
		}
	}
	t.Setenv("ATLAS_AGENT_SPEECH_HOME", home)
	for _, fixture := range fixtures {
		text, err := Transcribe(t.Context(), filepath.Join(home, "voice-research", fixture.file), DictationOptions{Language: fixture.language})
		require.NoError(t, err, fixture.language)
		for _, phrase := range fixture.phrases {
			require.Contains(t, text, phrase, fixture.language)
		}
	}
}
