// Package qualityflows implements measured delivery rehearsals and selective
// feature rollback without changing Git history.
package qualityflows

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const maxBytes = 8 * 1024 * 1024

// Decode accepts exactly one specification and rejects misspelled fields.
func Decode(data []byte, value any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return fmt.Errorf("specification must contain exactly one JSON value")
	}
	return nil
}

type boundedBuffer struct{ bytes.Buffer }

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > maxBytes {
		return 0, fmt.Errorf("command output exceeds 8 MiB")
	}
	return b.Buffer.Write(p)
}

func git(ctx context.Context, root string, env []string, input []byte, args ...string) ([]byte, error) {
	argv := append([]string{"-C", root, "--no-pager"}, args...)
	c := exec.CommandContext(ctx, "git", argv...)
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(entry), "GIT_") {
			c.Env = append(c.Env, entry)
		}
	}
	c.Env = append(c.Env, env...)
	c.Stdin = bytes.NewReader(input)
	var out, stderr boundedBuffer
	c.Stdout, c.Stderr = &out, &stderr
	if err := c.Run(); err != nil {
		return nil, fmt.Errorf("git %s failed: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return out.Bytes(), nil
}

// ProjectPath refuses links and traversal, including linked parent directories.
func ProjectPath(root, relative string) (string, error) {
	if relative == "" || !filepath.IsLocal(relative) || strings.ContainsAny(relative, "\x00\r\n") {
		return "", fmt.Errorf("a project-relative path is required")
	}
	clean := filepath.Clean(filepath.FromSlash(relative))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes project")
	}
	current := root
	for _, part := range strings.Split(clean, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("symlinked project path is unsupported: %s", relative)
		}
	}
	return current, nil
}

func ReadFile(root, relative string, limit int64) ([]byte, error) {
	path, err := ProjectPath(root, relative)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return nil, fmt.Errorf("file must be regular and at most %d bytes", limit)
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("file exceeds limit")
	}
	return data, err
}

// SameRoot handles Windows long/short path spellings of an existing directory.
func SameRoot(left, right string) bool {
	l, err := filepath.EvalSymlinks(left)
	if err != nil {
		return false
	}
	r, err := filepath.EvalSymlinks(right)
	if err != nil {
		return false
	}
	if filepath.Separator == '\\' {
		return strings.EqualFold(filepath.Clean(l), filepath.Clean(r))
	}
	return filepath.Clean(l) == filepath.Clean(r)
}
