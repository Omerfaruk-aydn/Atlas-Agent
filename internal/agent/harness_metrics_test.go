package agent

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/csync"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/harnessmetrics"
	"github.com/stretchr/testify/require"
)

func harnessKinds(t *testing.T, root string) map[harnessmetrics.Kind]int {
	t.Helper()
	events, err := harnessmetrics.Load(root, time.Time{})
	require.NoError(t, err)
	kinds := map[harnessmetrics.Kind]int{}
	for _, e := range events {
		kinds[e.Kind]++
	}
	return kinds
}

func TestHarnessDataDirPrefersTheConfiguredDirectory(t *testing.T) {
	t.Parallel()

	require.Equal(t, filepath.Join("proj", ".atlas"), harnessDataDir(nil, "proj")())
	require.Empty(t, harnessDataDir(nil, "")())
	store := config.NewTestStore(&config.Config{Options: &config.Options{DataDirectory: "data"}})
	require.Equal(t, "data", harnessDataDir(store, "proj")())
}

func TestToolSearchOutcomesAreRecorded(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	observe := observeToolSearch(harnessmetrics.New(func() string { return root }), false)
	ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, "s")
	observe(ctx, "browser", 1, 0)
	observe(ctx, "breakpoit", 0, 1)
	observe(ctx, "zzz", 0, 0)
	observeToolSearch(harnessmetrics.New(func() string { return root }), true)(ctx, "sub", 0, 0)

	kinds := harnessKinds(t, root)
	require.Equal(t, 1, kinds[harnessmetrics.ToolSearchHit])
	require.Equal(t, 1, kinds[harnessmetrics.ToolSearchClosest])
	require.Equal(t, 1, kinds[harnessmetrics.ToolSearchEmpty], "sub-agent searches are not counted")
}

func TestShellNudgesAreRecordedWhenGivenAndFollowed(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	a := &sessionAgent{
		shellNudgesGiven: csync.NewMap[string, bool](),
		harness:          harnessmetrics.New(func() string { return root }),
	}
	palette := nudgeShellTools([]fantasy.AgentTool{echoBashTool()}, fixturePalette([]string{"browser"}), a.shellNudgesGiven, a.observeShell)
	runNudged(t, palette, "s", "npx playwright test")

	a.recordNudgesFollowed("s", steps([]fantasy.Content{fantasy.ToolCallContent{ToolCallID: "1", ToolName: "browser", Input: "{}"}}))
	a.recordNudgesFollowed("s", steps([]fantasy.Content{fantasy.ToolCallContent{ToolCallID: "2", ToolName: "browser", Input: "{}"}}))

	kinds := harnessKinds(t, root)
	require.Equal(t, 1, kinds[harnessmetrics.NudgeGiven])
	require.Equal(t, 1, kinds[harnessmetrics.NudgeFollowed], "following is counted once per nudge")
}
