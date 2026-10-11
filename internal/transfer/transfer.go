// Package transfer moves files across a trust boundary (a remote node, an
// isolated guest, a phone upload, an Android device) with explicit limits:
// a relative destination under a root, no traversal, no symlink escape, a
// size bound and a SHA-256 the received bytes must match before the file
// becomes visible at its final path.
package transfer

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// DefaultMaxBytes bounds a single transfer unless a caller sets its own.
const DefaultMaxBytes int64 = 64 << 20

// Error is a classified transfer failure. Code is stable for tools.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func fail(code, format string, args ...any) error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Code extracts the classification of err, or "io_error".
func Code(err error) string {
	var te *Error
	if errors.As(err, &te) {
		return te.Code
	}
	return "io_error"
}

var hexSHA = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ValidSHA256 reports whether s is a lowercase hex SHA-256.
func ValidSHA256(s string) bool { return hexSHA.MatchString(s) }

// Resolve maps a slash-separated relative path to an absolute path under
// root. It rejects absolute paths, drive letters, UNC prefixes, "..", NUL
// bytes and any existing path component that is a symlink resolving outside
// root. The final component may not exist yet.
func Resolve(root, rel string) (string, error) {
	if root == "" || !filepath.IsAbs(root) {
		return "", fail("invalid_root", "transfer root must be absolute")
	}
	if rel == "" || strings.ContainsRune(rel, 0) || len(rel) > 1024 {
		return "", fail("invalid_path", "path must be a nonempty relative path")
	}
	norm := strings.ReplaceAll(rel, "\\", "/")
	if strings.HasPrefix(norm, "/") || filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" || (len(norm) > 1 && norm[1] == ':') {
		return "", fail("absolute_path", "path %q must be relative to the transfer root", rel)
	}
	for part := range strings.SplitSeq(norm, "/") {
		if part == ".." {
			return "", fail("path_traversal", "path %q leaves the transfer root", rel)
		}
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fail("invalid_root", "transfer root: %v", err)
	}
	full := filepath.Join(realRoot, filepath.FromSlash(norm))
	if !within(realRoot, full) {
		return "", fail("path_traversal", "path %q leaves the transfer root", rel)
	}
	// Follow every existing link component and refuse a path that lands
	// outside the root. filepath.EvalSymlinks does not evaluate Windows
	// junctions (mount points), so links are resolved component by
	// component here instead.
	linkedRoot, err := resolveLinks(realRoot, 0)
	if err != nil {
		return "", fail("invalid_root", "transfer root: %v", err)
	}
	resolved, err := resolveLinks(full, 0)
	if err != nil {
		return "", fail("symlink_escape", "cannot resolve %q: %v", rel, err)
	}
	if !within(linkedRoot, resolved) {
		return "", fail("symlink_escape", "path %q resolves outside the transfer root", rel)
	}
	return full, nil
}

// maxLinkHops bounds link resolution so a cycle cannot loop forever.
const maxLinkHops = 40

// isLink reports a symlink or a Windows reparse point such as a junction,
// which Lstat reports as an irregular file.
func isLink(info os.FileInfo) bool {
	return info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0
}

