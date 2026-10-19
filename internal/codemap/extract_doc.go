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

var (
	headingAnchorRe = regexp.MustCompile(`\s*\{#([^}\s]+)[^}]*\}\s*$`)
	mdInlineLinkRe  = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)
	mdEmphasisRe    = regexp.MustCompile("[*_`~]+")
)

// cleanHeading turns a heading's markup into its text.
func cleanHeading(s string) string {
	s = headingAnchorRe.ReplaceAllString(s, "")
	s = mdInlineLinkRe.ReplaceAllString(s, "$1")
	s = mdEmphasisRe.ReplaceAllString(s, "")
	return strings.Join(strings.Fields(s), " ")
}

// headingLevel reads the level back from a section's signature.
func headingLevel(sig string) int {
	n := 0
	for n < len(sig) && sig[n] == '#' {
		n++
	}
	return max(n, 1)
}

// finishSections ends each section where the next one of the same or a
// higher level starts.
func finishSections(out *File) {
	var idx []int
	for i, s := range out.Symbols {
		if s.Kind == "section" {
			idx = append(idx, i)
		}
	}
	for k, i := range idx {
		s := &out.Symbols[i]
		s.EndLine = out.Lines
		level := headingLevel(s.Signature)
		for _, j := range idx[k+1:] {
			if headingLevel(out.Symbols[j].Signature) <= level {
				s.EndLine = max(s.Line, out.Symbols[j].Line-1)
				break
			}
		}
	}
}

// slug is the anchor a heading gets on GitHub and most renderers, or its
// explicit {#anchor}.
func slug(sig string) string {
	if m := headingAnchorRe.FindStringSubmatch(sig); m != nil {
		return m[1]
	}
	title := cleanHeading(strings.TrimLeft(sig, "# "))
	var b strings.Builder
	for _, r := range strings.ToLower(title) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_':
			b.WriteRune(r)
		case r == ' ':
			b.WriteByte('-')
		}
	}
	return b.String()
}

var (
	atxRe        = regexp.MustCompile(`^ {0,3}(#{1,6})[ \t]+(.*?)(?:[ \t]+#+)?[ \t]*$`)
	setextRe     = regexp.MustCompile(`^ {0,3}(=+|-+)[ \t]*$`)
	mdImageRe    = regexp.MustCompile(`!\[[^\]]*\]\(\s*<?([^)\s>]+)>?(?:\s+(?:"[^"]*"|'[^']*'|\([^)]*\)))?\s*\)`)
	mdLinkRe     = regexp.MustCompile(`\[[^\]]*\]\(\s*<?([^)\s>]+)>?(?:\s+(?:"[^"]*"|'[^']*'|\([^)]*\)))?\s*\)`)
	mdRefDefRe   = regexp.MustCompile(`^ {0,3}\[[^\]]+\]:\s*<?(\S+?)>?(?:\s|$)`)
	htmlSrcRe    = regexp.MustCompile(`(?i)<(img|source|video|audio|embed|iframe|a|link|script)\b[^>]*?\b(src|href|poster|data)\s*=\s*["']([^"']+)["']`)
	wikiLinkRe   = regexp.MustCompile(`(!?)\[\[([^\]|#]+)(#[^\]|]*)?(?:\|[^\]]*)?\]\]`)
	codeSpanRe   = regexp.MustCompile("(`+)([^`]+?)`+")
	mdxImportRe  = regexp.MustCompile(`^\s*import\s+.*?from\s+["']([^"']+)["']`)
	identChainRe = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*(?:(?:\.|::|->|#)[A-Za-z_][A-Za-z0-9_]*)*(?:\(\))?`)
	proseFileRe  = regexp.MustCompile(`[A-Za-z0-9_./-]*[A-Za-z0-9_]\.[A-Za-z0-9]{1,8}\b`)
	jsxTagRe     = regexp.MustCompile(`<([A-Z][A-Za-z0-9]*)[\s/>]`)
	chainSepRe   = regexp.MustCompile(`\.|::|->|#`)
)

func extractMarkdown(out *File, lines []string, mdx bool) {
	fence := ""
	fenceLang := ""
	start := 0
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		// Front matter.
		for i := 1; i < len(lines); i++ {
			if t := strings.TrimSpace(lines[i]); t == "---" || t == "..." {
				start = i + 1
				break
			}
		}
	}
	for i := start; i < len(lines); i++ {
		line := lines[i]
		n := i + 1
		trimmed := strings.TrimLeft(line, " \t")
		if fence != "" {
			if strings.HasPrefix(trimmed, fence) && strings.TrimSpace(strings.TrimLeft(trimmed, fence[:1])) == "" {
				fence = ""
				continue
			}
			codeExample(out, line, n, fenceLang)
			continue
		}
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			marker := trimmed[:3]
			k := 3
			for k < len(trimmed) && trimmed[k] == marker[0] {
				k++
			}
			fence = trimmed[:k]
			fenceLang = strings.ToLower(strings.TrimSpace(strings.Trim(trimmed[k:], "{}.")))
			if f := strings.Fields(fenceLang); len(f) > 0 {
				fenceLang = f[0]
			}
			continue
		}
		if m := atxRe.FindStringSubmatch(line); m != nil {
			addSection(out, m[2], len(m[1]), n)
			docLinks(out, m[2], n)
			continue
		}
		if i+1 < len(lines) && strings.TrimSpace(line) != "" && setextRe.MatchString(lines[i+1]) && !isMarkdownBlock(trimmed) &&
			(i == start || strings.TrimSpace(lines[i-1]) == "") {
			level := 1
			if strings.Contains(lines[i+1], "-") {
				level = 2
			}
			addSection(out, line, level, n)
			i++
			continue
		}
		if mdx {
			if m := mdxImportRe.FindStringSubmatch(line); m != nil {
				addLink(out, FileLink{Target: m[1], Kind: LinkIncludes, Line: n})
				continue
			}
			for _, m := range jsxTagRe.FindAllStringSubmatch(line, -1) {
				mention(out, m[1], "", n)
			}
		}
		if len(line)-len(trimmed) >= 4 && !isMarkdownBlock(trimmed) {
			// An indented code block.
			codeExample(out, line, n, "")
			continue
		}
		if m := mdRefDefRe.FindStringSubmatch(line); m != nil {
			docLink(out, m[1], LinkLinks, n)
			continue
		}
		docLinks(out, line, n)
	}
}

