package codemap

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// tokenKind is what a token is.
type tokenKind uint8

const (
	tIdent tokenKind = iota
	tString
	tNumber
	tPunct
)

// lexToken is one word, literal or punctuation mark of the source.
type lexToken struct {
	kind tokenKind
	text string
	line int
	// first is set on the first token of a line, and indent is that
	// line's indentation in columns.
	first  bool
	indent int
}

