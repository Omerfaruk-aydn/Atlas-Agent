package rehearsal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DefaultMaxCopyBytes bounds how much of the project one rehearsal copies.
const DefaultMaxCopyBytes int64 = 1 << 30

// entry records what one file looked like when it was copied. Size and
// modification time let a later scan skip hashing files nothing touched.
type entry struct {
	Hash    string
	Size    int64
	ModTime int64
	Mode    fs.FileMode
}

// manifest maps slash-separated relative paths to their entries.
type manifest map[string]entry

// skipped reports whether a relative directory never enters the copy.
// Version-control internals and Atlas' own state are not part of the work
// being rehearsed, and a rehearsal inside a rehearsal would recurse.
func skipped(rel string) bool {
	switch rel {
	case ".git", ".atlas", ".claude/worktrees":
		return true
	}
	return false
}

// copyTree copies src into dst and returns the manifest of what was
// copied, measured on the copy. Symbolic links are not followed and not
// copied; their count is returned so the report can say so.
func copyTree(ctx context.Context, src, dst string, maxBytes int64) (manifest, int, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxCopyBytes
	}
	source, err := os.OpenRoot(src)
	if err != nil {
		return nil, 0, err
	}
	defer source.Close()
	dest, err := os.OpenRoot(dst)
	if err != nil {
		return nil, 0, err
	}
	defer dest.Close()
	files := manifest{}
	topSizes := map[string]int64{}
	var total int64
	links := 0
	err = filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		slash := filepath.ToSlash(rel)
		if d.IsDir() {
			if skipped(slash) {
				return filepath.SkipDir
			}
			return dest.MkdirAll(rel, 0o755)
		}
		if d.Type()&fs.ModeSymlink != 0 {
			links++
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		top, _, _ := strings.Cut(slash, "/")
		topSizes[top] += info.Size()
		if total > maxBytes {
			return fmt.Errorf("project is larger than the %d MiB rehearsal copy limit (largest top-level entry: %s); raise the limit or rehearse a smaller directory", maxBytes>>20, largest(topSizes))
		}
		hash, err := copyFile(source, dest, rel, info.Mode().Perm())
		if err != nil {
			return err
		}
		copied, err := dest.Stat(rel)
		if err != nil {
			return err
		}
		files[slash] = entry{Hash: hash, Size: copied.Size(), ModTime: copied.ModTime().UnixNano(), Mode: info.Mode().Perm()}
		return nil
	})
	if err != nil {
		return nil, links, err
	}
	return files, links, nil
}

func largest(sizes map[string]int64) string {
	best, bestSize := "", int64(-1)
	for name, size := range sizes {
		if size > bestSize || (size == bestSize && name < best) {
			best, bestSize = name, size
		}
	}
	return best
}

func copyFile(src, dst *os.Root, name string, perm fs.FileMode) (string, error) {
	in, err := src.Open(name)
	if err != nil {
		return "", err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("source %q is not a regular file", name)
	}
	out, tmp, err := createTempRoot(dst, name, perm|0o200)
	if err != nil {
		return "", err
	}
	defer dst.Remove(tmp)
	h := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(out, h), in)
	closeErr := out.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		return "", err
	}
	if err := dst.Rename(tmp, name); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// change is one file that differs between the copy's starting state and
// its state now.
type change struct {
	Path   string
	Status string // "added", "modified" or "deleted".
}

// scan compares the tree at dir with a manifest and returns what changed,
// sorted by path. Files whose size and modification time match are taken
// as unchanged without being read.
func scan(ctx context.Context, dir string, base manifest) ([]change, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	var changes []change
	seen := make(map[string]bool, len(base))
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		slash := filepath.ToSlash(rel)
		if d.IsDir() {
			if skipped(slash) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		seen[slash] = true
		info, err := d.Info()
		if err != nil {
			return err
		}
		before, ok := base[slash]
		if !ok {
			changes = append(changes, change{slash, "added"})
			return nil
		}
		if info.Size() == before.Size && info.ModTime().UnixNano() == before.ModTime {
			return nil
		}
		hash, err := hashRootFile(root, rel)
		if err != nil {
			return err
		}
		if hash != before.Hash {
			changes = append(changes, change{slash, "modified"})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for rel := range base {
		if !seen[rel] {
			changes = append(changes, change{rel, "deleted"})
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	return changes, nil
}

// restore returns the copy at dst to the state recorded in base by undoing
// whatever a baseline run changed, using src as the pristine source.
func restore(ctx context.Context, src, dst string, base manifest) error {
	changes, err := scan(ctx, dst, base)
	if err != nil {
		return err
	}
	source, err := os.OpenRoot(src)
	if err != nil {
		return err
	}
	defer source.Close()
	dest, err := os.OpenRoot(dst)
	if err != nil {
		return err
	}
	defer dest.Close()
	for _, c := range changes {
		name := filepath.FromSlash(c.Path)
		if c.Status == "added" {
			if err := dest.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			continue
		}
		before := base[c.Path]
		if _, err := copyFile(source, dest, name, before.Mode); err != nil {
			return err
		}
		// Re-stamp so the next scan treats the restored file as untouched.
		info, err := dest.Stat(name)
		if err != nil {
			return err
		}
		before.Size, before.ModTime = info.Size(), info.ModTime().UnixNano()
		base[c.Path] = before
	}
	return nil
}
