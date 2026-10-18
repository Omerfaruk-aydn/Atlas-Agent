package workplan

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func gitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "t@t"}, {"config", "user.name", "t"}} {
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = root
		require.NoError(t, cmd.Run())
	}
	require.NoError(t, os.MkdirAll(filepath.Join(root, Dir), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, Dir, ".gitignore"), []byte("*\n"), 0o644))
	return root
}

func gitOut(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = root
	out, err := cmd.Output()
	require.NoError(t, err)
	return strings.TrimSpace(string(out))
}

func TestCheckpointsLeaveTheUserRepositoryAlone(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := gitRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "cart.go"), []byte("package cart // v1\n"), 0o644))
	require.True(t, IsGitRepo(ctx, root))

	green, err := RecordGreen(ctx, root, []string{"F1"}, []string{"go test ./..."})
	require.NoError(t, err)
	require.NotEmpty(t, green.Commit)
	require.Equal(t, "?? cart.go", gitOut(t, root, "status", "--porcelain"), "the file is still untracked: the index was not touched")
	require.Equal(t, green.Commit, gitOut(t, root, "rev-parse", GreenRef))
	require.Contains(t, gitOut(t, root, "show", green.Commit+":cart.go"), "v1", "untracked files are captured")

	require.NoError(t, os.WriteFile(filepath.Join(root, "cart.go"), []byte("package cart // v2\n"), 0o644))
	second, err := RecordGreen(ctx, root, []string{"F1", "F2"}, []string{"go test ./..."})
	require.NoError(t, err)
	require.Equal(t, green.Commit, gitOut(t, root, "rev-parse", second.Commit+"^"), "greens are chained, so all stay reachable")

	greens, err := Greens(root)
	require.NoError(t, err)
	require.Len(t, greens, 2)
	before, ok := LastGreenWithout(greens, "F2")
	require.True(t, ok)
	require.Equal(t, green.Commit, before.Commit)
	_, ok = LastGreenWithout(greens, "F1")
	require.False(t, ok)
	passing, ok := LastGreenPassing(greens, "go test ./...")
	require.True(t, ok)
	require.Equal(t, second.Commit, passing.Commit)

	require.NoError(t, os.WriteFile(filepath.Join(root, "cart.go"), []byte("package cart // v3 broken\n"), 0o644))
	changed, err := ChangedSince(ctx, root, second.Commit, 10)
	require.NoError(t, err)
	require.NotEmpty(t, changed)
	require.Contains(t, changed[0], "cart.go")
}

func TestProbeChecksOutTheOldCode(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := gitRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.txt"), []byte("before"), 0o644))
	green, err := RecordGreen(ctx, root, nil, nil)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.txt"), []byte("after"), 0o644))

	dir, err := AddProbe(ctx, root, green.Commit, "F1")
	require.NoError(t, err)
	data, err := os.ReadFile(filepath.Join(dir, "a.txt"))
	require.NoError(t, err)
	require.Equal(t, "before", string(data))

	require.NoError(t, RemoveProbe(ctx, root, dir))
	_, err = os.Stat(dir)
	require.True(t, os.IsNotExist(err))
	current, err := os.ReadFile(filepath.Join(root, "a.txt"))
	require.NoError(t, err)
	require.Equal(t, "after", string(current), "the user's files are untouched")
}

func TestNotAGitRepository(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	require.False(t, IsGitRepo(context.Background(), root))
	greens, err := Greens(root)
	require.NoError(t, err)
	require.Empty(t, greens)
}

func TestDependenciesReadManifests(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	write := func(name, content string) {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(content), 0o644))
	}
	write("package.json", `{"dependencies":{"three":"^0.160.0","react":"^18.2.0"},"devDependencies":{"vite":"^5.0.0"}}`)
	write("node_modules/three/package.json", `{"version":"0.160.1"}`)
	write("go.mod", "module x\n\ngo 1.24\n\nrequire (\n\tgithub.com/a/b v1.2.3\n\tgithub.com/c/d v0.1.0 // indirect\n)\nrequire github.com/e/f v2.0.0\n")
	write("requirements.txt", "# pinned\nfastapi==0.110.0\nuvicorn>=0.29\n")
	write("Cargo.toml", "[package]\nname = \"k\"\n\n[dependencies]\nx86_64 = \"0.15\"\nspin = { version = \"0.9\", features = [\"x\"] }\n")

	got := map[string]string{}
	for _, d := range Dependencies(root) {
		got[d.Name] = d.Version
	}
	require.Equal(t, "0.160.1", got["three"], "the installed version wins over the range")
	require.Equal(t, "^18.2.0", got["react"])
	require.Equal(t, "^5.0.0", got["vite"])
	require.Equal(t, "v1.2.3", got["github.com/a/b"])
	require.NotContains(t, got, "github.com/c/d", "indirect requirements are skipped")
	require.Equal(t, "v2.0.0", got["github.com/e/f"])
	require.Equal(t, "==0.110.0", got["fastapi"])
	require.Equal(t, ">=0.29", got["uvicorn"])
	require.Equal(t, "0.15", got["x86_64"])
	require.Equal(t, "0.9", got["spin"])
	require.Empty(t, Dependencies(t.TempDir()))
}

