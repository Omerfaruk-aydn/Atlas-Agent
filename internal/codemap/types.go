package codemap

// File is what one file defines, imports and uses. It depends only on the
// file's content, so it is cached by content hash.
type File struct {
	Path     string   `json:"path"`
	Language string   `json:"language"`
	Hash     string   `json:"hash"`
	Lines    int      `json:"lines"`
	Symbols  []Symbol `json:"symbols,omitempty"`
	Imports  []Import `json:"imports,omitempty"`
	Refs     []Ref    `json:"refs,omitempty"`
	// Links name other files by path: an embedded image, a linked
	// document, a texture a script loads.
	Links []FileLink `json:"links,omitempty"`
	// Asset describes an image, font, media or data file.
	Asset *Asset `json:"asset,omitempty"`
}

// FileLink is a reference to another file by its path.
type FileLink struct {
	// Target is the path as written, with any #fragment; a ${...} in it
	// stands for any name.
	Target string `json:"target"`
	Kind   string `json:"kind"`
	Line   int    `json:"line"`
}

// FileLink kinds, worded to read "a.md embeds b.png".
const (
	LinkEmbeds   = "embeds"
	LinkLinks    = "links to"
	LinkIncludes = "includes"
	LinkLoads    = "loads"
	LinkNames    = "names"
	LinkEntry    = "entry"
	LinkRuns     = "runs"
)

// Asset is what the map measures of a file it does not read.
type Asset struct {
	Bytes  int64  `json:"bytes"`
	Format string `json:"format,omitempty"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}

// Symbol is a definition: a function, method, class, type and the like.
type Symbol struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	// Container is the class, type or module the symbol is defined in.
	Container string `json:"container,omitempty"`
	Line      int    `json:"line"`
	EndLine   int    `json:"end_line"`
	// Signature is the definition's first line, trimmed.
	Signature string `json:"signature,omitempty"`
	Exported  bool   `json:"exported,omitempty"`
}

// Import is one import statement.
type Import struct {
	// Spec is the module, path or package as written.
	Spec string `json:"spec"`
	// Bindings are the names the import brings into the file.
	Bindings []Binding `json:"bindings,omitempty"`
	// Module is set when the import binds the whole module to a name
	// (import * as X, import pkg, import numpy as np), so X.name refers
	// to a definition in the module.
	Module string `json:"module,omitempty"`
	Line   int    `json:"line"`
}

// Binding is a name an import brings in: Local in this file for Remote in
// the imported module.
type Binding struct {
	Local  string `json:"local"`
	Remote string `json:"remote"`
}

// Ref is a use of a name: a call, an instantiation or a base type.
type Ref struct {
	Name string `json:"name"`
	// Qualifier is the name before the dot or ::, if any (pkg in
	// pkg.Func, this in this.update).
	Qualifier string `json:"qualifier,omitempty"`
	Kind      string `json:"kind"`
	Line      int    `json:"line"`
}

// Ref kinds. The first four link code to code; the others cross into
// stylesheets, markup, databases, build files, APIs and documentation.
const (
	RefCall    = "call"
	RefNew     = "new"
	RefInherit = "inherit"
	RefUse     = "use"
	// RefCSS uses a stylesheet class, mixin, variable or animation.
	RefCSS = "css"
	// RefDOM uses an element id defined in markup.
	RefDOM = "element"
	// RefTable reads or writes a database table, view or function.
	RefTable = "table"
	// RefTask runs a script, target, job, stage or service.
	RefTask = "task"
	// RefInfra uses an infrastructure resource, module or variable.
	RefInfra = "infra"
	// RefRoute requests an API endpoint.
	RefRoute = "route"
	// RefMention names a definition in documentation.
	RefMention = "mention"
)

// Confidence says how a link was established.
type Confidence string

const (
	// Resolved links go through an import, the same file or the same
	// package.
	Resolved Confidence = "resolved"
	// Inferred links match the only definition with that name in the
	// project.
	Inferred Confidence = "inferred"
	// Ambiguous links match several definitions; each is a candidate.
	Ambiguous Confidence = "ambiguous"
)
