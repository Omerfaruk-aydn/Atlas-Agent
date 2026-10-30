package projectmodel

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/factstore"
	"github.com/stretchr/testify/require"
)

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		full := filepath.Join(root, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
	}
}

// sampleProject writes a small Go project with a test, a document and a
// decision record.
func sampleProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"go.mod":                      "module example.com/app\n\ngo 1.22\n",
		"main.go":                     "package main\n\nimport \"example.com/app/internal/util\"\n\nfunc main() {\n\tutil.Double(2)\n}\n",
		"internal/util/util.go":       "package util\n\n// Double doubles n.\nfunc Double(n int) int { return n * 2 }\n",
		"internal/util/util_test.go":  "package util\n\nimport \"testing\"\n\nfunc TestDouble(t *testing.T) {\n\tif Double(2) != 4 {\n\t\tt.Fatal(\"bad\")\n\t}\n}\n",
		"internal/other/other.go":     "package other\n\n// Hello says hello.\nfunc Hello() string { return \"hi\" }\n",
		"README.md":                   "# App\n\nAn example.\n",
		"docs/adr/0001-use-sqlite.md": "# Use SQLite for storage\n\nWe store data in SQLite.\n",
	})
	return root
}

func newTestService(t *testing.T, root string) (*Service, *factstore.Store) {
	t.Helper()
	dataDir := filepath.Join(root, ".atlas")
	facts := factstore.New(filepath.Join(dataDir, "facts.jsonl"))
	return NewService(root, dataDir, nil, facts), facts
}

func TestRefreshBuildsTheModel(t *testing.T) {
	t.Parallel()
	root := sampleProject(t)
	svc, _ := newTestService(t, root)

	m, report, err := svc.Refresh(t.Context(), false)
	require.NoError(t, err)
	require.True(t, report.Rebuilt)
	require.True(t, report.First)

	util := m.ModuleByPath("internal/util")
	require.NotNil(t, util)
	require.Equal(t, 1, util.Files)
	require.Equal(t, 1, util.TestFiles)
	require.Contains(t, util.Exports, "Double")
	require.Equal(t, []string{"."}, util.UsedBy)

	root0 := m.ModuleByPath(".")
	require.NotNil(t, root0)
	require.Contains(t, root0.DependsOn, "internal/util")
	require.True(t, root0.Entry)

	require.Contains(t, m.Entries, Entry{Path: "main.go", Kind: "main"})
	require.Contains(t, m.TestCommands, "go test ./...")
	require.Equal(t, []TestRef{{Path: "internal/util/util_test.go", Kind: TestDirect}}, m.TestMap["internal/util/util.go"])
	require.Contains(t, m.Edges["main.go"], "internal/util/util.go")

	var adr bool
	for _, d := range m.Decisions {
		adr = adr || d.Source == "adr" && d.Text == "Use SQLite for storage"
	}
	require.True(t, adr, "decision records are read as decisions")

	loaded, err := svc.Load()
	require.NoError(t, err)
	require.Equal(t, m.Fingerprint, loaded.Fingerprint)
	require.Equal(t, m.Modules, loaded.Modules)
}

func TestRefreshIsIncremental(t *testing.T) {
	t.Parallel()
	root := sampleProject(t)
	svc, _ := newTestService(t, root)

	first, _, err := svc.Refresh(t.Context(), false)
	require.NoError(t, err)

	again, report, err := svc.Refresh(t.Context(), false)
	require.NoError(t, err)
	require.False(t, report.Rebuilt, "nothing changed")
	require.Equal(t, first.BuiltAt, again.BuiltAt)

	writeFiles(t, root, map[string]string{
		"internal/util/util.go": "package util\n\nfunc Double(n int) int { return n * 2 }\n\nfunc Triple(n int) int { return n * 3 }\n",
	})
	changed, report, err := svc.Refresh(t.Context(), false)
	require.NoError(t, err)
	require.True(t, report.Rebuilt)
	require.Equal(t, 1, report.Changed)
	require.Zero(t, report.Added)
	require.Contains(t, report.Reused, "documents", "no Markdown file changed")
	require.NotEqual(t, first.Fingerprint, changed.Fingerprint)
	require.Contains(t, changed.ModuleByPath("internal/util").Exports, "Triple")

	writeFiles(t, root, map[string]string{"internal/extra/extra.go": "package extra\n\nfunc Extra() {}\n"})
	removed := filepath.Join(root, "internal", "other", "other.go")
	require.NoError(t, os.Remove(removed))
	_, report, err = svc.Refresh(t.Context(), false)
	require.NoError(t, err)
	require.Equal(t, 1, report.Added)
	require.Equal(t, 1, report.Removed)

	_, report, err = svc.Refresh(t.Context(), true)
	require.NoError(t, err)
	require.True(t, report.Rebuilt, "force rebuilds even when nothing changed")
	require.Empty(t, report.Reused)
}

