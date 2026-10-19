package codemap

import (
	"path"
	"regexp"
	"strings"
)

// A string in code often names something outside the code: a texture a
// script loads, a template a handler renders, a table a query reads, an
// endpoint a client calls. The map links those too.

// linkableExts are the extensions a path in a string or a link may end
// in to be recorded: assets, documents, and the markup, stylesheets,
// shaders, templates and queries that code loads by name.
var linkableExts = func() map[string]bool {
	out := map[string]bool{}
	for ext := range assetKinds {
		out[ext] = true
	}
	for _, ext := range strings.Fields(".md .markdown .mdx .rst .adoc .html .htm .xhtml .css .scss .sass .less .glsl .vert .frag .wgsl .hbs .handlebars .ejs .jinja .jinja2 .j2 .njk .twig .liquid .mustache .erb .tscn .tres .sql .graphql .gql .ipynb") {
		out[ext] = true
	}
	return out
}()

// stringAt records what a string literal names, unless it is the module
// of an import just read.
func (x *extractor) stringAt(i int) {
	t := x.toks[i]
	imports := x.file.Imports
	for _, imp := range imports[max(0, len(imports)-3):] {
		if imp.Line <= t.line && t.line-imp.Line < 30 && strings.TrimPrefix(imp.Spec, "./") == strings.TrimPrefix(t.text, "./") {
			return
		}
	}
	scanString(x.file, t.text, t.line)
}

// scanString records the file path, the tables of a query or the endpoint
// a string at line holds.
func scanString(f *File, s string, line int) {
	if len(s) < 2 || len(s) > 20000 {
		return
	}
	if target, ok := pathInString(s); ok {
		addLink(f, FileLink{Target: target, Kind: LinkNames, Line: line})
		return
	}
	if markupRe.MatchString(s) {
		scanEmbeddedMarkup(f, s, line)
		return
	}
	if sqlLike(s) {
		for _, t := range sqlNames(s) {
			addRef(f, Ref{Name: t.name, Kind: RefTable, Line: line + strings.Count(s[:t.at], "\n")})
		}
		return
	}
	if route, ok := routeOf(s); ok {
		addRef(f, Ref{Name: route, Kind: RefRoute, Line: line})
	}
}

// markupRe finds a tag with attributes, or an element that is opened and
// closed, as in HTML a script builds: `<p class="kicker">${x}</p>`.
var markupRe = regexp.MustCompile(`<[A-Za-z][\w-]*\s+[\w:@.-]+\s*=\s*["'{$]|<([A-Za-z][\w-]*)[^<>]*>[^<]*</[A-Za-z]`)

// scanEmbeddedMarkup reads HTML written in a string at line: the classes,
// components and files its tags use, at their own lines. The ids it
// gives elements are not recorded as definitions: the string is built at
// run time, and the code around it is still being read.
func scanEmbeddedMarkup(f *File, s string, line int) {
	var m File
	scanMarkup(&m, s, markupHTML)
	shift := line - 1
	for _, r := range m.Refs {
		r.Line += shift
		addRef(f, r)
	}
	for _, l := range m.Links {
		l.Line += shift
		addLink(f, l)
	}
}

// addLink records a link unless the same one is already recorded at the
// same line.
func addLink(f *File, l FileLink) {
	for k := len(f.Links) - 1; k >= 0 && k >= len(f.Links)-8; k-- {
		if f.Links[k].Line == l.Line && f.Links[k].Target == l.Target {
			return
		}
	}
	if len(f.Links) < maxRefsPerFile {
		f.Links = append(f.Links, l)
	}
}

// hasScheme reports whether s is a URL of another place: a web address,
// inline data, an email.
func hasScheme(s string) bool {
	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "//") {
		return true
	}
	colon := strings.Index(lower, ":")
	if colon <= 0 || colon > 12 {
		return false
	}
	scheme := lower[:colon]
	if scheme == "res" || len(scheme) == 1 {
		// res:// is a Godot project path, C: a drive.
		return false
	}
	for _, r := range scheme {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '+', r == '-', r == '.':
		default:
			return false
		}
	}
	return true
}

// pathInString returns the file path s holds, without its query or
// fragment: one ending in an extension the map links, written without
// spaces, possibly with ${...} for a part chosen at run time.
func pathInString(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if len(s) < 3 || len(s) > 300 || hasScheme(s) || strings.ContainsAny(s, " \t\r\n\"'<>|*?%\\") && !strings.Contains(s, "${") {
		return "", false
	}
	clean := s
	if k := strings.IndexAny(clean, "?#"); k >= 0 {
		clean = clean[:k]
	}
	if strings.ContainsAny(stripTemplates(clean), "{}$ \t") {
		return "", false
	}
	ext := strings.ToLower(path.Ext(clean))
	if !linkableExts[ext] {
		return "", false
	}
	if base := path.Base(clean); len(base) <= len(ext) {
		return "", false
	}
	return clean, true
}

