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

