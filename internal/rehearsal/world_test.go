package rehearsal

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
}

func read(t *testing.T, root, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
	require.NoError(t, err)
	return string(data)
}

// fakeRunner interprets a tiny command language so tests do not depend on
// a platform shell: "write <file> <text>", "rm <file>", "check <file>"
// (exit 1 when the file holds "bad"), "fail" and "artifact".
func fakeRunner(_ context.Context, dir, command string, _ []string) (string, int, error) {
	f := strings.Fields(command)
	switch f[0] {
	case "write":
		path := filepath.Join(dir, f[1])
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "", 1, err
		}
		return "", 0, os.WriteFile(path, []byte(strings.Join(f[2:], " ")), 0o644)
	case "rm":
		return "", 0, os.Remove(filepath.Join(dir, f[1]))
	case "check":
		data, _ := os.ReadFile(filepath.Join(dir, f[1]))
		if strings.Contains(string(data), "bad") {
			return "--- FAIL: TestThing\nFAIL\texample.com/pkg\n", 1, nil
		}
		return "ok", 0, nil
	case "artifact":
		return "", 0, os.WriteFile(filepath.Join(dir, "artifact.out"), []byte("x"), 0o644)
	case "fail":
		return "boom", 3, nil
	}
	return "", 127, errors.New("unknown command")
}

func newWorld(t *testing.T, files map[string]string) (root string, store *Store) {
	t.Helper()
	root = t.TempDir()
	writeTree(t, root, files)
	return root, NewStore(t.TempDir())
}

func TestRehearsalLeavesRealTreeUntouchedAndAppliesOnApproval(t *testing.T) {
	t.Parallel()
	root, store := newWorld(t, map[string]string{"a.txt": "one\n", "b.txt": "two\n", "keep.txt": "keep\n"})

	report, err := Run(t.Context(), Options{
		Root: root, Store: store, Task: "reshuffle", Run: fakeRunner,
		Steps:  []string{"write a.txt changed", "rm b.txt", "write sub/c.txt new"},
		Checks: []string{"check a.txt"},
	})
	require.NoError(t, err)
	require.Equal(t, VerdictOK, report.Verdict)

	// The real tree is exactly as before.
	require.Equal(t, "one\n", read(t, root, "a.txt"))
	require.Equal(t, "two\n", read(t, root, "b.txt"))
	require.NoFileExists(t, filepath.Join(root, "sub", "c.txt"))

	statuses := map[string]string{}
	for _, f := range report.Files {
		statuses[f.Path] = f.Status
	}
	require.Equal(t, map[string]string{"a.txt": "modified", "b.txt": "deleted", "sub/c.txt": "added"}, statuses)
	patch, err := store.Patch(report.ID)
	require.NoError(t, err)
	require.Contains(t, patch, "+changed")

	applied, err := store.Apply(t.Context(), report.ID, ApplyOptions{Root: root})
	require.NoError(t, err)
	require.Equal(t, "applied", applied.Status)
	require.Equal(t, "changed", read(t, root, "a.txt"))
	require.NoFileExists(t, filepath.Join(root, "b.txt"))
	require.Equal(t, "new", read(t, root, "sub/c.txt"))
	require.Equal(t, "keep\n", read(t, root, "keep.txt"))

	_, err = store.Apply(t.Context(), report.ID, ApplyOptions{Root: root})
	require.ErrorContains(t, err, "not previewed")
}

func TestRehearsalReportsNewlyBrokenChecksAndGatesApply(t *testing.T) {
	t.Parallel()
	root, store := newWorld(t, map[string]string{"a.txt": "good"})

	report, err := Run(t.Context(), Options{
		Root: root, Store: store, Run: fakeRunner,
		Steps: []string{"write a.txt bad"}, Checks: []string{"check a.txt"},
	})
	require.NoError(t, err)
	require.Equal(t, VerdictBreakChecks, report.Verdict)
	require.Len(t, report.Checks, 1)
	require.Equal(t, "newly_failing", report.Checks[0].Verdict)
	require.Equal(t, []string{"TestThing", "example.com/pkg"}, report.Checks[0].NewlyFailingTests)
	require.Contains(t, Render(report), "newly failing: TestThing")

	_, err = store.Apply(t.Context(), report.ID, ApplyOptions{Root: root})
	require.ErrorContains(t, err, "breaks_checks")
	require.Equal(t, "good", read(t, root, "a.txt"))

	_, err = store.Apply(t.Context(), report.ID, ApplyOptions{Root: root, Force: true})
	require.NoError(t, err)
	require.Equal(t, "bad", read(t, root, "a.txt"))
}

