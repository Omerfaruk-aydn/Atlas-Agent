package safejoin

import (
	"path/filepath"
	"testing"
)

func TestJoinInside(t *testing.T) {
	root := t.TempDir()
	got, err := Join(root, "a/b.txt")
	if err != nil || got != filepath.Join(root, "a", "b.txt") {
		t.Fatalf("Join = %q, %v", got, err)
	}
}
