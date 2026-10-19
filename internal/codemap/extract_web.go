package codemap

import (
	"path"
	"regexp"
	"sort"
	"strings"
)

// Markup and stylesheets: the scripts and styles a page loads and embeds,
// the images it shows, the element ids it defines, the classes it uses and
// the components a template renders; a stylesheet's classes, variables,
// mixins and animations and what uses them.

// block is a <script> or <style> element's content: start and end are
// the offsets of what is between its tags.
type block struct {
	start, end int
	kind       string
	lang       string
}

// findBlocks returns the script and style elements of markup.
func findBlocks(content string) []block {
	lower := strings.ToLower(content)
	var out []block
	pos := 0
	for pos < len(lower) {
		s := strings.Index(lower[pos:], "<script")
		t := strings.Index(lower[pos:], "<style")
		kind := "script"
		switch {
		case s < 0 && t < 0:
			return out
		case s < 0 || t >= 0 && t < s:
			s, kind = t, "style"
		}
		start := pos + s
		nameEnd := start + len("<"+kind)
		if nameEnd < len(lower) && !strings.ContainsRune(" \t\r\n>/", rune(lower[nameEnd])) {
			pos = nameEnd
			continue
		}
		tagEnd := tagClose(content, nameEnd)
		if tagEnd < 0 {
			return out
		}
		attrs := parseAttrs(content[nameEnd:tagEnd], nameEnd, false)
		closing := strings.Index(lower[tagEnd:], "</"+kind)
		if closing < 0 {
			closing = len(lower) - tagEnd
		}
		b := block{start: tagEnd + 1, end: tagEnd + closing, kind: kind}
		if b.start > b.end {
			b.start = b.end
		}
		lang := strings.ToLower(attrValue(attrs, "lang"))
		typ := strings.ToLower(attrValue(attrs, "type"))
		if kind == "script" {
			switch {
			case attrValue(attrs, "src") != "" && strings.TrimSpace(content[b.start:b.end]) == "":
				b.kind = "other"
			case typ != "" && typ != "module" && !strings.Contains(typ, "javascript") && !strings.Contains(typ, "typescript") && !strings.Contains(typ, "babel") && !strings.Contains(typ, "jsx"):
				b.kind = "other"
			}
			b.lang = "javascript"
			if lang == "ts" || lang == "tsx" || strings.Contains(typ, "typescript") {
				b.lang = "typescript"
			}
		} else {
			b.lang = "css"
			switch lang {
			case "scss", "sass", "postcss":
				b.lang = "scss"
			case "less":
				b.lang = "less"
			}
		}
		out = append(out, b)
		pos = b.end
	}
	return out
}

// tagClose returns the offset of the > that closes a tag whose attributes
// start at from, skipping quoted values.
func tagClose(s string, from int) int {
	quote := byte(0)
	for i := from; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '>':
			return i
		}
	}
	return -1
}

// keepBlocks returns content with everything outside the chosen blocks
// blanked, keeping the line breaks so line numbers stay.
func keepBlocks(content string, blocks []block, keep func(block) bool) string {
	var b strings.Builder
	pos := 0
	for _, bl := range blocks {
		if !keep(bl) {
			continue
		}
		b.WriteString(blankLines(content[pos:bl.start]))
		b.WriteString(content[bl.start:bl.end])
		pos = bl.end
	}
	b.WriteString(blankLines(content[pos:]))
	return b.String()
}

// dropBlocks blanks the content of every block.
func dropBlocks(content string, blocks []block) string {
	var b strings.Builder
	pos := 0
	for _, bl := range blocks {
		b.WriteString(content[pos:bl.start])
		b.WriteString(blankLines(content[bl.start:bl.end]))
		pos = bl.end
	}
	b.WriteString(content[pos:])
	return b.String()
}

// lineIndex maps offsets to line numbers.
type lineIndex []int

func newLineIndex(s string) lineIndex {
	idx := lineIndex{0}
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			idx = append(idx, i+1)
		}
	}
	return idx
}

func (l lineIndex) at(off int) int {
	return sort.Search(len(l), func(i int) bool { return l[i] > off })
}

type markupMode int

const (
	markupHTML markupMode = iota
	markupVue
	markupSvelte
	markupAstro
)

