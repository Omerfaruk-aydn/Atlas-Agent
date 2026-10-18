package codemap

import (
	"crypto/sha256"
	"encoding/hex"
	"path"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// maxSignature bounds the stored first line of a definition.
const maxSignature = 160

// classLike kinds contain methods and other definitions.
var classLike = map[string]bool{"class": true, "struct": true, "interface": true, "impl": true, "namespace": true, "enum": true}

// continuation tokens can start a line that still belongs to the
// definition header above it.
var continuation = words("{ ) ] . , : => -> extends implements where throws with ? && || + - * | & = < >")

// Hash returns the content hash the map keys files by.
func Hash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:16])
}

// Extract reads one source file. It never fails: a file it cannot make
// sense of yields fewer symbols, not an error.
func Extract(file, content string) File {
	out := File{Path: file, Language: LanguageOf(file), Hash: Hash(content), Lines: strings.Count(content, "\n") + 1}
	ext := strings.ToLower(path.Ext(file))
	if ext == ".v" && isVlang(content) {
		out.Language = "vlang"
	}
	switch Category(out.Language) {
	case "", CategoryAsset:
		return out
	case CategoryDoc:
		extractDoc(&out, content)
		return out
	}
	switch out.Language {
	case "go":
		extractGo(&out, content)
	case "sql":
		extractSQL(&out, content)
	case "graphql":
		extractGraphQL(&out, content)
	case "protobuf", "thrift":
		extractIDL(&out, content)
	case "prisma":
		extractPrisma(&out, content)
	case "openapi":
		extractOpenAPI(&out, content)
	case "terraform":
		extractTerraform(&out, content)
	case "css", "scss", "less":
		extractCSS(&out, out.Language, content)
	case "html", "template":
		extractHTML(&out, content)
	case "notebook":
		extractNotebook(&out, content)
	case "godot":
		extractGodot(&out, content)
	case "npm", "make", "just", "taskfile", "docker", "compose", "workflow", "gitlab-ci", "pyproject", "cargo":
		extractBuild(&out, content)
	default:
		if ext == ".vue" || ext == ".svelte" || ext == ".astro" {
			extractComponent(&out, ext, content)
			break
		}
		extractCode(&out, languages[out.Language], content)
	}
	return out
}

// extractCode reads content as lang and adds what it defines and uses to
// out. Markup and notebooks call it for the code they embed.
func extractCode(out *File, lang *language, content string) {
	if lang == nil {
		return
	}
	x := extractor{
		lang:  lang,
		toks:  lex(content, lang),
		lines: strings.Split(content, "\n"),
		file:  out,
		first: len(out.Symbols),
	}
	x.run()
}

// isVlang tells V from Verilog, which share the .v extension.
func isVlang(content string) bool {
	if strings.Contains(content, "endmodule") {
		return false
	}
	for _, line := range strings.SplitN(content, "\n", 200) {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "fn ") || strings.HasPrefix(line, "pub fn ") || strings.HasPrefix(line, "struct ") {
			return true
		}
	}
	return false
}

// scriptOnly blanks everything outside <script> blocks, keeping the line
// numbers.
func scriptOnly(content string) string {
	var b strings.Builder
	rest := content
	for {
		start := strings.Index(rest, "<script")
		if start < 0 {
			b.WriteString(blankLines(rest))
			return b.String()
		}
		open := strings.IndexByte(rest[start:], '>')
		if open < 0 {
			b.WriteString(blankLines(rest))
			return b.String()
		}
		open += start + 1
		b.WriteString(blankLines(rest[:open]))
		end := strings.Index(rest[open:], "</script>")
		if end < 0 {
			b.WriteString(rest[open:])
			return b.String()
		}
		b.WriteString(rest[open : open+end])
		rest = rest[open+end:]
	}
}

func blankLines(s string) string {
	return strings.Repeat("\n", strings.Count(s, "\n"))
}

