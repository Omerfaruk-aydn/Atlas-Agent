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

// multiPunct are punctuation sequences kept as one token.
var multiPunct = []string{"...", "::", "->", "=>", "?.", "<-"}

// lex splits src into tokens, dropping comments and whitespace. String
// literals are kept as one token holding their contents without quotes,
// since import paths are strings.
func lex(src string, lang *language) []lexToken {
	lx := lexer{src: src, lang: lang, line: 1, lineStart: true}
	lx.run()
	return lx.out
}

type lexer struct {
	src       string
	lang      *language
	pos       int
	line      int
	out       []lexToken
	lineStart bool
	indent    int
}

