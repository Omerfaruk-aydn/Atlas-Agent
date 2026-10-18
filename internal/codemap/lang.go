// Package codemap builds a map of a project without a parser library: its
// code in every common language, its documentation and its assets. Go is
// read with the standard library's parser; every other language with a
// tokenizer that knows the language's comments and strings, and a table of
// the words that start a definition or an import. Markup, stylesheets,
// schemas, build files and documents have readers of their own, and images
// are measured from their headers.
//
// The map links what each file defines to what uses it: calls across
// languages, a stylesheet's classes to the markup and scripts that use
// them, a table to the queries that read it, a document to the code it
// mentions and the images it embeds. It ranks files by how much the rest of
// the project depends on them, groups them into subsystems, and answers
// where something is defined, who uses it and how two parts connect.
//
// Every link carries how sure it is: resolved through an import or the
// same file, inferred from a name defined once in the project, or
// ambiguous between several definitions. The map is a guide to the
// project, not a replacement for reading it.
package codemap

import (
	"path"
	"strings"
)

// family is how a language marks where a definition ends.
type family int

