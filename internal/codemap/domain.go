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

