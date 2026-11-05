package tools

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/stretchr/testify/require"
)

func TestPathPolicyChecksSymlinkTargets(t *testing.T) {
	t.Parallel()
	root, outside := t.TempDir(), t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, "inside"), 0o755))
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	require.NoError(t, os.Symlink(filepath.Join(root, "inside"), filepath.Join(root, "link")))
	p := PathPolicy{Root: root, Restrict: true}
	require.Error(t, p.Check(filepath.Join(root, "escape", "new", "file.txt")))
	require.NoError(t, p.Check(filepath.Join(root, "link", "new", "file.txt")))
	require.NoError(t, os.Symlink(filepath.Join(root, "missing"), filepath.Join(root, "dangling")))
	require.Error(t, p.Check(filepath.Join(root, "dangling", "file.txt")))
}

// Off unless a workspace turns it on: editing a file in a sibling checkout
// is ordinary work, not something this package gets to forbid by default.
func TestPathPolicyPermitsEverythingByDefault(t *testing.T) {
	var p PathPolicy
	require.NoError(t, p.Check(filepath.Join(t.TempDir(), "anywhere.txt")))

	// Restrict without a root would confine writes to nowhere in
	// particular, so it stays off rather than refusing everything.
	require.NoError(t, PathPolicy{Restrict: true}.Check("/etc/passwd"))
}

func TestPathPolicyAllowsInsideTheRoot(t *testing.T) {
	root := t.TempDir()
	p := PathPolicy{Root: root, Restrict: true}

	require.NoError(t, p.Check(filepath.Join(root, "file.txt")))
	require.NoError(t, p.Check(filepath.Join(root, "deep", "nested", "file.txt")))
	require.NoError(t, p.Check(root))
}

func TestPathPolicyRefusesOutsideTheRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspace")
	p := PathPolicy{Root: root, Restrict: true}

	err := p.Check(filepath.Join(root, "..", "elsewhere.txt"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "outside the working directory")
	require.Contains(t, err.Error(), "restrict_writes_to_working_dir")

	require.Error(t, p.Check(filepath.Dir(root)))
}

// A sibling directory whose name merely starts with the root's name is not
// inside it.
func TestPathPolicyDoesNotMatchASiblingPrefix(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "work")
	p := PathPolicy{Root: root, Restrict: true}

	require.Error(t, p.Check(filepath.Join(base, "workspace", "file.txt")))
}

// A relative path is resolved against the root rather than slipping through
// unchecked.
func TestPathPolicyResolvesRelativePaths(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspace")
	p := PathPolicy{Root: root, Restrict: true}

	require.NoError(t, p.Check("inside.txt"))
	require.Error(t, p.Check(filepath.Join("..", "outside.txt")))
}

func TestNewPathPolicyReadsOptions(t *testing.T) {
	require.Equal(t, PathPolicy{}, NewPathPolicy(nil, "/work"))
	require.Equal(t, PathPolicy{}, NewPathPolicy(&config.Config{}, "/work"))

	got := NewPathPolicy(&config.Config{Options: &config.Options{RestrictWritesToWorkingDir: true}}, "/work")
	require.Equal(t, PathPolicy{Root: "/work", Restrict: true}, got)
}
