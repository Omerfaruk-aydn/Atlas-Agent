package tools

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/browser"

	"github.com/stretchr/testify/require"
)

func TestBrowserWorkflowCompilesHookedCallsAndVerificationGates(t *testing.T) {
	t.Parallel()
	steps, err := compileBrowserWorkflow(BrowserWorkflowParams{TabID: "owned", Origin: "https://example.test", Steps: []BrowserWorkflowStep{
		{ID: "fill", Action: "semantic_type", Text: "Atlas", Target: browser.Request{Role: "textbox", Name: "Name"}, Verify: &browser.Request{Role: "textbox", Name: "Name", Condition: "value", Expected: "Atlas"}},
		{ID: "send", Action: "semantic_click", Target: browser.Request{Role: "button", Name: "Save", Scope: "#profile"}},
		{ID: "result", Action: "wait_for", Target: browser.Request{Selector: "#status", Condition: "text", Expected: "Saved"}},
	}})
	require.NoError(t, err)
	require.Len(t, steps, 4)
	for _, step := range steps {
		require.Equal(t, BrowserToolName, step.Tool)
	}
	require.Equal(t, "fill/verify", steps[1].ID)
	require.True(t, steps[1].RequirePassed)
	require.True(t, steps[3].RequirePassed)
	advanced := steps[0].Arguments["advanced"].(map[string]any)
	require.Equal(t, "owned", advanced["expected_tab_id"])
	require.Equal(t, "https://example.test", advanced["expected_origin"])
	require.Equal(t, "Atlas", advanced["text"])
}
