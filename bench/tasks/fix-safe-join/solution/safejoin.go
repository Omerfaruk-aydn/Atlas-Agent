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
	if filepath.IsAbs(name) || filepath.VolumeName(name) != "" {
		return "", ErrEscapes
	}
	p := filepath.Join(root, name)
	rel, err := filepath.Rel(root, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", ErrEscapes
	}
	return p, nil
}
