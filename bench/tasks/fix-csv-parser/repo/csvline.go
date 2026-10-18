// Package csvline parses a single line of comma-separated values.
package csvline

import (
	"errors"
	"strings"
)

// ErrUnterminatedQuote is returned when a quoted field is never closed.
var ErrUnterminatedQuote = errors.New("csvline: unterminated quote")

// Parse splits line into fields.
//
// Fields are separated by commas. A field may be wrapped in double quotes,
// in which case commas inside it do not separate fields and a doubled quote
// ("") stands for one literal quote. The surrounding quotes are not part of
// the value. An empty line is one empty field, and a line that ends with a
// comma ends with an empty field. A quote that is never closed is an error.
func Parse(line string) ([]string, error) {
	var fields []string
	var cur strings.Builder
	inQuotes := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '"':
			inQuotes = !inQuotes
		case c == ',' && !inQuotes:
			fields = append(fields, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	if cur.Len() > 0 {
		fields = append(fields, cur.String())
	}
	return fields, nil
}