func extractHTML(out *File, content string) {
	blocks := findBlocks(content)
	if script := keepBlocks(content, blocks, func(b block) bool { return b.kind == "script" }); strings.TrimSpace(script) != "" {
		extractCode(out, languages["javascript"], script)
	}
	for _, lang := range []string{"css", "scss", "less"} {
		if style := keepBlocks(content, blocks, func(b block) bool { return b.kind == "style" && b.lang == lang }); strings.TrimSpace(style) != "" {
			extractCSS(out, lang, style)
		}
	}
	scanMarkup(out, dropBlocks(content, blocks), markupHTML)
	if out.Language == "template" {
		templateIncludes(out, content)
	}
}

// extractComponent reads a Vue, Svelte or Astro component: its script, its
// styles, its template, and the component the file itself is.
func extractComponent(out *File, ext, content string) {
	blocks := findBlocks(content)
	mode := map[string]markupMode{".vue": markupVue, ".svelte": markupSvelte, ".astro": markupAstro}[ext]
	if mode == markupAstro && strings.HasPrefix(strings.TrimLeft(content, string(rune(0xFEFF))), "---") {
		// The frontmatter between --- fences is the component's script.
		first := strings.Index(content, "---")
		if end := strings.Index(content[first+3:], "\n---"); end >= 0 {
			fm := block{start: first + 3, end: first + 3 + end, kind: "script", lang: "typescript"}
			blocks = append([]block{fm}, blocks...)
		}
	}
	lang := "typescript"
	if script := keepBlocks(content, blocks, func(b block) bool { return b.kind == "script" }); strings.TrimSpace(script) != "" {
		extractCode(out, languages[lang], script)
	}
	for _, style := range []string{"css", "scss", "less"} {
		if css := keepBlocks(content, blocks, func(b block) bool { return b.kind == "style" && b.lang == style }); strings.TrimSpace(css) != "" {
			extractCSS(out, style, css)
		}
	}
	scanMarkup(out, dropBlocks(content, blocks), mode)
	name := pascalCase(strings.TrimSuffix(path.Base(out.Path), path.Ext(out.Path)))
	if name != "" {
		out.Symbols = append(out.Symbols, Symbol{
			Name: name, Kind: "component", Line: 1, EndLine: out.Lines,
			Signature: "<" + name + "> (" + path.Base(out.Path) + ")", Exported: true,
		})
	}
}

