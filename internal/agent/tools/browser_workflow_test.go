package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/browser"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
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

func TestBrowserPipelineStopsBeforeDependentInput(t *testing.T) {
	t.Parallel()
	for _, evidence := range []string{`{"passed":false}`, `{}`, `not JSON`} {
		t.Run(evidence, func(t *testing.T) {
			var actions []string
			tool := NewToolPipeline(func(_ context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
				require.Equal(t, BrowserToolName, call.Name)
				var p BrowserParams
				require.NoError(t, json.Unmarshal([]byte(call.Input), &p))
				actions = append(actions, p.Action)
				if p.Action == "assert" {
					return fantasy.NewTextResponse(evidence), nil
				}
				return fantasy.NewTextResponse(`{"action_sent":true}`), nil
			})
			p := PipelineParams{Browser: &BrowserWorkflowParams{Steps: []BrowserWorkflowStep{
				{ID: "fill", Action: "semantic_type", Text: "Atlas", Target: browser.Request{Selector: "#name"}, Verify: &browser.Request{Selector: "#name", Condition: "value", Expected: "Atlas"}},
				{ID: "submit", Action: "semantic_click", Target: browser.Request{Role: "button", Name: "Save"}},
			}}}
			encoded, err := json.Marshal(p)
			require.NoError(t, err)
			result, err := tool.Run(t.Context(), fantasy.ToolCall{ID: "workflow", Input: string(encoded)})
			require.NoError(t, err)
			require.True(t, result.IsError)
			require.Equal(t, []string{"semantic_type", "assert"}, actions)
			require.Contains(t, result.Content, "gate_failed")
		})
	}
}

func TestBrowserPipelineDispatchesBoundIdentityAndInheritedVerification(t *testing.T) {
	t.Parallel()
	var calls []BrowserParams
	tool := NewToolPipeline(func(_ context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		var p BrowserParams
		require.NoError(t, json.Unmarshal([]byte(call.Input), &p))
		calls = append(calls, p)
		if p.Action == "popup_wait" {
			return fantasy.NewTextResponse(`{"passed":true,"tab_id":"observed-popup"}`), nil
		}
		return fantasy.NewTextResponse(`{"passed":true}`), nil
	})
	p := PipelineParams{Browser: &BrowserWorkflowParams{Steps: []BrowserWorkflowStep{
		{ID: "popup", Action: "popup_wait"},
		{ID: "select", Action: "tab_select", Bindings: map[string]string{"advanced.tab_id": "popup#/tab_id"}},
		{ID: "fill", Action: "semantic_type", Text: "Atlas", Target: browser.Request{Selector: "#name"}, Bindings: map[string]string{"advanced.expected_tab_id": "popup#/tab_id"}, Verify: &browser.Request{Selector: "#name", Condition: "value", Expected: "Atlas"}},
	}}}
	encoded, err := json.Marshal(p)
	require.NoError(t, err)
	result, err := tool.Run(t.Context(), fantasy.ToolCall{ID: "workflow", Input: string(encoded)})
	require.NoError(t, err)
	require.False(t, result.IsError)
	require.Len(t, calls, 4)
	require.Equal(t, "observed-popup", calls[1].Advanced.TabID)
	require.Equal(t, "observed-popup", calls[2].Advanced.ExpectedTabID)
	require.Equal(t, "observed-popup", calls[3].Advanced.ExpectedTabID)
}
