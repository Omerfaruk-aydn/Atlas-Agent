package notification

import (
	"log/slog"

	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
)

// SoundBackend announces each event with its own sound instead of the
// terminal bell's single system beep. A custom WAV file can replace any of
// Atlas's sounds. Where Atlas cannot play audio itself it rings the bell.
type SoundBackend struct {
	// custom maps a kind to the WAV file that replaces its sound.
	custom map[Kind]string
	// play is swappable for tests.
	play func(kind Kind, path string) error
}

// NewSoundBackend creates a sound backend. Keys of custom are kind names
// (permission, question, finished); values are WAV file paths.
func NewSoundBackend(custom map[string]string) *SoundBackend {
	b := &SoundBackend{custom: map[Kind]string{}, play: playSound}
	for k, path := range custom {
		if path != "" {
			b.custom[Kind(k)] = path
		}
	}
	return b
}

// Send plays the sound for the notification's kind.
func (b *SoundBackend) Send(n Notification) tea.Cmd {
	if !SoundSupported {
		return tea.Raw("\x07")
	}
	return func() tea.Msg {
		if err := b.play(n.Kind, b.custom[n.Kind]); err != nil {
			slog.Warn("Failed to play notification sound", "kind", n.Kind, "error", err)
		}
		return nil
	}
}

// SetPlayFunc replaces playback for testing.
func (b *SoundBackend) SetPlayFunc(fn func(kind Kind, path string) error) {
	b.play = fn
}
