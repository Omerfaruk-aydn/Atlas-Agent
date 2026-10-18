// Package semver compares Semantic Versioning 2.0.0 version strings.
package semver

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrInvalid is wrapped by the error Compare returns for a malformed version.
var ErrInvalid = errors.New("semver: invalid version")

type version struct {
	core [3]uint64
	pre  []string
}

// Compare returns -1, 0 or 1 when a has lower, equal or higher precedence
// than b.
func Compare(a, b string) (int, error) {
	va, err := parse(a)
	if err != nil {
		return 0, err
	}
	vb, err := parse(b)
	if err != nil {
		return 0, err
	}
	for i := range va.core {
		if c := cmpUint(va.core[i], vb.core[i]); c != 0 {
			return c, nil
		}
	}
	switch {
	case len(va.pre) == 0 && len(vb.pre) == 0:
		return 0, nil
	case len(va.pre) == 0:
		return 1, nil
	case len(vb.pre) == 0:
		return -1, nil
	}
	for i := 0; i < len(va.pre) && i < len(vb.pre); i++ {
		if c := comparePre(va.pre[i], vb.pre[i]); c != 0 {
			return c, nil
		}
	}
	return cmpUint(uint64(len(va.pre)), uint64(len(vb.pre))), nil
}

func invalid(s, why string) error {
	return fmt.Errorf("%w: %q: %s", ErrInvalid, s, why)
}

func parse(s string) (version, error) {
	var v version
	rest := strings.TrimPrefix(s, "v")
	if i := strings.IndexByte(rest, '+'); i >= 0 {
		if err := checkIdentifiers(s, rest[i+1:], false); err != nil {
			return v, err
		}
		rest = rest[:i]
	}
	if i := strings.IndexByte(rest, '-'); i >= 0 {
		pre := rest[i+1:]
		if err := checkIdentifiers(s, pre, true); err != nil {
			return v, err
		}
		v.pre = strings.Split(pre, ".")
		rest = rest[:i]
	}
	parts := strings.Split(rest, ".")
	if len(parts) != 3 {
		return v, invalid(s, "expected MAJOR.MINOR.PATCH")
	}
	for i, p := range parts {
		if !isNumeric(p) || (len(p) > 1 && p[0] == '0') {
			return v, invalid(s, "core parts must be numbers without leading zeros")
		}
		n, err := strconv.ParseUint(p, 10, 64)
		if err != nil {
			return v, invalid(s, "core part out of range")
		}
		v.core[i] = n
	}
	return v, nil
}

// checkIdentifiers validates a dot separated identifier list. Numeric
// prerelease identifiers must not have leading zeros; build identifiers may.
func checkIdentifiers(whole, list string, prerelease bool) error {
	if list == "" {
		return invalid(whole, "empty suffix")
	}
	for _, id := range strings.Split(list, ".") {
		if id == "" {
			return invalid(whole, "empty identifier")
		}
		for _, r := range id {
			if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '-') {
				return invalid(whole, "identifiers may only contain [0-9A-Za-z-]")
			}
		}
		if prerelease && isNumeric(id) && len(id) > 1 && id[0] == '0' {
			return invalid(whole, "numeric prerelease identifiers must not have leading zeros")
		}
	}
	return nil
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func comparePre(a, b string) int {
	an, bn := isNumeric(a), isNumeric(b)
	switch {
	case an && bn:
		// Compare by length first so very long numbers need no parsing.
		if len(a) != len(b) {
			return cmpUint(uint64(len(a)), uint64(len(b)))
		}
		return strings.Compare(a, b)
	case an:
		return -1
	case bn:
		return 1
	default:
		return strings.Compare(a, b)
	}
}

func cmpUint(a, b uint64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
