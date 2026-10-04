// Package speech provides offline dictation using Vosk or Windows recognition.
package speech

import (
	"context"
	"errors"
	"io"
	"os"
	"regexp"
	"time"
)

const MaxAudioBytes = 24 * 1024 * 1024

var (
	ErrUnsupported  = errors.New("offline speech recognition is currently supported on Windows")
	ErrNoRecognizer = errors.New("no offline speech recognizer is available for the selected language")
	languagePattern = regexp.MustCompile(`^[a-z]{2,3}(-[A-Z]{2})?$`)
)

// DictationOptions controls local recognition independently of the chat model and UI.
type DictationOptions struct {
	Backend    string `json:"backend,omitempty" jsonschema:"description=Offline recognition backend; defaults to vosk,enum=vosk,enum=windows"`
	ModelDir   string `json:"model_dir,omitempty" jsonschema:"description=Directory containing downloaded Vosk model folders"`
	RuntimeDir string `json:"runtime_dir,omitempty" jsonschema:"description=Directory containing trusted Vosk runtime libraries"`
	Language   string `json:"language,omitempty" jsonschema:"description=Dictation language such as tr-TR or en-US; Vosk defaults to en-US outside the TUI"`
	MaxSeconds int    `json:"max_seconds,omitempty" jsonschema:"description=Maximum microphone recording duration in seconds,minimum=1,maximum=180,default=120"`
}

type Status struct {
	Backend   string   `json:"backend,omitempty"`
	Language  string   `json:"language"`
	Installed []string `json:"installed"`
}

func (o DictationOptions) Duration() time.Duration {
	if o.MaxSeconds == 0 {
		return 120 * time.Second
	}
	return time.Duration(o.MaxSeconds) * time.Second
}

func (o DictationOptions) Validate() error {
	if o.Backend != "" && o.Backend != "vosk" && o.Backend != "windows" {
		return errors.New("voice backend must be vosk or windows")
	}
	if o.MaxSeconds < 0 || o.MaxSeconds > 180 {
		return errors.New("voice max_seconds must be between 1 and 180")
	}
	if o.Language != "" && !languagePattern.MatchString(o.Language) {
		return errors.New("voice language must be a language code such as en-US or de-DE")
	}
	return nil
}

// Check discovers installed recognizers without opening the microphone.
func Check(ctx context.Context, o DictationOptions) (Status, error) {
	if err := o.Validate(); err != nil {
		return Status{}, err
	}
	return check(ctx, o)
}

// Transcribe recognizes speech locally; audio is never uploaded.
func Transcribe(ctx context.Context, path string, o DictationOptions) (string, error) {
	if err := o.Validate(); err != nil {
		return "", err
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !st.Mode().IsRegular() || st.Size() <= 44 || st.Size() > MaxAudioBytes {
		return "", errors.New("audio must be a nonempty WAV file below 24 MB")
	}
	var header [12]byte
	if _, err := io.ReadFull(f, header[:]); err != nil || string(header[:4]) != "RIFF" || string(header[8:]) != "WAVE" {
		return "", errors.New("audio must contain a RIFF/WAVE header")
	}
	return transcribe(ctx, path, o)
}

// Record captures WAV audio until stop, cancellation or the duration limit.
// The caller owns the destination and must delete it after transcription.
func Record(ctx context.Context, path string, o DictationOptions, stop <-chan struct{}, ready chan<- error) error {
	if err := o.Validate(); err != nil {
		ready <- err
		return err
	}
	return record(ctx, path, o, stop, ready)
}
