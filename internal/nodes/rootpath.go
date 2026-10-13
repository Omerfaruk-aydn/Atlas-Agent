package nodes

import (
	"fmt"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/transfer"
)

// matchRoot selects the allowed root a request path belongs to and returns
// the remainder relative to it. A path is either label-relative
// ("C/Users/x", "Work/report.pdf") or absolute on the remote computer
// ("C:\Users\x", "C:/Users/x", "/home/x"). Absolute paths are matched on
// directory boundaries against each root's own absolute path, never by a
// bare string prefix, so "C:\Work2" is not inside "C:\Work". A
// drive-relative path such as "C:foo" depends on a per-drive working
// directory the controller cannot see, so it is refused.
//
// The remainder is still resolved by transfer.Resolve, which refuses "..",
// symlink and junction escapes.
func matchRoot(roots []Root, rel string) (Root, string, error) {
	norm := strings.ReplaceAll(rel, `\`, "/")
	if isDriveRelative(norm) {
		return Root{}, "", &transfer.Error{Code: "path_denied", Message: fmt.Sprintf("path %q is drive-relative; name it as %c:/folder/file or %c/folder/file", rel, norm[0], norm[0])}
	}
	if isAbsoluteRemotePath(norm) {
		var best Root
		var bestRest string
		found := false
		for _, r := range roots {
			rest, ok := withinRoot(r.Path, norm)
			if ok && (!found || len(r.Path) > len(best.Path)) {
				best, bestRest, found = r, rest, true
			}
		}
		if found {
			return best, bestRest, nil
		}
		// A leading slash with only drive-letter roots is the legacy
		// "/Label/path" spelling; fall through to label matching.
		if norm[0] != '/' || hasPOSIXRoot(roots) {
			return Root{}, "", deniedPath(roots, rel)
		}
	}
	label, rest, _ := strings.Cut(strings.TrimLeft(norm, "/"), "/")
	for _, r := range roots {
		if r.Label == label {
			return r, rest, nil
		}
	}
	return Root{}, "", deniedPath(roots, rel)
}

// deniedPath names every allowed folder with its real path, so a caller
// that sees the label "C" also learns it means C:\.
func deniedPath(roots []Root, rel string) error {
	names := make([]string, 0, len(roots))
	for _, r := range sortedRoots(roots) {
		names = append(names, fmt.Sprintf("%s (%s)", r.Label, r.Path))
	}
	return &transfer.Error{Code: "path_denied", Message: fmt.Sprintf("path %q is not under an allowed folder; allowed: %s; name a file as <label>/<path> or by its absolute path inside one of them", rel, strings.Join(names, ", "))}
}

func isDriveLetter(c byte) bool { return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') }

func hasDrivePrefix(norm string) bool {
	return len(norm) >= 2 && isDriveLetter(norm[0]) && norm[1] == ':'
}

// isDriveRelative reports "C:" or "C:foo"; slashes are already normalized.
func isDriveRelative(norm string) bool {
	return hasDrivePrefix(norm) && (len(norm) == 2 || norm[2] != '/')
}

// isAbsoluteRemotePath reports "C:/..." or "/...". It does not depend on
// the platform this process runs on: the path names a file on the remote
// computer.
func isAbsoluteRemotePath(norm string) bool {
	return (hasDrivePrefix(norm) && len(norm) >= 3 && norm[2] == '/') || strings.HasPrefix(norm, "/")
}

func hasPOSIXRoot(roots []Root) bool {
	for _, r := range roots {
		if strings.HasPrefix(strings.ReplaceAll(r.Path, `\`, "/"), "/") {
			return true
		}
	}
	return false
}

// withinRoot reports whether the absolute path norm lies in rootPath on a
// directory boundary and returns the remainder ("." for the root itself).
// Drive-letter paths compare case-insensitively, as Windows does.
func withinRoot(rootPath, norm string) (string, bool) {
	base := strings.TrimRight(strings.ReplaceAll(rootPath, `\`, "/"), "/")
	windows := hasDrivePrefix(base)
	if windows != hasDrivePrefix(norm) {
		return "", false
	}
	if base == "" {
		if !strings.HasPrefix(norm, "/") {
			return "", false
		}
		return orDot(strings.TrimLeft(norm, "/")), true
	}
	if len(norm) < len(base) {
		return "", false
	}
	head, tail := norm[:len(base)], norm[len(base):]
	if windows && !strings.EqualFold(head, base) || !windows && head != base {
		return "", false
	}
	if tail == "" {
		return ".", true
	}
	if tail[0] != '/' {
		return "", false
	}
	return orDot(strings.TrimLeft(tail, "/")), true
}
