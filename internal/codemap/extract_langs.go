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

// gdscriptDecl handles extends and class_name, which names the class the
// whole file defines.
func (x *extractor) gdscriptDecl(i int) bool {
	t := x.toks[i]
	if !t.first {
		return false
	}
	next := x.tok(i + 1)
	switch t.text {
	case "extends":
		switch next.kind {
		case tString:
			x.file.Imports = append(x.file.Imports, Import{Spec: next.text, Line: t.line})
		case tIdent:
			x.addRef(next.text, "", RefInherit, t.line)
			x.names[i+1] = true
		}
		return true
	case "class_name":
		if next.kind != tIdent {
			return false
		}
		x.names[i+1] = true
		x.addSymbol(i+1, next.text, "class", "", -1)
		s := len(x.file.Symbols) - 1
		x.file.Symbols[s].EndLine = len(x.lines)
		x.fixedEnd[s] = true
		return true
	}
	return false
}

// lispRef records (name args) as a call, alias/name as qualified, and
// ->Record and Class. as instantiations.
func (x *extractor) lispRef(i int) {
	t := x.toks[i]
	if x.tok(i-1).text != "(" {
		return
	}
	name := t.text
	if strings.HasPrefix(name, ".") || strings.HasPrefix(name, ":") || x.lang.notCalls[name] {
		return
	}
	if _, ok := x.lang.decls[name]; ok {
		return
	}
	if c := name[0]; c >= '0' && c <= '9' || len(name) > 1 && c == '-' && name[1] >= '0' && name[1] <= '9' {
		return
	}
	kind := RefCall
	if k := strings.LastIndex(name, "->"); k >= 0 && k+2 < len(name) {
		name, kind = name[k+2:], RefNew
	}
	qualifier := ""
	if k := strings.LastIndex(name, "/"); k > 0 && k < len(name)-1 {
		qualifier, name = name[:k], name[k+1:]
	}
	if len(name) > 1 && strings.HasSuffix(name, ".") {
		name, kind = strings.TrimSuffix(name, "."), RefNew
	}
	x.addRef(name, qualifier, kind, t.line)
}

// juxtaposedRef records a name in an expression of a language that calls
// functions without parentheses.
func (x *extractor) juxtaposedRef(i int) {
	t := x.toks[i]
	name := t.text
	if len(name) < 2 || x.lang.notCalls[name] {
		return
	}
	if _, ok := x.lang.decls[name]; ok {
		return
	}
	next := x.tok(i + 1).text
	if next == "." && x.tok(i+2).kind == tIdent {
		// A module qualifier: the use is the name after it.
		return
	}
	prev := x.tok(i - 1)
	if next == "=" && (t.first || prev.text == "let" || prev.text == "," || prev.text == "{" || prev.text == "and" || prev.text == "mutable") {
		// A binding or a record label.
		return
	}
	if (next == ":" || next == "::") && (prev.text == "{" || prev.text == "," || prev.text == ";" || prev.text == "(" || prev.text == "mutable" || t.first) {
		// A record field or an annotated parameter: { mass : float }.
		return
	}
	if prev.text == ":" && isLowerStart(name) && (x.lang.name == "ocaml" || x.lang.name == "fsharp") {
		// A type in an annotation: x : float.
		return
	}
	qualifier := ""
	if prev.text == "." && x.tok(i-2).kind == tIdent {
		qualifier = x.tok(i - 2).text
	}
	kind := RefCall
	if isUpperStart(name) {
		kind = RefUse
	}
	x.addRef(name, qualifier, kind, t.line)
}

// commandRef records a PowerShell command, called by its name at the
// start of a statement or pipeline stage.
func (x *extractor) commandRef(i int) bool {
	t := x.toks[i]
	prev := x.tok(i - 1)
	if !t.first && prev.text != "|" && prev.text != "=" && prev.text != "(" && prev.text != "{" && prev.text != ";" && prev.text != "&" {
		return false
	}
	w := strings.ToLower(t.text)
	if x.lang.notCalls[w] || strings.HasPrefix(w, "$") || strings.HasPrefix(w, "-") || x.tok(i+1).text == "=" {
		return false
	}
	if _, ok := x.lang.decls[w]; ok {
		return false
	}
	x.addRef(t.text, "", RefCall, t.line)
	return true
}

