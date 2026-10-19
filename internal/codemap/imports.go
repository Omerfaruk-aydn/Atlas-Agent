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

// stringAfter returns the string at k, or inside ( at k.
func (x *extractor) stringAfter(k int) lexToken {
	if x.tok(k).text == "(" {
		k++
	}
	return x.tok(k)
}

// dottedImport reads a.b.C (joined by sep) at k and records it, binding
// the last name when bind is set. A trailing * or a {A, B} group binds
// nothing or each name in it.
func (x *extractor) dottedImport(k int, sep string, bind bool) {
	if x.tok(k).text == "static" {
		k++
	}
	line := x.tok(k).line
	var parts []string
	for x.tok(k).kind == tIdent {
		parts = append(parts, x.toks[k].text)
		if x.tok(k+1).text != sep || x.tok(k+1).line != line {
			k++
			break
		}
		k += 2
	}
	if len(parts) == 0 {
		return
	}
	imp := Import{Spec: strings.Join(parts, sep), Line: line}
	switch {
	case x.tok(k).text == "*" || x.tok(k).text == "_":
		imp.Spec += sep + "*"
	case x.tok(k).text == "{" && x.tok(k).line == line:
		for j := k + 1; j < len(x.toks) && x.toks[j].text != "}"; j++ {
			if x.toks[j].kind == tIdent && x.tok(j-1).text != "as" && x.tok(j-1).text != "=>" {
				local := x.toks[j].text
				if a := x.tok(j + 1).text; (a == "as" || a == "=>") && x.tok(j+2).kind == tIdent {
					local = x.toks[j+2].text
				}
				imp.Bindings = append(imp.Bindings, Binding{Local: local, Remote: x.toks[j].text})
			}
		}
	case bind:
		local := parts[len(parts)-1]
		if x.tok(k).text == "as" && x.tok(k+1).kind == tIdent {
			local = x.toks[k+1].text
		}
		imp.Bindings = []Binding{{Local: local, Remote: parts[len(parts)-1]}}
		imp.Module = local
	}
	x.file.Imports = append(x.file.Imports, imp)
}

// includeImport reads "file.h" or <file.h> at k.
func (x *extractor) includeImport(k int) {
	t := x.tok(k)
	switch {
	case t.kind == tString:
		x.file.Imports = append(x.file.Imports, Import{Spec: t.text, Line: t.line})
	case t.text == "<":
		var b strings.Builder
		for j := k + 1; j < len(x.toks) && j < k+32 && x.toks[j].text != ">" && x.toks[j].line == t.line; j++ {
			b.WriteString(x.toks[j].text)
		}
		x.file.Imports = append(x.file.Imports, Import{Spec: "<" + b.String() + ">", Line: t.line})
	}
}

