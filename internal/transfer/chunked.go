package transfer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// MaxChunkBytes bounds one upload chunk, so a pause, cancel or disconnect
// never waits behind more than a few seconds of data.
const MaxChunkBytes int64 = 4 << 20

// Conflict decides what happens when the destination already exists.
type Conflict string

const (
	// ConflictError refuses to replace an existing file.
	ConflictError Conflict = "error"
	// ConflictRename keeps the existing file and saves as "name (1).ext".
	ConflictRename Conflict = "rename"
	// ConflictOverwrite replaces an existing regular file.
	ConflictOverwrite Conflict = "overwrite"
)

// ParseConflict maps a request value to a policy; empty means error.
func ParseConflict(s string) (Conflict, error) {
	switch Conflict(s) {
	case "", ConflictError:
		return ConflictError, nil
	case ConflictRename, ConflictOverwrite:
		return Conflict(s), nil
	}
	return "", fail("invalid_request", "conflict must be error, rename or overwrite")
}

// Destination resolves root/rel for writing under policy. It returns the
// path the file will have, which differs from rel only for rename.
func Destination(root, rel string, policy Conflict) (string, error) {
	dest, err := Resolve(root, rel)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(dest)
	if errors.Is(err, os.ErrNotExist) {
		return dest, nil
	}
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fail("symlink_escape", "destination %q is a symlink", rel)
	}
	if info.IsDir() {
		return "", fail("invalid_path", "destination %q is a directory", rel)
	}
	switch policy {
	case ConflictOverwrite:
		return dest, nil
	case ConflictRename:
		ext := filepath.Ext(dest)
		base := strings.TrimSuffix(dest, ext)
		for i := 1; i < 1000; i++ {
			candidate := fmt.Sprintf("%s (%d)%s", base, i, ext)
			if _, err := os.Lstat(candidate); errors.Is(err, os.ErrNotExist) {
				return candidate, nil
			}
		}
		return "", fail("exists", "no free name for %q", rel)
	}
	return "", fail("exists", "destination %q exists; choose rename or overwrite", rel)
}

// Install moves a verified file into root/rel. The bytes are hashed again
// while they are copied next to the destination, then renamed into place,
// so a half-copied or substituted file never appears at the final path.
func Install(root, rel, src string, size int64, sha string, policy Conflict) (string, error) {
	if !ValidSHA256(sha) {
		return "", fail("invalid_hash", "sha256 must be 64 lowercase hex characters")
	}
	dest, err := Destination(root, rel, policy)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	// Re-resolve after creating parents: a racing symlink would be caught.
	if _, err := Resolve(root, rel); err != nil {
		return "", err
	}
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".atlas-transfer-*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), in)
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", fail("interrupted", "install interrupted after %d bytes: %v", n, err)
	}
	if n != size {
		return "", fail("size_mismatch", "installed %d bytes, expected %d", n, size)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != sha {
		return "", fail("hash_mismatch", "received sha256 %s, expected %s", got, sha)
	}
	if policy != ConflictOverwrite {
		// The name was free when chosen; refuse if something appeared since.
		if _, err := os.Lstat(dest); err == nil {
			return "", fail("exists", "destination %q appeared during the transfer", filepath.Base(dest))
		}
	}
	if err := os.Rename(tmpName, dest); err != nil {
		return "", err
	}
	return dest, nil
}

