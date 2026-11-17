package subagents

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeSubagentFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name+FileExt)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

// fromDisk drops the modes that ship in the binary, leaving what Discover
// actually read off disk. Discover always includes the built-ins (see
// builtin.go), so the tests below -- which are about the file scanning
// itself -- filter them out rather than restating the shipped catalog.
func fromDisk(all []*Subagent) []*Subagent {
	var out []*Subagent
	for _, s := range all {
		if !s.Builtin {
			out = append(out, s)
		}
	}
	return out
}

func TestParseContentReadsFrontmatterAndBody(t *testing.T) {
	content := "---\nname: research\ndescription: Deep research tasks.\nmodel: \"@research\"\n---\n\nDig deep before answering.\n"

	s, err := ParseContent([]byte(content))
	require.NoError(t, err)
	require.Equal(t, "research", s.Name)
	require.Equal(t, "Deep research tasks.", s.Description)
	require.Equal(t, "@research", s.Model)
	require.Equal(t, "Dig deep before answering.", s.Instructions)
}

func TestParseContentRejectsMissingFrontmatter(t *testing.T) {
	_, err := ParseContent([]byte("just a body, no frontmatter"))
	require.Error(t, err)
}

func TestParseContentModelIsOptional(t *testing.T) {
	s, err := ParseContent([]byte("---\nname: generic\ndescription: No fixed model.\n---\nDo the work.\n"))
	require.NoError(t, err)
	require.Empty(t, s.Model)
}

func TestValidateRequiresNameAndDescription(t *testing.T) {
	require.Error(t, (&Subagent{}).Validate())
	require.Error(t, (&Subagent{Name: "x"}).Validate())
	require.NoError(t, (&Subagent{Name: "x", Description: "d"}).Validate())
}

func TestValidateRejectsBadNames(t *testing.T) {
	require.Error(t, (&Subagent{Name: "Has Spaces", Description: "d"}).Validate())
	require.Error(t, (&Subagent{Name: "-leading-hyphen", Description: "d"}).Validate())
}

func TestValidateRequiresNameToMatchItsFile(t *testing.T) {
	s := &Subagent{Name: "research", Description: "d", Path: "/agents/frontend.md"}
	err := s.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "must match file name")
}

func TestDiscoverFindsFilesInEachDir(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	writeSubagentFile(t, dirA, "frontend", "---\nname: frontend\ndescription: UI work.\n---\nBody.\n")
	writeSubagentFile(t, dirB, "backend", "---\nname: backend\ndescription: API work.\n---\nBody.\n")

	found := fromDisk(Discover([]string{dirA, dirB}))

	require.Len(t, found, 2)
	require.Equal(t, "backend", found[0].Name)
	require.Equal(t, "frontend", found[1].Name)
}

func TestDiscoverSkipsInvalidFilesWithoutFailing(t *testing.T) {
	dir := t.TempDir()
	writeSubagentFile(t, dir, "broken", "not even frontmatter")
	writeSubagentFile(t, dir, "good", "---\nname: good\ndescription: Fine.\n---\nBody.\n")

	found := fromDisk(Discover([]string{dir}))

	require.Len(t, found, 1)
	require.Equal(t, "good", found[0].Name)
}

func TestDiscoverIgnoresNonMarkdownFiles(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hello"), 0o644))
	writeSubagentFile(t, dir, "good", "---\nname: good\ndescription: Fine.\n---\nBody.\n")

	found := fromDisk(Discover([]string{dir}))

	require.Len(t, found, 1)
	require.Equal(t, "good", found[0].Name)
}

func TestDiscoverIsNotRecursive(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "nested")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	writeSubagentFile(t, sub, "hidden", "---\nname: hidden\ndescription: Should not be found.\n---\nBody.\n")

	found := fromDisk(Discover([]string{dir}))

	require.Empty(t, found)
}

func TestDiscoverIgnoresAMissingDirectory(t *testing.T) {
	found := fromDisk(Discover([]string{filepath.Join(t.TempDir(), "does-not-exist")}))
	require.Empty(t, found)
}

