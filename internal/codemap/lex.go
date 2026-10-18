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

func (lx *lexer) emit(kind tokenKind, text string, line int) {
	lx.out = append(lx.out, lexToken{kind: kind, text: text, line: line, first: lx.lineStart, indent: lx.indent})
	lx.lineStart = false
}

func (lx *lexer) run() {
	for lx.pos < len(lx.src) {
		c := lx.src[lx.pos]
		switch c {
		case '\n':
			lx.line++
			lx.pos++
			lx.lineStart, lx.indent = true, 0
			continue
		case ' ', '\t', '\r', '\f', '\v':
			if lx.lineStart {
				if c == '\t' {
					lx.indent += 4
				} else {
					lx.indent++
				}
			}
			lx.pos++
			continue
		}
		if lx.comment() || lx.stringLiteral() {
			continue
		}
		r, size := utf8.DecodeRuneInString(lx.src[lx.pos:])
		switch {
		case lx.identStart(r):
			lx.ident()
		case c >= '0' && c <= '9':
			lx.number()
		default:
			lx.punct(size)
		}
	}
}

// comment skips a comment at the current position.
func (lx *lexer) comment() bool {
	rest := lx.src[lx.pos:]
	for _, block := range lx.lang.blockComments {
		if !strings.HasPrefix(rest, block[0]) {
			continue
		}
		depth, i := 1, len(block[0])
		for i < len(rest) && depth > 0 {
			switch {
			case strings.HasPrefix(rest[i:], block[1]):
				depth--
				i += len(block[1])
			case lx.lang.nestedComments && strings.HasPrefix(rest[i:], block[0]):
				depth++
				i += len(block[0])
			default:
				if rest[i] == '\n' {
					lx.line++
				}
				i++
			}
		}
		lx.pos += i
		return true
	}
	for _, marker := range lx.lang.lineComments {
		if !strings.HasPrefix(rest, marker) || !lx.commentHere(marker) {
			continue
		}
		end := strings.IndexByte(rest, '\n')
		if end < 0 {
			end = len(rest)
		}
		lx.pos += end
		return true
	}
	return false
}