// webRef records what a script uses of the page: classes set through
// className or classList, elements found by id or selector, and classes of
// an imported CSS module (styles.button).
func (x *extractor) webRef(i int) {
	t := x.toks[i]
	str := func(k int) (lexToken, bool) {
		s := x.tok(k)
		return s, s.kind == tString
	}
	if kind, ok := x.domHelpers[t.text]; ok && x.tok(i-1).text != "." {
		// $<HTMLButtonElement>('skip'): a call of the script's own lookup
		// helper, type arguments and all.
		k := i + 1
		if x.tok(k).text == "<" {
			for k < len(x.toks) && x.toks[k].text != ">" && x.toks[k].line == t.line {
				k++
			}
			k++
		}
		if s, ok := str(k + 1); ok && x.tok(k).text == "(" {
			switch {
			case kind == RefDOM && isCSSName(s.text):
				addRef(x.file, Ref{Name: s.text, Kind: RefDOM, Line: s.line})
			case kind != RefDOM:
				selectorRefs(x.file, s.text, s.line)
			}
		}
		return
	}
	switch t.text {
	case "className", "class":
		if x.tok(i+1).text != "=" {
			break
		}
		k := i + 2
		if x.tok(k).text == "{" {
			k++
		}
		if s, ok := str(k); ok {
			classRefs(x.file, s.text, s.line)
		}
	case "classList":
		if x.tok(i+1).text != "." || x.tok(i+3).text != "(" {
			break
		}
		switch x.tok(i + 2).text {
		case "add", "remove", "toggle", "contains", "replace":
			for k := i + 4; ; k++ {
				s := x.tok(k)
				if s.kind == tString {
					classRefs(x.file, s.text, s.line)
				} else if s.text != "," {
					break
				}
			}
		}
	case "getElementById":
		if s, ok := str(i + 2); ok && x.tok(i+1).text == "(" && isCSSName(s.text) {
			addRef(x.file, Ref{Name: s.text, Kind: RefDOM, Line: s.line})
		}
	case "getElementsByClassName":
		if s, ok := str(i + 2); ok && x.tok(i+1).text == "(" {
			classRefs(x.file, s.text, s.line)
		}
	case "querySelector", "querySelectorAll", "closest", "matches", "$", "jQuery":
		if s, ok := str(i + 2); ok && x.tok(i+1).text == "(" && (t.text[0] != '$' && t.text != "jQuery" || x.tok(i-1).text != ".") {
			selectorRefs(x.file, s.text, s.line)
		}
	}
	if x.tok(i-1).text == "." || !x.cssModule(t.text) {
		return
	}
	switch x.tok(i + 1).text {
	case ".":
		if n := x.tok(i + 2); n.kind == tIdent {
			addRef(x.file, Ref{Name: n.text, Qualifier: t.text, Kind: RefCSS, Line: n.line})
		}
	case "[":
		if s, ok := str(i + 2); ok {
			addRef(x.file, Ref{Name: s.text, Qualifier: t.text, Kind: RefCSS, Line: s.line})
		}
	}
}

// findDOMHelpers finds the functions that look an element up by their
// parameter: const $ = (id) => document.getElementById(id), function
// byId(id) { return document.getElementById(id) }.
func (x *extractor) findDOMHelpers() map[string]string {
	var out map[string]string
	for i, t := range x.toks {
		var kind string
		switch t.text {
		case "getElementById":
			kind = RefDOM
		case "querySelector", "querySelectorAll":
			kind = "selector"
		default:
			continue
		}
		if x.tok(i+1).text != "(" || x.tok(i+2).kind != tIdent || x.tok(i+3).text != ")" {
			continue
		}
		param := x.toks[i+2].text
		// The definition is just before, on the same line or the two
		// before it, and takes param.
		for k := i - 1; k >= 0 && k > i-40 && x.toks[k].line >= t.line-2; k-- {
			w := x.toks[k].text
			if w != "const" && w != "let" && w != "var" && w != "function" || x.tok(k+1).kind != tIdent {
				continue
			}
			takesParam := false
			for j := k + 2; j < i; j++ {
				if x.toks[j].text == param {
					takesParam = true
					break
				}
			}
			if takesParam {
				if out == nil {
					out = map[string]string{}
				}
				out[x.toks[k+1].text] = kind
			}
			break
		}
	}
	return out
}

