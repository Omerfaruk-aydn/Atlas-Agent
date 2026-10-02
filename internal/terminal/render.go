package terminal

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ansi"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/execution"
)

// Screen is a safe text interpretation, separate from original PTY bytes.
// Unsupported controls are reported rather than presented as faithful output.
type Screen struct {
	Width       int      `json:"width"`
	Height      int      `json:"height"`
	Lines       []string `json:"lines"`
	Unsupported []string `json:"unsupported,omitempty"`
}

func RenderTranscript(ctx context.Context, data []byte, size execution.TerminalSize) (Screen, error) {
	result := Screen{Width: size.Width, Height: size.Height}
	if !validSize(size) || len(data) > MaxTranscriptBytes {
		return result, fmt.Errorf("terminal rendering exceeds bounds")
	}
	grid := make([][]string, size.Height)
	for i := range grid {
		grid[i] = make([]string, size.Width)
	}
	x, y := 0, 0
	unsupported := map[string]bool{}
	gap := func(value string) {
		if !unsupported[value] && len(result.Unsupported) < 16 {
			unsupported[value] = true
			result.Unsupported = append(result.Unsupported, value)
		}
	}
	scroll := func() { copy(grid, grid[1:]); grid[len(grid)-1] = make([]string, size.Width); y = size.Height - 1 }
	clear := func(row, start, end int) {
		for column := max(0, start); column < min(size.Width, end); column++ {
			grid[row][column] = ""
		}
	}
	for index := 0; index < len(data); {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		value := data[index]
		index++
		if value == 0x1b {
			if index >= len(data) {
				gap("truncated escape")
				break
			}
			kind := data[index]
			index++
			if kind == ']' || kind == 'P' || kind == '_' || kind == '^' {
				terminated := false
				for index < len(data) {
					if err := ctx.Err(); err != nil {
						return result, err
					}
					if data[index] == 7 && kind == ']' {
						index++
						terminated = true
						break
					}
					if data[index] == 0x1b && index+1 < len(data) && data[index+1] == '\\' {
						index += 2
						terminated = true
						break
					}
					index++
				}
				if !terminated {
					gap("truncated string control")
				}
				continue
			}
			if kind != '[' {
				gap("unsupported escape")
				continue
			}
			start := index
			for index < len(data) && !(data[index] >= 0x40 && data[index] <= 0x7e) {
				if err := ctx.Err(); err != nil {
					return result, err
				}
				index++
			}
			if index >= len(data) {
				gap("truncated CSI")
				break
			}
			body := string(data[start:index])
			final := data[index]
			index++
			if len(body) > 128 {
				gap("oversized CSI")
				continue
			}
			if strings.HasPrefix(body, "?") {
				gap("private terminal mode")
				continue
			}
			parts := strings.Split(body, ";")
			values := make([]int, len(parts))
			valid := true
			for i, p := range parts {
				if p != "" {
					n, err := strconv.Atoi(p)
					if err != nil || n < 0 || n > 100000 {
						valid = false
						break
					}
					values[i] = n
				}
			}
			if !valid {
				gap("invalid CSI parameter")
				continue
			}
			n := max(1, values[0])
			mode := values[0]
			switch final {
			case 'A':
				y = max(0, y-n)
			case 'B':
				y = min(size.Height-1, y+n)
			case 'C':
				x = min(size.Width-1, x+n)
			case 'D':
				x = max(0, x-n)
			case 'G':
				x = min(size.Width-1, n-1)
			case 'd':
				y = min(size.Height-1, n-1)
			case 'H', 'f':
				y = min(size.Height-1, n-1)
				column := 1
				if len(values) > 1 {
					column = max(1, values[1])
				}
				x = min(size.Width-1, column-1)
			case 'K':
				switch mode {
				case 0:
					clear(y, x, size.Width)
				case 1:
					clear(y, 0, x+1)
				case 2:
					clear(y, 0, size.Width)
				default:
					gap("unsupported erase line")
				}
			case 'J':
				switch mode {
				case 0:
					clear(y, x, size.Width)
					for row := y + 1; row < size.Height; row++ {
						clear(row, 0, size.Width)
					}
				case 1:
					for row := 0; row < y; row++ {
						clear(row, 0, size.Width)
					}
					clear(y, 0, x+1)
				case 2, 3:
					for row := range grid {
						clear(row, 0, size.Width)
					}
				default:
					gap("unsupported erase display")
				}
			case 'm':
			default:
				gap("unsupported CSI " + string(final))
			}
			continue
		}
		switch value {
		case '\r':
			x = 0
			continue
		case '\n':
			y++
			if y >= size.Height {
				scroll()
			}
			continue
		case '\b':
			x = max(0, x-1)
			continue
		case '\t':
			x = min(size.Width-1, (x/8+1)*8)
			continue
		case 7:
			continue
		}
		if value < 32 || value == 127 {
			gap("unsupported control byte")
			continue
		}
		index--
		r, count := utf8.DecodeRune(data[index:])
		index += count
		if r == utf8.RuneError && count == 1 {
			gap("invalid UTF-8")
			continue
		}
		text := string(r)
		width := ansi.StringWidth(text)
		if width == 0 {
			if x > 0 {
				grid[y][x-1] += text
			}
			continue
		}
		if width > 2 {
			gap("unsupported glyph width")
			continue
		}
		if x+width > size.Width {
			x = 0
			y++
			if y >= size.Height {
				scroll()
			}
		}
		grid[y][x] = text
		if width == 2 {
			grid[y][x+1] = "\x00"
		}
		x += width
	}
	for _, row := range grid {
		var line strings.Builder
		for _, cell := range row {
			if cell == "\x00" {
				continue
			}
			if cell == "" {
				line.WriteByte(' ')
			} else {
				line.WriteString(cell)
			}
		}
		result.Lines = append(result.Lines, strings.TrimRight(line.String(), " "))
	}
	return result, ctx.Err()
}
