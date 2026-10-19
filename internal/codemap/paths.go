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

// maxPatternMatches bounds what one ${...} path reaches.
const maxPatternMatches = 200

// resolvePaths resolves every FileLink. A link between two code files is
// also a dependency, weaker than an import.
func (m *Map) resolvePaths() {
	m.paths = make([][]pathTarget, len(m.Files))
	m.pathIn = map[int][]pathUse{}
	m.anchorIn = map[int][]pathUse{}
	for fi, f := range m.Files {
		m.paths[fi] = make([]pathTarget, len(f.Links))
		fixtures := isTestPath(f.Path)
		for k, l := range f.Links {
			if fixtures && l.Kind == LinkNames {
				// A test names files of the trees it builds, not the
				// project's own.
				m.paths[fi][k] = pathTarget{section: -1, fixture: true}
				continue
			}
			t := m.resolvePath(fi, l.Target)
			m.paths[fi][k] = t
			for _, target := range t.files {
				if target == fi {
					continue
				}
				m.pathIn[target] = append(m.pathIn[target], pathUse{fi, k})
				if m.code[fi] && m.code[target] && !t.pattern {
					m.fileLinks[[2]int{fi, target}] += 0.5
				}
			}
			if t.section >= 0 {
				m.anchorIn[t.section] = append(m.anchorIn[t.section], pathUse{fi, k})
			}
		}
	}
}

