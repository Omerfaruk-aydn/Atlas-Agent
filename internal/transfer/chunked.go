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