type extractor struct {
	lang  *language
	toks  []lexToken
	lines []string
	file  *File
	// match maps an opening bracket's index to its closing one.
	match map[int]int
	// names are the token indexes of definition names, which are not
	// uses.
	names map[int]bool
	// indents holds each symbol's line indentation, for indent languages.
	indents []int
	starts  []int
	// inArgs is set while the current brace scope is inside parentheses.
	inArgs bool
	// first is the index of this extractor's first symbol in the file,
	// which already holds what other passes found (markup around a
	// script).
	first int
	// fixedEnd marks symbols whose end is known, such as a class that is
	// the whole file, so indentation does not reset it.
	fixedEnd map[int]bool
	// defined holds the names already defined, for languages that define
	// a function over several clauses.
	defined map[string]bool
	// domHelpers are a script's own functions that find an element by
	// their argument, as RefDOM for an id or "selector".
	domHelpers map[string]string
}

// kw returns a word as the language's keyword tables spell it.
func (x *extractor) kw(s string) string {
	if x.lang.caseInsensitive {
		return strings.ToLower(s)
	}
	return s
}

func (x *extractor) run() {
	x.match = matchBrackets(x.toks)
	x.names = map[int]bool{}
	x.fixedEnd = map[int]bool{}
	x.defined = map[string]bool{}
	if x.lang.name == "typescript" || x.lang.name == "javascript" {
		x.domHelpers = x.findDOMHelpers()
	}
	// depth counts open parentheses per brace scope; inArgs marks a scope
	// opened inside parentheses, such as an object literal passed as an
	// argument, whose properties are not definitions of the file.
	depth := []int{0}
	inArgs := []bool{false}
	for i := 0; i < len(x.toks); i++ {
		t := x.toks[i]
		switch t.text {
		case "(", "[":
			depth[len(depth)-1]++
		case ")", "]":
			if depth[len(depth)-1] > 0 {
				depth[len(depth)-1]--
			}
		case "{":
			inArgs = append(inArgs, inArgs[len(inArgs)-1] || depth[len(depth)-1] > 0)
			depth = append(depth, 0)
		case "}":
			if len(depth) > 1 {
				depth = depth[:len(depth)-1]
				inArgs = inArgs[:len(inArgs)-1]
			}
		}
		x.inArgs = inArgs[len(inArgs)-1]
		if t.kind == tString {
			x.stringAt(i)
			continue
		}
		if t.kind == tPunct && t.text != "<" && t.text != "#" && t.text != "@" && t.text != "-" && t.text != "+" && t.text != "`" && t.text != "." {
			continue
		}
		top := depth[len(depth)-1] == 0
		if x.importAt(i) {
			continue
		}
		if t.kind == tIdent {
			if x.langDeclAt(i) {
				continue
			}
			x.declAt(i, top)
		} else if x.lang.name == "objc" {
			x.objcMethod(i)
		}
		x.refAt(i)
	}
	if x.lang.family == indent {
		x.indentEnds()
	}
	x.containers()
}

// matchBrackets pairs each opening bracket with its closing one.
func matchBrackets(toks []lexToken) map[int]int {
	match := map[int]int{}
	var stack []int
	for i, t := range toks {
		if t.kind != tPunct {
			continue
		}
		switch t.text {
		case "{", "(", "[":
			stack = append(stack, i)
		case "}", ")", "]":
			want := map[string]string{"}": "{", ")": "(", "]": "["}[t.text]
			// Pop to the nearest matching opener; an unbalanced file
			// still pairs what it can.
			for k := len(stack) - 1; k >= 0; k-- {
				if toks[stack[k]].text == want {
					match[stack[k]] = i
					stack = stack[:k]
					break
				}
			}
		}
	}
	return match
}

func (x *extractor) tok(i int) lexToken {
	if i < 0 || i >= len(x.toks) {
		return lexToken{kind: tPunct}
	}
	return x.toks[i]
}

// lineIndent is the indentation of line.
func (x *extractor) lineIndent(line int) int {
	if line < 1 || line > len(x.lines) {
		return 0
	}
	n := 0
	for _, c := range x.lines[line-1] {
		switch c {
		case ' ':
			n++
		case '\t':
			n += 4
		default:
			return n
		}
	}
	return n
}

