package codemap

import (
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Rules for languages whose definitions, imports or calls do not fit the
// keyword table: equations in Haskell, clauses in Erlang, type sections in
// Nim and Pascal, instances in Verilog and VHDL, calls without parentheses.

// langDeclAt records a definition the keyword table cannot describe and
// reports whether it consumed the token.
func (x *extractor) langDeclAt(i int) bool {
	t := x.toks[i]
	switch x.lang.name {
	case "haskell", "elm":
		if t.first && t.text == "module" {
			x.markHeader(i)
			return true
		}
		return x.equationDecl(i)
	case "erlang":
		return x.erlangDecl(i)
	case "nim":
		return x.nimTypeDecl(i)
	case "pascal":
		return x.pascalTypeDecl(i)
	case "verilog":
		x.verilogInstance(i)
	case "vhdl":
		return x.vhdlDecl(i)
	case "gdscript":
		return x.gdscriptDecl(i)
	case "vlang", "gleam":
		if t.first && t.text == "module" {
			x.markLine(i)
			return true
		}
	case "fortran":
		if x.kw(t.text) == "call" && x.tok(i+1).kind == tIdent {
			x.addRef(x.toks[i+1].text, "", RefCall, t.line)
			x.names[i+1] = true
			return true
		}
	}
	if x.lang.endPrefix || x.lang.caseInsensitive {
		// End Sub, end subroutine name, Exit Function: the keyword after
		// them closes or leaves a definition, it does not start one.
		if w := x.kw(t.text); (w == "end" || w == "exit") && x.tok(i+1).line == t.line {
			x.names[i+1] = true
			if x.tok(i+2).line == t.line {
				x.names[i+2] = true
			}
			return true
		}
	}
	return false
}

// markLine marks the rest of the line as names, which are not uses.
func (x *extractor) markLine(i int) {
	for j := i; j < len(x.toks) && x.toks[j].line == x.toks[i].line; j++ {
		x.names[j] = true
	}
}

// markHeader marks a module header up to its where or the end of its
// export list.
func (x *extractor) markHeader(i int) {
	for j := i; j < len(x.toks) && j < i+400; j++ {
		x.names[j] = true
		if x.toks[j].text == "where" {
			return
		}
		if x.toks[j].text == "(" {
			if end, ok := x.match[j]; ok {
				for k := j; k <= end; k++ {
					x.names[k] = true
				}
				if x.lang.name == "elm" {
					return
				}
				j = end
			}
		}
	}
}

func isLowerStart(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)
	return unicode.IsLower(r) || r == '_'
}

func isUpperStart(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)
	return unicode.IsUpper(r)
}

// equationDecl handles Haskell and Elm: a signature name :: Type, and an
// equation name args = body, whose arguments are not uses.
func (x *extractor) equationDecl(i int) bool {
	t := x.toks[i]
	if !t.first || t.indent != 0 || !isLowerStart(t.text) || x.lang.notCalls[t.text] {
		return false
	}
	sig := "::"
	if x.lang.name == "elm" {
		sig = ":"
	}
	if x.tok(i+1).text == sig {
		x.names[i] = true
		x.defineOnce(i, "function")
		return true
	}
	eq := -1
	for j := i + 1; j < len(x.toks) && x.toks[j].line == t.line; j++ {
		s := x.toks[j].text
		if s == "=" || s == "|" {
			eq = j
			break
		}
		if s == "::" || s == "<-" || s == "->" {
			return false
		}
	}
	if eq < 0 {
		return false
	}
	for j := i; j < eq; j++ {
		x.names[j] = true
	}
	x.defineOnce(i, "function")
	return true
}

