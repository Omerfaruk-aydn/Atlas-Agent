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

