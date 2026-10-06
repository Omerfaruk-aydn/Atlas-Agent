package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

// DesktopAdaptiveStep selects a method before input and requires actual readback.
type DesktopAdaptiveStep struct {
	When       *computer.AutomationRequest `json:"when,omitempty" description:"Optional predicate evaluated from complete fresh observation. False skips; missing readable content or ambiguous targets stop."`
	Input      ComputerParams              `json:"input,omitempty" description:"Single invoke/set_value with fresh explicit selector; method chosen from live supported patterns. Alternative to inputs."`
	Inputs     []ComputerParams            `json:"inputs,omitempty" description:"1-16 already resolved inputs in one window, alternative to input; each still passes normal guards."`
	Checkpoint computer.AutomationRequest  `json:"checkpoint" description:"Mandatory expected result in the same window. Failure stops later steps without replay."`
}

func runDesktopAdaptive(ctx context.Context, p DesktopWorkflowParams, parent fantasy.ToolCall, invoke func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error)) (fantasy.ToolResponse, error) {
	if p.Observation == "" {
		p.Observation = "auto"
	}
	if len(p.AdaptiveSteps) == 0 || len(p.AdaptiveSteps) > 8 || len(p.Steps) > 0 || p.Transition != nil || p.Input.Action != "" || len(p.Inputs) > 0 || p.WaitFor.Condition != "" || p.Application != "" || p.WindowID != "" || p.WaitMS != 0 {
		return fantasy.NewTextErrorResponse("adaptive requires 1-8 adaptive_steps without other recipe parameters"), nil
	}
	budget := 0
	for _, step := range p.AdaptiveSteps {
		inputs, err := desktopKnownInputs(step.Input, step.Inputs)
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		id := inputs[0].Automation.WindowID
		if len(step.Inputs) == 0 {
			if step.Input.Action != "invoke" && step.Input.Action != "set_value" {
				return fantasy.NewTextErrorResponse("adaptive input must be invoke/set_value; use inputs for resolved keyboard/pointer operations"), nil
			}
			budget += 7
		} else {
			budget += len(inputs) + 3
		}
		if err := validateDesktopCheckpoint(step.Checkpoint, id); err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		if step.When != nil {
			if step.When.WaitMS != 0 {
				return fantasy.NewTextErrorResponse("adaptive when evaluates the current observation; wait_ms belongs on the result checkpoint"), nil
			}
			if err := validateDesktopCheckpoint(*step.When, id); err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
		}
		if err := validateDesktopChild(desktopRecipeObservation(p, id)); err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
	}
	if budget > 64 {
		return fantasy.NewTextErrorResponse("adaptive plan exceeds 64 normal child operations"), nil
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	child := desktopRecipeChild(parent, invoke)
	trace := []map[string]any{}
	var last fantasy.ToolResponse
	lastID := ""
	for i, step := range p.AdaptiveSteps {
		if err := ctx.Err(); err != nil {
			return desktopAdaptiveProgress(fantasy.ToolResponse{}, trace), err
		}
		id := step.Input.Automation.WindowID
		if len(step.Inputs) > 0 {
			id = step.Inputs[0].Automation.WindowID
		}
		entry := map[string]any{"step": i + 1, "window_id": id, "status": "preflight", "mutation_attempts": 0}
		trace = append(trace, entry)
		fail := func(r fantasy.ToolResponse, err error) (fantasy.ToolResponse, error) {
			entry["status"] = "stopped"
			return desktopAdaptiveProgress(r, trace), err
		}
		var err error
		if lastID != id {
			last, err = child(ctx, desktopRecipeObservation(p, id))
			if desktopRecipeStopped(last, err) {
				return fail(last, err)
			}
			lastID = id
		}
		var observation struct {
			desktopObservation
			Foreground *desktopWindowInfo `json:"foreground_window"`
		}
		if len(last.Content) > 64*1024 || json.Unmarshal([]byte(last.Content), &observation) != nil || observation.WindowID != id || observation.Foreground == nil || observation.Foreground.ID != id || !observation.Foreground.Foreground || observation.Foreground.Minimized {
			return fail(fantasy.NewTextErrorResponse("wrong_window: adaptive requires fresh observation of the intended foreground window"), nil)
		}
		if observation.Truncated {
			last.IsError = true
			last.Content += "\nobservation_incomplete: use returned visual evidence to replan; no adaptive mutation executed in this step"
			return fail(last, nil)
		}
		entry["application_adapter"] = desktopAdapterForWindow(*observation.Foreground).ID
		if step.When != nil {
			passed, predicateErr := desktopAdaptiveCondition(observation.desktopObservation, *step.When)
			if predicateErr != nil {
				return fail(fantasy.NewTextErrorResponse(predicateErr.Error()), nil)
			}
			entry["predicate_passed"] = passed
			if !passed {
				entry["status"] = "skipped"
				continue
			}
		}
		inputs := step.Inputs
		method := "resolved_inputs"
		if len(inputs) == 0 {
			inputs, method, err = desktopAdaptiveMethod(observation.desktopObservation, step.Input)
			if err != nil {
				last.IsError = true
				last.Content += "\n" + err.Error() + "; no adaptive mutation executed in this step; replan from returned evidence"
				return fail(last, nil)
			}
		}
		entry["method"] = method
		for _, input := range inputs {
			if desktopMutation(input.Action) {
				entry["mutation_attempts"] = entry["mutation_attempts"].(int) + 1
			}
			r, inputErr := child(ctx, input)
			if desktopRecipeStopped(r, inputErr) {
				return fail(r, inputErr)
			}
			if input.Action == "assert" && !desktopAdaptiveAssertion(r.Content, id) {
				return fail(fantasy.NewTextErrorResponse("focus_denied: adaptive keyboard field focus was not verified; text/Enter not sent"), nil)
			}
		}
		entry["status"] = "awaiting_checkpoint"
		r, checkpointErr := child(ctx, ComputerParams{Action: "assert", Automation: step.Checkpoint})
		if desktopRecipeStopped(r, checkpointErr) {
			return fail(r, checkpointErr)
		}
		if !desktopAdaptiveAssertion(r.Content, id) {
			if len(r.Content) <= 8*1024 && json.Valid([]byte(r.Content)) {
				entry["failed_assertion"] = json.RawMessage(r.Content)
			}
			return fail(fantasy.NewTextErrorResponse("gate_failed: adaptive result was not verified; do not replay the attempted operation"), nil)
		}
		entry["status"] = "verified"
		entry["checkpoint_verified"] = true
		entry["assertion"] = json.RawMessage(r.Content)
		entry["phase"] = "final_observation"
		last, err = child(ctx, desktopRecipeObservation(p, id))
		if desktopRecipeStopped(last, err) {
			return fail(last, err)
		}
		var finalIdentity struct {
			WindowID   string             `json:"window_id"`
			Foreground *desktopWindowInfo `json:"foreground_window"`
		}
		if json.Unmarshal([]byte(last.Content), &finalIdentity) != nil || finalIdentity.WindowID != id || finalIdentity.Foreground == nil || finalIdentity.Foreground.ID != id || !finalIdentity.Foreground.Foreground || finalIdentity.Foreground.Minimized {
			last.IsError = true
			last.Content += "\nmodal_transition: checkpoint passed but final foreground changed or is unavailable; inspect returned evidence before further input"
			return fail(last, nil)
		}
		entry["phase"] = "complete"
		lastID = id
	}
	if err := ctx.Err(); err != nil {
		return desktopAdaptiveProgress(fantasy.ToolResponse{}, trace), err
	}
	var final map[string]any
	if json.Unmarshal([]byte(last.Content), &final) != nil || final == nil {
		return desktopAdaptiveProgress(fantasy.NewTextErrorResponse("Invalid adaptive final observation"), trace), nil
	}
	if final["window_id"] != lastID {
		return desktopAdaptiveProgress(fantasy.NewTextErrorResponse("Invalid adaptive final window identity; operations were not replayed"), trace), nil
	}
	verified, skipped := 0, 0
	for _, entry := range trace {
		if entry["status"] == "verified" {
			verified++
		}
		if entry["status"] == "skipped" {
			skipped++
		}
	}
	final["workflow"] = map[string]any{"mode": "adaptive", "verified_steps": verified, "skipped_steps": skipped, "condition_verified": verified > 0, "task_completion_requires_verification": true, "steps": trace}
	data, err := marshalDesktopResult(final)
	if err != nil {
		return desktopAdaptiveProgress(fantasy.ToolResponse{}, trace), err
	}
	if len(data) > 64*1024 {
		return desktopAdaptiveProgress(fantasy.NewTextErrorResponse("Adaptive evidence exceeds limit; operations were not replayed"), trace), nil
	}
	last.Content = string(data)
	return last, nil
}

func desktopAdaptiveAssertion(content, id string) bool {
	if len(content) > 8*1024 {
		return false
	}
	var state struct {
		Passed    *bool  `json:"passed"`
		WindowID  string `json:"window_id"`
		Truncated bool   `json:"actual_truncated"`
	}
	return json.Unmarshal([]byte(content), &state) == nil && state.Passed != nil && *state.Passed && state.WindowID == id && !state.Truncated
}

func desktopAdaptiveProgress(r fantasy.ToolResponse, trace []map[string]any) fantasy.ToolResponse {
	data, err := json.Marshal(map[string]any{"steps": trace, "input_replayed": false, "guidance": "Stop and reconcile the actual state. Do not replay attempted operations; resume from fresh evidence after verified checkpoints."})
	if err != nil {
		return r
	}
	metadata := map[string]any{}
	if r.Metadata != "" {
		if json.Unmarshal([]byte(r.Metadata), &metadata) != nil || metadata == nil {
			metadata = map[string]any{"original_metadata": r.Metadata}
		}
	}
	metadata["adaptive_progress"] = json.RawMessage(data)
	encoded, err := json.Marshal(metadata)
	if err == nil && len(encoded) <= 64*1024 {
		r.Metadata = string(encoded)
	}
	if len(r.Content)+len(data)+32 <= 64*1024 {
		r.Content += fmt.Sprintf("\nAdaptive progress: %s", data)
	}
	return r
}
