package safejoin

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestHiddenJoinInside(t *testing.T) {
	root := t.TempDir()
	cases := map[string]string{
		"a/b.txt":      filepath.Join(root, "a", "b.txt"),
		"a/./b":        filepath.Join(root, "a", "b"),
		"a/../b":       filepath.Join(root, "b"),
		"a/b/../../c":  filepath.Join(root, "c"),
		".":            root,
		"":             root,
		"..hidden":     filepath.Join(root, "..hidden"),
		"dir/..x/file": filepath.Join(root, "dir", "..x", "file"),
	}
	for name, want := range cases {
		got, err := Join(root, name)
		if err != nil || got != want {
			t.Errorf("Join(%q) = %q, %v; want %q", name, got, err, want)
		}
	}
}

func TestHiddenJoinEscapes(t *testing.T) {
	root := t.TempDir()
	sibling := ".." + string(filepath.Separator) + filepath.Base(root) + "-evil" + string(filepath.Separator) + "x"
	absolute := filepath.Join(os.TempDir(), "outside.txt")
	for _, name := range []string{
		"..",
		"../x",
		"a/../../x",
		"a/b/../../../x",
		sibling,
		absolute,
	} {
		got, err := Join(root, name)
		if !errors.Is(err, ErrEscapes) {
			t.Errorf("Join(%q) = %q, %v; want ErrEscapes", name, got, err)
		}
	}
}

func TestHiddenJoinRelativeRoot(t *testing.T) {
	if _, err := Join("uploads", "../secrets.txt"); !errors.Is(err, ErrEscapes) {
		t.Fatalf("relative root escape error = %v", err)
	}
	got, err := Join("uploads", "a.txt")
	if err != nil || got != filepath.Join("uploads", "a.txt") {
		t.Fatalf("Join = %q, %v", got, err)
	}
}