// cssModule reports whether name is bound to an imported stylesheet.
func (x *extractor) cssModule(name string) bool {
	for _, imp := range x.file.Imports {
		switch strings.ToLower(path.Ext(imp.Spec)) {
		case ".css", ".scss", ".sass", ".less":
		default:
			continue
		}
		if imp.Module == name {
			return true
		}
		for _, b := range imp.Bindings {
			if b.Local == name {
				return true
			}
		}
	}
	return false
}

// isCSSName reports whether s is a class or id name.
func isCSSName(s string) bool {
	if s == "" || len(s) > 120 {
		return false
	}
	for i, r := range s {
		switch {
		case r == '_' || r == '-' || unicode.IsLetter(r):
		case unicode.IsDigit(r) && i > 0:
		default:
			return false
		}
	}
	return s != "-"
}

// classRefs records each class in a space-separated class list.
func classRefs(f *File, list string, line int) {
	for _, c := range strings.Fields(list) {
		if isCSSName(c) {
			addRef(f, Ref{Name: c, Kind: RefCSS, Line: line})
		}
	}
}

// selectorRefs records the classes and ids a CSS selector names.
func selectorRefs(f *File, selector string, line int) {
	for i := 0; i < len(selector); i++ {
		c := selector[i]
		if c != '.' && c != '#' {
			continue
		}
		j := i + 1
		for j < len(selector) && (selector[j] == '-' || selector[j] == '_' || selector[j] >= 'a' && selector[j] <= 'z' || selector[j] >= 'A' && selector[j] <= 'Z' || selector[j] >= '0' && selector[j] <= '9' || selector[j] >= 0x80) {
			j++
		}
		name := selector[i+1 : j]
		if isCSSName(name) {
			kind := RefCSS
			if c == '#' {
				kind = RefDOM
			}
			addRef(f, Ref{Name: name, Kind: kind, Line: line})
		}
		i = j - 1
	}
}

// moreImports reads the imports of the languages importAt does not.
func (x *extractor) moreImports(i int) bool {
	t := x.toks[i]
	w := x.kw(t.text)
	switch x.lang.name {
	case "haskell", "elm":
		if w == "import" && t.first {
			x.mlImport(i)
			return true
		}
	case "ocaml", "fsharp":
		if w == "open" || w == "include" && x.lang.name == "ocaml" && t.first {
			x.dottedImport(i+1, ".", false)
			x.markLine(i)
			return true
		}
	case "clojure", "lisp":
		return x.lispImport(i)
	case "erlang":
		return x.erlangImport(i)
	case "gleam":
		if w == "import" && t.first {
			x.gleamImport(i)
			return true
		}
	case "nim":
		if t.first && (w == "import" || w == "include" || w == "from") {
			x.nimImport(i)
			return true
		}
	case "vlang":
		if w == "import" && t.first {
			x.dottedImport(i+1, ".", true)
			x.markLine(i)
			return true
		}
	case "solidity":
		if w == "import" {
			return x.jsImport(i)
		}
	case "verilog":
		if t.text == "`" && x.tok(i+1).text == "include" && x.tok(i+2).kind == tString {
			x.file.Imports = append(x.file.Imports, Import{Spec: x.toks[i+2].text, Line: t.line})
			return true
		}
		if w == "import" {
			x.dottedImport(i+1, "::", false)
			x.markLine(i)
			return true
		}
	case "vhdl":
		switch w {
		case "use":
			var parts []string
			for k := i + 1; x.tok(k).kind == tIdent; k += 2 {
				parts = append(parts, strings.ToLower(x.toks[k].text))
				if x.tok(k+1).text != "." {
					break
				}
			}
			if len(parts) >= 2 && parts[0] == "work" {
				x.file.Imports = append(x.file.Imports, Import{Spec: parts[1], Line: t.line})
			} else if len(parts) > 0 {
				x.file.Imports = append(x.file.Imports, Import{Spec: strings.Join(parts, "."), Line: t.line, Module: "_"})
			}
			x.markLine(i)
			return true
		case "library":
			x.markLine(i)
			return true
		}
	case "powershell":
		switch {
		case t.first && t.text == ".":
			x.psDotSource(i)
			return true
		case w == "import-module" || w == "using" && x.kw(x.tok(i+1).text) == "module":
			if spec := psModuleSpec(x.lines[t.line-1]); spec != "" {
				x.file.Imports = append(x.file.Imports, Import{Spec: psPath(spec), Line: t.line})
			}
			x.markLine(i)
			return true
		}
	case "vb":
		if w == "imports" && t.first {
			x.dottedImport(i+1, ".", false)
			x.markLine(i)
			return true
		}
	case "pascal":
		if w == "uses" {
			x.pascalUses(i)
			return true
		}
	case "fortran":
		if w == "use" && t.first {
			k := i + 1
			for x.tok(k).text == "," || x.tok(k).text == "::" || x.kw(x.tok(k).text) == "intrinsic" || x.kw(x.tok(k).text) == "non_intrinsic" {
				k++
			}
			if x.tok(k).kind == tIdent {
				x.file.Imports = append(x.file.Imports, Import{Spec: strings.ToLower(x.toks[k].text), Line: t.line})
			}
			x.markLine(i)
			return true
		}
	case "gdscript":
		if (w == "preload" || w == "load") && x.tok(i+1).text == "(" && x.tok(i+2).kind == tString && strings.HasSuffix(x.toks[i+2].text, ".gd") {
			x.file.Imports = append(x.file.Imports, Import{Spec: x.toks[i+2].text, Line: t.line})
			return true
		}
	}
	return false
}

