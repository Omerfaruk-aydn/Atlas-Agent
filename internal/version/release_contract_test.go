package version

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Release and local build flags must address the version variable in this
// module. Go silently ignores a linker assignment to a nonexistent package.
func TestBuildVersionLinkerTargets(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..")
	mod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	require.NoError(t, err)
	module := strings.Fields(string(mod))
	require.GreaterOrEqual(t, len(module), 2)
	require.Equal(t, "module", module[0])
	want := module[1] + "/internal/version.Version"
	pattern := regexp.MustCompile(`-X\s+([^\s=]+\.Version)=`)
	for _, name := range []string{".goreleaser.yml", "Taskfile.yaml", filepath.Join(".github", "workflows", "release.yml")} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(filepath.Join(root, name))
			require.NoError(t, err)
			matches := pattern.FindAllStringSubmatch(string(data), -1)
			require.NotEmpty(t, matches, "version linker flags missing")
			for _, match := range matches {
				// This covers both the release binary and the embedded AUR
				// build command, rather than just checking the first flag.
				require.Equal(t, want, match[1])
			}
		})
	}
}
