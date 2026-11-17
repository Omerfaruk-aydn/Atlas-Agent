package rules

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
}

func TestLoadAndRender(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write(t, dir, "style.md", "---\ntrigger: always_on\n---\nUse gofumpt.\n")
	write(t, dir, "tests.md", "---\ntrigger: glob\nglobs: [\"**/*_test.go\", \"testdata/**\"]\ndescription: how we write tests\n---\nUse require.\n")
	write(t, dir, "deploy.md", "---\ndescription: release steps\n---\nTag and push.\n")
	write(t, dir, "secret.md", "---\ntrigger: manual\n---\nOnly when asked.\n")
	write(t, dir, "notes.txt", "ignored")

	rs, err := Load(dir)
	require.NoError(t, err)
	require.Len(t, rs, 4)
	byID := map[string]Rule{}
	for _, r := range rs {
		byID[r.ID] = r
	}
	require.Equal(t, AlwaysOn, byID["style"].Trigger)
	require.Equal(t, Glob, byID["tests"].Trigger)
	require.Equal(t, ModelDecision, byID["deploy"].Trigger)
	require.Equal(t, Manual, byID["secret"].Trigger)

	out := Render(rs)
	require.Contains(t, out, "Use gofumpt.")
	require.NotContains(t, out, "Use require.")
	require.Contains(t, out, "tests (when working on **/*_test.go, testdata/**): how we write tests")
	require.Contains(t, out, "release steps")
	require.NotContains(t, out, "Only when asked")
}

func TestMatchesAndFor(t *testing.T) {
	t.Parallel()
	r := Rule{ID: "t", Trigger: Glob, Globs: []string{"**/*_test.go", "docs/**", "*.sql"}}
	require.True(t, r.Matches("internal/a/b_test.go"))
	require.True(t, r.Matches("b_test.go"))
	require.True(t, r.Matches("docs/x/y.md"))
	require.True(t, r.Matches("db/schema.sql"))
	require.False(t, r.Matches("internal/a/b.go"))
	require.Len(t, For([]Rule{r, {ID: "m", Trigger: Manual}}, []string{"a.go", "x_test.go"}), 1)
}

func TestMissingDirAndOversizeAlwaysOn(t *testing.T) {
	t.Parallel()
	rs, err := Load(filepath.Join(t.TempDir(), "none"))
	require.NoError(t, err)
	require.Empty(t, rs)
	require.Empty(t, Render(nil))
	big := Rule{ID: "big", Path: "p.md", Trigger: AlwaysOn, Body: string(make([]byte, alwaysOnBudget+1))}
	require.Contains(t, Render([]Rule{big}), "too large to inline")
}
