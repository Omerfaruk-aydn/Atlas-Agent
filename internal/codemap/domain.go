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

