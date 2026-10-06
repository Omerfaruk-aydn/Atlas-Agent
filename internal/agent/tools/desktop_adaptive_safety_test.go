package tools

import (
	"context"
	"encoding/json"

	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func adaptiveFixtureStep() DesktopAdaptiveStep {
	return DesktopAdaptiveStep{Input: ComputerParams{Action: "set_value", Automation: computer.AutomationRequest{WindowID: "11", Name: "Field", Text: "hello"}}, Checkpoint: computer.AutomationRequest{WindowID: "11", Name: "Field", Condition: "value", Expected: "hello"}}
}

func adaptiveFixtureObservation(e desktopElement, truncated bool) fantasy.ToolResponse {
	data, _ := json.Marshal(map[string]any{"window_id": "11", "snapshot_id": "fresh", "truncated": truncated, "foreground_window": map[string]any{"window_id": "11", "foreground": true}, "elements": []desktopElement{e}})
	return fantasy.NewTextResponse(string(data))
}

func TestDesktopAdaptiveRejectsWholeInvalidPlanBeforeDispatch(t *testing.T) {
	for _, kind := range []string{"wrong_checkpoint", "wrong_predicate", "both_inputs", "too_many", "budget", "predicate_wait", "cross_window", "unsupported_mode"} {
		t.Run(kind, func(t *testing.T) {
			p := DesktopWorkflowParams{Mode: "adaptive", AdaptiveSteps: []DesktopAdaptiveStep{adaptiveFixtureStep(), adaptiveFixtureStep()}}
			s := &p.AdaptiveSteps[1]
			switch kind {
			case "wrong_checkpoint":
				s.Checkpoint.WindowID = "22"
			case "wrong_predicate":
				s.When = &computer.AutomationRequest{WindowID: "22", Name: "Field", Condition: "visible"}
			case "both_inputs":
				s.Inputs = []ComputerParams{s.Input}
			case "too_many":
				for range 7 {
					p.AdaptiveSteps = append(p.AdaptiveSteps, adaptiveFixtureStep())
				}
			case "budget":
				p.AdaptiveSteps = nil
				for range 8 {
					step := adaptiveFixtureStep()
					step.Input = ComputerParams{}
					for range 8 {
						step.Inputs = append(step.Inputs, ComputerParams{Action: "key", Key: "enter", Automation: computer.AutomationRequest{WindowID: "11"}})
					}
					p.AdaptiveSteps = append(p.AdaptiveSteps, step)
				}
			case "predicate_wait":
				s.When = &computer.AutomationRequest{WindowID: "11", Name: "Field", Condition: "visible", WaitMS: 1000}
			case "cross_window":
				s.Input = ComputerParams{}
				s.Inputs = []ComputerParams{{Action: "key", Key: "enter", Automation: computer.AutomationRequest{WindowID: "11"}}, {Action: "key", Key: "enter", Automation: computer.AutomationRequest{WindowID: "22"}}}
			case "unsupported_mode":
				p.Mode = "act"
			}
			calls := 0
			r, err := runDesktopWorkflow(t.Context(), p, fantasy.ToolCall{}, func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
				calls++
				return fantasy.ToolResponse{}, nil
			})
			require.NoError(t, err)
			require.True(t, r.IsError)
			require.Zero(t, calls)
		})
	}
}
