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

