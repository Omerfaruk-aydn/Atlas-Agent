package speech

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOptionsValidation(t *testing.T) {
	t.Parallel()
	require.Equal(t, 120*time.Second, (DictationOptions{}).Duration())
	for _, language := range []string{"", "en", "tr-TR", "de-DE", "ar"} {
		require.NoError(t, (DictationOptions{Language: language, MaxSeconds: 180}).Validate())
	}
	for _, o := range []DictationOptions{{Language: "en-US;exit"}, {Language: "../../file"}, {MaxSeconds: -1}, {MaxSeconds: 181}} {
		require.Error(t, o.Validate())
	}
}

func TestTranscribeRejectsMalformedAudioBeforeLaunchingRecognition(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "not-a-wave.wav")
	require.NoError(t, os.WriteFile(path, make([]byte, 100), 0o600))
	_, err := Transcribe(t.Context(), path, DictationOptions{})
	require.ErrorContains(t, err, "RIFF/WAVE")
}
