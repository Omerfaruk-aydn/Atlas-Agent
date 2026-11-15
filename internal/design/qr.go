package design

import (
	"fmt"
	"strings"

	"rsc.io/qr"
)

// QRPath encodes text as a QR code (medium error correction) and returns
// {"size": modules, "d": path}, the path drawing each run of dark modules in
// a row as one rectangle, in module units. The quiet zone is the caller's.
func QRPath(text string) (map[string]any, error) {
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("qr needs the text to encode")
	}
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return nil, fmt.Errorf("qr: %w", err)
	}
	n := code.Size
	var b strings.Builder
	for y := range n {
		for x := 0; x < n; {
			if !code.Black(x, y) {
				x++
				continue
			}
			start := x
			for x < n && code.Black(x, y) {
				x++
			}
			fmt.Fprintf(&b, "M%d %dh%dv1h-%dZ", start, y, x-start, x-start)
		}
	}
	return map[string]any{"size": n, "d": b.String()}, nil
}
