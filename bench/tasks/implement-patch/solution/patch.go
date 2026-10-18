// Package patch applies single-file unified diffs.
package patch

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// ErrMalformed is wrapped by the error Apply returns for a diff it cannot parse.
var ErrMalformed = errors.New("patch: malformed diff")

// ErrConflict is wrapped by the error Apply returns when a hunk does not fit
// the text.
var ErrConflict = errors.New("patch: hunk does not apply")

var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

type hunk struct {
	oldStart int
	oldLines []string
	newLines []string
}

// Apply applies diff to original and returns the patched text.
//
// Lines. original is a sequence of lines separated by "\n". A final "\n" ends
// the last line and does not start another one, and the result ends with "\n"
// exactly when original does. The exception is an empty original: the result
// then ends with "\n" if it has any line at all, and is "" otherwise. A result
// with no lines is always "".
//
// The diff. Lines before the first hunk header (file headers such as "--- a",
// "+++ b", "diff ...") are ignored. A hunk starts with a header
//
//	@@ -OLDSTART[,OLDCOUNT] +NEWSTART[,NEWCOUNT] @@
//
// that may be followed by any text. A missing count is 1. The hunk body lines
// follow: " " context, "-" deleted, "+" added. A body line that starts with
// "\" is ignored (it is a "no newline" marker). An empty body line counts as
// an empty context line, since some tools strip the trailing space. A hunk
// ends at the next header or at the end of the diff. The empty piece after a
// trailing "\n" at the very end of diff is not a body line. A diff with no
// hunks returns original unchanged.
//
// Validation. OLDCOUNT must equal the number of context plus deleted lines
// and NEWCOUNT the number of context plus added lines (NEWSTART is not
// used). A line starting with "@@" that is not a valid header, or a body line
// with any other first character, is an error wrapping ErrMalformed.
//
// Position. OLDSTART is the 1-based line where the hunk's old lines (context
// and deleted, in order) begin. When OLDCOUNT is 0 the hunk only inserts, and
// OLDSTART is the line it goes after (0 means before the first line). Old
// lines must match the text exactly. If they do not match at the stated
// position, try the positions at offsets -1, +1, -2, +2, ... from it and use
// the first that matches, so equally near candidates resolve to the earlier
// one. A hunk may never be placed before the end of the previous hunk's
// matched lines. If nothing fits, the error wraps ErrConflict. Hunks are
// positioned in the order they appear, each against the original text.
func Apply(original, diff string) (string, error) {
	hunks, err := parse(diff)
	if err != nil {
		return "", err
	}
	if len(hunks) == 0 {
		return original, nil
	}
	lines, finalNewline := splitLines(original)

	var out []string
	cursor := 0
	for _, h := range hunks {
		pos, ok := locate(lines, h, cursor)
		if !ok {
			return "", fmt.Errorf("%w: hunk at line %d", ErrConflict, h.oldStart)
		}
		out = append(out, lines[cursor:pos]...)
		out = append(out, h.newLines...)
		cursor = pos + len(h.oldLines)
	}
	out = append(out, lines[cursor:]...)

	if original == "" {
		finalNewline = true
	}
	if len(out) == 0 {
		return "", nil
	}
	result := strings.Join(out, "\n")
	if finalNewline {
		result += "\n"
	}
	return result, nil
}

func splitLines(s string) (lines []string, finalNewline bool) {
	if s == "" {
		return nil, false
	}
	finalNewline = strings.HasSuffix(s, "\n")
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n"), finalNewline
}

func parse(diff string) ([]hunk, error) {
	pieces := strings.Split(diff, "\n")
	if pieces[len(pieces)-1] == "" {
		pieces = pieces[:len(pieces)-1]
	}
	var hunks []hunk
	var cur *hunk
	var oldCount, newCount int
	finish := func() error {
		if cur == nil {
			return nil
		}
		if len(cur.oldLines) != oldCount || len(cur.newLines) != newCount {
			return fmt.Errorf("%w: hunk at line %d has %d old and %d new lines, header says %d and %d",
				ErrMalformed, cur.oldStart, len(cur.oldLines), len(cur.newLines), oldCount, newCount)
		}
		hunks = append(hunks, *cur)
		cur = nil
		return nil
	}
	for _, line := range pieces {
		if strings.HasPrefix(line, "@@") {
			if err := finish(); err != nil {
				return nil, err
			}
			m := hunkHeader.FindStringSubmatch(line)
			if m == nil {
				return nil, fmt.Errorf("%w: bad hunk header %q", ErrMalformed, line)
			}
			start, _ := strconv.Atoi(m[1])
			oldCount, newCount = 1, 1
			if m[2] != "" {
				oldCount, _ = strconv.Atoi(m[2])
			}
			if m[4] != "" {
				newCount, _ = strconv.Atoi(m[4])
			}
			cur = &hunk{oldStart: start}
			continue
		}
		if cur == nil {
			continue
		}
		switch {
		case line == "":
			cur.oldLines = append(cur.oldLines, "")
			cur.newLines = append(cur.newLines, "")
		case line[0] == ' ':
			cur.oldLines = append(cur.oldLines, line[1:])
			cur.newLines = append(cur.newLines, line[1:])
		case line[0] == '-':
			cur.oldLines = append(cur.oldLines, line[1:])
		case line[0] == '+':
			cur.newLines = append(cur.newLines, line[1:])
		case line[0] == '\\':
		default:
			return nil, fmt.Errorf("%w: unexpected line %q", ErrMalformed, line)
		}
	}
	if err := finish(); err != nil {
		return nil, err
	}
	return hunks, nil
}

// locate finds where the hunk's old lines sit, starting at the stated line and
// moving outward, never before floor.
func locate(lines []string, h hunk, floor int) (int, bool) {
	stated := h.oldStart - 1
	if len(h.oldLines) == 0 {
		stated = h.oldStart
	}
	stated = max(0, min(stated, len(lines)))
	for d := 0; d <= len(lines)+1; d++ {
		candidates := []int{stated}
		if d > 0 {
			candidates = []int{stated - d, stated + d}
		}
		for _, pos := range candidates {
			end := pos + len(h.oldLines)
			if pos < floor || end > len(lines) {
				continue
			}
			if slices.Equal(lines[pos:end], h.oldLines) {
				return pos, true
			}
		}
	}
	return 0, false
}
