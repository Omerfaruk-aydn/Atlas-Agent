//go:build windows

package computer

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWindowsLetterHotkeyUsesVirtualKeyInsteadOfUnicode(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		modifier, key string
		vk            uint16
	}{
		{"win", "s", 0x53}, {"ctrl", "s", 0x53}, {"ctrl", "v", 0x56}, {"alt", "A", 0x41}, {"ctrl", "1", 0x31},
	} {
		inputs, err := hotkeyInputs([]string{test.modifier}, test.key)
		require.NoError(t, err)
		require.Len(t, inputs, 4)
		require.Equal(t, test.vk, binary.LittleEndian.Uint16(inputs[1].Payload[0:2]))
		require.Equal(t, uint16(0), binary.LittleEndian.Uint16(inputs[1].Payload[2:4]))
		require.Zero(t, binary.LittleEndian.Uint32(inputs[1].Payload[4:8])&keyUnicode)
		require.Equal(t, uint32(keyUp), binary.LittleEndian.Uint32(inputs[2].Payload[4:8]))
	}
}

func TestWindowsHotkeyDoesNotPretendUnicodeTextIsAShortcut(t *testing.T) {
	t.Parallel()
	inputs, err := hotkeyInputs([]string{"ctrl"}, "界")
	require.Error(t, err)
	require.Empty(t, inputs)
	inputs, err = hotkeyInputs([]string{"ctrl", "shift"}, "enter")
	require.NoError(t, err)
	require.Len(t, inputs, 6)
	require.Equal(t, uint16(0x0d), binary.LittleEndian.Uint16(inputs[2].Payload[0:2]))
}
