package codemap

import (
	"path"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Documents become sections, one per heading, with what each links to,
// embeds and includes, and the code it mentions: names in inline code,
// API roles, distinctive identifiers in prose, calls in code examples, and
// the scripts and tasks in shell examples.

const maxDocRefs = 3000

func extractDoc(out *File, content string) {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	switch out.Language {
	case "markdown":
		extractMarkdown(out, lines, strings.HasSuffix(strings.ToLower(out.Path), ".mdx"))
	case "rst":
		extractRST(out, lines)
	case "asciidoc":
		extractAsciiDoc(out, lines)
	}
	finishSections(out)
}

// addSection records a heading. Its signature is the heading written the
// Markdown way, # per level, which also keeps an explicit {#anchor}.
func addSection(out *File, title string, level, line int) {
	title = strings.TrimSpace(title)
	name := cleanHeading(title)
	if name == "" || len(out.Symbols) >= maxSymbolsPerFile {
		return
	}
	out.Symbols = append(out.Symbols, Symbol{
		Name: name, Kind: "section", Line: line, EndLine: line,
		Signature: clipSignature(strings.Repeat("#", level) + " " + title),
	})
}

