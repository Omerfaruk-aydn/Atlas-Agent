// Package semver compares Semantic Versioning 2.0.0 version strings.
package semver

import "errors"

// ErrInvalid is wrapped by the error Compare returns for a malformed version.
var ErrInvalid = errors.New("semver: invalid version")

// Compare returns -1, 0 or 1 when a has lower, equal or higher precedence
// than b.
func Compare(a, b string) (int, error) {
	return 0, errors.New("semver: not implemented")
}
