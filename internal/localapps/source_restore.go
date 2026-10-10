package localapps

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// sourceFiles enumerates files without following links or including
// directories intentionally omitted from source backups.
func sourceFiles(root string, dataDirs []string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil || rel == "." {
			return err
		}
		if e.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		if strings.HasPrefix(e.Name(), ".atlas-") || e.IsDir() && (slices.Contains(skipSourceDirs, e.Name()) || isDataDir(rel, dataDirs)) {
			if e.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !e.IsDir() {
			files = append(files, rel)
		}
		return nil
	})
	return files, err
}

func restoreSourceFiles(root, restored, staging string, dataDirs []string) error {
	if _, err := os.Stat(filepath.Join(restored, ManifestFile)); err != nil {
		return opErr(ErrInvalidBackup, "", "the backup's files have no %s", ManifestFile)
	}
	oldFiles, err := sourceFiles(root, dataDirs)
	if err != nil {
		return err
	}
	newFiles, err := sourceFiles(restored, dataDirs)
	if err != nil {
		return err
	}
	aside := filepath.Join(staging, "previous-source")
	var movedAside, movedIn, createdDirs []string
	undo := func(cause error) error {
		var recovery []error
		for i := len(movedIn) - 1; i >= 0; i-- {
			if err := os.Remove(filepath.Join(root, movedIn[i])); err != nil {
				recovery = append(recovery, err)
			}
		}
		for i := len(createdDirs) - 1; i >= 0; i-- {
			if err := os.Remove(createdDirs[i]); err != nil {
				recovery = append(recovery, err)
			}
		}
		for _, rel := range movedAside {
			if err := os.Rename(filepath.Join(aside, rel), filepath.Join(root, rel)); err != nil {
				recovery = append(recovery, err)
			}
		}
		return errors.Join(append([]error{cause}, recovery...)...)
	}
	for _, rel := range oldFiles {
		target := filepath.Join(aside, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return undo(err)
		}
		if err := os.Rename(filepath.Join(root, rel), target); err != nil {
			return undo(fmt.Errorf("move %s aside: %w", rel, err))
		}
		movedAside = append(movedAside, rel)
	}
	// Check each destination ancestor without traversing existing links.
	var ensureDir func(string) error
	ensureDir = func(dir string) error {
		if samePath(dir, root) {
			return nil
		}
		if err := ensureDir(filepath.Dir(dir)); err != nil {
			return err
		}
		info, err := os.Lstat(dir)
		if err == nil {
			if !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
				return opErr(ErrUnsafePath, "", "restore destination %s is not a plain directory", dir)
			}
			return nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := os.Mkdir(dir, 0o755); err != nil {
			return err
		}
		createdDirs = append(createdDirs, dir)
		return nil
	}
	for _, rel := range newFiles {
		target := filepath.Join(root, rel)
		if err := ensureDir(filepath.Dir(target)); err != nil {
			return undo(err)
		}
		if _, err := os.Lstat(target); err == nil {
			return undo(opErr(ErrUnsafePath, "", "restore would replace a preserved directory or link: %s", rel))
		} else if !errors.Is(err, os.ErrNotExist) {
			return undo(err)
		}
		if err := os.Rename(filepath.Join(restored, rel), target); err != nil {
			return undo(fmt.Errorf("restore %s: %w", rel, err))
		}
		movedIn = append(movedIn, rel)
	}
	return nil
}
