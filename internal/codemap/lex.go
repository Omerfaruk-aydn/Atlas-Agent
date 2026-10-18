package codemap

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// tokenKind is what a token is.
type tokenKind uint8

