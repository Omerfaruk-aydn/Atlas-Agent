package backend

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/fsext"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/proto"
)

// File browsing limits. Listing is one directory level at a time so a
// client can expand a large tree lazily; reads are capped so a stray
// click on a multi-gigabyte log cannot stall the server.
const (
	maxDirectoryEntries = 2000
	defaultReadBytes    = 1 << 20
	maxReadBytes        = 8 << 20
	binarySniffBytes    = 8 << 10
)

// Errors returned by the workspace file operations.
var (
	ErrPathOutsideWorkspace = errors.New("path is outside the workspace")
	ErrNotADirectory        = errors.New("path is not a directory")
	ErrIsADirectory         = errors.New("path is a directory")
	ErrFileNotFound         = errors.New("file not found")
)

// resolveWorkspacePath maps a slash-separated path relative to the
// workspace root onto the filesystem, refusing anything that resolves
// outside the root -- through "..", an absolute path, a drive letter or a
// symlink. The empty path is the root itself.
func resolveWorkspacePath(root, rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	native := filepath.FromSlash(rel)
	if filepath.IsAbs(native) || filepath.VolumeName(native) != "" || strings.HasPrefix(rel, "/") {
		return "", ErrPathOutsideWorkspace
	}
	clean := filepath.Clean(native)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", ErrPathOutsideWorkspace
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root: %w", err)
	}
	full := filepath.Join(realRoot, clean)
	realFull, err := filepath.EvalSymlinks(full)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", ErrFileNotFound
		}
		return "", err
	}
	inside, err := filepath.Rel(realRoot, realFull)
	if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) || filepath.IsAbs(inside) {
		return "", ErrPathOutsideWorkspace
	}
	return realFull, nil
}

// workspaceRoot returns the absolute root of a workspace.
func workspaceRoot(ws *Workspace) (string, error) {
	root := ws.Path
	if ws.Cfg != nil && ws.Cfg.WorkingDir() != "" {
		root = ws.Cfg.WorkingDir()
	}
	return filepath.Abs(root)
}

// ListWorkspaceDirectory lists one level of a workspace directory,
// directories first, hiding what the agent's own listing tools hide
// (.gitignore, .atlasignore and the common build/dependency folders).
func (b *Backend) ListWorkspaceDirectory(workspaceID, rel string) (proto.DirectoryListing, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return proto.DirectoryListing{}, err
	}
	root, err := workspaceRoot(ws)
	if err != nil {
		return proto.DirectoryListing{}, err
	}
	dir, err := resolveWorkspacePath(root, rel)
	if err != nil {
		return proto.DirectoryListing{}, err
	}
	info, err := os.Stat(dir)
	if err != nil {
		return proto.DirectoryListing{}, err
	}
	if !info.IsDir() {
		return proto.DirectoryListing{}, ErrNotADirectory
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return proto.DirectoryListing{}, err
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return proto.DirectoryListing{}, err
	}
	lister := fsext.NewDirectoryLister(realRoot)
	out := proto.DirectoryListing{Path: filepath.ToSlash(filepath.Clean(filepath.FromSlash(rel)))}
	if out.Path == "." {
		out.Path = ""
	}
	for _, entry := range entries {
		full := filepath.Join(dir, entry.Name())
		isDir := entry.IsDir()
		if entry.Type()&os.ModeSymlink != 0 {
			// Follow the link only to learn what it points at; a link
			// leaving the workspace is listed as nothing at all.
			target, err := resolveWorkspacePath(realRoot, relSlash(realRoot, full))
			if err != nil {
				continue
			}
			if st, err := os.Stat(target); err == nil {
				isDir = st.IsDir()
			}
		}
		if lister.ShouldIgnore(full, isDir) {
			continue
		}
		if len(out.Entries) >= maxDirectoryEntries {
			out.Truncated = true
			break
		}
		e := proto.DirectoryEntry{Name: entry.Name(), Path: relSlash(realRoot, full), IsDir: isDir}
		if fi, err := entry.Info(); err == nil {
			if !isDir {
				e.Size = fi.Size()
			}
			e.ModifiedAt = fi.ModTime().UnixMilli()
		}
		out.Entries = append(out.Entries, e)
	}
	slices.SortFunc(out.Entries, func(a, b proto.DirectoryEntry) int {
		if a.IsDir != b.IsDir {
			if a.IsDir {
				return -1
			}
			return 1
		}
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return out, nil
}

// ReadWorkspaceFile reads up to maxBytes of a workspace file. Binary files
// report their size and digest but no content. The digest lets a client
// tell that the file changed on disk since it last looked.
func (b *Backend) ReadWorkspaceFile(workspaceID, rel string, maxBytes int64) (proto.FileContent, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return proto.FileContent{}, err
	}
	root, err := workspaceRoot(ws)
	if err != nil {
		return proto.FileContent{}, err
	}
	if strings.TrimSpace(rel) == "" {
		return proto.FileContent{}, ErrPathRequired
	}
	path, err := resolveWorkspacePath(root, rel)
	if err != nil {
		return proto.FileContent{}, err
	}
	if maxBytes <= 0 {
		maxBytes = defaultReadBytes
	}
	maxBytes = min(maxBytes, maxReadBytes)

	f, err := os.Open(path)
	if err != nil {
		return proto.FileContent{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return proto.FileContent{}, err
	}
	if info.IsDir() {
		return proto.FileContent{}, ErrIsADirectory
	}

	// Hash the whole file while keeping only the requested prefix, so the
	// digest identifies the file on disk even when the content shown is
	// truncated.
	hasher := sha256.New()
	var kept bytes.Buffer
	if _, err := io.Copy(io.MultiWriter(hasher, &limitedBuffer{buf: &kept, limit: maxBytes}), f); err != nil {
		return proto.FileContent{}, err
	}
	sniff := kept.Bytes()
	if len(sniff) > binarySniffBytes {
		sniff = sniff[:binarySniffBytes]
	}
	out := proto.FileContent{
		Path:       filepath.ToSlash(filepath.Clean(filepath.FromSlash(rel))),
		Size:       info.Size(),
		ModifiedAt: info.ModTime().UnixMilli(),
		SHA256:     hex.EncodeToString(hasher.Sum(nil)),
		Truncated:  info.Size() > maxBytes,
		Binary:     bytes.IndexByte(sniff, 0) >= 0,
	}
	if !out.Binary {
		out.Content = kept.String()
	}
	return out, nil
}

// relSlash returns path relative to root with forward slashes.
func relSlash(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

// limitedBuffer keeps the first limit bytes written to it and silently
// discards the rest, so it can sit next to a hasher in a MultiWriter.
type limitedBuffer struct {
	buf   *bytes.Buffer
	limit int64
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	if room := l.limit - int64(l.buf.Len()); room > 0 {
		l.buf.Write(p[:min(int64(len(p)), room)])
	}
	return len(p), nil
}
