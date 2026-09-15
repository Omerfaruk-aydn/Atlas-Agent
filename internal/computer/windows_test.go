//go:build windows

package computer

import (
	"encoding/binary"
	"testing"
)

// These tests cover the pure INPUT-encoding helpers. They inject no
// input and touch no device state, so they are safe to run on a real
// Windows desktop — unlike the backend methods themselves, which have
// no automated coverage by design.

func TestMousePayloadLayout(t *testing.T) {
	t.Parallel()
	p := mousePayload(10, -20, 120, mouseWheel)
	if got := int32(binary.LittleEndian.Uint32(p[0:4])); got != 10 {
		t.Fatalf("dx = %d, want 10", got)
	}
	if got := int32(binary.LittleEndian.Uint32(p[4:8])); got != -20 {
		t.Fatalf("dy = %d, want -20", got)
	}
	if got := binary.LittleEndian.Uint32(p[8:12]); got != 120 {
		t.Fatalf("mouseData = %d, want 120", got)
	}
	if got := binary.LittleEndian.Uint32(p[12:16]); got != mouseWheel {
		t.Fatalf("flags = %#x, want %#x", got, mouseWheel)
	}
}

func TestKeyboardPayloadLayout(t *testing.T) {
	t.Parallel()
	p := keyboardPayload(0x0D, 0, keyUnicode)
	if got := binary.LittleEndian.Uint16(p[0:2]); got != 0x0D {
		t.Fatalf("vk = %#x, want 0x0D", got)
	}
	if got := binary.LittleEndian.Uint32(p[4:8]); got != keyUnicode {
		t.Fatalf("flags = %#x, want %#x", got, keyUnicode)
	}
}

func TestMouseClickEmitsDownThenUp(t *testing.T) {
	t.Parallel()
	for button, du := range map[MouseButton][2]uint32{
		ButtonLeft:   {mouseLeftDown, mouseLeftUp},
		ButtonRight:  {mouseRightDown, mouseRightUp},
		ButtonMiddle: {mouseMiddleDown, mouseMiddleUp},
	} {
		down, up := du[0], du[1]
		inputs := mouseClick(button)
		if len(inputs) != 2 {
			t.Fatalf("mouseClick(%q) = %d inputs, want 2", button, len(inputs))
		}
		if got := binary.LittleEndian.Uint32(inputs[0].Payload[12:16]); got != down {
			t.Fatalf("mouseClick(%q)[0] flags = %#x, want %#x", button, got, down)
		}
		if got := binary.LittleEndian.Uint32(inputs[1].Payload[12:16]); got != up {
			t.Fatalf("mouseClick(%q)[1] flags = %#x, want %#x", button, got, up)
		}
	}
}

func TestScrollDataSign(t *testing.T) {
	t.Parallel()
	// Downward scroll must encode as two's-complement negative wheel
	// data, not a compile-time overflow: regression test for the
	// uint32(-120) constant conversion that broke the Windows build.
	down := uint32(wheelDelta)
	down = -down
	if down != 0xFFFFFF88 {
		t.Fatalf("downward wheel data = %#x, want 0xFFFFFF88", down)
	}
}
