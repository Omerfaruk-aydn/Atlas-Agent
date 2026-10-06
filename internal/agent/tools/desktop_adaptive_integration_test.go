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

func TestDesktopAdaptiveKeepsVisualEvidenceWithoutBlindInput(t *testing.T) {
	for _, kind := range []string{"incomplete", "unsupported_role", "modal"} {
		t.Run(kind, func(t *testing.T) {
			observes, mutations := 0, 0
			r, err := runDesktopWorkflow(t.Context(), DesktopWorkflowParams{Mode: "adaptive", AdaptiveSteps: []DesktopAdaptiveStep{adaptiveFixtureStep(), adaptiveFixtureStep()}}, fantasy.ToolCall{}, func(_ context.Context, c fantasy.ToolCall) (fantasy.ToolResponse, error) {
				var p ComputerParams
				require.NoError(t, json.Unmarshal([]byte(c.Input), &p))
				if p.Action == "observe" {
					observes++
					e := desktopElement{ID: "field", Name: "Field", Role: "ControlType.Edit", Enabled: true, Patterns: []string{"Value"}}
					if kind == "unsupported_role" {
						e.Role = "ControlType.Pane"
						e.Patterns = nil
					}
					response := adaptiveFixtureObservation(e, kind == "incomplete")
					if kind == "modal" && observes == 2 {
						response.Content = `{"window_id":"11","foreground_window":{"window_id":"22","foreground":true},"elements":[]}`
					}
					image := fantasy.NewImageResponse([]byte("visual evidence"), "image/png")
					image.Content = response.Content
					return image, nil
				}
				if p.Action == "set_value" {
					mutations++
					return fantasy.NewTextResponse(`{}`), nil
				}
				return fantasy.NewTextResponse(`{"window_id":"11","passed":true}`), nil
			})
			require.NoError(t, err)
			require.True(t, r.IsError)
			require.Equal(t, "image", r.Type)
			require.NotEmpty(t, r.Data)
			if kind == "modal" {
				require.Equal(t, 1, mutations)
				require.Contains(t, r.Content, `"checkpoint_verified":true`)
			} else {
				require.Zero(t, mutations)
			}
		})
	}
}