// declAt records a definition whose name is at or after token i.
func (x *extractor) declAt(i int, top bool) {
	if x.names[i] {
		return
	}
	t := x.toks[i]
	prev := x.tok(i - 1)
	if kind, ok := x.lang.decls[x.kw(t.text)]; ok && prev.text != "." && prev.text != "?." {
		if x.lang.topLevel[x.kw(t.text)] && (!t.first || t.indent > 0) {
			return
		}
		if x.keywordDecl(i, kind) {
			return
		}
	}
	if !top {
		return
	}
	if x.lang.arrows && x.arrowDecl(i) {
		return
	}
	if x.lang.cLike {
		x.cLikeDecl(i)
	}
}

// keywordDecl handles a definition introduced by a keyword.
func (x *extractor) keywordDecl(i int, kind string) bool {
	needsCall := strings.HasSuffix(kind, "?")
	kind = strings.TrimSuffix(kind, "?")
	k := i + 1
	for x.tok(k).text == "*" || x.tok(k).text == "&" || x.lang.declSkip[x.kw(x.tok(k).text)] {
		k++
	}
	if x.lang.lisp && x.tok(k).text == "(" {
		// (define (name args) ...).
		k++
	}
	container := ""
	if x.lang.receivers && x.tok(k).text == "(" {
		// fn (r Receiver) name(): a method of Receiver.
		if end, ok := x.match[k]; ok {
			for j := k + 1; j < end; j++ {
				if x.toks[j].kind == tIdent && x.toks[j].text != "mut" {
					container = x.toks[j].text
				}
			}
			k = end + 1
		}
	}
	if x.tok(k).text == "<" {
		k = x.skipAngles(k)
	}
	name := x.tok(k)
	if name.kind != tIdent {
		return false
	}
	if name.line != x.toks[i].line && !x.lang.lisp {
		// The keyword ends its line; what follows is not its name.
		return false
	}
	if _, isKeyword := x.lang.decls[x.kw(name.text)]; isKeyword && x.kw(name.text) != x.kw(x.toks[i].text) {
		// class func, companion object: the next word is the keyword.
		return false
	}
	if x.lang.selfPrefix && kind == "method" && x.tok(k+1).text == "." && x.tok(k+2).kind == tIdent {
		// member this.Name: this names the instance, not a container.
		k += 2
		name = x.toks[k]
	}
	nameAt := k
	text := name.text
	switch {
	case kind == "namespace" || x.lang.name == "elixir" && kind == "impl":
		for (x.tok(k+1).text == "." || x.tok(k+1).text == "::") && x.tok(k+2).kind == tIdent {
			k += 2
			text += "." + x.toks[k].text
		}
	case (x.tok(k+1).text == "." || x.tok(k+1).text == ":" && x.lang.name == "lua") && x.tok(k+2).kind == tIdent && kind == "function":
		// Kotlin fun Foo.bar, Lua function M.name and M:name.
		container, text, nameAt = name.text, x.toks[k+2].text, k+2
		k += 2
	}
	if needsCall && x.tok(k+1).text != "(" {
		return false
	}
	if x.lang.name == "typescript" || x.lang.name == "javascript" {
		switch x.toks[i].text {
		case "type", "namespace", "module":
			if next := x.tok(k + 1).text; next != "=" && next != "<" && next != "{" && next != "." {
				return false
			}
		}
	}
	body := -1
	if x.lang.lisp {
		body = i - 1
	} else if x.lang.family == braces {
		body = x.findBody(k)
		if body < 0 && (x.lang.name == "c" || x.lang.name == "cpp" || x.lang.name == "objc") && kind != "function" {
			// struct foo *p; uses a type, it does not define one.
			return false
		}
		if x.lang.name == "rust" && x.toks[i].text == "mod" && body < 0 {
			x.file.Imports = append(x.file.Imports, Import{Spec: "mod:" + text, Line: name.line})
			x.names[nameAt] = true
			return true
		}
	}
	if kind == "impl" && x.lang.name == "rust" {
		// impl path::Trait<T> for path::Type<T>: the methods belong to
		// Type, which implements Trait.
		first, next := x.rustPath(k)
		text = first
		if x.tok(next).text == "for" {
			x.addRef(first, "", RefInherit, x.toks[k].line)
			text, _ = x.rustPath(next + 1)
		}
	}
	x.names[nameAt] = true
	x.addSymbol(nameAt, text, kind, container, body)
	if classLike[kind] {
		x.headerRefs(k+1, body)
	}
	if x.lang.juxtapose {
		// let f x y = ...: the parameters are not uses.
		for j := k + 1; j < len(x.toks) && x.toks[j].line == name.line && x.toks[j].text != "=" && x.toks[j].text != ":"; j++ {
			x.names[j] = true
		}
	}
	return true
}

