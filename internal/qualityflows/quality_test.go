package qualityflows

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func repo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	g(t, root, "init")
	g(t, root, "config", "user.email", "fixture@example.test")
	g(t, root, "config", "user.name", "Fixture")
	g(t, root, "config", "core.autocrlf", "false")
	g(t, root, "config", "core.hooksPath", filepath.Join(root, "no-hooks"))
	write(t, root, "base.txt", "base\n")
	commit(t, root, "baseline")
	return root
}

func g(t *testing.T, root string, args ...string) string {
	t.Helper()
	out, err := git(t.Context(), root, nil, nil, args...)
	require.NoError(t, err)
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, root, path, text string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte(text), 0o600))
}

func commit(t *testing.T, root, msg string) string {
	t.Helper()
	g(t, root, "add", ".")
	g(t, root, "commit", "-m", msg)
	return g(t, root, "rev-parse", "HEAD")
}

func TestQualityUndoMultipleCommitsPreservesHistoryAndUnrelatedEdits(t *testing.T) {
	t.Parallel()
	root := repo(t)
	write(t, root, "feature.txt", "one\n")
	first := commit(t, root, "feature one")
	write(t, root, "feature.txt", "two\n")
	second := commit(t, root, "feature two")
	write(t, root, "other.txt", "other\n")
	head := commit(t, root, "other feature")
	write(t, root, "other.txt", "dirty unrelated\n")
	plan, err := PrepareUndo(t.Context(), root, []string{first, second})
	require.NoError(t, err)
	require.Equal(t, []string{second, first}, plan.Commits)
	require.Empty(t, plan.LaterChanges)
	require.NoError(t, ApplyUndo(t.Context(), plan, plan.ID, false))
	_, err = os.Stat(filepath.Join(root, "feature.txt"))
	require.True(t, os.IsNotExist(err))
	data, err := os.ReadFile(filepath.Join(root, "other.txt"))
	require.NoError(t, err)
	require.Equal(t, "dirty unrelated\n", string(data))
	require.Equal(t, head, g(t, root, "rev-parse", "HEAD"))
	require.Empty(t, g(t, root, "diff", "--cached"))
	require.Error(t, ApplyUndo(t.Context(), plan, plan.ID, false), "already-applied plan cannot replay")
}

func TestQualityUndoStaleAndOverlappingChangesRefused(t *testing.T) {
	t.Parallel()
	root := repo(t)
	write(t, root, "base.txt", "feature\n")
	id := commit(t, root, "feature")
	plan, err := PrepareUndo(t.Context(), root, []string{id})
	require.NoError(t, err)
	write(t, root, "base.txt", "new user edit\n")
	require.ErrorContains(t, ApplyUndo(t.Context(), plan, plan.ID, false), "changed since preview")
	_, err = PrepareUndo(t.Context(), root, []string{id})
	require.Error(t, err)
	data, err := os.ReadFile(filepath.Join(root, "base.txt"))
	require.NoError(t, err)
	require.Equal(t, "new user edit\n", string(data))
	write(t, root, "base.txt", "feature\n")
	plan.Patch += "tamper"
	require.ErrorContains(t, ApplyUndo(t.Context(), plan, plan.ID, false), "fingerprint")
}

func TestQualityUndoLaterChangesRequireAcknowledgement(t *testing.T) {
	t.Parallel()
	root := repo(t)
	write(t, root, "base.txt", strings.Repeat("before\n", 12)+"ORIGINAL\n"+strings.Repeat("after\n", 12))
	commit(t, root, "long baseline")
	write(t, root, "base.txt", strings.Repeat("before\n", 12)+"FEATURE\n"+strings.Repeat("after\n", 12))
	feature := commit(t, root, "feature")
	write(t, root, "base.txt", "later independent line\n"+strings.Repeat("before\n", 11)+"FEATURE\n"+strings.Repeat("after\n", 12))
	later := commit(t, root, "later")
	plan, err := PrepareUndo(t.Context(), root, []string{feature})
	require.NoError(t, err)
	require.Equal(t, []string{later}, plan.LaterChanges)
	require.ErrorContains(t, ApplyUndo(t.Context(), plan, plan.ID, false), "dependents")
	require.NoError(t, ApplyUndo(t.Context(), plan, plan.ID, true))
	data, err := os.ReadFile(filepath.Join(root, "base.txt"))
	require.NoError(t, err)
	require.Contains(t, string(data), "later independent line")
	require.NotContains(t, string(data), "FEATURE")
}