func TestImpactFollowsDependentsAndFindsTests(t *testing.T) {
	t.Parallel()
	root := sampleProject(t)
	svc, _ := newTestService(t, root)
	m, err := svc.Current(t.Context())
	require.NoError(t, err)

	im := m.Impact([]string{"./internal/util/util.go", "missing.go"}, 0)
	require.Equal(t, []string{"internal/util/util.go"}, im.Changed)
	require.Equal(t, []string{"missing.go"}, im.Unknown)
	require.Equal(t, []string{"main.go"}, im.Direct)
	require.ElementsMatch(t, []string{".", "internal/util"}, im.Modules)
	require.NotEmpty(t, im.Tests)
	require.Equal(t, "internal/util/util_test.go", im.Tests[0].Path)
	require.Equal(t, []string{"go test ./internal/util"}, im.Commands)

	// A directory stands for the files under it.
	dir := m.Impact([]string{"internal/util"}, 1)
	require.Contains(t, dir.Changed, "internal/util/util.go")
	require.Contains(t, dir.Changed, "internal/util/util_test.go")
	require.Equal(t, "internal/util/util_test.go", dir.Tests[0].Path)
	require.Equal(t, "changed", dir.Tests[0].Kind)

	// Nothing depends on the leaf module.
	leaf := m.Impact([]string{"internal/other/other.go"}, 3)
	require.Empty(t, leaf.Direct)
	require.Empty(t, leaf.Tests)
	require.Equal(t, []string{"go test ./..."}, leaf.Commands, "falls back to the project command")

	text := FormatImpact(im)
	require.Contains(t, text, "main.go")
	require.Contains(t, text, "go test ./internal/util")
}

func TestDecisionsAreRecordedAndScoped(t *testing.T) {
	t.Parallel()
	root := sampleProject(t)
	svc, facts := newTestService(t, root)

	before, err := svc.Current(t.Context())
	require.NoError(t, err)

	d, err := svc.RecordDecision("Keep util free of I/O", "it is imported everywhere", []string{"internal/util"})
	require.NoError(t, err)
	require.Equal(t, "Keep util free of I/O", d.Text)
	require.Equal(t, "it is imported everywhere", d.Rationale)
	require.Equal(t, []string{"internal/util"}, d.Files)

	// The fact store sees it too, under the decision tag.
	recalled, err := facts.Recall("decision", 5)
	require.NoError(t, err)
	require.Len(t, recalled, 1)

	after, report, err := svc.Refresh(t.Context(), false)
	require.NoError(t, err)
	require.True(t, report.Rebuilt, "a new decision changes the model")
	require.NotEqual(t, before.Fingerprint, after.Fingerprint)

	scoped := after.Impact([]string{"internal/util/util.go"}, 1)
	require.Len(t, scoped.Decisions, 1)
	require.Equal(t, "Keep util free of I/O", scoped.Decisions[0].Text)

	unrelated := after.Impact([]string{"internal/other/other.go"}, 1)
	require.Empty(t, unrelated.Decisions)

	require.Contains(t, after.DecisionsAbout("i/o"), "Keep util free of I/O")
	require.Contains(t, after.DecisionsAbout("sqlite"), "docs/adr/0001-use-sqlite.md")

	_, err = svc.RecordDecision("   ", "", nil)
	require.Error(t, err)
	_, err = NewService(root, filepath.Join(root, ".atlas"), nil, nil).RecordDecision("x", "", nil)
	require.Error(t, err)
}

func TestSummaryIsStableAndBounded(t *testing.T) {
	t.Parallel()
	root := sampleProject(t)
	svc, _ := newTestService(t, root)
	m, err := svc.Current(t.Context())
	require.NoError(t, err)

	summary := m.Summary(0)
	require.NotEmpty(t, summary)
	require.LessOrEqual(t, len(summary), DefaultSummaryBytes)
	require.Contains(t, summary, "main.go")
	require.Contains(t, summary, "go test ./...")
	require.Equal(t, summary, m.Summary(0), "rendering is deterministic")

	// An unchanged project, rebuilt from scratch, yields the same block.
	rebuilt, _, err := svc.Refresh(t.Context(), true)
	require.NoError(t, err)
	require.Equal(t, summary, rebuilt.Summary(0))

	require.Empty(t, m.Summary(20), "a block that cannot fit is left out")
	require.Empty(t, (&Model{}).Summary(0), "an empty model says nothing")
	require.LessOrEqual(t, len(m.Summary(1_000_000)), HardSummaryBytes)
	require.NotEmpty(t, m.Overview(0))
}

func TestSummaryShrinksToFit(t *testing.T) {
	t.Parallel()
	m := &Model{Fingerprint: strings.Repeat("a", 64), Languages: map[string]int{"go": 400}, TestCommands: []string{"go test ./..."}}
	for i := range 40 {
		m.Modules = append(m.Modules, Module{
			Path: "internal/area" + string(rune('a'+i%26)) + string(rune('a'+i/26)) + "/pkg", Files: 10 + i, Language: "go",
			Owners: []Owner{{Name: "Ada", Commits: 9, Share: 90}}, UsedBy: []string{"x"},
		})
		m.Entries = append(m.Entries, Entry{Path: "cmd/tool" + string(rune('a'+i%26)) + "/main.go", Kind: "main"})
	}
	full := m.Overview(maxBudget)
	small := m.Summary(700)
	require.NotEmpty(t, small)
	require.LessOrEqual(t, len(small), 700)
	require.Less(t, len(small), len(full))
}

