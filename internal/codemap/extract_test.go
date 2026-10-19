package codemap

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// symbolsOf lists a file's symbols as "kind container.name line-end".
func symbolsOf(f File) []string {
	var out []string
	for _, s := range f.Symbols {
		name := s.Name
		if s.Container != "" {
			name = s.Container + "." + name
		}
		out = append(out, fmt.Sprintf("%s %s %d-%d", s.Kind, name, s.Line, s.EndLine))
	}
	return out
}

func importsOf(f File) []string {
	var out []string
	for _, imp := range f.Imports {
		line := imp.Spec
		if imp.Module != "" {
			line += " as " + imp.Module
		}
		for _, b := range imp.Bindings {
			if b.Local == b.Remote {
				line += " " + b.Local
			} else {
				line += " " + b.Remote + "=>" + b.Local
			}
		}
		out = append(out, line)
	}
	return out
}

func refsOf(f File) []string {
	var out []string
	for _, r := range f.Refs {
		name := r.Name
		if r.Qualifier != "" {
			name = r.Qualifier + "." + name
		}
		out = append(out, r.Kind+" "+name)
	}
	return out
}

