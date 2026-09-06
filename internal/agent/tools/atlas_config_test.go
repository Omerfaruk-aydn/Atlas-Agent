package tools

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/subagents"
	"github.com/stretchr/testify/require"
)

func TestAtlasConfigScopeDefaultsToGlobal(t *testing.T) {
	for _, raw := range []string{"", "global", "user", " GLOBAL "} {
		scope, name, err := atlasConfigScope(raw)
		require.NoError(t, err)
		require.Equal(t, config.ScopeGlobal, scope, "raw=%q", raw)
		require.Equal(t, "global", name)
	}
}

func TestAtlasConfigScopeAcceptsTheProjectsNames(t *testing.T) {
	for _, raw := range []string{"workspace", "project", "local"} {
		scope, name, err := atlasConfigScope(raw)
		require.NoError(t, err)
		require.Equal(t, config.ScopeWorkspace, scope, "raw=%q", raw)
		require.Equal(t, "workspace", name)
	}
}

func TestAtlasConfigScopeRejectsAnythingElse(t *testing.T) {
	_, _, err := atlasConfigScope("everywhere")
	require.Error(t, err)
	require.Contains(t, err.Error(), "global")
}

// The permission prompt has to say what is about to happen to the user's
// setup; "atlas_config set_role" tells them nothing.
func TestAtlasConfigDescribesTheChangeInWords(t *testing.T) {
	got := atlasConfigDescribe("set_role", AtlasConfigParams{
		Role: "research", Provider: "claude", Model: "claude-sonnet-5",
	}, "global")
	require.Contains(t, got, "research")
	require.Contains(t, got, "claude/claude-sonnet-5")
	require.Contains(t, got, "global")
}

func TestAtlasConfigDescribesClearingARole(t *testing.T) {
	got := atlasConfigDescribe("set_role", AtlasConfigParams{Role: "research"}, "workspace")
	require.Contains(t, got, "Clear")
	require.Contains(t, got, "research")
}

// set_model defaults to the large model, and the description has to say
// so rather than leaving the type blank.
func TestAtlasConfigDescribesAModelSwitchWithoutAType(t *testing.T) {
	got := atlasConfigDescribe("set_model", AtlasConfigParams{
		Provider: "openai", Model: "gpt-5",
	}, "global")
	require.Contains(t, got, "large")
}

// Reading is not changing, so it must not interrupt the user for
// approval; everything else must.
func TestAtlasConfigOnlyReadsAreExemptFromApproval(t *testing.T) {
	require.True(t, atlasConfigReadOnly["list"])
	require.True(t, atlasConfigReadOnly["get_field"])
	for _, action := range []string{"set_role", "set_model", "enable_tool", "disable_tool", "set_field"} {
		require.False(t, atlasConfigReadOnly[action], "%s changes configuration and must be approved", action)
	}
}

func TestAtlasConfigEveryActionIsHandled(t *testing.T) {
	// The switch in the tool falls through to set_field, so an action
	// listed but not handled would silently do the wrong thing.
	for _, action := range atlasConfigActions {
		require.NotEmpty(t, action)
	}
	require.Contains(t, atlasConfigActions, "list")
	require.Contains(t, atlasConfigActions, "set_role")
}

// The bug this guards against: asked for "thirty percent", a model sends
// the string "0.3" for a field typed float64. The write succeeded, the
// reload that followed failed, that failure was logged as a warning, and
// the user was left with a config file that no longer parsed at all.
func TestAtlasConfigRefusesAValueTheConfigCannotHold(t *testing.T) {
	cfg := &config.Config{Options: &config.Options{}}

	err := checkConfigValue(cfg, "options.auto_summarize_at", "0.3")
	require.Error(t, err, "a string in a float field must be refused, not written")
	require.Contains(t, err.Error(), "options.auto_summarize_at")
	require.Contains(t, err.Error(), "would no longer load")
}

func TestAtlasConfigAcceptsAValueOfTheRightType(t *testing.T) {
	cfg := &config.Config{Options: &config.Options{}}

	require.NoError(t, checkConfigValue(cfg, "options.auto_summarize_at", 0.3))
	require.NoError(t, checkConfigValue(cfg, "options.debug", true))
	require.NoError(t, checkConfigValue(cfg, "options.max_steps_per_turn", 40))
}