func TestOwnersFromHistory(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := sampleProject(t)
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Ada", "GIT_AUTHOR_EMAIL=ada@example.com",
			"GIT_COMMITTER_NAME=Ada", "GIT_COMMITTER_EMAIL=ada@example.com")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	}
	run("init", "-q")
	run("add", "-A")
	run("-c", "commit.gpgsign=false", "commit", "-q", "-m", "init")

	svc, _ := newTestService(t, root)
	m, report, err := svc.Refresh(t.Context(), false)
	require.NoError(t, err)
	require.NotEmpty(t, m.Head)
	util := m.ModuleByPath("internal/util")
	require.NotNil(t, util)
	require.Len(t, util.Owners, 1)
	require.Equal(t, "Ada", util.Owners[0].Name)
	require.Equal(t, 100, util.Owners[0].Share)
	require.Contains(t, m.OwnersOf("internal/util/util.go"), "Ada")
	require.NotContains(t, report.Reused, "ownership")

	// An edit that is not committed leaves the history untouched.
	writeFiles(t, root, map[string]string{"internal/other/other.go": "package other\n\nfunc Hello() string { return \"hello\" }\n"})
	changed := svc.ChangedFiles(t.Context())
	require.Equal(t, []string{"internal/other/other.go"}, changed)
	m2, report, err := svc.Refresh(t.Context(), false)
	require.NoError(t, err)
	require.Contains(t, report.Reused, "ownership")
	require.Equal(t, util.Owners, m2.ModuleByPath("internal/util").Owners)
}

func TestDescribe(t *testing.T) {
	t.Parallel()
	root := sampleProject(t)
	svc, _ := newTestService(t, root)
	m, err := svc.Current(t.Context())
	require.NoError(t, err)

	mod := m.Describe("internal/util")
	require.Contains(t, mod, "Module internal/util")
	require.Contains(t, mod, "Used by: .")

	file := m.Describe("internal/util/util.go")
	require.Contains(t, file, "Used by: main.go")
	require.Contains(t, file, "internal/util/util_test.go (direct)")

	require.Contains(t, m.Describe("util.go"), "File internal/util/util.go")
	require.Contains(t, m.Describe("nothing-like-this"), "Nothing in the project model")
	require.Contains(t, m.Describe("internal"), "Several things match")
	require.Contains(t, m.TestsOf([]string{"internal/util/util.go", "gone.go"}), "gone.go: not in the model")
}

func TestIsTestFile(t *testing.T) {
	t.Parallel()
	for _, p := range []string{"a_test.go", "src/a.test.ts", "src/b.spec.js", "tests/helper.py", "pkg/test_a.py", "src/FooTest.java", "__tests__/x.js", "spec/a_spec.rb"} {
		require.True(t, IsTestFile(p), p)
	}
	for _, p := range []string{"a.go", "src/latest.ts", "contest.go", "docs/tests.md", "main.py", "protest/readme.txt"} {
		require.False(t, IsTestFile(p), p)
	}
}

func TestDecisionFormatRoundTrips(t *testing.T) {
	t.Parallel()
	raw := FormatDecision("Use X", "Y is slower", []string{"a/b.go", "c"})
	d := parseDecision("id1", raw)
	require.Equal(t, Decision{ID: "id1", Text: "Use X", Rationale: "Y is slower", Files: []string{"a/b.go", "c"}, Source: "fact"}, d)

	plain := parseDecision("id2", "Just prose")
	require.Equal(t, Decision{ID: "id2", Text: "Just prose", Source: "fact"}, plain)
}

func TestLoadIgnoresOtherVersions(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	m, err := Load(dir)
	require.NoError(t, err)
	require.Nil(t, m)

	require.NoError(t, os.WriteFile(Path(dir), []byte(`{"version": 999}`), 0o644))
	m, err = Load(dir)
	require.NoError(t, err)
	require.Nil(t, m, "a model from another version is rebuilt, not trusted")

	require.NoError(t, os.WriteFile(Path(dir), []byte(`{broken`), 0o644))
	_, err = Load(dir)
	require.Error(t, err)

	// A damaged file does not stop a refresh.
	root := sampleProject(t)
	svc, _ := newTestService(t, root)
	require.NoError(t, os.MkdirAll(svc.DataDir(), 0o755))
	require.NoError(t, os.WriteFile(Path(svc.DataDir()), []byte(`{broken`), 0o644))
	_, report, err := svc.Refresh(t.Context(), false)
	require.NoError(t, err)
	require.True(t, report.Rebuilt)
}
