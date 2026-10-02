package prompt

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"

	"github.com/stretchr/testify/require"
)

func newPromptTestConfig(t *testing.T, dir string) *config.ConfigStore {
	t.Helper()
	t.Setenv("ATLAS_AGENT_GLOBAL_CONFIG", t.TempDir())
	t.Setenv("ATLAS_AGENT_GLOBAL_DATA", t.TempDir())
	t.Setenv("ATLAS_AGENT_CACHE_DIR", t.TempDir())
	t.Setenv("NVIDIA_NIM_API_KEY", "")
	t.Setenv("NVIDIA_API_KEY", "")
	store, err := config.Init(dir, "", false)
	require.NoError(t, err)
	return store
}

func TestContextReadBoundedUTF8(t *testing.T) {
	p := filepath.Join(t.TempDir(), "AGENTS.md")
	require.NoError(t, os.WriteFile(p, []byte(strings.Repeat("ğ", 40000)), 0o644))
	f := processFile(p)
	require.NotNil(t, f)
	require.LessOrEqual(t, len(f.Content), 64*1024)
	require.True(t, utf8.ValidString(f.Content))
	require.Contains(t, f.Content, "[Context truncated")
}

func TestContextRejectsBinary(t *testing.T) {
	for _, content := range []string{"abc\x00def", "abc\xff"} {
		p := filepath.Join(t.TempDir(), "data")
		require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
		require.Nil(t, processFile(p))
	}
}

func TestContextOrderingDedupAndDirectoryExclusions(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"AGENTS.md", "z.md", "deep/a.md", "node_modules/ignore.md", ".env", ".env.example", "secret.key"} {
		p := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(name), 0o644))
	}
	store := newPromptTestConfig(t, dir)
	for range 10 {
		loader := newContextLoader()
		files := loader.load([]string{"AGENTS.md", ".", "./AGENTS.md", "missing"}, store, "project")
		require.Len(t, files, 4)
		require.Equal(t, "AGENTS.md", files[0].Content)
		require.Equal(t, ".env.example", files[1].Content)
		require.Equal(t, "z.md", files[2].Content)
		require.Equal(t, "deep/a.md", files[3].Content)
		require.Equal(t, "project", files[0].Scope)
		require.Empty(t, loader.load([]string{"AGENTS.md"}, store, "global"))
	}
}

func TestContextSharedBudgetAndFileCount(t *testing.T) {
	dir := t.TempDir()
	var paths []string
	for i := range 70 {
		name := fmt.Sprintf("%02d.md", i)
		paths = append(paths, name)
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(strings.Repeat("a", 70000)), 0o644))
	}
	store := newPromptTestConfig(t, dir)
	loader := newContextLoader()
	files := loader.load(paths[:2], store, "project")
	files = append(files, loader.load(paths[2:], store, "global")...)
	require.Len(t, files, 4)
	total := 0
	for _, f := range files {
		total += len(f.Content)
	}
	require.Equal(t, 256*1024, total)
	loader = newContextLoader()
	for _, p := range paths {
		require.NoError(t, os.WriteFile(filepath.Join(dir, p), []byte("a"), 0o644))
	}
	require.Len(t, loader.load(paths, store, "project"), 64)
}

func TestContextDenseDirectoryIsOmittedDeterministically(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("root instructions"), 0o644))
	for i := range 8193 {
		require.NoError(t, os.WriteFile(filepath.Join(dir, fmt.Sprintf("%05d.md", i)), []byte("text"), 0o644))
	}
	store := newPromptTestConfig(t, dir)
	for range 2 {
		loader := newContextLoader()
		files := loader.load([]string{"."}, store, "project")
		require.Len(t, files, 1)
		require.Equal(t, "root instructions", files[0].Content)
		require.True(t, loader.limited)
	}
}

func TestContextDirectoryDepthAndGeneratedPruning(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("instructions"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "000.md"), []byte("ordinary"), 0o644))
	nested := dir
	for range 33 {
		nested = filepath.Join(nested, "deep")
	}
	require.NoError(t, os.MkdirAll(nested, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(nested, "hidden.md"), []byte("too deep"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "node_modules"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "node_modules", "secret.md"), []byte("generated"), 0o644))
	loader := newContextLoader()
	candidates := loader.directoryCandidates(dir)
	require.Equal(t, filepath.Join(dir, "AGENTS.md"), candidates[0])
	require.NotContains(t, candidates, filepath.Join(nested, "hidden.md"))
	require.NotContains(t, candidates, filepath.Join(dir, "node_modules", "secret.md"))
	require.True(t, loader.limited)
	require.LessOrEqual(t, loader.visits, 8193)
}

func TestContextCandidateBudgetIsShared(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.md", "b.md", "c.md"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(name), 0o644))
	}
	loader := newContextLoader()
	loader.discovered = contextCandidateLimit - 1
	require.Equal(t, []string{filepath.Join(dir, "a.md")}, loader.directoryCandidates(dir))
	require.True(t, loader.limited)
	require.Empty(t, loader.directoryCandidates(dir))
	require.Equal(t, contextCandidateLimit, loader.discovered)
}
