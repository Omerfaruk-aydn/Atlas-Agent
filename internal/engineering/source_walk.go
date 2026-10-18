package engineering

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// walkSourceLimit matches the file limit of a git snapshot.
const walkSourceLimit = 10000

// errWalkLimit stops a walk that passed walkSourceLimit.
var errWalkLimit = errors.New("snapshot exceeds 10000 files")

// generatedDirs are skipped when a folder without git is walked: installed
// dependencies, build output, caches and Atlas's own state, which a
// .gitignore would normally exclude.
var generatedDirs = map[string]bool{
	".git": true, ".atlas": true, ".atlas-env": true, ".venv": true, "venv": true,
	"node_modules": true, "dist": true, "build": true, "out": true, "target": true,
	"coverage": true, "test-results": true, "playwright-report": true,
	".next": true, ".nuxt": true, ".svelte-kit": true, ".cache": true, ".turbo": true,
	"__pycache__": true, ".pytest_cache": true, ".mypy_cache": true, ".gradle": true,
}

// walkSourceFiles lists the files under root, slash-separated and relative,
// skipping generated directories.
func walkSourceFiles(ctx context.Context, root string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && generatedDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		paths = append(paths, filepath.ToSlash(rel))
		if len(paths) > walkSourceLimit {
			return errWalkLimit
		}
		return nil
	})
	return paths, err
}

// insideGitWorkTree reports whether root is inside a git work tree. A
// failing git there is a real error; anywhere else it only means there is
// no repository.
func insideGitWorkTree(ctx context.Context, root string) bool {
	cmd := exec.CommandContext(ctx, "git", "-C", root, "rev-parse", "--is-inside-work-tree")
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GIT_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}
