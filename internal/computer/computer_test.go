package computer

import (
	"errors"
	"strings"
	"testing"
)

func TestValidatePointAcceptsNonNegative(t *testing.T) {
	t.Parallel()
	if err := ValidatePoint(0, 0); err != nil {
		t.Fatalf("ValidatePoint(0, 0) = %v, want nil", err)
	}
	if err := ValidatePoint(1920, 1080); err != nil {
		t.Fatalf("ValidatePoint(1920, 1080) = %v, want nil", err)
	}
}

func TestValidatePointRejectsNegative(t *testing.T) {
	t.Parallel()
	for _, p := range [][2]int{{-1, 0}, {0, -1}, {-5, -5}} {
		if err := ValidatePoint(p[0], p[1]); err == nil {
			t.Fatalf("ValidatePoint(%d, %d) = nil, want error", p[0], p[1])
		}
	}
}

func TestParseButtonDefaultsToLeft(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"", "left"} {
		b, err := ParseButton(s)
		if err != nil || b != ButtonLeft {
			t.Fatalf("ParseButton(%q) = %q, %v; want left, nil", s, b, err)
		}
	}
}

func TestParseButtonAcceptsRightAndMiddle(t *testing.T) {
	t.Parallel()
	for s, want := range map[string]MouseButton{"right": ButtonRight, "middle": ButtonMiddle} {
		b, err := ParseButton(s)
		if err != nil || b != want {
			t.Fatalf("ParseButton(%q) = %q, %v; want %q, nil", s, b, want, err)
		}
	}
}

func TestParseButtonRejectsUnknown(t *testing.T) {
	t.Parallel()
	if _, err := ParseButton("sideways"); err == nil {
		t.Fatal("ParseButton(sideways) = nil, want error")
	}
}

func TestResolveKeyKnownNames(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]uint16{"enter": 0x0D, "esc": 0x1B, "f5": 0x74, "up": 0x26} {
		vk, ok := ResolveKey(name)
		if !ok || vk != want {
			t.Fatalf("ResolveKey(%q) = %#x, %v; want %#x, true", name, vk, ok, want)
		}
	}
}

func TestResolveKeySingleCharIsUnicode(t *testing.T) {
	t.Parallel()
	if vk, ok := ResolveKey("a"); !ok || vk != 0 {
		t.Fatalf("ResolveKey(a) = %#x, %v; want 0, true", vk, ok)
	}
}

func TestResolveKeyRejectsUnknown(t *testing.T) {
	t.Parallel()
	if _, ok := ResolveKey("hyper"); ok {
		t.Fatal("ResolveKey(hyper) = true, want false")
	}
	// Multi-character strings that are not named keys are rejected even
	// when every character is otherwise typable.
	if _, ok := ResolveKey("ab"); ok {
		t.Fatal("ResolveKey(ab) = true, want false")
	}
}

func TestParseModifiersAcceptsKnown(t *testing.T) {
	t.Parallel()
	mods, err := ParseModifiers([]string{"ctrl", "shift"})
	if err != nil {
		t.Fatalf("ParseModifiers = %v, want nil", err)
	}
	if len(mods) != 2 || mods[0] != ModCtrl || mods[1] != ModShift {
		t.Fatalf("ParseModifiers = %v, want [ctrl shift]", mods)
	}
}

func TestParseModifiersRejectsUnknown(t *testing.T) {
	t.Parallel()
	if _, err := ParseModifiers([]string{"ctrl", "hyper"}); err == nil {
		t.Fatal("ParseModifiers with hyper = nil, want error")
	}
}

func TestValidatePointInAcceptsInside(t *testing.T) {
	t.Parallel()
	size := Size{Width: 1920, Height: 1080}
	for _, p := range [][2]int{{0, 0}, {1919, 1079}, {100, 200}} {
		if err := ValidatePointIn(size, p[0], p[1]); err != nil {
			t.Fatalf("ValidatePointIn(%d, %d) = %v, want nil", p[0], p[1], err)
		}
	}
}

func TestValidatePointInRejectsOutside(t *testing.T) {
	t.Parallel()
	size := Size{Width: 1920, Height: 1080}
	for _, p := range [][2]int{{1920, 0}, {0, 1080}, {2500, 100}, {-1, 5}} {
		err := ValidatePointIn(size, p[0], p[1])
		if err == nil {
			t.Fatalf("ValidatePointIn(%d, %d) = nil, want error", p[0], p[1])
		}
	}
}

func TestValidatePointInNamesScreenSize(t *testing.T) {
	t.Parallel()
	err := ValidatePointIn(Size{Width: 1920, Height: 1080}, 2500, 100)
	if err == nil {
		t.Fatal("ValidatePointIn = nil, want error")
	}
	if got, want := err.Error(), "1920 x 1080"; !strings.Contains(got, want) {
		t.Fatalf("error %q does not name the screen size %q", got, want)
	}
}

func TestOpenReportsUnsupportedOffWindows(t *testing.T) {
	t.Parallel()
	backend, err := Open()
	if err == nil {
		_ = backend
		t.Skip("platform has a computer backend; nothing to assert here")
	}
	if !errors.Is(err, ErrUnsupportedPlatform) {
		t.Fatalf("Open() = %v, want ErrUnsupportedPlatform", err)
	}
}
