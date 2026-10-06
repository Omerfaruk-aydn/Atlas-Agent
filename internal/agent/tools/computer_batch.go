package tools

import (
	"context"
	"encoding/json"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

// ComputerDispatcher retains the standard child hook and permission path.
type ComputerDispatcher func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error)

type computerBatchDispatchKey struct{}

type computerBatchDispatchTool struct {
	fantasy.AgentTool
	invoke ComputerDispatcher
}

func (t *computerBatchDispatchTool) Run(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	return t.AgentTool.Run(context.WithValue(ctx, computerBatchDispatchKey{}, t.invoke), call)
}

// ComputerBatchInput cannot embed another batch or control-flow tool.
type ComputerBatchInput struct {
	Action     string                     `json:"action" description:"Known input: invoke/set_value/type/key/hotkey/click/double_click/right_click/move/drag/scroll. Explicit window_id required."`
	Automation computer.AutomationRequest `json:"automation"`
	Text       string                     `json:"text,omitempty"`
	Key        string                     `json:"key,omitempty"`
	Modifiers  string                     `json:"modifiers,omitempty"`
	X          int                        `json:"x,omitempty"`
	Y          int                        `json:"y,omitempty"`
	EndX       int                        `json:"end_x,omitempty"`
	EndY       int                        `json:"end_y,omitempty"`
	Button     string                     `json:"button,omitempty"`
	ScrollX    int                        `json:"scroll_x,omitempty"`
	ScrollY    int                        `json:"scroll_y,omitempty"`
}

type ComputerBatchGroup struct {
	FocusWindow bool                       `json:"focus_window,omitempty" description:"Explicitly activate this observed window before its group; activation must be confirmed."`
	Inputs      []ComputerBatchInput       `json:"inputs" description:"1-16 already-resolved same-window inputs in order. Split at unknown dialogs or results."`
	Checkpoint  computer.AutomationRequest `json:"checkpoint" description:"Mandatory actual result checkpoint after this logical group, in the same explicit window."`
}

type ComputerBatchParams struct {
	Groups []ComputerBatchGroup `json:"groups" description:"1-24 groups, at most 128 normal child operations, 120-second deadline. All groups validate before the first child."`
}

func runComputerBatch(ctx context.Context, p ComputerParams, call fantasy.ToolCall, invoke ComputerDispatcher) (fantasy.ToolResponse, error) {
	if p.Batch == nil || len(p.Batch.Groups) == 0 || len(p.Batch.Groups) > desktopSequenceStepLimit || invoke == nil {
		return fantasy.NewTextErrorResponse("computer batch requires 1-24 checkpointed groups and configured guarded child dispatch"), nil
	}
	outer := p
	outer.Action, outer.Batch = "", nil
	data, err := json.Marshal(outer)
	if err != nil {
		return fantasy.ToolResponse{}, err
	}
	// Automation is a non-pointer struct and serializes even when empty.
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil {
		return fantasy.NewTextErrorResponse("invalid computer batch parameters"), nil
	}
	for key, value := range fields {
		if key == "action" && string(value) == `""` {
			continue
		}
		if key == "automation" && string(value) == `{}` {
			continue
		}
		return fantasy.NewTextErrorResponse("batch cannot be combined with standalone input/observation parameters"), nil
	}
	steps := make([]DesktopWorkflowStep, 0, len(p.Batch.Groups))
	for _, group := range p.Batch.Groups {
		inputs := make([]ComputerParams, 0, len(group.Inputs))
		for _, input := range group.Inputs {
			inputs = append(inputs, ComputerParams{Action: input.Action, Automation: input.Automation, Text: input.Text, Key: input.Key, Modifiers: input.Modifiers, X: input.X, Y: input.Y, EndX: input.EndX, EndY: input.EndY, Button: input.Button, ScrollX: input.ScrollX, ScrollY: input.ScrollY})
		}
		steps = append(steps, DesktopWorkflowStep{FocusWindow: group.FocusWindow, Inputs: inputs, Checkpoint: group.Checkpoint})
	}
	return runDesktopWorkflow(ctx, DesktopWorkflowParams{Mode: "sequence", Result: "checkpoints", Steps: steps}, call, invoke)
}
