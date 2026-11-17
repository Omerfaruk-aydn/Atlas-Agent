package mcp

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/stretchr/testify/require"
)

func newAppsTestConfig(t *testing.T, apps bool) *config.ConfigStore {
	t.Helper()
	t.Setenv("ATLAS_AGENT_GLOBAL_CONFIG", t.TempDir())
	t.Setenv("ATLAS_AGENT_GLOBAL_DATA", t.TempDir())
	t.Setenv("ATLAS_AGENT_CACHE_DIR", t.TempDir())
	store, err := config.Init(t.TempDir(), "", false)
	require.NoError(t, err)
	store.Config().Options.MCPApps = apps
	return store
}

func TestAppForIsOffUnlessEnabled(t *testing.T) {
	store := newAppsTestConfig(t, false)
	_, err := AppFor(t.Context(), store, "srv", "tool")
	require.ErrorContains(t, err, "MCP apps are off")
	_, err = AppForTool(t.Context(), store, "mcp_srv_tool")
	require.NoError(t, err, "an unknown tool is simply not found")
}

func TestAppForReportsUnknownToolsWhenEnabled(t *testing.T) {
	store := newAppsTestConfig(t, true)
	_, err := AppFor(t.Context(), store, "srv", "tool")
	require.ErrorContains(t, err, "no tool")
	page, err := AppForTool(t.Context(), store, "mcp_srv_tool")
	require.NoError(t, err)
	require.Nil(t, page)
}

func TestRecordAppToolsKeepsOnlyPageCallableOnes(t *testing.T) {
	t.Parallel()
	meta := func(v ...any) map[string]any {
		return map[string]any{"ui": map[string]any{"visibility": v}}
	}
	tools := []*Tool{
		{Name: "plain"},
		{Name: "model_only", Meta: meta("model")},
		{Name: "refresh", Meta: meta("app")},
		{Name: "both", Meta: meta("model", "app")},
		{Name: "off", Meta: meta("app")},
	}
	recordAppTools("srv-record-test", tools, []string{"off"})
	got, ok := allAppTools.Get("srv-record-test")
	require.True(t, ok)
	var names []string
	for _, tool := range got {
		names = append(names, tool.Name)
	}
	require.Equal(t, []string{"refresh", "both"}, names)
	recordAppTools("srv-record-test", []*Tool{{Name: "plain"}}, nil)
	_, ok = allAppTools.Get("srv-record-test")
	require.False(t, ok)
}

func TestCallFromAppRefusesWhenOffOrUnknown(t *testing.T) {
	off := newAppsTestConfig(t, false)
	_, err := CallFromApp(t.Context(), off, "mcp_srv_page", "refresh", nil)
	require.ErrorContains(t, err, "MCP apps are off")
	on := newAppsTestConfig(t, true)
	_, err = CallFromApp(t.Context(), on, "mcp_srv_page", "refresh", nil)
	require.ErrorContains(t, err, "no tool")
}
