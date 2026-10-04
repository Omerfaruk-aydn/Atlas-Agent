//go:build windows

package speech

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNativeRecognizerDiscovery(t *testing.T) {
	t.Parallel()
	status, err := Check(t.Context(), DictationOptions{Backend: "windows"})
	if errors.Is(err, ErrNoRecognizer) {
		require.Empty(t, status.Language)
		return
	}
	require.NoError(t, err)
	require.Contains(t, status.Installed, status.Language)
}

func TestNativeSpeechFailureAndUnicodeProtocol(t *testing.T) {
	t.Parallel()
	r, err := decodeResult([]byte(`{"installed":["tr-TR"],"language":"tr-TR","text":"merhaba dünya","error_code":""}`))
	require.NoError(t, err)
	require.Equal(t, "merhaba dünya", r.Text)
	_, err = decodeResult([]byte(`{"installed":[],"error_code":"no_recognizer"}`))
	require.ErrorIs(t, err, ErrNoRecognizer)
	_, err = decodeResult([]byte("invalid output"))
	require.Error(t, err)
}
