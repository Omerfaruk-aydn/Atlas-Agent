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

