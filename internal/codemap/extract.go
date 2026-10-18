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

