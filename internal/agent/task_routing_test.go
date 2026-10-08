package agent

import (
	"encoding/json"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func TestTaskRoutingProtocolsFollowAdvertisedCapabilities(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		tools []string
		want  string
	}{
		{"browser", []string{"browser"}, "Built-in browser use"},
		{"desktop", []string{"computer", "tool_pipeline"}, "fresh"},
		{"git", []string{"git_status"}, "Git"},
		{"testing", []string{"test_run"}, "exit status"},
		{"files", []string{"view"}, "credentials"},
		{"mcp", []string{"mcp_example_lookup"}, "connector"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			original := []fantasy.Message{fantasy.NewSystemMessage("base"), fantasy.NewUserMessage("task")}
			got := withToolProtocols(original, tc.tools)
			encoded, err := json.Marshal(got)
			require.NoError(t, err)
			require.Contains(t, string(encoded), tc.want)
			require.Contains(t, string(encoded), "loaded tool")
			require.Len(t, original, 2)
			if tc.name != "browser" {
				require.NotContains(t, string(encoded), "Built-in browser use")
			}
		})
	}
	require.Len(t, withToolProtocols(nil, nil), 0)
}

func TestYouTubeRequestPreloadsBrowserAcrossProviders(t *testing.T) {
	for _, format := range []wireFormat{wireOpenAI, wireAnthropic, wireGemini} {
		t.Run(string(format), func(t *testing.T) {
			h := newPayloadCoordinator(t, format, "", automationTools)
			_, err := h.coord.Run(t.Context(), newSession(t, h), "youtube dan rraenee i açarmısın en son attığı yeni video yu")
			require.NoError(t, err)
			require.Contains(t, stepTools(t, h)[0], "browser")
			require.NotContains(t, stepTools(t, h)[0], "computer")
			require.Contains(t, stepBody(t, h, 0), "Built-in browser use")
		})
	}
}