// rustPath reads a::b::C<T> at k and returns C and the index after it.
func (x *extractor) rustPath(k int) (string, int) {
	name := ""
	for x.tok(k).kind == tIdent {
		name = x.toks[k].text
		if x.tok(k+1).text != "::" {
			k++
			break
		}
		k += 2
	}
	if x.tok(k).text == "<" {
		k = x.skipAngles(k)
	}
	return name, k
}

// arrowDecl handles name = (...) => and name = function, and the object
// property forms name: ... of the same.
func (x *extractor) arrowDecl(i int) bool {
	t := x.toks[i]
	prev := x.tok(i - 1)
	op := x.tok(i + 1).text
	if op != "=" && op != ":" && op != "<-" {
		return false
	}
	switch {
	case t.first, prev.text == "const", prev.text == "let", prev.text == "var", prev.text == "export", prev.text == "static",
		prev.text == "readonly", prev.text == "private", prev.text == "public", prev.text == "protected", prev.text == "local", prev.text == "pub":
	case op == ":" && (prev.text == "," || prev.text == "{") && !x.inArgs:
	case prev.text == "." && x.tok(i-2).kind == tIdent && x.tok(i-2).first:
	default:
		return false
	}
	k := i + 2
	if x.tok(k).text == "async" {
		k++
	}
	kind := ""
	switch v := x.tok(k); {
	case v.text == "function":
		kind = "function"
	case x.lang.name == "zig" && (v.text == "struct" || v.text == "enum" || v.text == "union"):
		kind = map[string]string{"struct": "struct", "enum": "enum", "union": "struct"}[v.text]
	case v.kind == tIdent && x.tok(k+1).text == "=>":
		kind = "function"
	case v.text == "<" || v.text == "(":
		if v.text == "<" {
			k = x.skipAngles(k)
		}
		end, ok := x.match[k]
		if !ok || x.tok(k).text != "(" {
			return false
		}
		k = end
		for j := end + 1; j < len(x.toks) && j < end+24; j++ {
			switch x.toks[j].text {
			case "=>":
				kind = "function"
			case ";", "{", "=", "}":
			default:
				continue
			}
			break
		}
	}
	if kind == "" {
		return false
	}
	container := ""
	if prev.text == "." {
		switch q := x.tok(i - 2).text; q {
		case "this", "self", "exports", "module", "window", "prototype":
		default:
			container = q
		}
	}
	x.names[i] = true
	body := -1
	if x.lang.family == braces {
		body = x.findBody(k)
	}
	x.addSymbol(i, t.text, kind, container, body)
	return true
}

