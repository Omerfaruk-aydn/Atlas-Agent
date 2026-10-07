package notification

import (
	"encoding/binary"

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