// mlImport reads a Haskell or Elm import: import qualified A.B as C
// (x, y) hiding (z), import A.B as C exposing (x).
func (x *extractor) mlImport(i int) {
	line := x.toks[i].line
	k := i + 1
	qualified := false
	if x.tok(k).text == "qualified" {
		qualified = true
		k++
	}
	var parts []string
	for x.tok(k).kind == tIdent && x.tok(k).line == line {
		parts = append(parts, x.toks[k].text)
		if x.tok(k+1).text == "." && x.tok(k+2).kind == tIdent {
			k += 2
			continue
		}
		k++
		break
	}
	end := k
	defer func() {
		for j := i; j <= end && j < len(x.toks); j++ {
			x.names[j] = true
		}
	}()
	if len(parts) == 0 {
		return
	}
	spec := strings.Join(parts, ".")
	alias := parts[len(parts)-1]
	if x.tok(k).text == "qualified" {
		qualified = true
		k++
	}
	if x.tok(k).text == "as" && x.tok(k+1).kind == tIdent {
		alias = x.toks[k+1].text
		k += 2
	}
	everything := !qualified
	var bindings []Binding
	if x.tok(k).text == "exposing" {
		k++
		everything = false
	}
	hiding := false
	if x.tok(k).text == "hiding" {
		hiding = true
		k++
	}
	if x.tok(k).text == "(" {
		if close, ok := x.match[k]; ok {
			end = close
			if hiding {
				everything = true
			} else {
				everything = false
				for j := k + 1; j < close; j++ {
					s := x.toks[j]
					if s.text == ".." && x.tok(j-1).text == "(" && j == k+1 {
						everything = true
					}
					if s.kind == tIdent && x.tok(j-1).text != "." {
						bindings = append(bindings, Binding{Local: s.text, Remote: s.text})
					}
					if s.text == "(" {
						// Type(..) or Type(A, B): its constructors.
						if c, ok := x.match[j]; ok {
							j = c
						}
					}
				}
			}
		}
	} else {
		end = k
	}
	x.file.Imports = append(x.file.Imports, Import{Spec: spec, Line: line, Module: alias, Bindings: bindings})
	if everything {
		// An unqualified import without a list brings every name in.
		x.file.Imports = append(x.file.Imports, Import{Spec: spec, Line: line})
	}
}

