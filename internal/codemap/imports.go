package codemap

import (
	"strings"
)

// importAt records an import statement starting at token i and reports
// whether it consumed the token.
func (x *extractor) importAt(i int) bool {
	t := x.toks[i]
	prev := x.tok(i - 1)
	// A member access is not an import; a line starting after the . that
	// ends an Erlang or Prolog-like form is.
	if prev.text == "." && !t.first || prev.text == "?." || prev.text == "->" || prev.text == "::" && t.text != "include" {
		return false
	}
	switch x.lang.name {
	case "typescript", "javascript":
		return x.jsImport(i)
	case "python", "mojo":
		return x.pythonImport(i)
	case "java", "kotlin", "scala", "groovy":
		if t.text == "import" && t.first {
			x.dottedImport(i+1, ".", true)
			return true
		}
	case "csharp":
		if t.text == "using" && x.tok(i+1).text != "(" && x.tok(i+1).text != "var" {
			k := i + 1
			if x.tok(k).text == "static" {
				k++
			}
			if x.tok(k+1).text == "=" {
				k += 2
			}
			x.dottedImport(k, ".", false)
			return true
		}
	case "c", "cpp", "objc", "glsl":
		if t.text == "#" && (x.tok(i+1).text == "include" || x.tok(i+1).text == "import") {
			x.includeImport(i + 2)
			return true
		}
		if t.text == "@" && x.tok(i+1).text == "import" {
			x.dottedImport(i+2, ".", false)
			return true
		}
	case "rust":
		if t.text == "use" {
			x.rustUse(i + 1)
			return true
		}
	case "swift", "julia":
		if (t.text == "import" || t.text == "using") && t.first {
			x.dottedImport(i+1, ".", t.text == "import" && x.lang.name == "julia")
			return true
		}
	case "php":
		return x.phpImport(i)
	case "ruby", "crystal":
		if (t.text == "require" || t.text == "require_relative" || t.text == "load") && t.first {
			if s := x.stringAfter(i + 1); s.kind == tString {
				spec := s.text
				if t.text == "require_relative" {
					spec = "./" + strings.TrimPrefix(spec, "./")
				}
				x.file.Imports = append(x.file.Imports, Import{Spec: spec, Line: t.line})
				return true
			}
		}
	case "lua":
		if t.text == "require" {
			if s := x.stringAfter(i + 1); s.kind == tString {
				imp := Import{Spec: s.text, Line: t.line}
				if x.tok(i-1).text == "=" && x.tok(i-2).kind == tIdent {
					imp.Module = x.tok(i - 2).text
				}
				x.file.Imports = append(x.file.Imports, imp)
				return true
			}
		}
	case "shell":
		if t.first && (t.text == "source" || t.text == ".") {
			fields := strings.Fields(x.lines[t.line-1])
			if len(fields) >= 2 {
				x.file.Imports = append(x.file.Imports, Import{Spec: strings.Trim(fields[1], `"'`), Line: t.line})
			}
			return true
		}
	case "dart":
		if (t.text == "import" || t.text == "export" || t.text == "part") && x.tok(i+1).kind == tString {
			imp := Import{Spec: x.toks[i+1].text, Line: t.line}
			if x.tok(i+2).text == "as" && x.tok(i+3).kind == tIdent {
				imp.Module = x.toks[i+3].text
			}
			x.file.Imports = append(x.file.Imports, imp)
			return true
		}
	case "perl":
		if (t.text == "use" || t.text == "require") && t.first && x.tok(i+1).kind == tIdent {
			x.dottedImport(i+1, "::", true)
			return true
		}
	case "elixir":
		if (t.text == "alias" || t.text == "import" || t.text == "use" || t.text == "require") && t.first {
			x.dottedImport(i+1, ".", true)
			return true
		}
	case "r":
		if (t.text == "library" || t.text == "require" || t.text == "source") && x.tok(i+1).text == "(" {
			arg := x.tok(i + 2)
			if arg.kind == tString || arg.kind == tIdent {
				x.file.Imports = append(x.file.Imports, Import{Spec: arg.text, Line: t.line})
				return true
			}
		}
	case "zig":
		if t.text == "@" && x.tok(i+1).text == "import" && x.tok(i+2).text == "(" && x.tok(i+3).kind == tString {
			imp := Import{Spec: x.toks[i+3].text, Line: t.line}
			if x.tok(i-1).text == "=" && x.tok(i-2).kind == tIdent {
				imp.Module = x.tok(i - 2).text
			}
			x.file.Imports = append(x.file.Imports, imp)
			return true
		}
	}
	return x.moreImports(i)
}

