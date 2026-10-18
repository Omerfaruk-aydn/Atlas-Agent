//go:build !solaris && !illumos

package shell

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"mvdan.cc/sh/v3/interp"
)

// The Go coreutils mv renames, and a rename cannot leave its drive: on
// Windows "mv C:/a D:/b" fails with "the system cannot move the file to a
// different disk drive", which real mv handles by copying and deleting.
// coreutilsMove does that for a move across volumes and leaves every other
// mv to the upstream command.

// coreutilsMove handles mv when a source and its destination are on
// different volumes; otherwise, or for flags it does not know, it returns
// handled false.
func coreutilsMove(ctx context.Context, args []string) (handled bool, err error) {
	var noClobber, verbose bool
	operands := make([]string, 0, len(args))
	flags := true
	for _, arg := range args {
		switch {
		case flags && arg == "--":
			flags = false
		case flags && len(arg) > 1 && arg[0] == '-':
			for _, f := range arg[1:] {
				switch f {
				case 'f':
					noClobber = false
				case 'n':
					noClobber = true
				case 'v':
					verbose = true
				default:
					return false, nil
				}
			}
		default:
			operands = append(operands, arg)
		}
	}
	if len(operands) < 2 {
		return false, nil
	}
	hc := interp.HandlerCtx(ctx)
	abs := func(p string) string {
		if !filepath.IsAbs(p) {
			p = filepath.Join(hc.Dir, p)
		}
		return filepath.Clean(p)
	}
	sources, dest := operands[:len(operands)-1], abs(operands[len(operands)-1])
	destInfo, destErr := os.Stat(dest)
	destIsDir := destErr == nil && destInfo.IsDir()
	if len(sources) > 1 && !destIsDir {
		return false, nil
	}
	crosses := false
	for _, src := range sources {
		if !strings.EqualFold(filepath.VolumeName(abs(src)), filepath.VolumeName(dest)) {
			crosses = true
		}
	}
	if !crosses {
		return false, nil
	}

	failed := false
	for _, src := range sources {
		src = abs(src)
		target := dest
		if destIsDir {
			target = filepath.Join(dest, filepath.Base(src))
		}
		if err := moveAcrossVolumes(src, target, noClobber); err != nil {
			fmt.Fprintf(hc.Stderr, "mv: %v\n", err)
			failed = true
			continue
		}
		if verbose {
			fmt.Fprintf(hc.Stdout, "renamed '%s' -> '%s'\n", filepath.ToSlash(src), filepath.ToSlash(target))
		}
	}
	if failed {
		return true, interp.ExitStatus(1)
	}
	return true, nil
}

// moveAcrossVolumes copies src to target and then removes src. A failed
// copy removes what it wrote and keeps src.
func moveAcrossVolumes(src, target string, noClobber bool) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if existing, err := os.Lstat(target); err == nil {
		if noClobber {
			return nil
		}
		if info.IsDir() || existing.IsDir() {
			return fmt.Errorf("cannot move %s to %s: destination exists", src, target)
		}
	}
	if err := os.Rename(src, target); err == nil {
		return nil
	}
	if err := copyTree(src, target); err != nil {
		_ = os.RemoveAll(target)
		return fmt.Errorf("cannot move %s to %s: %w", src, target, err)
	}
	return os.RemoveAll(src)
}

// copyTree copies a file, symlink or directory tree, keeping permissions.
func copyTree(src, target string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		out := filepath.Join(target, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			return os.MkdirAll(out, info.Mode().Perm())
		case d.Type()&fs.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, out)
		default:
			return copyFile(path, out, info.Mode().Perm())
		}
	})
}

func copyFile(src, target string, perm fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
