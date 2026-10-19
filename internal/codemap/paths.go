package codemap

import (
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// pathTarget is what a FileLink resolved to.
type pathTarget struct {
	// files are the files the link reaches; none means its target is
	// missing. A link with ${...} in it may reach several.
	files   []int
	pattern bool
	// anchor is the #fragment; section the heading it names in a
	// document, or -1. brokenAnchor is set when the document has no such
	// heading.
	anchor       string
	section      int
	brokenAnchor bool
	// fixture marks a path a test names, which is left unresolved.
	fixture bool
	// outside marks an absolute path that is not in the project.
	outside bool
	// unread is the file or directory the link names when the map does
	// not read it (a JSON file, a folder of them).
	unread string
}

// pathUse locates a link: Files[file].Links[link].
type pathUse struct{ file, link int }

