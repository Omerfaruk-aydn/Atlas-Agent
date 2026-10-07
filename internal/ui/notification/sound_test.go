package notification

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuiltinSoundsAreDistinctShortWAVs(t *testing.T) {
	t.Parallel()
	seen := map[string]Kind{}
	for _, kind := range Kinds {
		s := BuiltinSound(kind)
		require.Equal(t, "RIFF", string(s[:4]))
		require.Equal(t, "WAVE", string(s[8:12]))
		require.Equal(t, uint32(len(s)-8), binary.LittleEndian.Uint32(s[4:8]))
		seconds := float64(binary.LittleEndian.Uint32(s[40:44])) / 2 / soundRate
		require.Greater(t, seconds, .3, kind)
		require.Less(t, seconds, 3.0, kind)
		_, dup := seen[string(s)]
		require.False(t, dup, "%s sounds like %s", kind, seen[string(s)])
		seen[string(s)] = kind
	}
	require.Equal(t, BuiltinSound(KindFinished), BuiltinSound(KindGeneric))
}

func TestSoundBackendPlaysEachKindWithItsOwnFile(t *testing.T) {
	t.Parallel()
	if !SoundSupported {
		t.Skip("Atlas plays its own sounds only on Windows")
	}
	b := NewSoundBackend(map[string]string{"permission": `C:\Sesler\izin.wav`, "question": ""})
	type played struct {
		kind Kind
		path string
	}
	var got []played
	b.SetPlayFunc(func(kind Kind, path string) error {
		got = append(got, played{kind, path})
		return nil
	})
	for _, kind := range []Kind{KindPermission, KindQuestion, KindFinished} {
		require.Nil(t, b.Send(Notification{Title: "t", Kind: kind})())
	}
	require.Equal(t, []played{{KindPermission, `C:\Sesler\izin.wav`}, {KindQuestion, ""}, {KindFinished, ""}}, got)
}

// TestWriteSoundPreviews saves the sounds for listening:
// ATLAS_SOUND_PREVIEW=<dir> go test ./internal/ui/notification -run Preview.
func TestWriteSoundPreviews(t *testing.T) {
	dir := os.Getenv("ATLAS_SOUND_PREVIEW")
	if dir == "" {
		t.Skip("Set ATLAS_SOUND_PREVIEW to a directory to write the sounds")
	}
	for _, kind := range Kinds {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "atlas-sound-"+string(kind)+".wav"), BuiltinSound(kind), 0o644))
	}
}
