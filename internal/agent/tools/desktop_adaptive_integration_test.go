package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func TestPipelineAdaptiveReusesObservationAndReturnsOneResult(t *testing.T) {
	observes, mutations := 0, 0
	tool := NewToolPipeline(func(_ context.Context, c fantasy.ToolCall) (fantasy.ToolResponse, error) {
		var p ComputerParams
		require.NoError(t, json.Unmarshal([]byte(c.Input), &p))
		switch p.Action {
		case "observe":
			observes++
			require.Equal(t, "auto", p.Observation)
			return adaptiveFixtureObservation(desktopElement{ID: "field", Name: "Field", Role: "ControlType.Edit", Enabled: true, ValueAvailable: true, Value: "hello", Patterns: []string{"Value"}}, false), nil
		case "set_value":
			mutations++
			return fantasy.NewTextResponse(`{}`), nil
		case "assert":
			return fantasy.NewTextResponse(`{"window_id":"11","passed":true,"actual":"hello"}`), nil
		default:
			t.Fatalf("Unexpected action %s", p.Action)
			return fantasy.ToolResponse{}, nil
		}
	})
	first := adaptiveFixtureStep()
	second := adaptiveFixtureStep()
	second.When = &computer.AutomationRequest{WindowID: "11", Name: "Field", Condition: "value", Expected: "different"}
	third := adaptiveFixtureStep()
	third.When = &computer.AutomationRequest{WindowID: "11", Name: "Field", Condition: "value", Expected: "hello"}
	data, err := json.Marshal(PipelineParams{Desktop: &DesktopWorkflowParams{Mode: "adaptive", AdaptiveSteps: []DesktopAdaptiveStep{first, second, third}}})
	require.NoError(t, err)
	r, err := tool.Run(t.Context(), fantasy.ToolCall{ID: "adaptive", Input: string(data)})
	require.NoError(t, err)
	require.False(t, r.IsError, r.Content)
	require.Equal(t, 2, mutations)
	require.Equal(t, 3, observes, "No duplicate preflight observations between same-window steps")
	require.Contains(t, r.Content, `"verified_steps":2`)
	require.Contains(t, r.Content, `"skipped_steps":1`)
}
