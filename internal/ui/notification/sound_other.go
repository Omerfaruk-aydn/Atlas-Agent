//go:build !windows

package notification

// SoundSupported reports whether Atlas plays its own notification sounds.
// Elsewhere the sound style rings the terminal bell.
const SoundSupported = false

func playSound(Kind, string) error { return nil }
