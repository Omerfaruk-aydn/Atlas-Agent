package ansi

import (
	"fmt"
	"image/color"
	"strconv"
	"strings"

	"github.com/lucasb-eyer/go-colorful"
)

// colorToHexString returns a hex string representation of a color.
func colorToHexString(c color.Color) string { //nolint:unused
	if c == nil {
		return ""
	}
	shift := func(v uint32) uint32 {
		if v > 0xff {
			return v >> 8
		}
		return v
	}
	r, g, b, _ := c.RGBA()
	r, g, b = shift(r), shift(g), shift(b)
	return fmt.Sprintf("#%02x%02x%02x", r, g, b)
}

// rgbToHex converts red, green, and blue values to a hexadecimal value.
//
//	hex := rgbToHex(0, 0, 255) // 0x0000FF
func rgbToHex(r, g, b uint32) uint32 { //nolint:unused
	return r<<16 + g<<8 + b
}

type shiftable interface {
	~uint | ~uint16 | ~uint32 | ~uint64
}

func shift[T shiftable](x T) T {
	if x > 0xff {
		x >>= 8
	}
	return x
}

// XParseColor is a helper function that parses a string into a color.Color. It
// provides a similar interface to the XParseColor function in Xlib. It
// supports the following formats:
//
//   - #RGB
//   - #RRGGBB
//   - rgb:RRRR/GGGG/BBBB
//   - rgba:RRRR/GGGG/BBBB/AAAA
//
// If the string is not a valid color, nil is returned.
//
// See: https://linux.die.net/man/3/xparsecolor
func XParseColor(s string) color.Color {
	switch {
	case strings.HasPrefix(s, "#"):
		c, err := colorful.Hex(s)
		if err != nil {
			return nil
		}

		return c
	case strings.HasPrefix(s, "rgb:"):
		parts := strings.Split(s[4:], "/")
		if len(parts) != 3 {
			return nil
		}
		values, ok := parseXColorComponents(parts)
		if !ok {
			return nil
		}
		return color.RGBA{values[0], values[1], values[2], 255}
	case strings.HasPrefix(s, "rgba:"):
		parts := strings.Split(s[5:], "/")
		if len(parts) != 4 {
			return nil
		}

		values, ok := parseXColorComponents(parts)
		if !ok {
			return nil
		}
		return color.RGBA{values[0], values[1], values[2], values[3]}
	}
	return nil
}

func parseXColorComponents(parts []string) ([]uint8, bool) {
	values := make([]uint8, len(parts))
	for i, part := range parts {
		if len(part) == 0 || len(part) > 4 {
			return nil, false
		}
		v, err := strconv.ParseUint(part, 16, 16)
		if err != nil {
			return nil, false
		}
		values[i] = uint8(shift(v) & 0xff)
	}
	return values, true
}
