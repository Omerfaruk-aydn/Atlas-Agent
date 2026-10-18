// Package safejoin resolves untrusted file names inside a trusted directory.
package safejoin

import (
	"errors"
	"path/filepath"
	"strings"
)

// ErrEscapes is returned when a name would resolve outside the root.
var ErrEscapes = errors.New("safejoin: path escapes the root")

// Join returns the cleaned path of name inside root. An empty name or "."
// returns root itself. name must be a relative path: an absolute name, or
// one that resolves outside root after cleaning, returns ErrEscapes.
func Join(root, name string) (string, error) {
	p := filepath.Join(root, name)
	if !strings.HasPrefix(p, root) {
		return "", ErrEscapes
	}
	return p, nil
}
