package tools

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
)

// PathPolicy decides whether a tool may write to a path at all, before any
// permission is even requested.
//
// It is off unless a workspace turns it on: refusing writes outside the
// working directory would break the ordinary case of editing a file in a
// sibling checkout, and that decision belongs to the person configuring the
// workspace, not to this package.
type PathPolicy struct {
	// Root is the directory writes are confined to. Empty disables the
	// check however Restrict is set -- confining writes to nowhere in
	// particular would refuse everything.
	Root string
	// Restrict turns the confinement on.
	Restrict bool
}

// policyFile pins the restricted root for an entire tool operation, including
// the time spent waiting for permission. Root methods reject symlink escapes.
type policyFile struct {
	root *os.Root
	name string
}

func (p PathPolicy) open(path string) (*policyFile, error) {
	if !p.Restrict || p.Root == "" {
		return &policyFile{name: path}, nil
	}
	root, err := filepath.Abs(p.Root)
	if err != nil {
		return nil, err
	}
	target := path
	if !filepath.IsAbs(target) {
		target = filepath.Join(root, target)
	}
	root, err = resolveExistingPrefix(root)
	if err != nil {
		return nil, err
	}
	target, err = resolveExistingPrefix(target)
	if err != nil {
		return nil, err
	}
	name, err := filepath.Rel(root, target)
	if err != nil || !filepath.IsLocal(name) {
		return nil, outsideErr(path, root)
	}
	confined, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	return &policyFile{root: confined, name: name}, nil
}

func (f *policyFile) Close() {
	if f.root != nil {
		_ = f.root.Close()
	}
}

func (f *policyFile) Stat() (fs.FileInfo, error) {
	if f.root == nil {
		return os.Stat(f.name)
	}
	return f.root.Stat(f.name)
}

func (f *policyFile) ReadFile() ([]byte, error) {
	if f.root == nil {
		return os.ReadFile(f.name)
	}
	return f.root.ReadFile(f.name)
}

func (f *policyFile) MkdirParents() error {
	if f.root == nil {
		return os.MkdirAll(filepath.Dir(f.name), 0o755)
	}
	return f.root.MkdirAll(filepath.Dir(f.name), 0o755)
}

func (f *policyFile) WriteFile(data []byte, perm fs.FileMode) error {
	if f.root == nil {
		return os.WriteFile(f.name, data, perm)
	}
	return f.root.WriteFile(f.name, data, perm)
}

func (f *policyFile) WriteNew(data []byte, perm fs.FileMode) error {
	var file *os.File
	var err error
	if f.root == nil {
		file, err = os.OpenFile(f.name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)
	} else {
		file, err = f.root.OpenFile(f.name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)
	}
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	return errors.Join(writeErr, file.Close())
}

func (f *policyFile) Create() (*os.File, error) {
	if f.root == nil {
		return os.Create(f.name)
	}
	return f.root.Create(f.name)
}

func (f *policyFile) Remove() error {
	if f.root == nil {
		return os.Remove(f.name)
	}
	return f.root.Remove(f.name)
}

// NewPathPolicy reads the policy out of the config's options.
func NewPathPolicy(cfg *config.Config, workingDir string) PathPolicy {
	if cfg == nil || cfg.Options == nil {
		return PathPolicy{}
	}
	return PathPolicy{Root: workingDir, Restrict: cfg.Options.RestrictWritesToWorkingDir}
}

// Check reports whether path may be written. path is expected to be
// absolute already -- every writing tool joins it against the working
// directory first -- but a relative one is resolved against Root rather than
// silently passing.
func (p PathPolicy) Check(path string) error {
	if !p.Restrict || p.Root == "" {
		return nil
	}

	root, err := filepath.Abs(p.Root)
	if err != nil {
		return fmt.Errorf("resolving the working directory %q: %w", p.Root, err)
	}
	target := path
	if !filepath.IsAbs(target) {
		target = filepath.Join(root, target)
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("resolving %q: %w", path, err)
	}

	rel, err := filepath.Rel(root, target)
	if err != nil {
		// Different volumes on Windows: not relative to the root at
		// all, so certainly outside it.
		return outsideErr(path, root)
	}
	// ".." alone, or any path starting with a ".." segment, has climbed
	// out. filepath.Rel has already cleaned the result, so a ".." further
	// along cannot appear.
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return outsideErr(path, root)
	}

	// Resolve existing path components as well as the root: a lexical check
	// alone permits writes through a symlink that points outside the project.
	realRoot, err := resolveExistingPrefix(root)
	if err != nil {
		return fmt.Errorf("resolving the working directory %q: %w", p.Root, err)
	}
	realTarget, err := resolveExistingPrefix(target)
	if err != nil {
		return fmt.Errorf("resolving %q: %w", path, err)
	}
	rel, err = filepath.Rel(realRoot, realTarget)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return outsideErr(path, root)
	}
	return nil
}

// resolveExistingPrefix resolves links even when a later component has not
// been created. Broken links and other resolution errors fail closed.
func resolveExistingPrefix(path string) (string, error) {
	current := path
	var suffix []string
	for {
		_, err := os.Lstat(current)
		if err == nil {
			resolved, err := filepath.EvalSymlinks(current)
			if err != nil {
				return "", err
			}
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return resolved, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		suffix = append(suffix, filepath.Base(current))
		current = parent
	}
}

func outsideErr(path, root string) error {
	return fmt.Errorf("%s is outside the working directory %s, which this workspace does not allow writing to "+
		"(unset restrict_writes_to_working_dir to allow it)", path, root)
}
