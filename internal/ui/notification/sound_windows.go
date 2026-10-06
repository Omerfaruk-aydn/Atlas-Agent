//go:build windows

package notification

import (
	"fmt"
	"syscall"
	"unsafe"
)

// SoundSupported reports whether Atlas plays its own notification sounds.
const SoundSupported = true

var playSoundW = syscall.NewLazyDLL("winmm.dll").NewProc("PlaySoundW")

const (
	sndAsync     = 0x0001
	sndNoDefault = 0x0002
	sndMemory    = 0x0004
	sndFilename  = 0x00020000
)

// playSound plays a custom WAV file, or Atlas's own sound for kind when no
// file is set or the file cannot be played. Playback is asynchronous; the
// built-in sounds live for the whole process, as PlaySound requires.
func playSound(kind Kind, path string) error {
	var fileErr error
	if path != "" {
		name, err := syscall.UTF16PtrFromString(path)
		if err == nil {
			var ok uintptr
			ok, _, err = playSoundW.Call(uintptr(unsafe.Pointer(name)), 0, sndFilename|sndAsync|sndNoDefault)
			if ok != 0 {
				return nil
			}
		}
		fileErr = fmt.Errorf("custom sound %q could not be played, using the built-in sound: %w", path, err)
	}
	sound := BuiltinSound(kind)
	if ok, _, err := playSoundW.Call(uintptr(unsafe.Pointer(&sound[0])), 0, sndMemory|sndAsync|sndNoDefault); ok == 0 {
		return fmt.Errorf("built-in sound: %w", err)
	}
	return fileErr
}
