package tools

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

// DesktopTransitionParams identifies the expected window after known inputs.
type DesktopTransitionParams struct {
	ExpectedTitle       string `json:"expected_title,omitempty" description:"Exact expected dialog title. Owned dialogs must match this title and owner_window_id of the input window."`
	ExpectedApplication string `json:"expected_application,omitempty" description:"Expected application identity, using the same strict supported identities as prepare; unrelated foreground windows are never selected."`
	WaitMS              int    `json:"wait_ms,omitempty" description:"Transition timeout in milliseconds, default 5000, maximum 15000."`
}

func runDesktopTransition(ctx context.Context, p DesktopWorkflowParams, parent fantasy.ToolCall, invoke func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error)) (fantasy.ToolResponse, error) {
	if p.Transition == nil || len(p.Steps) != 0 || p.WaitFor.Condition != "" || p.Application != "" || p.WaitMS != 0 {
		return fantasy.NewTextErrorResponse("transition requires input or inputs and transition parameters only"), nil
	}
	inputs, inputErr := desktopKnownInputs(p.Input, p.Inputs)
	if inputErr != nil {
		return fantasy.NewTextErrorResponse(inputErr.Error()), nil
	}
	p.Input = inputs[0]
	if p.WindowID != "" && p.WindowID != p.Input.Automation.WindowID {
		return fantasy.NewTextErrorResponse("transition window_id conflicts with its scoped input; no input sent"), nil
	}
	target := *p.Transition
	target.ExpectedTitle = strings.TrimSpace(target.ExpectedTitle)
	target.ExpectedApplication = strings.TrimSpace(target.ExpectedApplication)
	if (target.ExpectedTitle == "" && target.ExpectedApplication == "") || target.WaitMS < 0 || target.WaitMS > 15000 {
		return fantasy.NewTextErrorResponse("transition requires expected_title or expected_application and wait_ms 0-15000"), nil
	}
	if err := validateDesktopInput(p.Input); err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	if err := validateDesktopChild(desktopRecipeObservation(p, p.Input.Automation.WindowID)); err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	data, err := json.Marshal(target)
	if err != nil || len(data) > 64*1024 {
		return fantasy.NewTextErrorResponse("transition arguments exceed limit"), nil
	}
	wait := target.WaitMS
	if wait == 0 {
		wait = 5000
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(wait)*time.Millisecond)
	defer cancel()
	child := desktopRecipeChild(parent, invoke)
	list := func() ([]desktopWindowInfo, fantasy.ToolResponse, error) {
		r, err := child(ctx, ComputerParams{Action: "windows"})
		if desktopRecipeStopped(r, err) {
			return nil, r, err
		}
		var envelope struct {
			Result []desktopWindowInfo `json:"result"`
		}
		if json.Unmarshal([]byte(r.Content), &envelope) != nil || envelope.Result == nil || len(envelope.Result) >= 500 {
			return nil, fantasy.NewTextErrorResponse("Invalid transition window list"), nil
		}
		return envelope.Result, r, nil
	}
	before, r, err := list()
	if desktopRecipeStopped(r, err) {
		return r, err
	}
	baseline := make(map[string]bool, len(before))
	var source desktopWindowInfo
	for _, w := range before {
		baseline[w.ID] = w.Foreground
		if w.ID == p.Input.Automation.WindowID {
			source = w
		}
	}
	for _, input := range inputs {
		r, err = child(ctx, input)
		if desktopRecipeStopped(r, err) {
			return r, err
		}
	}
	for {
		windows, listed, listErr := list()
		if desktopRecipeStopped(listed, listErr) {
			return listed, listErr
		}
		matches := []string{}
		visibleIDs := make(map[string]bool, len(windows))
		for _, w := range windows {
			visibleIDs[w.ID] = true
		}
		for _, w := range windows {
			if !w.Foreground || w.ID == "" || w.ID == p.Input.Automation.WindowID {
				continue
			}
			if target.ExpectedTitle != "" && w.Name != target.ExpectedTitle {
				continue
			}
			owned := target.ExpectedTitle != "" && w.Owner == p.Input.Automation.WindowID
			_, existed := baseline[w.ID]
			// Shell property dialogs can have a hidden same-process helper owner.
			// Only a newly appeared native dialog with an exact expected title qualifies.
			helperDialog := target.ExpectedTitle != "" && !existed && source.ProcessID != 0 && w.ProcessID == source.ProcessID && w.WindowClass == "#32770" && !visibleIDs[w.Owner] && (w.Owner == "" || w.Owner == "0" || w.OwnerProcessID == source.ProcessID)
			owned = owned || helperDialog
			application := target.ExpectedApplication != "" && desktopApplicationWindowMatches(w, target.ExpectedApplication)
			if (target.ExpectedApplication != "" && !application) || (target.ExpectedApplication == "" && !owned) {
				continue
			}
			if wasForeground, existed := baseline[w.ID]; existed && wasForeground {
				continue
			}
			matches = append(matches, w.ID)
		}
		if len(matches) > 1 {
			return fantasy.NewTextErrorResponse("ambiguous_target: multiple expected transition windows; input was not replayed"), nil
		}
		if len(matches) == 1 {
			r, err := child(ctx, desktopRecipeObservation(p, matches[0]))
			if desktopRecipeStopped(r, err) {
				return r, err
			}
			return desktopRecipeResult(r, "transition", matches[0], 1)
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			if ctx.Err() == context.Canceled {
				return fantasy.ToolResponse{}, ctx.Err()
			}
			return fantasy.NewTextErrorResponse("condition_timeout: expected related window did not appear; inspect windows without replaying input"), nil
		case <-timer.C:
		}
	}
}
