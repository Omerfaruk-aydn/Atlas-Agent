package tools

import (
	"context"
	"encoding/json"
	"reflect"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

// Observe reads an explicit window without activating or changing applications.
func runDesktopObserve(ctx context.Context, p DesktopWorkflowParams, parent fantasy.ToolCall, invoke func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error)) (fantasy.ToolResponse, error) {
	if p.WindowID == "" || p.Application != "" || p.Rename != nil || p.Flow != nil || p.Result != "" || p.Transition != nil || len(p.Inputs)+len(p.Steps)+len(p.AdaptiveSteps) != 0 || !reflect.DeepEqual(p.Input, ComputerParams{}) || p.WaitFor != (computer.AutomationRequest{}) || p.WaitMS != 0 || p.MaxElements < 0 || p.MaxElements > 500 {
		return fantasy.NewTextErrorResponse("observe requires only explicit window_id, optional max_elements and observation; no inputs or launch parameters"), nil
	}
	if p.Observation == "" {
		p.Observation = "semantic"
	}
	if p.Observation != "semantic" && p.Observation != "auto" && p.Observation != "visual" {
		return fantasy.NewTextErrorResponse("invalid desktop observation mode"), nil
	}
	limit := p.MaxElements
	if limit == 0 {
		limit = 80
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	child := desktopRecipeChild(parent, invoke)
	r, err := child(ctx, ComputerParams{Action: "observe", Observation: p.Observation, Automation: computer.AutomationRequest{WindowID: p.WindowID, MaxElements: limit}})
	if !desktopRecipeStopped(r, err) {
		var observed map[string]any
		if json.Unmarshal([]byte(r.Content), &observed) == nil {
			desktopCheckpointGuidance(observed, p.WindowID)
			if data, marshalErr := marshalDesktopResult(observed); marshalErr == nil {
				r.Content = string(data)
			}
		}
	}
	return r, err
}