func TestRehearsalPreexistingFailuresAreNotBlamedOnTheChange(t *testing.T) {
	t.Parallel()
	root, store := newWorld(t, map[string]string{"a.txt": "bad"})

	report, err := Run(t.Context(), Options{
		Root: root, Store: store, Run: fakeRunner,
		Steps: []string{"write other.txt x"}, Checks: []string{"check a.txt"},
	})
	require.NoError(t, err)
	require.Equal(t, "still_failing", report.Checks[0].Verdict)
	require.Equal(t, VerdictOK, report.Verdict)
}

func TestRehearsalFailedStepStopsAndBlocksApply(t *testing.T) {
	t.Parallel()
	root, store := newWorld(t, map[string]string{"a.txt": "x"})

	report, err := Run(t.Context(), Options{
		Root: root, Store: store, Run: fakeRunner,
		Steps:  []string{"write a.txt y", "fail", "write z.txt never"},
		Checks: []string{"check a.txt"},
	})
	require.NoError(t, err)
	require.Equal(t, VerdictFailed, report.Verdict)
	require.Empty(t, report.Checks)
	for _, f := range report.Files {
		require.NotEqual(t, "z.txt", f.Path)
	}
	_, err = store.Apply(t.Context(), report.ID, ApplyOptions{Root: root})
	require.ErrorContains(t, err, "failed")
}

func TestRehearsalBaselineArtifactsDoNotLeakIntoTheChange(t *testing.T) {
	t.Parallel()
	root, store := newWorld(t, map[string]string{"a.txt": "x"})

	artifactRunner := func(ctx context.Context, dir, command string, env []string) (string, int, error) {
		if command == "artifact" {
			return "", 0, os.WriteFile(filepath.Join(dir, "artifact.out"), []byte("x"), 0o644)
		}
		return fakeRunner(ctx, dir, command, env)
	}
	report, err := Run(t.Context(), Options{
		Root: root, Store: store, Run: artifactRunner,
		Steps: []string{"write a.txt y"}, Checks: []string{"artifact"},
	})
	require.NoError(t, err)
	paths := []string{}
	for _, f := range report.Files {
		paths = append(paths, f.Path)
	}
	require.Equal(t, []string{"a.txt"}, paths)
}

func TestApplyRefusesWhenRealFileChangedSinceRehearsal(t *testing.T) {
	t.Parallel()
	root, store := newWorld(t, map[string]string{"a.txt": "one", "b.txt": "two"})

	report, err := Run(t.Context(), Options{
		Root: root, Store: store, Run: fakeRunner,
		Steps: []string{"write b.txt B", "write a.txt A"},
	})
	require.NoError(t, err)
	require.Equal(t, VerdictUnchecked, report.Verdict)

	writeTree(t, root, map[string]string{"a.txt": "edited meanwhile"})
	_, err = store.Apply(t.Context(), report.ID, ApplyOptions{Root: root})
	require.ErrorContains(t, err, "stale rehearsal")
	// Nothing was partially applied.
	require.Equal(t, "two", read(t, root, "b.txt"))
}

func TestApplyRefusesAddedFileThatNowExists(t *testing.T) {
	t.Parallel()
	root, store := newWorld(t, map[string]string{"a.txt": "one"})
	report, err := Run(t.Context(), Options{Root: root, Store: store, Run: fakeRunner, Steps: []string{"write new.txt N"}})
	require.NoError(t, err)
	writeTree(t, root, map[string]string{"new.txt": "someone else"})
	_, err = store.Apply(t.Context(), report.ID, ApplyOptions{Root: root})
	require.ErrorContains(t, err, "now exists")
}

func TestRunRejectsCommandsNamingTheRealTree(t *testing.T) {
	t.Parallel()
	root, store := newWorld(t, map[string]string{"a.txt": "one"})
	_, err := Run(t.Context(), Options{Root: root, Store: store, Run: fakeRunner, Steps: []string{"rm " + filepath.Join(root, "a.txt")}})
	require.ErrorContains(t, err, "relative paths")
	require.FileExists(t, filepath.Join(root, "a.txt"))

	_, err = Run(t.Context(), Options{Root: root, Store: store, Run: fakeRunner})
	require.ErrorContains(t, err, "nothing to rehearse")
}

