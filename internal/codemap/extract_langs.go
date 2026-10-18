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

// defineOnce records the definition named at i unless an earlier clause
// already did.
func (x *extractor) defineOnce(i int, kind string) {
	name := x.toks[i].text
	if x.defined[name] {
		return
	}
	x.defined[name] = true
	container := ""
	if x.lang.name == "erlang" {
		container = strings.TrimSuffix(path.Base(x.file.Path), path.Ext(x.file.Path))
	}
	x.addSymbol(i, name, kind, container, -1)
}

// erlangDecl handles name(Args) -> clauses and -record and -define
// attributes.
func (x *extractor) erlangDecl(i int) bool {
	t := x.toks[i]
	if prev := x.tok(i - 1); prev.text == "-" && prev.first {
		// An attribute: -module(m), -export([...]), -record(name, ...).
		if (t.text == "record" || t.text == "define") && x.tok(i+1).text == "(" && x.tok(i+2).kind == tIdent {
			kind := "struct"
			if t.text == "define" {
				kind = "macro"
			}
			x.names[i+2] = true
			x.addSymbol(i+2, x.toks[i+2].text, kind, "", -1)
		}
		if end, ok := x.match[i+1]; ok && x.tok(i+1).text == "(" && t.text != "define" {
			for j := i; j <= end; j++ {
				x.names[j] = true
			}
		}
		return true
	}
	if !t.first || t.indent != 0 || !isLowerStart(t.text) || x.tok(i+1).text != "(" {
		return false
	}
	end, ok := x.match[i+1]
	if !ok {
		return false
	}
	for j := end + 1; j < len(x.toks) && j < end+60; j++ {
		switch x.toks[j].text {
		case "->":
			x.names[i] = true
			x.defineOnce(i, "function")
			return true
		case ".", ";":
			return false
		}
	}
	return false
}

// nimTypeDecl handles a type section entry: Name* = object, ref object,
// enum, tuple, concept, distinct.
func (x *extractor) nimTypeDecl(i int) bool {
	t := x.toks[i]
	if !t.first || t.indent == 0 {
		return false
	}
	k := i + 1
	if x.tok(k).text == "*" {
		k++
	}
	if x.tok(k).text == "[" {
		if end, ok := x.match[k]; ok {
			k = end + 1
		}
	}
	if x.tok(k).text != "=" {
		return false
	}
	v := x.tok(k + 1).text
	if v == "ref" || v == "ptr" {
		if x.tok(k+2).text == "object" {
			v = "object"
		}
	}
	kind := map[string]string{"object": "struct", "enum": "enum", "tuple": "struct", "concept": "interface", "distinct": "type", "ref": "type", "ptr": "type"}[v]
	if kind == "" {
		return false
	}
	x.names[i] = true
	x.addSymbol(i, t.text, kind, "", -1)
	for j := k + 1; j < len(x.toks) && x.toks[j].line == t.line; j++ {
		if x.toks[j].text == "of" && x.tok(j+1).kind == tIdent {
			x.addRef(x.toks[j+1].text, "", RefInherit, t.line)
			x.names[j+1] = true
		}
	}
	return true
}

// pascalTypeDecl handles TFoo = class(TBase), record, interface and object.
func (x *extractor) pascalTypeDecl(i int) bool {
	t := x.toks[i]
	if x.tok(i+1).text != "=" {
		return false
	}
	k := i + 2
	if x.kw(x.tok(k).text) == "packed" {
		k++
	}
	kind := map[string]string{"class": "class", "record": "struct", "interface": "interface", "object": "class"}[x.kw(x.tok(k).text)]
	if kind == "" {
		return false
	}
	next := x.tok(k + 1)
	if next.text == ";" || x.kw(next.text) == "of" {
		// A forward declaration or a class reference type.
		return true
	}
	x.names[i] = true
	x.addSymbol(i, t.text, kind, "", -1)
	if next.text == "(" {
		if end, ok := x.match[k+1]; ok {
			for j := k + 2; j < end; j++ {
				if x.toks[j].kind == tIdent {
					x.addRef(x.toks[j].text, "", RefInherit, x.toks[j].line)
					x.names[j] = true
				}
			}
		}
	}
	return true
}

// verilogInstance records Module #(...) name (...) and Module name (...):
// an instance is a use of the module.
func (x *extractor) verilogInstance(i int) {
	t := x.toks[i]
	if !t.first && x.tok(i-1).text != ";" {
		return
	}
	if x.lang.notCalls[t.text] {
		return
	}
	if _, ok := x.lang.decls[t.text]; ok {
		return
	}
	next := x.tok(i + 1)
	if next.text == "#" || next.kind == tIdent && x.tok(i+2).text == "(" {
		x.addRef(t.text, "", RefNew, t.line)
		x.names[i] = true
		if next.kind == tIdent {
			x.names[i+1] = true
		}
	}
}

// vhdlDecl handles architectures, component declarations, instances and
// the end of a unit.
func (x *extractor) vhdlDecl(i int) bool {
	t := x.toks[i]
	switch x.kw(t.text) {
	case "architecture":
		if x.tok(i+1).kind == tIdent && x.kw(x.tok(i+2).text) == "of" && x.tok(i+3).kind == tIdent {
			x.names[i+1], x.names[i+3] = true, true
			x.addSymbol(i+1, x.toks[i+1].text, "impl", x.toks[i+3].text, -1)
			x.addRef(x.toks[i+3].text, "", RefUse, t.line)
			return true
		}
	case "component":
		if x.tok(i+1).kind == tIdent {
			x.names[i+1] = true
			x.addRef(x.toks[i+1].text, "", RefUse, t.line)
			return true
		}
	case "entity":
		if x.tok(i-1).text == ":" {
			// u1 : entity work.counter
			k := i + 1
			for x.tok(k).kind == tIdent && x.tok(k+1).text == "." && x.tok(k+2).kind == tIdent {
				k += 2
			}
			if x.tok(k).kind == tIdent {
				x.addRef(x.toks[k].text, "", RefNew, t.line)
				for j := i + 1; j <= k; j++ {
					x.names[j] = true
				}
			}
			return true
		}
	}
	if x.tok(i-1).text == ":" {
		// u1 : counter port map (...)
		if w := x.kw(x.tok(i + 1).text); w == "port" || w == "generic" {
			x.addRef(t.text, "", RefNew, t.line)
			x.names[i] = true
			return true
		}
	}
	return false
}