// resolveLinks returns path with every existing link component replaced by
// its target. Components that do not exist yet are kept as they are.
func resolveLinks(path string, hops int) (string, error) {
	path = filepath.Clean(path)
	volume := filepath.VolumeName(path)
	rest := strings.TrimPrefix(path[len(volume):], string(filepath.Separator))
	cur := volume + string(filepath.Separator)
	parts := strings.Split(rest, string(filepath.Separator))
	for i, part := range parts {
		if part == "" {
			continue
		}
		next := filepath.Join(cur, part)
		info, err := os.Lstat(next)
		if err != nil {
			return filepath.Join(append([]string{next}, parts[i+1:]...)...), nil
		}
		if !isLink(info) {
			cur = next
			continue
		}
		target, err := os.Readlink(next)
		if err != nil {
			if info.Mode()&os.ModeSymlink == 0 {
				// An irregular file that is not a link (for example a
				// deduplicated file) is an ordinary path component.
				cur = next
				continue
			}
			return "", err
		}
		if hops >= maxLinkHops {
			return "", errors.New("too many links")
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(cur, target)
		}
		// Readlink may return a \\?\ prefixed path for a junction.
		target = strings.TrimPrefix(target, `\\?\`)
		resolvedTarget, err := resolveLinks(target, hops+1)
		if err != nil {
			return "", err
		}
		cur = resolvedTarget
	}
	return cur, nil
}

func within(root, path string) bool {
	r, err := filepath.Rel(root, path)
	return err == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator)) && !filepath.IsAbs(r)
}

// Receive streams src into root/rel. It writes to a temp file in the target
// directory, enforces maxBytes and the exact expected size, checks the
// SHA-256 and only then renames into place. An existing symlink at the
// destination is never followed or replaced. overwrite controls whether an
// existing regular file may be replaced.
func Receive(root, rel string, src io.Reader, size int64, sha string, maxBytes int64, overwrite bool) (string, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	if size < 0 || size > maxBytes {
		return "", fail("too_large", "transfer of %d bytes exceeds the %d byte limit", size, maxBytes)
	}
	if !ValidSHA256(sha) {
		return "", fail("invalid_hash", "sha256 must be 64 lowercase hex characters")
	}
	dest, err := Resolve(root, rel)
	if err != nil {
		return "", err
	}
	if info, err := os.Lstat(dest); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fail("symlink_escape", "destination %q is a symlink", rel)
		}
		if info.IsDir() {
			return "", fail("invalid_path", "destination %q is a directory", rel)
		}
		if !overwrite {
			return "", fail("exists", "destination %q exists; pass overwrite to replace it", rel)
		}
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	// Re-resolve after creating parents: a racing symlink would be caught.
	if again, err := Resolve(root, rel); err != nil || again != dest {
		return "", fail("symlink_escape", "destination changed while preparing %q", rel)
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".atlas-transfer-*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	hasher := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, hasher), io.LimitReader(src, maxBytes+1))
	if err != nil {
		tmp.Close()
		return "", fail("interrupted", "transfer interrupted after %d bytes: %v", n, err)
	}
	if n > maxBytes {
		tmp.Close()
		return "", fail("too_large", "transfer exceeded the %d byte limit", maxBytes)
	}
	if n != size {
		tmp.Close()
		return "", fail("size_mismatch", "received %d bytes, expected %d", n, size)
	}
	got := hex.EncodeToString(hasher.Sum(nil))
	if got != sha {
		tmp.Close()
		return "", fail("hash_mismatch", "received sha256 %s, expected %s", got, sha)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmpName, dest); err != nil {
		return "", err
	}
	return dest, nil
}

// Open resolves root/rel for reading and returns the file, its size and
// SHA-256. Symlinks resolving outside root and non-regular files are refused.
func Open(root, rel string, maxBytes int64) (*os.File, int64, string, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	path, err := Resolve(root, rel)
	if err != nil {
		return nil, 0, "", err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, 0, "", fail("not_found", "%s: %v", rel, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, 0, "", fail("symlink_escape", "source %q is a symlink", rel)
	}
	if !info.Mode().IsRegular() {
		return nil, 0, "", fail("invalid_path", "source %q is not a regular file", rel)
	}
	if info.Size() > maxBytes {
		return nil, 0, "", fail("too_large", "source is %d bytes; limit is %d", info.Size(), maxBytes)
	}
	sum, err := HashFile(path)
	if err != nil {
		return nil, 0, "", err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, "", err
	}
	return f, info.Size(), sum, nil
}

// HashFile returns the lowercase hex SHA-256 of a file.
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// HashBytes returns the lowercase hex SHA-256 of data.
func HashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
