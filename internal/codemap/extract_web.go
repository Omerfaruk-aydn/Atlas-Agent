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