// lispImport reads (:require [a.b :as c :refer [d]]), (require 'x) and
// (require "x.rkt"), and the ns name of a Clojure file.
func (x *extractor) lispImport(i int) bool {
	t := x.toks[i]
	if x.tok(i-1).text != "(" {
		return false
	}
	switch t.text {
	case "ns":
		if x.tok(i+1).kind == tIdent {
			x.names[i+1] = true
		}
		return false
	case ":require", "require", ":use", "use", ":import":
	default:
		return false
	}
	end, ok := x.match[i-1]
	if !ok {
		return false
	}
	for j := i; j <= end; j++ {
		x.names[j] = true
	}
	if t.text == ":import" {
		return true
	}
	add := func(spec string, line int) *Import {
		x.file.Imports = append(x.file.Imports, Import{Spec: spec, Line: line})
		return &x.file.Imports[len(x.file.Imports)-1]
	}
	for j := i + 1; j < end; j++ {
		s := x.toks[j]
		switch {
		case s.kind == tString:
			add(s.text, s.line)
		case s.text == "[" || s.text == "(":
			close, ok := x.match[j]
			if !ok || x.tok(j+1).kind != tIdent {
				continue
			}
			imp := add(x.toks[j+1].text, x.toks[j+1].line)
			plain := true
			for k := j + 2; k < close; k++ {
				switch x.toks[k].text {
				case ":as":
					if x.tok(k+1).kind == tIdent {
						imp.Module = x.toks[k+1].text
						plain = false
					}
				case ":refer":
					if x.tok(k+1).text == ":all" {
						plain = true
					} else if x.tok(k+1).text == "[" {
						plain = false
						if c, ok := x.match[k+1]; ok {
							for r := k + 2; r < c; r++ {
								if x.toks[r].kind == tIdent {
									imp.Bindings = append(imp.Bindings, Binding{Local: x.toks[r].text, Remote: x.toks[r].text})
								}
							}
						}
					}
				}
			}
			if !plain && imp.Module == "" && len(imp.Bindings) == 0 {
				imp.Module = "_"
			}
			j = close
		case s.kind == tIdent && !strings.HasPrefix(s.text, ":"):
			add(s.text, s.line)
		}
	}
	return true
}

// erlangImport reads -import(mod, [f/1]) and -include("x.hrl").
func (x *extractor) erlangImport(i int) bool {
	t := x.toks[i]
	if t.text != "-" || !t.first || x.tok(i+2).text != "(" {
		return false
	}
	switch x.tok(i + 1).text {
	case "include", "include_lib":
		if s := x.tok(i + 3); s.kind == tString {
			x.file.Imports = append(x.file.Imports, Import{Spec: s.text, Line: t.line})
		}
	case "import":
		mod := x.tok(i + 3)
		if mod.kind != tIdent {
			return false
		}
		imp := Import{Spec: mod.text, Line: t.line}
		if end, ok := x.match[i+2]; ok {
			for k := i + 4; k < end; k++ {
				if x.toks[k].kind == tIdent && x.tok(k+1).text == "/" {
					imp.Bindings = append(imp.Bindings, Binding{Local: x.toks[k].text, Remote: x.toks[k].text})
				}
			}
		}
		x.file.Imports = append(x.file.Imports, imp)
	default:
		return false
	}
	return true
}

// gleamImport reads import a/b.{c, type D} as e.
func (x *extractor) gleamImport(i int) {
	line := x.toks[i].line
	k := i + 1
	var parts []string
	for x.tok(k).kind == tIdent {
		parts = append(parts, x.toks[k].text)
		if x.tok(k+1).text != "/" {
			k++
			break
		}
		k += 2
	}
	if len(parts) == 0 {
		return
	}
	imp := Import{Spec: strings.Join(parts, "/"), Line: line, Module: parts[len(parts)-1]}
	if x.tok(k).text == "." && x.tok(k+1).text == "{" {
		if end, ok := x.match[k+1]; ok {
			for j := k + 2; j < end; j++ {
				if s := x.toks[j]; s.kind == tIdent && s.text != "type" && s.text != "as" && x.tok(j-1).text != "as" {
					imp.Bindings = append(imp.Bindings, Binding{Local: s.text, Remote: s.text})
				}
			}
			k = end + 1
		}
	}
	if x.tok(k).text == "as" && x.tok(k+1).kind == tIdent {
		imp.Module = x.toks[k+1].text
	}
	x.file.Imports = append(x.file.Imports, imp)
	x.markLine(i)
}

