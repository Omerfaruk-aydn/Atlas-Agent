package codemap

import (
	"path"
	"regexp"
	"sort"
	"strings"
)

// Markup and stylesheets: the scripts and styles a page loads and embeds,
// the images it shows, the element ids it defines, the classes it uses and
// the components a template renders; a stylesheet's classes, variables,
// mixins and animations and what uses them.

// block is a <script> or <style> element's content: start and end are
// the offsets of what is between its tags.
type block struct {
	start, end int
	kind       string
	lang       string
}