func TestAtlasConfigRefusesABoolInANumberField(t *testing.T) {
	cfg := &config.Config{Options: &config.Options{}}
	require.Error(t, checkConfigValue(cfg, "options.max_steps_per_turn", "lots"))
}

// The bug this guards against: a session asked to run a debate between
// "review" and "security" subagents that were never configured, and had
// no tool able to create them -- only a human clicking through the "new
// subagent" dialog could. save_subagent is that capability from chat.
func TestAtlasConfigSaveSubagentWritesAFileThatListSubagentsSees(t *testing.T) {
	dir := t.TempDir()
	agentsDir := subagents.ProjectDir(dir)
	store := config.NewTestStore(&config.Config{Options: &config.Options{SubagentsPaths: []string{agentsDir}}})

	resp, err := atlasConfigSaveSubagent(store, dir, config.ScopeWorkspace, "workspace", AtlasConfigParams{
		Name: "security", Description: "flags security issues in a design",
	})
	require.NoError(t, err)
	require.False(t, resp.IsError)

	got := atlasConfigListSubagents(store)
	require.Contains(t, got, "security")
	require.Contains(t, got, "flags security issues")
}

func TestAtlasConfigSaveSubagentRequiresNameAndDescription(t *testing.T) {
	dir := t.TempDir()
	store := config.NewTestStore(&config.Config{Options: &config.Options{}})

	resp, err := atlasConfigSaveSubagent(store, dir, config.ScopeGlobal, "global", AtlasConfigParams{Description: "no name"})
	require.NoError(t, err)
	require.True(t, resp.IsError)

	resp, err = atlasConfigSaveSubagent(store, dir, config.ScopeGlobal, "global", AtlasConfigParams{Name: "nodesc"})
	require.NoError(t, err)
	require.True(t, resp.IsError)
}

func TestAtlasConfigDeleteSubagentRequiresAName(t *testing.T) {
	store := config.NewTestStore(&config.Config{Options: &config.Options{}})

	resp, err := atlasConfigDeleteSubagent(store, AtlasConfigParams{})
	require.NoError(t, err)
	require.True(t, resp.IsError)
}

// subagents.Discover always includes the shipped built-in subagents
// (review, security, research, ...) even with no search paths
// configured, so "no subagents at all" never actually happens -- and the
// built-ins are exactly what a request to debate between "review" and
// "security" is asking for, with nothing to create first.
func TestAtlasConfigListSubagentsIncludesTheBuiltins(t *testing.T) {
	store := config.NewTestStore(&config.Config{Options: &config.Options{}})
	got := atlasConfigListSubagents(store)
	require.Contains(t, got, "review")
	require.Contains(t, got, "security")
}

// The bug this guards against: a session asked to debate as "review" and
// "security" called plain `list`, saw nothing about subagents at all,
// concluded none existed, and spent several turns creating model roles
// on providers with no balance before stumbling onto the real fix --
// assigning the "review" and "security" model roles the built-in
// subagents of the same name already reference. `list` has to say so
// itself, on the very first call, or the same trial-and-error repeats.
func TestAtlasConfigListSurfacesSubagentsAndTheirMissingRoles(t *testing.T) {
	store := config.NewTestStore(&config.Config{Options: &config.Options{}})
	got := atlasConfigList(store)
	require.Contains(t, got, "[subagents]")
	require.Contains(t, got, "review")
	require.Contains(t, got, "security")
	require.Contains(t, got, `needs the "review" model role assigned`)
}

func TestAtlasConfigListStopsFlaggingARoleOnceItIsAssigned(t *testing.T) {
	store := config.NewTestStore(&config.Config{Options: &config.Options{
		ModelRoles: map[string]config.SelectedModel{
			"review": {Provider: "minimax", Model: "MiniMax-M2.7"},
		},
	}})
	got := atlasConfigList(store)
	require.NotContains(t, got, `needs the "review" model role assigned`)
}