// cLikeDecl handles name(...) followed by a body: functions in C and C++,
// methods in Java, C#, Dart and JavaScript classes.
func (x *extractor) cLikeDecl(i int) {
	t := x.toks[i]
	if x.tok(i+1).text != "(" || x.lang.notCalls[x.kw(t.text)] {
		return
	}
	if _, ok := x.lang.decls[x.kw(t.text)]; ok {
		return
	}
	prev := x.tok(i - 1)
	container := ""
	switch {
	case t.first:
	case prev.text == "::" && x.tok(i-2).kind == tIdent:
		container = x.tok(i - 2).text
	case prev.text == "~" && x.tok(i-2).text == "::":
		container = x.tok(i - 3).text
	case prev.kind == tIdent:
		switch prev.text {
		case "return", "new", "throw", "await", "yield", "typeof", "case", "else", "in", "of", "delete", "void", "do", "and", "or", "not":
			if prev.text != "void" || x.lang.name == "javascript" || x.lang.name == "typescript" {
				return
			}
		}
	case prev.text == "*", prev.text == "&", prev.text == "&&", prev.text == ">", prev.text == "]", prev.text == "}", prev.text == ";", prev.text == "~":
	default:
		return
	}
	end, ok := x.match[i+1]
	if !ok {
		return
	}
	depth := 0
	for j := end + 1; j < len(x.toks) && j < end+48; j++ {
		s := x.toks[j]
		switch s.text {
		case "(", "[":
			depth++
			continue
		case ")", "]":
			depth--
			if depth < 0 {
				return
			}
			continue
		}
		if depth > 0 {
			continue
		}
		switch s.text {
		case "{":
			x.names[i] = true
			x.addSymbol(i, t.text, "function", container, j)
			return
		case "=>":
			if x.lang.name == "csharp" || x.lang.name == "dart" {
				x.names[i] = true
				x.addSymbol(i, t.text, "function", container, -1)
			}
			return
		case ";", "=", "}", ",":
			return
		}
		if s.first && s.line > t.line && s.kind != tIdent && !continuation[s.text] {
			return
		}
	}
}

// objcMethod handles - (type)name and + (type)name.
func (x *extractor) objcMethod(i int) {
	t := x.toks[i]
	if !t.first || (t.text != "-" && t.text != "+") || x.tok(i+1).text != "(" {
		return
	}
	end, ok := x.match[i+1]
	if !ok || x.tok(end+1).kind != tIdent {
		return
	}
	x.names[end+1] = true
	x.addSymbol(end+1, x.toks[end+1].text, "function", "", x.findBody(end+1))
}

// skipAngles returns the index after a balanced <...> starting at k.
func (x *extractor) skipAngles(k int) int {
	depth := 0
	for j := k; j < len(x.toks) && j < k+64; j++ {
		switch x.toks[j].text {
		case "<":
			depth++
		case ">":
			depth--
			if depth == 0 {
				return j + 1
			}
		case ";", "{", "}":
			return k
		}
	}
	return k
}

// findBody returns the index of the { that opens the body of the
// definition whose name is at k, or -1 when it has none.
func (x *extractor) findBody(k int) int {
	declLine := x.toks[k].line
	declIndent := x.lineIndent(declLine)
	depth := 0
	for j := k + 1; j < len(x.toks) && j < k+400; j++ {
		s := x.toks[j]
		if s.kind == tPunct {
			switch s.text {
			case "(", "[":
				depth++
				continue
			case ")", "]":
				depth--
				if depth < 0 {
					return -1
				}
				continue
			}
		}
		if depth > 0 {
			continue
		}
		switch s.text {
		case "{":
			return j
		case ";", "}":
			return -1
		}
		if s.first && s.line > declLine && s.indent <= declIndent && !continuation[s.text] {
			return -1
		}
	}
	return -1
}

// headerRefs records the base types named between a class's name and
// its body: after extends, implements or a : supertype marker, or in a
// Python-style base list. Parameter types of a primary constructor are not
// base types, so a : inside parentheses does not count.
func (x *extractor) headerRefs(from, body int) {
	line := x.tok(from - 1).line
	end := body
	if end < 0 {
		end = from + 40
	}
	bases := x.lang.family == indent
	depth := 0
	for j := from; j < end && j < len(x.toks); j++ {
		t := x.toks[j]
		if body < 0 && t.line > line {
			break
		}
		switch t.text {
		case "(", "[":
			depth++
			continue
		case ")", "]":
			depth--
			continue
		case "extends", "implements", "with", "<:", "is":
			bases = true
			continue
		case ":":
			if depth == 0 {
				bases = true
			}
			continue
		}
		if t.kind != tIdent || !bases || (depth > 0 && x.lang.family != indent) {
			continue
		}
		r, _ := utf8.DecodeRuneInString(t.text)
		if !unicode.IsUpper(r) || x.tok(j+1).text == "." || x.tok(j+1).text == "::" {
			continue
		}
		x.addRef(t.text, "", RefInherit, t.line)
	}
}

