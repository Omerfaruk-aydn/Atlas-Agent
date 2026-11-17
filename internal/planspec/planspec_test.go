package planspec

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const sample = "# Plan\n- [NEW] `pkg/new.go` - adds the thing\n- [MODIFY] main.go: wire it\n[DELETE] old.go\n\nRe-plan if:\n- the build fails twice\n- main.go moved\n\nNext section\n"

func TestParse(t *testing.T) {
	t.Parallel()
	p := Parse(sample)
	require.Len(t, p.Entries, 3)
	require.Equal(t, New, p.Entries[0].Action)
	require.Equal(t, "pkg/new.go", p.Entries[0].Path)
	require.Equal(t, "adds the thing", p.Entries[0].Note)
	require.Equal(t, "main.go", p.Entries[1].Path)
	require.Equal(t, []string{"the build fails twice", "main.go moved"}, p.ReplanIf)
	require.Len(t, p.Digest, 16)
	require.Equal(t, p.Digest, Parse(sample).Digest)
}

func TestFreeFormPlanHasNoEntries(t *testing.T) {
	t.Parallel()
	p := Parse("Just do it.\n")
	require.Empty(t, p.Entries)
	require.Empty(t, p.Lint(t.TempDir()))
}

func TestLint(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("x"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "have.go"), []byte("x"), 0o644))
	p := Parse("[MODIFY] main.go\n[MODIFY] ghost.go\n[NEW] have.go\n[NEW] ../escape.go\n[DELETE] main.go\n")
	problems := p.Lint(dir)
	require.Len(t, problems, 4)
	require.Contains(t, problems[0], "ghost.go")
	require.Contains(t, problems[1], "already exists")
	require.Contains(t, problems[2], "relative path")
	require.Contains(t, problems[3], "already planned")
}

func TestSummaryAndPaths(t *testing.T) {
	t.Parallel()
	p := Parse(sample)
	require.Contains(t, p.Summary(), "1 new, 1 modified, 1 deleted, 2 re-plan")
	require.Equal(t, []string{"pkg/new.go", "main.go", "old.go"}, p.Paths())
}

func TestAutoProceed(t *testing.T) {
	t.Parallel()
	ok, why := Parse("[NEW] a.go\n[MODIFY] b.go\n").AutoProceed()
	require.True(t, ok, why)

	for plan, want := range map[string]string{
		"just do it":                        "does not list",
		"[DELETE] old.go":                   "deletes old.go",
		"[MODIFY] go.mod":                   "go.mod",
		"[MODIFY] .github/workflows/ci.yml": "CI",
		"[NEW] deploy/Dockerfile":           "Dockerfile",
		"[MODIFY] app/.env.local":           ".env",
		"[1] x\n[NEW] a\n[NEW] b\n[NEW] c\n[NEW] d\n[NEW] e\n[NEW] f\n[NEW] g\n[NEW] h\n[NEW] i": "9 files",
	} {
		ok, why := Parse(plan).AutoProceed()
		require.False(t, ok, plan)
		require.Contains(t, why, want, plan)
	}
}
