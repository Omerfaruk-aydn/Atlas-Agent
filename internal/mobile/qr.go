package mobile

import (
	"mime"
	"strings"

	"rsc.io/qr"
)

func init() {
	_ = mime.AddExtensionType(".webmanifest", "application/manifest+json")
}

// QRLines renders text as a QR code using Unicode half blocks, two modules
// per character row, with the quiet zone the spec requires. Dark modules
// are drawn as spaces on a light background by the caller's style, so the
// code scans on both light and dark terminals.
func QRLines(text string) ([]string, error) {
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return nil, err
	}
	const quiet = 2
	n := code.Size
	black := func(x, y int) bool {
		if x < 0 || y < 0 || x >= n || y >= n {
			return false
		}
		return code.Black(x, y)
	}
	var lines []string
	for y := -quiet; y < n+quiet; y += 2 {
		var b strings.Builder
		for x := -quiet; x < n+quiet; x++ {
			top, bottom := black(x, y), black(x, y+1)
			switch {
			case top && bottom:
				b.WriteRune(' ')
			case top:
				b.WriteRune('▄')
			case bottom:
				b.WriteRune('▀')
			default:
				b.WriteRune('█')
			}
		}
		lines = append(lines, b.String())
	}
	return lines, nil
}
