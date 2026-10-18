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

// commentHere reports whether marker starts a comment here: in a shell
// script # does only after a space ($# is a variable), and in PHP #[ is an
// attribute.
func (lx *lexer) commentHere(marker string) bool {
	if marker != "#" {
		return true
	}
	switch lx.lang.name {
	case "shell":
		if lx.pos > 0 {
			prev := lx.src[lx.pos-1]
			return prev == ' ' || prev == '\t' || prev == '\n' || prev == ';'
		}
	case "php":
		return !strings.HasPrefix(lx.src[lx.pos:], "#[")
	}
	return true
}

// stringLiteral reads a string at the current position.
func (lx *lexer) stringLiteral() bool {
	rest := lx.src[lx.pos:]
	line := lx.line
	c := rest[0]
	switch {
	case lx.lang.tripleQuotes && (strings.HasPrefix(rest, `"""`) || strings.HasPrefix(rest, `'''`)):
		end := strings.Index(rest[3:], rest[:3])
		if end < 0 {
			end = len(rest) - 3
		}
		lx.consume(3+end+3, tString, rest[3:3+end], line)
		return true
	case lx.lang.longBrackets && strings.HasPrefix(rest, "[["):
		end := strings.Index(rest[2:], "]]")
		if end < 0 {
			end = len(rest) - 2
		}
		lx.consume(2+end+2, tString, rest[2:2+end], line)
		return true
	case c == '@' && len(rest) > 1 && rest[1] == '"' && lx.lang.name == "csharp":
		// Verbatim string: "" is a quote, backslashes are literal.
		i := 2
		for i < len(rest) {
			if rest[i] == '"' {
				if i+1 < len(rest) && rest[i+1] == '"' {
					i += 2
					continue
				}
				break
			}
			i++
		}
		lx.consume(min(i+1, len(rest)), tString, rest[2:min(i, len(rest))], line)
		return true
	case c == 'r' && lx.lang.name == "rust" && rawRustString(rest) > 0:
		n := rawRustString(rest)
		hashes := strings.Repeat("#", n-2)
		closing := `"` + hashes
		end := strings.Index(rest[n:], closing)
		if end < 0 {
			end = len(rest) - n
		}
		lx.consume(min(n+end+len(closing), len(rest)), tString, rest[n:n+end], line)
		return true
	case c == '`' && lx.lang.backtickStrings || c == '`' && lx.lang.name == "go":
		lx.quoted('`', lx.lang.templates, line)
		return true
	case c == '"':
		lx.quoted('"', false, line)
		return true
	case c == '\'' && lx.lang.quoteIsPunct:
		return false
	case c == '\'':
		if lx.lang.lifetimes && isLifetime(rest) {
			lx.pos++
			return true
		}
		lx.quoted('\'', false, line)
		return true
	}
	return false
}

// rawRustString returns the length of the opening of a raw string (r",
// r#", r##") at the start of s, or 0.
func rawRustString(s string) int {
	i := 1
	for i < len(s) && s[i] == '#' {
		i++
	}
	if i < len(s) && s[i] == '"' {
		return i + 1
	}
	return 0
}

// isLifetime reports whether a quote starts a lifetime ('a) or a Scala
// symbol rather than a character literal ('a').
func isLifetime(s string) bool {
	i := 1
	for i < len(s) && (s[i] == '_' || s[i] >= 'a' && s[i] <= 'z' || s[i] >= 'A' && s[i] <= 'Z' || s[i] >= '0' && s[i] <= '9') {
		i++
	}
	return i > 1 && (i >= len(s) || s[i] != '\'')
}

// quoted reads a string closed by quote, honoring backslash escapes. With
// templates, ${...} inside it is skipped as a whole, braces balanced.
func (lx *lexer) quoted(quote byte, templates bool, line int) {
	rest := lx.src[lx.pos:]
	i := 1
	depth := 0
	for i < len(rest) {
		c := rest[i]
		switch {
		case c == '\\':
			i += 2
			continue
		case c == '\n':
			if quote != '`' && depth == 0 {
				// An unterminated string ends at the line.
				lx.consume(i, tString, rest[1:i], line)
				return
			}
			lx.line++
		case templates && depth == 0 && c == '$' && i+1 < len(rest) && rest[i+1] == '{':
			depth = 1
			i += 2
			continue
		case depth > 0 && c == '{':
			depth++
		case depth > 0 && c == '}':
			depth--
		case depth == 0 && c == quote:
			lx.pos += i + 1
			lx.emit(tString, rest[1:i], line)
			return
		}
		i++
	}
	lx.pos += len(rest)
	lx.emit(tString, rest[1:], line)
}

// consume advances over n bytes that form one token, counting lines.
func (lx *lexer) consume(n int, kind tokenKind, text string, line int) {
	lx.line += strings.Count(lx.src[lx.pos:lx.pos+n], "\n")
	lx.pos += n
	lx.emit(kind, text, line)
}

