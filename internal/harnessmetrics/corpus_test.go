package harnessmetrics

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCasesRoundTripAndPrune(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	cases, err := LoadCases(root)
	require.NoError(t, err)
	require.Empty(t, cases)

	id := NewCaseID(time.Date(2026, 10, 16, 14, 15, 0, 0, time.UTC), "Sess-ABC_123456")
	require.Equal(t, "20261016-141500-sessabc1", id)
	c := Case{
		ID: id, Reason: "contract", Request: "fix it", Checks: []string{"make test"},
		Files: []CaseFile{{Path: "a.go", Before: "package a", Existed: true}},
	}
	require.NoError(t, SaveCase(root, c))
	got, err := LoadCase(root, id)
	require.NoError(t, err)
	require.Equal(t, c, got)

	_, err = LoadCase(root, "../escape")
	require.Error(t, err)

	dir := filepath.Join(root, Dir, CorpusDir)
	for i := range maxCases + 3 {
		require.NoError(t, SaveCase(root, Case{ID: NewCaseID(time.Date(2027, 1, 1, 0, 0, i, 0, time.UTC), "s")}))
	}
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, maxCases)
	cases, err = LoadCases(root)
	require.NoError(t, err)
	require.NotEqual(t, id, cases[0].ID, "the oldest cases are dropped first")
}

func TestProjectKindsAndCheckHints(t *testing.T) {
	t.Parallel()
	project := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(project, "go.mod"), []byte("module x"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(project, "Makefile"), []byte("all:"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(project, "App.csproj"), []byte("<Project/>"), 0o644))
	require.Equal(t, []string{"dotnet", "go", "make"}, ProjectKinds(project))

	attrs := CheckAttrs([]string{"go"}, []string{"cd /src/app && go test ./...", "/usr/bin/check.sh", "go vet ./..."})
	require.Equal(t, "go test ./...", attrs["c0"])
	require.NotContains(t, attrs, "c1", "a command tied to an absolute path is dropped")
	require.Equal(t, "go vet ./...", attrs["c2"])
	require.Equal(t, "3", attrs["checks"])

	data := t.TempDir()
	r := New(func() string { return data })
	r.Record(ContractPassed, "s1", CheckAttrs([]string{"go"}, []string{"go test ./...", "go vet ./..."}))
	r.Record(ContractPassed, "s1", CheckAttrs([]string{"go"}, []string{"go test ./..."}))
	r.Record(ContractPassed, "s2", CheckAttrs([]string{"go"}, []string{"go test ./..."}))
	r.Record(ContractPassed, "s3", CheckAttrs([]string{"node"}, []string{"npm test"}))
	require.Equal(t, []string{"go test ./...", "go vet ./..."}, CheckHints(data, []string{"go"}, 5))
	require.Equal(t, []string{"go test ./..."}, CheckHints(data, []string{"go", "rust"}, 1))
	require.Empty(t, CheckHints(data, []string{"python"}, 5))

	events, err := Load(data, time.Time{})
	require.NoError(t, err)
	summary := Summarize(events)
	require.Equal(t, []string{"go test ./... (2)", "go vet ./... (1)"}, summary.Checks["go"])
	require.Contains(t, Format(summary, time.Time{}), "Checks that verified work, by project kind\n  go: go test ./... (2), go vet ./... (1)\n  node: npm test (1)")
}
