package subagents

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// builtinNames is the set of modes the binary is expected to ship. It is
// spelled out rather than derived so that dropping or renaming one is a
// deliberate edit here, not a silent change in behavior: these names are
// also the model roles users assign models to, and a rename orphans a
// user's configuration.
var builtinNames = []string{
	"architect",
	"backend", "debug", "docs", "frontend", "planner",
	"refactor", "research", "review", "security", "test",
}

func TestBuiltinShipsTheExpectedModes(t *testing.T) {
	got := make([]string, 0, len(Builtin()))
	for _, s := range Builtin() {
		got = append(got, s.Name)
	}
	require.Equal(t, builtinNames, got, "built-in modes must match the documented set, sorted by name")
}

func TestBuiltinDefinitionsAreValid(t *testing.T) {
	for _, s := range Builtin() {
		t.Run(s.Name, func(t *testing.T) {
			require.NoError(t, s.Validate())
			require.True(t, s.Builtin)
			require.Empty(t, s.Path, "a built-in has no file on disk")
			if s.Name == "architect" {
				require.Equal(t, "research", s.Model)
			} else {
				require.Equal(t, s.Name, s.Model, "each mode runs on the model role sharing its name")
			}
			require.NotEmpty(t, strings.TrimSpace(s.Instructions))
			if s.Name == "architect" || s.Name == "planner" || s.Name == "research" || s.Name == "review" {
				require.True(t, s.ReadOnly)
				require.True(t, s.AllowCommands)
				require.Contains(t, s.Instructions, "Run scoped commands")
			}
		})
	}
}

// The instructions are the whole point of a mode: a stub would load and
// validate fine while doing nothing useful. The upper bound matters as
// much as the lower one -- every mode prompt is prepended to a real
// request, so an essay costs the user context on every single turn.
// Bound both lines and bytes so detailed role protocols remain a finite
// working set rather than an unbounded reference manual.
func TestBuiltinInstructionsAreSubstantial(t *testing.T) {
	for _, s := range Builtin() {
		t.Run(s.Name, func(t *testing.T) {
			lines := strings.Count(strings.TrimSpace(s.Instructions), "\n") + 1
			require.GreaterOrEqual(t, lines, 90, "mode prompt is too thin to be useful")
			require.LessOrEqual(t, lines, 300, "mode prompt is long enough to cost real context")
			require.LessOrEqual(t, len(s.Instructions), 18*1024, "role detail must fit its context budget")
		})
	}
}

func TestBuiltinReturnsIndependentCopies(t *testing.T) {
	first := Builtin()
	require.NotEmpty(t, first)
	first[0].Instructions = "mutated"

	second := Builtin()
	require.NotEqual(t, "mutated", second[0].Instructions, "callers must not be able to corrupt the shared catalog")
}

func TestDeleteNamedRefusesABuiltinWithAUsefulMessage(t *testing.T) {
	err := DeleteNamed([]string{t.TempDir()}, "review")
	require.Error(t, err)
	require.Contains(t, err.Error(), "built-in mode")
	require.Contains(t, err.Error(), "override")
}

// Saving over a built-in's name writes a real file rather than failing,
// which is how a user customizes a shipped mode.
func TestSaveNamedOverridesABuiltin(t *testing.T) {
	dir := t.TempDir()
	path, err := SaveNamed([]string{dir}, dir, Subagent{Name: "review", Description: "mine", Instructions: "body"}, false)
	require.NoError(t, err)
	require.FileExists(t, path)

	got, ok := Find(Discover([]string{ProjectDir(dir)}), "review")
	require.True(t, ok)
	require.False(t, got.Builtin)
	require.Equal(t, "mine", got.Description)
}

func TestDiscoverIncludesBuiltinModes(t *testing.T) {
	all := Discover(nil)
	_, ok := Find(all, "review")
	require.True(t, ok, "built-in modes must be discoverable with no search dirs configured")
}

func TestDiscoverLetsAFileOverrideABuiltin(t *testing.T) {
	dir := t.TempDir()
	writeSubagentFile(t, dir, "review", "---\nname: review\ndescription: mine\n---\nlocal body\n")

	all := Discover([]string{dir})
	got, ok := Find(all, "review")
	require.True(t, ok)
	require.Equal(t, "mine", got.Description, "a user's own file must win over the shipped mode")
	require.False(t, got.Builtin)

	// The override replaces exactly one mode and leaves the rest alone.
	require.Len(t, all, len(builtinNames))
}