func TestCostsAreChargedToTheCurrentFeature(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writePlan(t, root, `{"features":[{"id":"A","title":"a","done":true},{"id":"B","title":"b","done":true},{"id":"C","title":"c"}]}`, "")
	require.NoError(t, os.WriteFile(filepath.Join(root, Dir, MetricsFile), []byte(`{"A":{"turns":2},"B":{"turns":2}}`), 0o644))
	for range 6 {
		require.NoError(t, AddTurn(root, 3*time.Minute, 20_000))
	}
	costs, err := LoadCosts(root)
	require.NoError(t, err)
	require.Equal(t, FeatureCost{Turns: 6, Millis: 18 * 60 * 1000, Tokens: 120_000}, costs["C"])

	block := PromptBlock(root)
	require.Contains(t, block, "Current feature C so far: 6 turns, 18m0s, 120k tokens.")
	require.Contains(t, block, "Warning: far longer than finished features took (median 2 turns)")
}

func TestCostWarningWithoutHistory(t *testing.T) {
	t.Parallel()

	plan := &Plan{Features: []Feature{{ID: "A", Title: "a"}}}
	require.Empty(t, costWarning(plan, Costs{"A": {Turns: 3}}))
	require.Contains(t, costWarning(plan, Costs{"A": {Turns: costWarningTurns}}), "many turns")
}

func TestPromptBlockShowsAssumptionsPreviewAndDependencies(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writePlan(t, root, `{"goal":"g","assumptions":["SQLite instead of Postgres","No login"],"features":[{"id":"A","title":"a","done":true},{"id":"B","title":"b"}]}`, "")
	require.NoError(t, os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"dependencies":{"three":"0.160.0"}}`), 0o644))

	block := PromptBlock(root)
	require.Contains(t, block, "- SQLite instead of Postgres")
	require.Contains(t, block, "show the user a preview")
	require.Contains(t, block, "three 0.160.0")

	writePlan(t, root, `{"previewed":true,"features":[{"id":"A","title":"a","done":true}]}`, "")
	require.NotContains(t, PromptBlock(root), "show the user a preview", "the reminder stops once previewed")
}

func TestReviewKeyIgnoresProgress(t *testing.T) {
	t.Parallel()

	a := &Plan{Goal: "g", Features: []Feature{{ID: "A", Title: "a"}}}
	b := &Plan{Goal: "g", Features: []Feature{{ID: "A", Title: "a", Done: true}}, Previewed: true}
	require.Equal(t, a.ReviewKey(), b.ReviewKey())
	c := &Plan{Goal: "g", Features: []Feature{{ID: "A", Title: "a", Verify: "make"}}}
	require.NotEqual(t, a.ReviewKey(), c.ReviewKey())
}

func TestSnapshotExcludesThePlanDirectoryWithoutAGitignore(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := gitRepo(t)
	require.NoError(t, os.Remove(filepath.Join(root, Dir, ".gitignore")))
	require.NoError(t, os.WriteFile(filepath.Join(root, Dir, "progress.md"), []byte("notes"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), []byte("package main"), 0o644))
	commit, err := Snapshot(ctx, root, "test")
	require.NoError(t, err)
	files := gitOut(t, root, "ls-tree", "-r", "--name-only", commit)
	require.Contains(t, files, "main.go")
	require.NotContains(t, files, Dir)
}

func TestPruneProbesRemovesOnlyOldOnes(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	old := filepath.Join(root, Dir, ProbeDir, "old")
	fresh := filepath.Join(root, Dir, ProbeDir, "fresh")
	require.NoError(t, os.MkdirAll(old, 0o755))
	require.NoError(t, os.MkdirAll(fresh, 0o755))
	past := time.Now().Add(-48 * time.Hour)
	require.NoError(t, os.Chtimes(old, past, past))

	PruneProbes(context.Background(), root, 24*time.Hour)
	_, err := os.Stat(old)
	require.True(t, os.IsNotExist(err))
	_, err = os.Stat(fresh)
	require.NoError(t, err)
}
