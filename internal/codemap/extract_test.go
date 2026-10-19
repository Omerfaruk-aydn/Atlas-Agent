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