// isMarkdownBlock reports whether a line starts a list item, a quote or a
// table row, which an underline below does not make a heading.
func isMarkdownBlock(t string) bool {
	if t == "" {
		return false
	}
	switch t[0] {
	case '-', '*', '+', '>', '|':
		return true
	}
	k := 0
	for k < len(t) && t[k] >= '0' && t[k] <= '9' {
		k++
	}
	return k > 0 && k < len(t) && (t[k] == '.' || t[k] == ')')
}

// docLinks records the links, embeds and code a line of prose has.
func docLinks(out *File, line string, n int) {
	rest := line
	for _, m := range mdImageRe.FindAllStringSubmatch(rest, -1) {
		docLink(out, m[1], LinkEmbeds, n)
	}
	rest = mdImageRe.ReplaceAllString(rest, " ")
	for _, m := range mdLinkRe.FindAllStringSubmatch(rest, -1) {
		docLink(out, m[1], LinkLinks, n)
	}
	rest = mdLinkRe.ReplaceAllStringFunc(rest, func(s string) string {
		// Keep the link text, which may mention code.
		if end := strings.Index(s, "]("); end > 0 {
			return s[1:end]
		}
		return " "
	})
	for _, m := range htmlSrcRe.FindAllStringSubmatch(rest, -1) {
		kind := LinkEmbeds
		if t := strings.ToLower(m[1]); t == "a" || t == "iframe" {
			kind = LinkLinks
		}
		docLink(out, m[3], kind, n)
	}
	rest = htmlSrcRe.ReplaceAllString(rest, " ")
	for _, m := range wikiLinkRe.FindAllStringSubmatch(rest, -1) {
		target := strings.TrimSpace(m[2])
		if path.Ext(target) == "" {
			target += ".md"
		}
		kind := LinkLinks
		if m[1] == "!" {
			kind = LinkEmbeds
		}
		docLink(out, target+m[3], kind, n)
	}
	rest = wikiLinkRe.ReplaceAllString(rest, " ")
	for _, m := range codeSpanRe.FindAllStringSubmatch(rest, -1) {
		codeSpan(out, strings.TrimSpace(m[2]), n)
	}
	prose(out, codeSpanRe.ReplaceAllString(rest, " "), n)
}

// docLink records a link to another file or an anchor, unless it leaves
// the project.
func docLink(out *File, target, kind string, n int) {
	target = strings.TrimSpace(target)
	if target == "" || hasScheme(target) {
		return
	}
	addLink(out, FileLink{Target: target, Kind: kind, Line: n})
}

