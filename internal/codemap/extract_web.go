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

