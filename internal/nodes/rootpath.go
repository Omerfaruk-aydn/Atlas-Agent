
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
