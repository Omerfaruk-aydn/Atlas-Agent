package codemap

import (
	"path"
	"strings"
)

// Definitions live in domains: a call in code means a function, a class
// in markup means a stylesheet's class, a table in a query means a SQL
// table. A use is resolved only within its domain, so a heading named
// Install never answers a call of install() and a CSS class named button
// never answers a function.
const (
	domainCode  = "code"
	domainDoc   = "doc"
	domainCSS   = "css"
	domainDOM   = "dom"
	domainSQL   = "sql"
	domainTask  = "task"
	domainInfra = "infra"
	domainRoute = "route"
	domainScene = "scene"
)

var cssKinds = words("mixin keyframes placeholder")

// symbolDomain returns the domain of a definition of kind in a file of
// lang.
func symbolDomain(lang, kind string) string {
	switch kind {
	case "section":
		return domainDoc
	case "element id":
		return domainDOM
	case "endpoint":
		return domainRoute
	case "css class", "css variable", "scss variable", "less variable":
		return domainCSS
	}
	if cssKinds[kind] {
		return domainCSS
	}
	switch lang {
	case "css", "scss", "less":
		return domainCSS
	case "sql":
		return domainSQL
	case "graphql", "protobuf", "thrift", "prisma", "openapi":
		return "schema:" + lang
	case "terraform":
		return domainInfra
	case "npm", "make", "just", "taskfile", "docker", "compose", "workflow", "gitlab-ci", "pyproject", "cargo":
		return domainTask
	case "markdown", "rst", "asciidoc":
		return domainDoc
	case "godot":
		return domainScene
	}
	return domainCode
}

// domainKey is how a definition is looked up within its domain.
func domainKey(domain string, s *Symbol) string {
	switch domain {
	case domainCSS:
		switch s.Kind {
		case "keyframes", "mixin", "function":
			return "@" + s.Kind + ":" + s.Name
		}
	case domainSQL:
		return strings.ToLower(s.Name)
	case domainTask:
		return s.Container + "." + s.Name
	case domainRoute:
		_, p, _ := strings.Cut(s.Name, " ")
		if route, ok := normRoute(p); ok {
			return route
		}
	}
	return s.Name
}

// refDomain returns the domain a use of kind in a file of lang looks in.
func refDomain(lang, kind string) string {
	switch kind {
	case RefCSS:
		return domainCSS
	case RefDOM:
		return domainDOM
	case RefTable:
		return domainSQL
	case RefTask:
		return domainTask
	case RefInfra:
		return domainInfra
	case RefRoute:
		return domainRoute
	case RefMention:
		return domainCode
	}
	switch lang {
	case "graphql", "protobuf", "thrift", "prisma", "openapi":
		return "schema:" + lang
	}
	return domainCode
}

// refKey is how a use is looked up within its domain.
func refKey(domain string, r Ref) string {
	switch domain {
	case domainCSS:
		if strings.HasPrefix(r.Qualifier, "@") {
			return r.Qualifier + ":" + r.Name
		}
	case domainSQL:
		return strings.ToLower(r.Name)
	case domainTask:
		return r.Qualifier + "." + r.Name
	}
	return r.Name
}

// nearFiles returns the files whose definitions a use in fi resolves to
// first: those a page and its scripts share for classes and element ids,
// the imported ones for a schema, the same directory for tasks and
// infrastructure, which are scoped by it.
func (m *Map) nearFiles(fi int, domain string) map[int]bool {
	near := map[int]bool{fi: true}
	imported := func(f int) {
		for _, targets := range m.imports[f] {
			for _, t := range targets {
				near[t] = true
			}
		}
	}
	switch {
	case domain == domainCSS || domain == domainDOM:
		imported(fi)
		for _, page := range m.importedBy[fi] {
			near[page] = true
			imported(page)
		}
	case strings.HasPrefix(domain, "schema:"):
		imported(fi)
	case domain == domainTask || domain == domainInfra:
		for _, f := range m.dirs[path.Dir(m.Files[fi].Path)] {
			near[f] = true
		}
	}
	return near
}

// resolveDomainRef finds the definitions a use outside code can mean.
func (m *Map) resolveDomainRef(fi int, r Ref, domain string, near map[int]bool, cssModules map[string][]int) ([]int, Confidence) {
	idx := m.byDomain[domain]
	var ids []int
	if domain == domainRoute {
		ids = m.routeCandidates(r.Name)
	} else {
		ids = idx[refKey(domain, r)]
	}
	if domain == domainCSS && r.Qualifier != "" && !strings.HasPrefix(r.Qualifier, "@") {
		// styles.button: a class of the CSS module bound to styles, also
		// written styles.primaryButton for .primary-button.
		files, ok := cssModules[r.Qualifier]
		if !ok {
			return nil, ""
		}
		var out []int
		for _, id := range append(append([]int(nil), ids...), idx[kebabCase(r.Name)]...) {
			for _, f := range files {
				if m.symbols[id].file == f {
					out = appendUnique(out, id)
				}
			}
		}
		if len(out) == 0 {
			return nil, ""
		}
		return limit(out), Resolved
	}
	if len(ids) == 0 {
		return nil, ""
	}
	var local []int
	for _, id := range ids {
		if near[m.symbols[id].file] {
			local = append(local, id)
		}
	}
	if len(local) > 0 {
		return limit(local), Resolved
	}
	if domain == domainInfra {
		// var.x and module.y mean the module's own.
		return nil, ""
	}
	if len(ids) == 1 {
		return ids, Inferred
	}
	return candidates(ids)
}

// kebabCase turns primaryButton into primary-button.
func kebabCase(s string) string {
	var b strings.Builder
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r + 'a' - 'A')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