// cliTools start a shell command whose arguments are not code names.
var cliTools = words("npm npx pnpm yarn bun deno node go cargo rustc make task just pip pip3 python python3 uv poetry docker podman kubectl helm git cd ls mkdir curl wget brew apt dotnet mvn gradle gradlew ./gradlew bundle rake rails mix gem composer php java terraform sh bash zsh pwsh export sudo")

// codeSpan handles inline code: a path, a shell command, or names.
func codeSpan(out *File, code string, n int) {
	if code == "" {
		return
	}
	if target, ok := pathInString(code); ok {
		addLink(out, FileLink{Target: target, Kind: LinkNames, Line: n})
		return
	}
	if !strings.ContainsAny(code, " \t") && strings.Contains(code, "/") && isCodePath(code) {
		addLink(out, FileLink{Target: code, Kind: LinkNames, Line: n})
		return
	}
	fields := strings.Fields(code)
	if len(fields) > 1 && cliTools[strings.TrimPrefix(fields[0], "$")] {
		scanCommand(out, strings.TrimPrefix(code, "$ "), n)
		return
	}
	for _, m := range identChainRe.FindAllString(code, 12) {
		name, qualifier := splitChain(m)
		if len(name) >= 3 && !commonWords[strings.ToLower(name)] {
			mention(out, name, qualifier, n)
		}
	}
}

// commonWords are not taken as code names even in inline code.
var commonWords = words("true false null nil none undefined yes no and or not the for with from import export return class function func def var let const new this self int string bool float void error todo note see also example usage env")

// prose records the distinctive identifiers and the paths in running
// text: camelCase and snake_case names, Type.member, name().
func prose(out *File, text string, n int) {
	text = proseFileRe.ReplaceAllStringFunc(text, func(m string) string {
		ext := strings.ToLower(path.Ext(m))
		if linkableExts[ext] && ext != ".sql" || strings.Contains(m, "/") && isCodePath(m) {
			addLink(out, FileLink{Target: m, Kind: LinkNames, Line: n})
			return " "
		}
		return m
	})
	for _, m := range identChainRe.FindAllString(text, 200) {
		name, qualifier := splitChain(m)
		call := strings.HasSuffix(m, "()")
		if len(name) < 3 || commonWords[strings.ToLower(name)] {
			continue
		}
		if call || distinctive(name) || qualifier != "" && (distinctive(qualifier) || isUpperStart(qualifier)) && isLowerStart(name) {
			mention(out, name, qualifier, n)
		}
	}
}

// splitChain splits a.b.c, A::b or a->b into the last name and the one
// before it.
func splitChain(s string) (string, string) {
	s = strings.TrimSuffix(s, "()")
	parts := chainSepRe.Split(s, -1)
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[len(parts)-1], parts[len(parts)-2]
}

// distinctive reports whether a word is written like code rather than
// English: camelCase, PascalCase with two humps, snake_case.
func distinctive(w string) bool {
	if strings.Trim(w, "_") != w && len(w) > 2 {
		return true
	}
	if strings.Contains(strings.Trim(w, "_"), "_") {
		return strings.ContainsFunc(w, unicode.IsLetter)
	}
	humps := 0
	prevLower := false
	for _, r := range w {
		if unicode.IsUpper(r) && prevLower {
			humps++
		}
		prevLower = unicode.IsLower(r) || unicode.IsDigit(r)
	}
	if humps == 0 {
		return false
	}
	r, _ := utf8.DecodeRuneInString(w)
	return unicode.IsLower(r) || humps >= 1 && strings.ContainsFunc(w, unicode.IsLower)
}

// mention records that a document names a definition.
func mention(out *File, name, qualifier string, n int) {
	if len(out.Refs) >= maxDocRefs {
		return
	}
	for k := len(out.Refs) - 1; k >= 0 && k >= len(out.Refs)-12; k-- {
		if r := out.Refs[k]; r.Line == n && r.Name == name && r.Qualifier == qualifier {
			return
		}
	}
	addRef(out, Ref{Name: name, Qualifier: qualifier, Kind: RefMention, Line: n})
}