func TestDeduplicateLastOccurrenceWins(t *testing.T) {
	first := &Subagent{Name: "research", Description: "old"}
	second := &Subagent{Name: "research", Description: "new"}

	got := Deduplicate([]*Subagent{first, second})

	require.Len(t, got, 1)
	require.Equal(t, "new", got[0].Description)
}

func TestFindIsCaseInsensitive(t *testing.T) {
	all := []*Subagent{{Name: "Research"}}

	got, ok := Find(all, "research")
	require.True(t, ok)
	require.Equal(t, "Research", got.Name)

	_, ok = Find(all, "nope")
	require.False(t, ok)
}

func TestMatchPicksTheHighestOverlap(t *testing.T) {
	all := []*Subagent{
		{Name: "frontend", Description: "React and CSS component work."},
		{Name: "research", Description: "Deep research into unfamiliar libraries and APIs."},
	}

	got, ok := Match(all, "Research the best library for parsing CSV files")
	require.True(t, ok)
	require.Equal(t, "research", got.Name)
}

func TestMatchReturnsFalseWithNoOverlap(t *testing.T) {
	all := []*Subagent{{Name: "frontend", Description: "React and CSS component work."}}

	_, ok := Match(all, "Investigate a database migration failure")
	require.False(t, ok)
}

func TestMatchReturnsFalseForEmptyPromptOrEmptyList(t *testing.T) {
	_, ok := Match([]*Subagent{{Name: "research", Description: "Deep research."}}, "")
	require.False(t, ok, "an empty prompt has nothing to match against")

	_, ok = Match(nil, "research the library")
	require.False(t, ok, "no subagents means nothing to match")
}

func TestMatchIgnoresShortConnectiveWords(t *testing.T) {
	// "the", "for", "and" are all short filler that would otherwise
	// match almost any description; only "database" is a real signal.
	all := []*Subagent{
		{Name: "docs", Description: "Write docs for the project and the wiki."},
		{Name: "database", Description: "Schema design and database migrations."},
	}

	got, ok := Match(all, "Look at the database for the migration issue")
	require.True(t, ok)
	require.Equal(t, "database", got.Name)
}

func TestMatchTiesGoToTheFirstCandidate(t *testing.T) {
	all := []*Subagent{
		{Name: "first", Description: "Handles research tasks."},
		{Name: "second", Description: "Also handles research tasks."},
	}

	got, ok := Match(all, "research tasks")
	require.True(t, ok)
	require.Equal(t, "first", got.Name)
}

func TestMatchIsCaseInsensitive(t *testing.T) {
	all := []*Subagent{{Name: "research", Description: "DEEP RESEARCH into APIs."}}

	got, ok := Match(all, "research apis")
	require.True(t, ok)
	require.Equal(t, "research", got.Name)
}

func TestSubagentLimitsAreParsedAndBounded(t *testing.T) {
	t.Parallel()
	s, err := ParseContent([]byte("---\nname: scout\ndescription: Looks around\nmax_steps: 12\ntimeout_seconds: 300\nmcp: [docs, search]\n---\nBody"))
	require.NoError(t, err)
	require.Equal(t, 12, s.MaxSteps)
	require.Equal(t, 300, s.TimeoutSeconds)
	require.Equal(t, []string{"docs", "search"}, s.MCP)
	require.NoError(t, s.Validate())

	for name, bad := range map[string]Subagent{
		"negative steps":  {Name: "x", Description: "d", MaxSteps: -1},
		"too many steps":  {Name: "x", Description: "d", MaxSteps: MaxStepsLimit + 1},
		"long timeout":    {Name: "x", Description: "d", TimeoutSeconds: MaxTimeoutSeconds + 1},
		"empty mcp name":  {Name: "x", Description: "d", MCP: []string{" "}},
		"too many to use": {Name: "x", Description: "d", MCP: make([]string, 17)},
	} {
		require.Error(t, bad.Validate(), name)
	}
}