func TestRunEnforcesCopyLimit(t *testing.T) {
	t.Parallel()
	root, store := newWorld(t, map[string]string{"big/data.bin": strings.Repeat("x", 4096)})
	_, err := Run(t.Context(), Options{Root: root, Store: store, Run: fakeRunner, Steps: []string{"write a x"}, MaxCopyBytes: 1024})
	require.ErrorContains(t, err, "copy limit")
	require.ErrorContains(t, err, "big")
}

func TestRehearsalSkipsAtlasStateAndReportsAPIChanges(t *testing.T) {
	t.Parallel()
	root, store := newWorld(t, map[string]string{
		"go.mod":         "module example.com/m\n\ngo 1.22\n",
		"pkg/pkg.go":     "package pkg\n\n// Old is exported.\nfunc Old(a int) int { return a }\n\n// Keep stays.\nfunc Keep() {}\n",
		".atlas/some.db": "state",
	})
	report, err := Run(t.Context(), Options{
		Root: root, Store: store, Run: fakeRunner,
		Steps: []string{"write pkg/pkg.go package pkg\n"},
	})
	require.NoError(t, err)
	require.NotNil(t, report.API)
	require.Len(t, report.API.Removed, 2)
	require.Equal(t, VerdictAPIChanges, report.Verdict)
	for _, f := range report.Files {
		require.NotContains(t, f.Path, ".atlas")
	}
}

func TestDiscardRemovesRehearsedContents(t *testing.T) {
	t.Parallel()
	root, store := newWorld(t, map[string]string{"a.txt": "one"})
	report, err := Run(t.Context(), Options{Root: root, Store: store, Run: fakeRunner, Steps: []string{"write a.txt two"}})
	require.NoError(t, err)

	discarded, err := store.Discard(report.ID)
	require.NoError(t, err)
	require.Equal(t, "discarded", discarded.Status)
	_, err = store.Apply(t.Context(), report.ID, ApplyOptions{Root: root})
	require.ErrorContains(t, err, "not previewed")

	list, err := store.List()
	require.NoError(t, err)
	require.Len(t, list, 1)
}

func TestStoreRejectsTraversalIDs(t *testing.T) {
	t.Parallel()
	store := NewStore(t.TempDir())
	_, err := store.Load("../../etc")
	require.ErrorContains(t, err, "invalid rehearsal id")
}

func TestRunWithRealShell(t *testing.T) {
	t.Parallel()
	root, store := newWorld(t, map[string]string{"a.txt": "one\n"})
	report, err := Run(t.Context(), Options{
		Root: root, Store: store,
		Steps:  []string{"echo two > a.txt", "echo made > made.txt"},
		Checks: []string{"test -f made.txt"},
	})
	require.NoError(t, err)
	require.Equal(t, VerdictOK, report.Verdict, Render(report))
	require.Len(t, report.Files, 2)
	require.Equal(t, "one\n", read(t, root, "a.txt"))
}

func TestClassifyNamesRiskyCommands(t *testing.T) {
	t.Parallel()
	risky := map[string]string{
		"rm -rf build":                                 "delete",
		"git reset --hard HEAD~3":                      "vcs",
		"git clean -fd":                                "vcs",
		"git push origin main --force":                 "vcs",
		"psql -c 'DROP TABLE users'":                   "database",
		"npx prisma migrate deploy":                    "database",
		"npm update":                                   "dependencies",
		"go get -u ./...":                              "dependencies",
		"find . -name '*.log' -delete":                 "delete",
		"find . -name '*.go' -exec sed -i s/a/b/ {} +": "mass-edit",
		"gofmt -w .":                                   "mass-edit",
	}
	for command, category := range risky {
		risk, ok := Classify(command)
		require.True(t, ok, command)
		require.Equal(t, category, risk.Category, command)
		require.Contains(t, risk.Hint(), "rehearse")
	}
	for _, command := range []string{"ls -la", "go test ./...", "git status", "rm file.txt", "git push origin main", "npm install"} {
		_, ok := Classify(command)
		require.False(t, ok, command)
	}
}

func TestFailedTestsRecognisesCommonRunners(t *testing.T) {
	t.Parallel()
	require.Equal(t, []string{"TestA", "TestA/sub", "x/y"}, FailedTests("--- FAIL: TestA (0.0s)\n    --- FAIL: TestA/sub (0.0s)\nFAIL\tx/y\t0.1s"))
	require.Equal(t, []string{"tests/test_a.py::test_x"}, FailedTests("FAILED tests/test_a.py::test_x - assert"))
	require.Equal(t, []string{"mod::t"}, FailedTests("test mod::t ... FAILED"))
	require.Empty(t, FailedTests("all good"))
}