var (
	exampleCallRe = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*(?:(?:\.|::|->)[A-Za-z_][A-Za-z0-9_]*)*)\s*\(`)
	shellFences   = words("sh bash shell console zsh fish powershell pwsh ps1 cmd bat terminal")
	exampleSkip   = words("if for while switch return print println printf console log require import function def fn func new catch elif foreach")
)

// codeExample records what a line of an example uses: the commands of a
// shell example, the calls of a code example.
func codeExample(out *File, line string, n int, lang string) {
	t := strings.TrimSpace(line)
	if t == "" {
		return
	}
	if shellFences[lang] || lang == "" && cliTools[strings.TrimPrefix(strings.Fields(t)[0], "$")] {
		scanCommand(out, strings.TrimPrefix(strings.TrimPrefix(t, "$ "), "> "), n)
		if shellFences[lang] {
			return
		}
	}
	if target, ok := pathInString(strings.Trim(t, `"'`)); ok {
		addLink(out, FileLink{Target: target, Kind: LinkNames, Line: n})
	}
	for _, m := range exampleCallRe.FindAllStringSubmatch(t, 12) {
		name, qualifier := splitChain(m[1])
		if len(name) >= 3 && !exampleSkip[strings.ToLower(name)] && !commonWords[strings.ToLower(name)] {
			mention(out, name, qualifier, n)
		}
	}
}

var (
	rstDirectiveRe = regexp.MustCompile(`^\s*\.\.\s+(image|figure|include|literalinclude|video|csv-table|raw|thumbnail)::\s*(\S+)`)
	rstFileOptRe   = regexp.MustCompile(`^\s*:file:\s*(\S+)`)
	rstDocRoleRe   = regexp.MustCompile("(?::doc:|:download:)`([^`<]*?)(?:\\s*<([^>]+)>)?`")
	rstRefLinkRe   = regexp.MustCompile("`[^`<]*<([^>]+)>`_")
	rstAPIRoleRe   = regexp.MustCompile("(?::[a-z]+)?:(?:func|class|meth|attr|mod|data|obj|exc|const|member|type|enum|struct|macro|var|function):`~?\\.?([^`<]*?)(?:\\s*<([^>]+)>)?`")
	rstLiteralRe   = regexp.MustCompile("``([^`]+)``")
	rstHeadChars   = "=-~^\"'`#*+:._"
)

func extractRST(out *File, lines []string) {
	styles := map[string]int{}
	code := false
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		n := i + 1
		t := strings.TrimSpace(line)
		if code {
			if t != "" && line[0] != ' ' && line[0] != '\t' {
				code = false
			} else {
				codeExample(out, line, n, "")
				continue
			}
		}
		if i+1 < len(lines) && t != "" && !isRSTRule(t) && isRSTRule(strings.TrimSpace(lines[i+1])) && len(strings.TrimSpace(lines[i+1])) >= utf8.RuneCountInString(t) {
			over := i > 0 && isRSTRule(strings.TrimSpace(lines[i-1]))
			style := lines[i+1][:1]
			if over {
				style += "o"
			}
			if _, ok := styles[style]; !ok {
				styles[style] = len(styles) + 1
			}
			addSection(out, t, styles[style], n)
			i++
			continue
		}
		if m := rstDirectiveRe.FindStringSubmatch(line); m != nil {
			kind := LinkEmbeds
			if m[1] == "include" || m[1] == "literalinclude" || m[1] == "csv-table" || m[1] == "raw" {
				kind = LinkIncludes
			}
			docLink(out, m[2], kind, n)
			continue
		}
		if strings.HasPrefix(t, ".. code") || strings.HasSuffix(t, "::") && !strings.HasPrefix(t, "..") {
			code = true
		}
		if m := rstFileOptRe.FindStringSubmatch(line); m != nil {
			docLink(out, m[1], LinkIncludes, n)
			continue
		}
		rest := line
		for _, m := range rstDocRoleRe.FindAllStringSubmatch(rest, -1) {
			target := m[1]
			if m[2] != "" {
				target = m[2]
			}
			if path.Ext(target) == "" {
				target += ".rst"
			}
			docLink(out, strings.TrimPrefix(target, "/"), LinkLinks, n)
		}
		rest = rstDocRoleRe.ReplaceAllString(rest, " ")
		for _, m := range rstRefLinkRe.FindAllStringSubmatch(rest, -1) {
			docLink(out, m[1], LinkLinks, n)
		}
		rest = rstRefLinkRe.ReplaceAllString(rest, " ")
		for _, m := range rstAPIRoleRe.FindAllStringSubmatch(rest, -1) {
			target := m[1]
			if m[2] != "" {
				target = m[2]
			}
			name, qualifier := splitChain(strings.TrimPrefix(strings.TrimPrefix(target, "~"), "."))
			if name != "" {
				mention(out, name, qualifier, n)
			}
		}
		rest = rstAPIRoleRe.ReplaceAllString(rest, " ")
		for _, m := range rstLiteralRe.FindAllStringSubmatch(rest, -1) {
			codeSpan(out, m[1], n)
		}
		prose(out, rstLiteralRe.ReplaceAllString(rest, " "), n)
	}
}

