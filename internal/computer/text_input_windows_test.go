//go:build windows

package computer

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

// keyboardEvent decodes the KEYBDINPUT fields the tests care about.
func keyboardEvent(in winInput) (vk, scan uint16, flags uint32) {
	return binary.LittleEndian.Uint16(in.Payload[0:2]), binary.LittleEndian.Uint16(in.Payload[2:4]), binary.LittleEndian.Uint32(in.Payload[4:8])
}

// A logical line break must reach the target as a real Enter key press in
// the same ordered input batch. A Unicode LF packet is not a line break in
// many editors, which forced line-by-line type/Enter tool calls.
func TestTextInputsTypeLineBreaksAsEnter(t *testing.T) {
	t.Parallel()
	inputs := textInputs("a\nb")
	require.Len(t, inputs, 6)
	vk, scan, flags := keyboardEvent(inputs[0])
	require.Equal(t, uint16(0), vk)
	require.Equal(t, uint16('a'), scan)
	require.Equal(t, uint32(keyUnicode), flags)
	vk, _, flags = keyboardEvent(inputs[2])
	require.Equal(t, uint16(0x0D), vk, "line break is VK_RETURN")
	require.Zero(t, flags&keyUnicode)
	vk, _, flags = keyboardEvent(inputs[3])
	require.Equal(t, uint16(0x0D), vk)
	require.Equal(t, uint32(keyUp), flags)
	_, scan, _ = keyboardEvent(inputs[4])
	require.Equal(t, uint16('b'), scan)
	for _, in := range inputs {
		_, scan, _ := keyboardEvent(in)
		require.NotEqual(t, uint16('\n'), scan, "no raw LF packet is sent")
	}
}