func TestQualityUndoBinaryAndRenamedFile(t *testing.T) {
	t.Parallel()
	root := repo(t)
	require.NoError(t, os.Rename(filepath.Join(root, "base.txt"), filepath.Join(root, "renamed.txt")))
	write(t, root, "binary.dat", "\x00\x01\x02")
	id := commit(t, root, "rename and binary")
	plan, err := PrepareUndo(t.Context(), root, []string{id})
	require.NoError(t, err)
	require.NoError(t, ApplyUndo(t.Context(), plan, plan.ID, false))
	data, err := os.ReadFile(filepath.Join(root, "base.txt"))
	require.NoError(t, err)
	require.Equal(t, "base\n", string(data))
	_, err = os.Stat(filepath.Join(root, "binary.dat"))
	require.True(t, os.IsNotExist(err))
}

func TestQualityHandoverHelper(t *testing.T) {
	idx := -1
	for i, arg := range os.Args {
		if arg == "--quality-helper" {
			idx = i
			break
		}
	}
	if idx < 0 {
		return
	}
	if os.Args[idx+1] == "install" {
		if err := os.WriteFile("installed.txt", []byte("ready"), 0o600); err != nil {
			os.Exit(2)
		}
		fmt.Print("installed")
		os.Exit(0)
	}
	if os.Args[idx+1] == "verify" {
		data, err := os.ReadFile("installed.txt")
		if err != nil || string(data) != "ready" {
			os.Exit(3)
		}
		fmt.Print("verified")
		os.Exit(0)
	}
	os.Exit(4)
}

func TestQualityHandoverRunsRealProcessesInCleanCopy(t *testing.T) {
	t.Parallel()
	root := repo(t)
	write(t, root, "README.md", "Install the app. Verify the app.")
	write(t, root, ".env", "SECRET=value")
	write(t, root, "atlas.json", "private configuration")
	exe, err := os.Executable()
	require.NoError(t, err)
	spec := HandoverSpec{Version: 1, Readme: "README.md", Steps: []HandoverStep{
		{Name: "install", Argv: []string{exe, "-test.run=^TestQualityHandoverHelper$", "--", "--quality-helper", "install"}, ReadmeQuote: "Install the app.", OutputContains: "installed", File: "installed.txt", FileContains: "ready"},
		{Name: "verify", Argv: []string{exe, "-test.run=^TestQualityHandoverHelper$", "--", "--quality-helper", "verify"}, ReadmeQuote: "Verify the app.", OutputContains: "verified"},
	}}
	var temporary string
	report, err := RunHandover(t.Context(), root, spec, func(ctx context.Context, dir string, argv []string) (Observation, error) {
		temporary = dir
		require.NotEqual(t, root, dir)
		_, err := os.Stat(filepath.Join(dir, ".env"))
		require.True(t, os.IsNotExist(err))
		_, err = os.Stat(filepath.Join(dir, "atlas.json"))
		require.True(t, os.IsNotExist(err))
		start := time.Now()
		cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
		cmd.Dir = dir
		data, err := cmd.CombinedOutput()
		if err != nil {
			return Observation{}, err
		}
		return Observation{Output: string(data), DurationMS: time.Since(start).Milliseconds()}, nil
	})
	require.NoError(t, err)
	require.True(t, report.Passed)
	require.Len(t, report.Results, 2)
	require.Contains(t, report.Excluded, ".env")
	require.NotEmpty(t, report.SnapshotHash)
	_, err = os.Stat(temporary)
	require.True(t, os.IsNotExist(err))
	_, err = os.Stat(filepath.Join(root, "installed.txt"))
	require.True(t, os.IsNotExist(err))
}

func TestQualityHandoverFailureBlocksLaterStepsAndRejectsInventedReadme(t *testing.T) {
	t.Parallel()
	root := repo(t)
	write(t, root, "README.md", "Install. Test.")
	spec := HandoverSpec{Version: 1, Readme: "README.md", Steps: []HandoverStep{{Name: "install", Argv: []string{"fixture"}, ReadmeQuote: "Install."}, {Name: "test", Argv: []string{"fixture"}, ReadmeQuote: "Test."}}}
	count := 0
	report, err := RunHandover(t.Context(), root, spec, func(context.Context, string, []string) (Observation, error) {
		count++
		return Observation{ExitCode: 1}, nil
	})
	require.NoError(t, err)
	require.False(t, report.Passed)
	require.Equal(t, 1, count)
	require.Equal(t, "blocked", report.Results[1].Status)
	spec.Steps[0].ReadmeQuote = "Invented command"
	_, err = RunHandover(t.Context(), root, spec, nil)
	require.ErrorContains(t, err, "README")
}

func TestQualityProjectPathsRefuseEscape(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, path := range []string{"../escape", ".", "", filepath.Join(root, "absolute")} {
		_, err := ProjectPath(root, path)
		require.Error(t, err)
	}
}

func TestQualitySpecificationsRejectUnknownAndTrailingJSON(t *testing.T) {
	t.Parallel()
	for _, data := range []string{`{"version":1,"unknown":true}`, `{"version":1} {"version":2}`} {
		var spec HandoverSpec
		require.Error(t, Decode([]byte(data), &spec))
	}
}