// pascalCase turns my-button and my_button into MyButton.
func pascalCase(s string) string {
	var b strings.Builder
	upper := true
	for _, r := range s {
		switch {
		case r == '-' || r == '_' || r == '.' || r == ' ':
			upper = true
		case upper:
			b.WriteString(strings.ToUpper(string(r)))
			upper = false
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

type attr struct {
	name, value string
	// at is the offset of the value.
	at int
	// braced values are {expressions}.
	braced bool
}

// parseAttrs reads a tag's attributes; offset is where s starts in the
// file. With braces, a value may be a {expression}.
func parseAttrs(s string, offset int, braces bool) []attr {
	var out []attr
	i := 0
	for i < len(s) {
		for i < len(s) && strings.ContainsRune(" \t\r\n/", rune(s[i])) {
			i++
		}
		if i >= len(s) {
			break
		}
		if braces && s[i] == '{' {
			// {spread} or a shorthand {name}.
			end := matchBrace(s, i)
			out = append(out, attr{name: "{}", value: s[i+1 : max(i+1, end)], at: offset + i + 1, braced: true})
			i = end + 1
			continue
		}
		start := i
		for i < len(s) && !strings.ContainsRune(" \t\r\n=/>", rune(s[i])) {
			i++
		}
		if i == start {
			i++
			continue
		}
		a := attr{name: s[start:i]}
		j := i
		for j < len(s) && (s[j] == ' ' || s[j] == '\t') {
			j++
		}
		if j < len(s) && s[j] == '=' {
			j++
			for j < len(s) && (s[j] == ' ' || s[j] == '\t') {
				j++
			}
			if j < len(s) {
				switch c := s[j]; {
				case c == '"' || c == '\'':
					end := strings.IndexByte(s[j+1:], c)
					if end < 0 {
						end = len(s) - j - 1
					}
					a.value, a.at = s[j+1:j+1+end], offset+j+1
					j += end + 2
				case c == '{' && braces:
					end := matchBrace(s, j)
					a.value, a.at, a.braced = s[j+1:max(j+1, end)], offset+j+1, true
					j = end + 1
				default:
					end := j
					for end < len(s) && !strings.ContainsRune(" \t\r\n>", rune(s[end])) {
						end++
					}
					a.value, a.at = s[j:end], offset+j
					j = end
				}
			}
			i = j
		}
		out = append(out, a)
	}
	return out
}

// matchBrace returns the offset of the } closing the { at i.
func matchBrace(s string, i int) int {
	depth := 0
	quote := byte(0)
	for k := i; k < len(s); k++ {
		c := s[k]
		switch {
		case quote != 0:
			switch c {
			case '\\':
				k++
			case quote:
				quote = 0
			}
		case c == '"' || c == '\'' || c == '`':
			quote = c
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				return k
			}
		}
	}
	return len(s)
}

func attrValue(attrs []attr, name string) string {
	for _, a := range attrs {
		if strings.EqualFold(a.name, name) {
			return a.value
		}
	}
	return ""
}

var (
	mustacheRe     = regexp.MustCompile(`\{\{-?([\s\S]*?)-?\}\}`)
	svelteBlockRe  = regexp.MustCompile(`^\s*[#:/@](?:if|else if|else|each|await|then|catch|key|html|const|debug|render|snippet)?\b`)
	htmlTagNameRe  = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9:._-]*`)
	cssURLRe       = regexp.MustCompile(`url\(\s*["']?([^"')]+)["']?\s*\)`)
	cssVarUseRe    = regexp.MustCompile(`var\(\s*(--[\w-]+)`)
	quotedRe       = regexp.MustCompile(`["'\x60]([^"'\x60]*)["'\x60]`)
	standardTagSet = words("html head body div span p a img ul ol li table tr td th thead tbody tfoot section article aside nav header footer main form input button select option textarea label h1 h2 h3 h4 h5 h6 br hr meta link script style title template slot component svg path g circle rect line polygon polyline text use defs symbol canvas video audio source track picture iframe embed object strong em b i u small code pre blockquote figure figcaption details summary dialog")
)

// scanMarkup reads tags and template expressions.
func scanMarkup(out *File, s string, mode markupMode) {
	lines := newLineIndex(s)
	braces := mode == markupSvelte || mode == markupAstro
	i := 0
	for i < len(s) {
		lt := strings.IndexByte(s[i:], '<')
		end := len(s)
		if lt >= 0 {
			end = i + lt
		}
		textExpressions(out, s[i:end], i, lines, mode)
		if lt < 0 {
			return
		}
		i = end
		switch {
		case strings.HasPrefix(s[i:], "<!--"):
			close := strings.Index(s[i:], "-->")
			if close < 0 {
				return
			}
			i += close + 3
			continue
		case i+1 < len(s) && (s[i+1] == '/' || s[i+1] == '!' || s[i+1] == '?'):
			close := strings.IndexByte(s[i:], '>')
			if close < 0 {
				return
			}
			i += close + 1
			continue
		}
		name := htmlTagNameRe.FindString(s[i+1:])
		if name == "" {
			i++
			continue
		}
		attrStart := i + 1 + len(name)
		close := tagEnd(s, attrStart, braces)
		attrs := parseAttrs(s[attrStart:close], attrStart, braces)
		markupTag(out, name, attrs, lines.at(i), lines, mode)
		i = close + 1
	}
}

// tagEnd finds the end of a tag, skipping quoted and braced values.
func tagEnd(s string, from int, braces bool) int {
	quote := byte(0)
	for k := from; k < len(s); k++ {
		c := s[k]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '{' && braces:
			k = matchBrace(s, k)
		case c == '>':
			return k
		}
	}
	return len(s)
}

// textExpressions reads {{ expr }} in text, and in Svelte and Astro { expr }.
func textExpressions(out *File, text string, offset int, lines lineIndex, mode markupMode) {
	if !strings.Contains(text, "{") {
		return
	}
	for _, m := range mustacheRe.FindAllStringSubmatchIndex(text, -1) {
		exprRefs(out, text[m[2]:m[3]], lines.at(offset+m[2]), false)
	}
	if mode != markupSvelte && mode != markupAstro {
		return
	}
	for k := 0; k < len(text); k++ {
		if text[k] != '{' || k+1 < len(text) && text[k+1] == '{' {
			continue
		}
		end := matchBrace(text, k)
		expr := text[k+1 : min(end, len(text))]
		expr = svelteBlockRe.ReplaceAllString(expr, "")
		exprRefs(out, expr, lines.at(offset+k), false)
		k = end
	}
}

// exprRefs records the calls and paths in a template expression. A
// handler that is only a name is a call of it.
func exprRefs(out *File, expr string, line int, handler bool) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return
	}
	js := languages["javascript"]
	toks := lex(expr, js)
	if handler && len(toks) > 0 && toks[len(toks)-1].kind == tIdent {
		simple := true
		for k, t := range toks {
			if t.kind != tIdent && (t.text != "." || k == 0) {
				simple = false
			}
		}
		if simple {
			qualifier := ""
			if len(toks) >= 3 {
				qualifier = toks[len(toks)-3].text
			}
			addRef(out, Ref{Name: toks[len(toks)-1].text, Qualifier: qualifier, Kind: RefCall, Line: line + toks[len(toks)-1].line - 1})
			return
		}
	}
	for k, t := range toks {
		at := line + t.line - 1
		switch t.kind {
		case tString:
			scanString(out, t.text, at)
		case tIdent:
			if k+1 >= len(toks) || toks[k+1].text != "(" || js.notCalls[t.text] {
				continue
			}
			qualifier := ""
			if k >= 2 && (toks[k-1].text == "." || toks[k-1].text == "?.") && toks[k-2].kind == tIdent {
				qualifier = toks[k-2].text
			}
			addRef(out, Ref{Name: t.text, Qualifier: qualifier, Kind: RefCall, Line: at})
		}
	}
}

// markupTag records what one tag loads, embeds, links, defines and uses.
func markupTag(out *File, name string, attrs []attr, line int, lines lineIndex, mode markupMode) {
	tag := strings.ToLower(name)
	val := func(a attr) (string, int) { return strings.TrimSpace(a.value), lines.at(a.at) }
	for _, a := range attrs {
		an := strings.ToLower(a.name)
		v, at := val(a)
		switch {
		case a.braced:
			handler := strings.HasPrefix(an, "on:") || strings.HasPrefix(an, "on")
			exprRefs(out, v, at, handler)
			switch {
			case strings.HasPrefix(an, "class:"):
				addRef(out, Ref{Name: a.name[len("class:"):], Kind: RefCSS, Line: at})
			case an == "class" || an == "classname":
				for _, m := range quotedRe.FindAllStringSubmatch(v, -1) {
					classRefs(out, stripTemplates(m[1]), at)
				}
			}
			continue
		case mode == markupVue && (strings.HasPrefix(an, "@") || strings.HasPrefix(an, "v-on:")):
			exprRefs(out, v, at, true)
			continue
		case mode == markupVue && (strings.HasPrefix(an, ":") || strings.HasPrefix(an, "v-")):
			exprRefs(out, v, at, false)
			continue
		case mode == markupSvelte && (strings.HasPrefix(an, "use:") || strings.HasPrefix(an, "transition:") || strings.HasPrefix(an, "in:") || strings.HasPrefix(an, "out:") || strings.HasPrefix(an, "animate:")):
			addRef(out, Ref{Name: a.name[strings.IndexByte(a.name, ':')+1:], Kind: RefCall, Line: at})
			continue
		case mode == markupSvelte && strings.HasPrefix(an, "class:"):
			addRef(out, Ref{Name: a.name[len("class:"):], Kind: RefCSS, Line: at})
			continue
		case mode == markupHTML && strings.HasPrefix(an, "on") && len(an) > 2:
			exprRefs(out, v, at, true)
			continue
		}
		switch an {
		case "id":
			if isCSSName(v) {
				defineElement(out, v, tag, at)
			}
		case "class", "classname":
			if !strings.ContainsAny(v, "{}<%") {
				classRefs(out, v, at)
			} else {
				classRefs(out, templateFree(v), at)
			}
		case "style":
			cssValueRefs(out, v, at)
		case "src", "href", "poster", "data", "xlink:href", "action", "content", "srcset", "data-src", "background":
			markupLink(out, tag, an, v, at, attrs)
		}
	}
	if mode != markupHTML || strings.Contains(tag, "-") {
		component(out, name, line, mode)
	}
}

// templateFree drops {{ }}, {% %}, <% %> and { } parts of a value.
func templateFree(v string) string {
	for _, pair := range [][2]string{{"{{", "}}"}, {"{%", "%}"}, {"<%", "%>"}, {"{", "}"}} {
		for {
			s := strings.Index(v, pair[0])
			if s < 0 {
				break
			}
			e := strings.Index(v[s+len(pair[0]):], pair[1])
			if e < 0 {
				v = v[:s]
				break
			}
			v = v[:s] + " " + v[s+len(pair[0])+e+len(pair[1]):]
		}
	}
	return v
}

// defineElement records an element id, once per file.
func defineElement(out *File, id, tag string, line int) {
	for _, s := range out.Symbols {
		if s.Kind == "element id" && s.Name == id {
			return
		}
	}
	out.Symbols = append(out.Symbols, Symbol{Name: id, Kind: "element id", Line: line, EndLine: line, Signature: "<" + tag + ` id="` + id + `">`})
}

// component records the use of a component in a template: <MyButton> or
// <my-button>.
func component(out *File, name string, line int, mode markupMode) {
	if strings.Contains(name, ":") || strings.Contains(name, ".") && mode != markupSvelte && mode != markupAstro {
		return
	}
	if standardTagSet[strings.ToLower(name)] && !isUpperStart(name) {
		return
	}
	if !isUpperStart(name) && !strings.Contains(name, "-") {
		return
	}
	if k := strings.LastIndexByte(name, '.'); k >= 0 {
		// <Icons.Star>: the namespace's member.
		addRef(out, Ref{Name: name[k+1:], Qualifier: name[:k], Kind: RefUse, Line: line})
		return
	}
	addRef(out, Ref{Name: pascalCase(name), Kind: RefUse, Line: line})
}

// markupLink records a tag's reference to a file: a script or stylesheet
// it loads, an image it shows, a page it links to, an element it points
// at.
func markupLink(out *File, tag, attrName, v string, line int, attrs []attr) {
	if v == "" || strings.ContainsAny(v, "{}") && !strings.Contains(v, "${") {
		return
	}
	if strings.HasPrefix(v, "#") {
		if id := v[1:]; isCSSName(id) && (tag == "a" || tag == "use" || tag == "area" || tag == "label") {
			addRef(out, Ref{Name: id, Kind: RefDOM, Line: line})
		}
		return
	}
	if hasScheme(v) {
		return
	}
	switch tag {
	case "script":
		if attrName == "src" {
			out.Imports = append(out.Imports, Import{Spec: v, Line: line})
		}
		return
	case "link":
		rel := strings.ToLower(attrValue(attrs, "rel"))
		if attrName == "href" && (strings.Contains(rel, "stylesheet") || rel == "modulepreload" || rel == "import") {
			out.Imports = append(out.Imports, Import{Spec: v, Line: line})
			return
		}
		addLink(out, FileLink{Target: v, Kind: LinkLoads, Line: line})
		return
	case "form":
		if route, ok := routeOf(v); ok {
			addRef(out, Ref{Name: route, Kind: RefRoute, Line: line})
		}
		return
	case "meta":
		if target, ok := pathInString(v); ok {
			addLink(out, FileLink{Target: target, Kind: LinkLoads, Line: line})
		}
		return
	}
	targets := []string{v}
	if attrName == "srcset" {
		targets = nil
		for _, part := range strings.Split(v, ",") {
			if f := strings.Fields(part); len(f) > 0 {
				targets = append(targets, f[0])
			}
		}
	}
	kind := LinkEmbeds
	if tag == "a" || tag == "area" || tag == "iframe" || tag == "frame" {
		kind = LinkLinks
	}
	for _, t := range targets {
		if strings.HasPrefix(t, "/") && kind == LinkLinks {
			if route, ok := routeOf(t); ok {
				addRef(out, Ref{Name: route, Kind: RefRoute, Line: line})
				continue
			}
		}
		addLink(out, FileLink{Target: t, Kind: kind, Line: line})
	}
}

var templateIncludeRes = []*regexp.Regexp{
	regexp.MustCompile(`\{%-?\s*(?:include|extends|import|from|embed|render|layout|section)\s+["']([^"']+)["']`),
	regexp.MustCompile(`\{\{#?>\s*([\w./-]+)`),
	regexp.MustCompile(`<%[-=]?\s*include\(?\s*["']([^"']+)["']`),
	regexp.MustCompile(`render\(?\s*(?:partial:\s*)?["']([^"']+)["']`),
}

// templateIncludes records the partials and layouts a template uses.
func templateIncludes(out *File, content string) {
	lines := newLineIndex(content)
	ext := path.Ext(out.Path)
	for _, re := range templateIncludeRes {
		for _, m := range re.FindAllStringSubmatchIndex(content, -1) {
			target := content[m[2]:m[3]]
			if path.Ext(target) == "" {
				target += ext
			}
			addLink(out, FileLink{Target: target, Kind: LinkIncludes, Line: lines.at(m[2])})
		}
	}
}

// cssValueRefs records the url()s and var()s of a style value.
func cssValueRefs(out *File, v string, line int) {
	for _, m := range cssURLRe.FindAllStringSubmatch(v, -1) {
		if t := strings.TrimSpace(m[1]); !hasScheme(t) && !strings.HasPrefix(t, "#") {
			addLink(out, FileLink{Target: t, Kind: LinkEmbeds, Line: line})
		}
	}
	for _, m := range cssVarUseRe.FindAllStringSubmatch(v, -1) {
		addRef(out, Ref{Name: m[1], Kind: RefCSS, Line: line})
	}
}

var (
	cssClassRe     = regexp.MustCompile(`\.(-?[_a-zA-Z\x{80}-\x{10FFFF}][\w\-\x{80}-\x{10FFFF}]*)`)
	cssIDRe        = regexp.MustCompile(`#([_a-zA-Z][\w-]*)`)
	cssPlaceRe     = regexp.MustCompile(`%([_a-zA-Z][\w-]*)`)
	cssAttrRe      = regexp.MustCompile(`\[[^\]]*\]`)
	scssVarRe      = regexp.MustCompile(`\$[_a-zA-Z][\w-]*`)
	lessVarRe      = regexp.MustCompile(`@[_a-zA-Z][\w-]*`)
	cssStringRe    = regexp.MustCompile(`["']([^"']+)["']`)
	cssAnimWords   = words("none infinite linear ease ease-in ease-out ease-in-out step-start step-end alternate alternate-reverse reverse normal forwards backwards both running paused initial inherit unset revert")
	cssAtNestWords = words("@media @supports @layer @container @document @page @at-root @each @for @if @else @while @font-face @starting-style @scope")
)

// extractCSS reads a stylesheet. flavor is css, scss or less.
func extractCSS(out *File, flavor, content string) {
	src := stripCSSComments(content, flavor != "css")
	lines := newLineIndex(src)
	type frame struct {
		classes []string
		syms    []int
	}
	var stack []frame
	seen := map[string]bool{}
	define := func(name, kind string, off int) int {
		key := kind + ":" + name
		if seen[key] || len(out.Symbols) >= maxSymbolsPerFile {
			return -1
		}
		seen[key] = true
		line := lines.at(off)
		sig := strings.TrimSpace(src[lines[line-1]:min(len(src), lineEndAt(src, lines[line-1]))])
		out.Symbols = append(out.Symbols, Symbol{Name: name, Kind: kind, Line: line, EndLine: line, Signature: clipSignature(sig), Exported: true})
		return len(out.Symbols) - 1
	}
	ref := func(name, qualifier string, off int) {
		addRef(out, Ref{Name: name, Qualifier: qualifier, Kind: RefCSS, Line: lines.at(off)})
	}
	valueRefs := func(prop, value string, off int) {
		line := lines.at(off)
		cssValueRefs(out, value, line)
		if flavor == "scss" {
			for _, v := range scssVarRe.FindAllString(value, -1) {
				ref(v, "", off)
			}
		}
		if flavor == "less" {
			for _, v := range lessVarRe.FindAllString(value, -1) {
				ref(v, "", off)
			}
		}
		if p := strings.ToLower(prop); p == "animation" || p == "animation-name" {
			for _, w := range strings.FieldsFunc(value, func(r rune) bool { return r == ' ' || r == ',' || r == '\t' }) {
				if isCSSName(w) && !cssAnimWords[strings.ToLower(w)] && !strings.ContainsAny(w[:1], "0123456789") {
					ref(w, "@keyframes", off)
				}
			}
		}
	}
	prelude := func(text string, off int) frame {
		t := strings.TrimSpace(text)
		off += strings.Index(text, t)
		var f frame
		if len(stack) > 0 {
			f.classes = stack[len(stack)-1].classes
		}
		if t == "" {
			return f
		}
		if t[0] == '@' {
			word, rest, _ := strings.Cut(t, " ")
			rest = strings.TrimSpace(rest)
			name := strings.TrimSpace(strings.SplitN(strings.SplitN(rest, "(", 2)[0], " ", 2)[0])
			switch {
			case strings.HasSuffix(word, "keyframes"):
				if s := define(name, "keyframes", off); s >= 0 {
					f.syms = append(f.syms, s)
				}
			case word == "@mixin":
				if s := define(name, "mixin", off); s >= 0 {
					f.syms = append(f.syms, s)
				}
			case word == "@function":
				if s := define(name, "function", off); s >= 0 {
					f.syms = append(f.syms, s)
				}
			case word == "@include":
				ref(name, "@mixin", off)
			case cssAtNestWords[word]:
			}
			return f
		}
		if flavor == "less" && strings.HasSuffix(t, ")") && strings.HasPrefix(t, ".") && !strings.Contains(t, " ") {
			// A Less mixin with parameters: .button(@color) { }.
			name := strings.TrimPrefix(strings.SplitN(t, "(", 2)[0], ".")
			if s := define(name, "mixin", off); s >= 0 {
				f.syms = append(f.syms, s)
			}
			return f
		}
		var classes []string
		for _, part := range splitTopLevel(t, ',') {
			p := strings.TrimSpace(part)
			if strings.HasPrefix(p, "&") && len(stack) > 0 {
				suffix := p[1:]
				k := 0
				for k < len(suffix) && (suffix[k] == '-' || suffix[k] == '_' || suffix[k] >= 'a' && suffix[k] <= 'z' || suffix[k] >= 'A' && suffix[k] <= 'Z' || suffix[k] >= '0' && suffix[k] <= '9') {
					k++
				}
				if k > 0 {
					for _, parent := range stack[len(stack)-1].classes {
						classes = append(classes, parent+suffix[:k])
					}
				}
			}
			p = cssAttrRe.ReplaceAllString(p, " ")
			for _, m := range cssClassRe.FindAllStringSubmatch(p, -1) {
				classes = append(classes, m[1])
			}
			for _, m := range cssPlaceRe.FindAllStringSubmatch(p, -1) {
				classes = append(classes, "%"+m[1])
			}
			for _, m := range cssIDRe.FindAllStringSubmatch(p, -1) {
				addRef(out, Ref{Name: m[1], Kind: RefDOM, Line: lines.at(off)})
			}
		}
		for _, c := range classes {
			kind := "css class"
			if strings.HasPrefix(c, "%") {
				kind = "placeholder"
			}
			if s := define(c, kind, off); s >= 0 {
				f.syms = append(f.syms, s)
			}
		}
		if len(classes) > 0 {
			f.classes = classes
		}
		return f
	}
	statement := func(text string, off int) {
		t := strings.TrimSpace(text)
		if t == "" {
			return
		}
		off += strings.Index(text, t)
		if t[0] == '@' {
			word, rest, _ := strings.Cut(t, " ")
			rest = strings.TrimSpace(rest)
			switch word {
			case "@import", "@use", "@forward", "@require":
				specs := cssStringRe.FindAllStringSubmatch(rest, -1)
				if len(specs) == 0 {
					specs = cssURLRe.FindAllStringSubmatch(rest, -1)
				}
				for _, m := range specs {
					out.Imports = append(out.Imports, Import{Spec: strings.TrimSpace(m[1]), Line: lines.at(off)})
				}
				return
			case "@include":
				ref(strings.TrimSpace(strings.SplitN(rest, "(", 2)[0]), "@mixin", off)
				return
			case "@extend":
				target := strings.TrimSpace(strings.TrimSuffix(strings.Fields(rest + " ")[0], "!optional"))
				ref(strings.TrimPrefix(target, "."), "", off)
				return
			case "@apply":
				for _, c := range strings.Fields(rest) {
					if isCSSName(c) {
						ref(c, "", off)
					}
				}
				return
			}
			if flavor == "less" {
				if name, value, ok := strings.Cut(t, ":"); ok && isCSSName(strings.TrimPrefix(strings.TrimSpace(name), "@")) {
					define(strings.TrimSpace(name), "less variable", off)
					valueRefs("", value, off)
				}
			}
			return
		}
		colon := topLevelIndex(t, ':')
		if colon < 0 {
			if flavor == "less" && strings.HasPrefix(t, ".") {
				// A Less mixin call: .rounded(); or .rounded;
				ref(strings.TrimPrefix(strings.SplitN(t, "(", 2)[0], "."), "", off)
			}
			return
		}
		prop, value := strings.TrimSpace(t[:colon]), t[colon+1:]
		switch {
		case strings.HasPrefix(prop, "--"):
			define(prop, "css variable", off)
		case flavor == "scss" && strings.HasPrefix(prop, "$"):
			define(prop, "scss variable", off)
		}
		valueRefs(prop, value, off)
	}
	segStart := 0
	for i := 0; i < len(src); i++ {
		switch c := src[i]; c {
		case '"', '\'':
			end := strings.IndexByte(src[i+1:], c)
			if end < 0 {
				i = len(src)
			} else {
				i += end + 1
			}
		case '(':
			depth := 1
			for i++; i < len(src) && depth > 0; i++ {
				switch src[i] {
				case '(':
					depth++
				case ')':
					depth--
				}
			}
			i--
		case '{':
			if i > 0 && src[i-1] == '#' {
				// #{$interpolation}
				i = matchBrace(src, i)
				continue
			}
			stack = append(stack, prelude(src[segStart:i], segStart))
			segStart = i + 1
		case ';':
			statement(src[segStart:i], segStart)
			segStart = i + 1
		case '}':
			statement(src[segStart:i], segStart)
			if len(stack) > 0 {
				for _, s := range stack[len(stack)-1].syms {
					out.Symbols[s].EndLine = lines.at(i)
				}
				stack = stack[:len(stack)-1]
			}
			segStart = i + 1
		}
	}
}

func lineEndAt(s string, from int) int {
	if k := strings.IndexByte(s[from:], '\n'); k >= 0 {
		return from + k
	}
	return len(s)
}

// stripCSSComments blanks /* */ comments and, with lineComments, // ones,
// keeping offsets and line breaks.
func stripCSSComments(s string, lineComments bool) string {
	b := []byte(s)
	for i := 0; i < len(b); i++ {
		switch {
		case b[i] == '"' || b[i] == '\'':
			q := b[i]
			for i++; i < len(b) && b[i] != q && b[i] != '\n'; i++ {
				if b[i] == '\\' {
					i++
				}
			}
		case b[i] == '/' && i+1 < len(b) && b[i+1] == '*':
			for ; i < len(b) && (b[i] != '*' || i+1 >= len(b) || b[i+1] != '/'); i++ {
				if b[i] != '\n' {
					b[i] = ' '
				}
			}
			if i+1 < len(b) {
				b[i], b[i+1] = ' ', ' '
				i++
			}
		case lineComments && b[i] == '/' && i+1 < len(b) && b[i+1] == '/' && (i == 0 || b[i-1] != ':'):
			for ; i < len(b) && b[i] != '\n'; i++ {
				b[i] = ' '
			}
		}
	}
	return string(b)
}

// splitTopLevel splits s at sep outside parentheses and brackets.
func splitTopLevel(s string, sep byte) []string {
	var out []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(', '[':
			depth++
		case ')', ']':
			depth--
		case sep:
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

// topLevelIndex returns the index of the first c outside parentheses.
func topLevelIndex(s string, c byte) int {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(', '[':
			depth++
		case ')', ']':
			depth--
		case c:
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}