// nimImport reads import a, b/c, std/[d, e], from a import b and include a.
func (x *extractor) nimImport(i int) {
	t := x.toks[i]
	line := t.line
	from := x.kw(t.text) == "from"
	k := i + 1
	readPath := func() []string {
		var prefix []string
		for x.tok(k).kind == tIdent && x.tok(k).line == line {
			prefix = append(prefix, x.toks[k].text)
			k++
			if x.tok(k).text != "/" {
				return []string{strings.Join(prefix, "/")}
			}
			k++
			if x.tok(k).text == "[" {
				end, ok := x.match[k]
				if !ok {
					return nil
				}
				var out []string
				for j := k + 1; j < end; j++ {
					if x.toks[j].kind == tIdent {
						out = append(out, strings.Join(append(append([]string(nil), prefix...), x.toks[j].text), "/"))
					}
				}
				k = end + 1
				return out
			}
		}
		if len(prefix) > 0 {
			return []string{strings.Join(prefix, "/")}
		}
		return nil
	}
	if from {
		specs := readPath()
		if len(specs) == 0 || x.tok(k).text != "import" {
			return
		}
		imp := Import{Spec: specs[0], Line: line}
		for j := k + 1; j < len(x.toks) && x.toks[j].line == line; j++ {
			if x.toks[j].kind == tIdent {
				imp.Bindings = append(imp.Bindings, Binding{Local: x.toks[j].text, Remote: x.toks[j].text})
			}
		}
		x.file.Imports = append(x.file.Imports, imp)
		x.markLine(i)
		return
	}
	for {
		for _, spec := range readPath() {
			// Nim's import brings every exported name in, and still
			// allows module.name.
			x.file.Imports = append(x.file.Imports, Import{Spec: spec, Line: line}, Import{Spec: spec, Line: line, Module: path.Base(spec)})
		}
		if x.tok(k).text == "as" {
			k += 2
		}
		if x.tok(k).text != "," {
			break
		}
		k++
	}
	x.markLine(i)
}

// pascalUses reads uses A, B.C in 'path', D;
func (x *extractor) pascalUses(i int) {
	k := i + 1
	for k < len(x.toks) && x.toks[k].text != ";" {
		if x.toks[k].kind != tIdent {
			k++
			continue
		}
		parts := []string{x.toks[k].text}
		line := x.toks[k].line
		for x.tok(k+1).text == "." && x.tok(k+2).kind == tIdent {
			k += 2
			parts = append(parts, x.toks[k].text)
		}
		spec := strings.Join(parts, ".")
		if x.kw(x.tok(k+1).text) == "in" && x.tok(k+2).kind == tString {
			spec = strings.ReplaceAll(x.toks[k+2].text, "\\", "/")
			k += 2
		}
		x.file.Imports = append(x.file.Imports, Import{Spec: spec, Line: line})
		k++
	}
	for j := i; j <= k && j < len(x.toks); j++ {
		x.names[j] = true
	}
}

// psDotSource reads . ./lib.ps1 and . $PSScriptRoot\lib.ps1.
func (x *extractor) psDotSource(i int) {
	t := x.toks[i]
	line := x.lines[t.line-1]
	rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "."))
	if fields := strings.Fields(rest); len(fields) > 0 {
		x.file.Imports = append(x.file.Imports, Import{Spec: psPath(fields[0]), Line: t.line})
	}
	x.markLine(i)
}

// psModuleSpec returns the module an Import-Module or using module line
// names: the first argument that is not a parameter, or the value of
// -Name.
func psModuleSpec(line string) string {
	fields := strings.Fields(line)
	start := -1
	for k, f := range fields {
		if lower := strings.ToLower(f); lower == "import-module" || lower == "module" && k > 0 && strings.EqualFold(fields[k-1], "using") {
			start = k + 1
			break
		}
	}
	if start < 0 {
		return ""
	}
	for k := start; k < len(fields); k++ {
		f := strings.TrimRight(fields[k], ";")
		lower := strings.ToLower(f)
		switch {
		case lower == "-name" || lower == "-path":
			if k+1 < len(fields) {
				return strings.TrimRight(fields[k+1], ";")
			}
		case strings.HasPrefix(f, "-"):
			// A switch such as -Force; one with a value takes it along.
			if k+1 < len(fields) && strings.EqualFold(f, "-Prefix") {
				k++
			}
		case f != "" && f != "|":
			return f
		}
	}
	return ""
}

// psPath turns a PowerShell path into a slash path relative to the script.
func psPath(p string) string {
	p = strings.Trim(p, `"'`)
	for _, root := range []string{"$PSScriptRoot", "${PSScriptRoot}"} {
		if rest, ok := strings.CutPrefix(p, root); ok {
			p = "." + rest
		}
	}
	return strings.ReplaceAll(p, "\\", "/")
}