// refAt records a use at token i: a call, an instantiation, a JSX
// component.
func (x *extractor) refAt(i int) {
	t := x.toks[i]
	if t.text == "<" && (x.lang.name == "typescript" || x.lang.name == "javascript") {
		next := x.tok(i + 1)
		prev := x.tok(i - 1)
		r, _ := utf8.DecodeRuneInString(next.text)
		afterValue := prev.kind == tIdent && prev.text != "return" && prev.text != "yield" && prev.text != "default" || prev.kind == tNumber || prev.text == ")" || prev.text == "]"
		if next.kind == tIdent && unicode.IsUpper(r) && !afterValue {
			x.addRef(next.text, "", RefUse, next.line)
		}
		return
	}
	if t.kind != tIdent || x.names[i] {
		return
	}
	switch {
	case x.lang.lisp:
		x.lispRef(i)
		return
	case x.lang.juxtapose:
		x.juxtaposedRef(i)
		return
	case x.lang.commands && x.commandRef(i):
		return
	}
	if x.lang.name == "typescript" || x.lang.name == "javascript" {
		x.webRef(i)
	}
	if x.tok(i+1).text != "(" || x.lang.notCalls[x.kw(t.text)] {
		return
	}
	if _, ok := x.lang.decls[x.kw(t.text)]; ok {
		return
	}
	if t.first && (x.lang.name == "typescript" || x.lang.name == "javascript") {
		// update(dt: number): void; in an interface declares a member.
		if end, ok := x.match[i+1]; ok && x.tok(end+1).text == ":" {
			return
		}
	}
	if x.lang.name == "verilog" && x.tok(i-1).text == "." && x.tok(i-2).kind != tIdent {
		// .port(signal) connects an instance's port; it calls nothing.
		return
	}
	kind := RefCall
	qualifier := ""
	switch prev := x.tok(i - 1); prev.text {
	case "new":
		kind = RefNew
	case ".", "?.", "::", "->", ":":
		if q := x.tok(i - 2); q.kind == tIdent {
			qualifier = q.text
			if x.tok(i-3).text == "new" {
				kind = RefNew
			}
		}
	}
	x.addRef(t.text, qualifier, kind, t.line)
}

func (x *extractor) addRef(name, qualifier, kind string, line int) {
	if x.lang.caseInsensitive {
		// Names in these languages match in any case; the map indexes
		// their definitions in lower case too.
		name, qualifier = strings.ToLower(name), strings.ToLower(qualifier)
	}
	addRef(x.file, Ref{Name: name, Qualifier: qualifier, Kind: kind, Line: line})
}

func addRef(f *File, r Ref) {
	if len(f.Refs) < maxRefsPerFile {
		f.Refs = append(f.Refs, r)
	}
}

// maxRefsPerFile and maxSymbolsPerFile bound what one file contributes, so
// a generated file cannot flood the map.
const (
	maxRefsPerFile    = 20000
	maxSymbolsPerFile = 5000
)

func (x *extractor) addSymbol(at int, name, kind, container string, body int) {
	if len(x.file.Symbols) >= maxSymbolsPerFile {
		return
	}
	t := x.toks[at]
	end := t.line
	if body >= 0 {
		if close, ok := x.match[body]; ok {
			end = x.toks[close].line
		}
	}
	x.file.Symbols = append(x.file.Symbols, Symbol{
		Name: name, Kind: kind, Container: container, Line: t.line, EndLine: end,
		Signature: x.signature(t.line), Exported: x.exported(at, name),
	})
	x.starts = append(x.starts, at)
	x.indents = append(x.indents, x.lineIndent(t.line))
}