// stripTemplates removes ${...} parts.
func stripTemplates(s string) string {
	for {
		start := strings.Index(s, "${")
		if start < 0 {
			return s
		}
		end := strings.IndexByte(s[start:], '}')
		if end < 0 {
			return s
		}
		s = s[:start] + "x" + s[start+end+1:]
	}
}

// isCodePath reports whether a path names a file the map reads as code.
func isCodePath(p string) bool {
	return Category(LanguageOf(p)) == CategoryCode
}

var sqlStarts = words("select insert update delete with create alter drop merge replace upsert truncate")

// sqlLike reports whether s is a query: it starts, after comments, with a
// statement keyword followed by more words.
func sqlLike(s string) bool {
	t := strings.TrimSpace(stripSQLComments(s))
	if len(t) < 10 {
		return false
	}
	end := strings.IndexAny(t, " \t\r\n(")
	if end <= 0 {
		return false
	}
	return sqlStarts[strings.ToLower(t[:end])]
}

// stripSQLComments drops leading -- comment lines, keeping the newlines.
func stripSQLComments(s string) string {
	for {
		t := strings.TrimLeft(s, " \t\r\n")
		if !strings.HasPrefix(t, "--") {
			return s
		}
		nl := strings.IndexByte(t, '\n')
		if nl < 0 {
			return ""
		}
		s = t[nl:]
	}
}

var (
	sqlTableRe = regexp.MustCompile(`(?is)\b(from|join|into|update|references|table)\s+(?:only\s+|if\s+(?:not\s+)?exists\s+)?([A-Za-z_"\x60\[][\w."\x60\[\]$]*)`)
	sqlQueryRe = regexp.MustCompile(`(?m)^\s*--\s*name:\s*(\w+)`)
	sqlNotName = words("select lateral set values unnest only where on as with recursive exists not the a an each row statement")
)

type sqlName struct {
	name string
	at   int
}

// sqlNames returns the tables and named queries a query string uses, in
// lower case: SQL names are case-insensitive.
func sqlNames(s string) []sqlName {
	var out []sqlName
	for _, m := range sqlQueryRe.FindAllStringSubmatchIndex(s, -1) {
		// A query generated from a named query (sqlc's -- name: GetUser).
		out = append(out, sqlName{strings.ToLower(s[m[2]:m[3]]), m[2]})
	}
	for _, m := range sqlTableRe.FindAllStringSubmatchIndex(s, -1) {
		name := sqlIdent(s[m[4]:m[5]])
		if name != "" && !sqlNotName[name] {
			out = append(out, sqlName{name, m[4]})
		}
	}
	return out
}

// sqlIdent normalizes a possibly quoted, possibly schema-qualified name to
// its lower-case last part.
func sqlIdent(s string) string {
	s = strings.Trim(s, ".")
	if k := strings.LastIndex(s, "."); k >= 0 {
		s = s[k+1:]
	}
	s = strings.Trim(s, "\"`[]")
	if len(s) < 2 {
		return ""
	}
	return strings.ToLower(s)
}

// routeOf returns the endpoint path a string requests, with parameters
// written {}: /users/${id} and /users/:id become /users/{}.
func routeOf(s string) (string, bool) {
	if !strings.HasPrefix(s, "/") || strings.HasPrefix(s, "//") || len(s) > 200 || strings.ContainsAny(s, " \t\r\n\"'<>") {
		return "", false
	}
	if k := strings.IndexAny(s, "?#"); k >= 0 {
		s = s[:k]
	}
	if path.Ext(stripTemplates(s)) != "" {
		return "", false
	}
	return normRoute(s)
}

// normRoute writes a path's parameters as {} and reports whether it has a
// named segment.
func normRoute(p string) (string, bool) {
	var segs []string
	named := false
	for _, seg := range strings.Split(p, "/") {
		switch {
		case seg == "":
			continue
		case strings.HasPrefix(seg, ":") || strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") || strings.HasPrefix(seg, "${") || strings.HasPrefix(seg, "[") && strings.HasSuffix(seg, "]"):
			seg = "{}"
		default:
			if strings.Contains(seg, "${") {
				seg = "{}"
			} else {
				named = true
			}
		}
		segs = append(segs, seg)
	}
	if !named || len(segs) > 12 {
		return "", false
	}
	return "/" + strings.Join(segs, "/"), true
}