func (x *extractor) signature(line int) string {
	if line < 1 || line > len(x.lines) {
		return ""
	}
	return clipSignature(x.lines[line-1])
}

func clipSignature(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > maxSignature {
		cut := maxSignature
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		s = s[:cut] + "…"
	}
	return s
}

// exported reports whether a definition is visible outside its file or
// module, by the language's convention.
func (x *extractor) exported(at int, name string) bool {
	switch x.lang.name {
	case "python", "mojo", "ruby", "crystal", "lua", "julia", "r", "perl", "shell", "gdscript", "haskell", "elm", "ocaml",
		"lisp", "erlang", "powershell", "fortran", "verilog", "vhdl":
		return !strings.HasPrefix(name, "_")
	case "elixir":
		return x.tok(at-1).text != "defp"
	case "clojure":
		return x.tok(at-1).text != "defn-"
	case "nim":
		return x.tok(at+1).text == "*"
	}
	line := x.toks[at].line
	for j := at - 1; j >= 0 && j >= at-6 && x.toks[j].line == line; j-- {
		switch x.kw(x.toks[j].text) {
		case "export", "public", "pub", "open":
			return true
		}
	}
	return false
}

// indentEnds sets where each definition ends in a language that marks it
// by indentation or by an end at the same indentation.
func (x *extractor) indentEnds() {
	type lineStart struct{ index, line, indent int }
	var starts []lineStart
	depth := 0
	for i, t := range x.toks {
		if t.first && depth == 0 {
			starts = append(starts, lineStart{i, t.line, t.indent})
		}
		switch t.text {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			if depth > 0 {
				depth--
			}
		}
	}
	last := 0
	if len(x.toks) > 0 {
		last = x.toks[len(x.toks)-1].line
	}
	for s := x.first; s < len(x.file.Symbols); s++ {
		if x.fixedEnd[s] {
			continue
		}
		sym := &x.file.Symbols[s]
		level := x.indents[s-x.first]
		k := sort.Search(len(starts), func(n int) bool { return starts[n].line > sym.Line })
		sym.EndLine = last
		for ; k < len(starts); k++ {
			if starts[k].indent > level {
				continue
			}
			word := x.toks[starts[k].index].text
			lower := strings.ToLower(word)
			if starts[k].indent == level && (x.lang.continues[lower] || x.lang.clauses && word == sym.Name) {
				continue
			}
			if (word == "end" || x.lang.endPrefix && strings.HasPrefix(lower, "end")) && starts[k].indent == level {
				sym.EndLine = starts[k].line
			} else {
				sym.EndLine = max(sym.Line, x.tok(starts[k].index-1).line)
			}
			break
		}
	}
}

// containers sets each definition's container to the innermost class-like
// definition around it, and calls functions inside one methods.
func (x *extractor) containers() {
	syms := x.file.Symbols[x.first:]
	order := make([]int, len(syms))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		sa, sb := syms[order[a]], syms[order[b]]
		if sa.Line != sb.Line {
			return sa.Line < sb.Line
		}
		return sa.EndLine > sb.EndLine
	})
	var stack []int
	for _, i := range order {
		s := &syms[i]
		for len(stack) > 0 && syms[stack[len(stack)-1]].EndLine < s.Line {
			stack = stack[:len(stack)-1]
		}
		for k := len(stack) - 1; k >= 0; k-- {
			outer := syms[stack[k]]
			if !classLike[outer.Kind] || outer.EndLine < s.EndLine || outer.Line == s.Line && outer.EndLine == s.EndLine {
				continue
			}
			if s.Container == "" {
				s.Container = outer.Name
			}
			if s.Kind == "function" && outer.Kind != "namespace" {
				s.Kind = "method"
			}
			break
		}
		if s.Kind == "function" && s.Container != "" && x.lang.name != "lua" && x.lang.name != "erlang" && x.lang.name != "ocaml" {
			s.Kind = "method"
		}
		stack = append(stack, i)
	}
}
