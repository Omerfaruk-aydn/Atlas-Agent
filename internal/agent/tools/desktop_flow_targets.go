package tools

import (
	"context"
	"encoding/json"
	"math"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

type desktopFlowChild func(context.Context, ComputerParams) (fantasy.ToolResponse, error)

func desktopFlowWindows(ctx context.Context, child desktopFlowChild) ([]desktopWindowInfo, fantasy.ToolResponse, error) {
	r, err := child(ctx, ComputerParams{Action: "windows"})
	if desktopRecipeStopped(r, err) {
		return nil, r, err
	}
	var data struct {
		Result []desktopWindowInfo `json:"result"`
	}
	if json.Unmarshal([]byte(r.Content), &data) != nil || data.Result == nil || len(data.Result) >= 500 {
		return nil, fantasy.NewTextErrorResponse("observation_incomplete: flow requires a complete native window list"), nil
	}
	return data.Result, r, nil
}

func desktopFlowResolve(ctx context.Context, n DesktopFlowNode, bindings map[string]desktopWindowInfo, child desktopFlowChild) (desktopWindowInfo, fantasy.ToolResponse, error) {
	wait := n.WaitMS
	if wait == 0 {
		wait = 5000
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(wait)*time.Millisecond)
	defer cancel()
	for {
		windows, r, err := desktopFlowWindows(ctx, child)
		if desktopRecipeStopped(r, err) {
			return desktopWindowInfo{}, r, err
		}
		var matches []desktopWindowInfo
		for _, w := range windows {
			if w.ID == "" || w.ProcessID == 0 || w.WindowClass == "" || w.Minimized || (n.Title != "" && w.Name != n.Title) || (n.Application != "" && !desktopApplicationWindowMatches(w, n.Application)) {
				continue
			}
			if n.OwnerRef != "" {
				owner, ok := bindings[n.OwnerRef]
				if !ok {
					return desktopWindowInfo{}, fantasy.NewTextErrorResponse("missing_binding: dialog owner was not resolved on this path"), nil
				}
				if w.Owner != owner.ID || w.ProcessID != owner.ProcessID || !w.Foreground {
					continue
				}
				ownerPresent := false
				for _, current := range windows {
					if current.ID == owner.ID && current.ProcessID == owner.ProcessID && current.ProcessName == owner.ProcessName && current.WindowClass == owner.WindowClass {
						ownerPresent = true
					}
				}
				if !ownerPresent {
					continue
				}
			}
			matches = append(matches, w)
		}
		if len(matches) > 1 {
			return desktopWindowInfo{}, fantasy.NewTextErrorResponse("ambiguous_target: flow window has multiple matches; add exact title"), nil
		}
		if len(matches) == 1 {
			return matches[0], r, nil
		}
		timer := time.NewTimer(250 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return desktopWindowInfo{}, fantasy.NewTextErrorResponse("condition_timeout: flow window did not appear; no input was replayed"), nil
		case <-timer.C:
		}
	}
}

func desktopFlowIdentity(ctx context.Context, window desktopWindowInfo, focus bool, child desktopFlowChild) (fantasy.ToolResponse, error) {
	if focus {
		r, err := child(ctx, ComputerParams{Action: "focus", Automation: computer.AutomationRequest{WindowID: window.ID}})
		if desktopRecipeStopped(r, err) {
			return r, err
		}
		var data struct {
			Result struct {
				Focused bool `json:"focused"`
			} `json:"result"`
		}
		if json.Unmarshal([]byte(r.Content), &data) != nil || !data.Result.Focused {
			return fantasy.NewTextErrorResponse("focus_denied: flow activation not confirmed"), nil
		}
	}
	windows, r, err := desktopFlowWindows(ctx, child)
	if desktopRecipeStopped(r, err) {
		return r, err
	}
	for _, w := range windows {
		if w.ID == window.ID && w.ProcessID == window.ProcessID && w.ProcessName == window.ProcessName && w.WindowClass == window.WindowClass && w.Foreground && !w.Minimized {
			return r, nil
		}
	}
	return fantasy.NewTextErrorResponse("wrong_window: flow binding is stale or not foreground; resolve again without replay"), nil
}

func desktopFlowFind(ctx context.Context, q computer.AutomationRequest, child desktopFlowChild) (desktopObservation, fantasy.ToolResponse, error) {
	q.Action, q.WaitMS, q.Text, q.Expected = "", 0, "", ""
	r, err := child(ctx, ComputerParams{Action: "find", Automation: q})
	if desktopRecipeStopped(r, err) {
		return desktopObservation{}, r, err
	}
	var data struct {
		Result struct {
			Matches   []desktopElement `json:"matches"`
			Truncated bool             `json:"truncated"`
		} `json:"result"`
	}
	if json.Unmarshal([]byte(r.Content), &data) != nil || data.Result.Matches == nil || data.Result.Truncated || len(data.Result.Matches) > 1 {
		return desktopObservation{}, fantasy.NewTextErrorResponse("observation_incomplete: flow selector must be unique and untruncated"), nil
	}
	return desktopObservation{WindowID: q.WindowID, Elements: data.Result.Matches}, r, nil
}

func desktopFlowTarget(ctx context.Context, q computer.AutomationRequest, child desktopFlowChild) (*desktopElement, fantasy.ToolResponse, error) {
	o, r, err := desktopFlowFind(ctx, q, child)
	if desktopRecipeStopped(r, err) {
		return nil, r, err
	}
	e, err := desktopAdaptiveTarget(o, q)
	if err != nil || e == nil || e.ID == "" || e.Password {
		return nil, fantasy.NewTextErrorResponse("target_missing: flow requires one fresh non-password runtime control"), nil
	}
	return e, r, nil
}

func desktopFlowPredicate(ctx context.Context, q computer.AutomationRequest, child desktopFlowChild) (bool, fantasy.ToolResponse, error) {
	o, r, err := desktopFlowFind(ctx, q, child)
	if desktopRecipeStopped(r, err) {
		return false, r, err
	}
	matched, err := desktopAdaptiveCondition(o, q)
	if err != nil {
		return false, fantasy.NewTextErrorResponse(err.Error()), nil
	}
	return matched, r, nil
}

func desktopFlowFailureCrop(ctx context.Context, q computer.AutomationRequest, window desktopWindowInfo, original fantasy.ToolResponse, child desktopFlowChild) fantasy.ToolResponse {
	e, r, err := desktopFlowTarget(ctx, q, child)
	if desktopRecipeStopped(r, err) || e.Offscreen || e.Width <= 0 || e.Height <= 0 {
		original.StopTurn = original.StopTurn || r.StopTurn
		return original
	}
	if r, err := desktopFlowIdentity(ctx, window, false, child); desktopRecipeStopped(r, err) {
		original.StopTurn = original.StopTurn || r.StopTurn
		return original
	}
	// UIA bounds are native desktop pixels; capture uses virtual-screen origin.
	var screen struct {
		Origin *computer.Point `json:"screen_origin"`
	}
	if json.Unmarshal([]byte(r.Content), &screen) != nil || screen.Origin == nil {
		return original
	}
	x, y := int(math.Floor(e.X))-screen.Origin.X, int(math.Floor(e.Y))-screen.Origin.Y
	w, h := int(math.Ceil(e.X+e.Width))-int(math.Floor(e.X)), int(math.Ceil(e.Y+e.Height))-int(math.Floor(e.Y))
	if x < 0 || y < 0 || w > 4096 || h > 4096 {
		return original
	}
	crop, err := child(ctx, ComputerParams{Action: "capture_region", X: x, Y: y, Width: w, Height: h})
	if desktopRecipeStopped(crop, err) || crop.Type != "image" {
		original.StopTurn = original.StopTurn || crop.StopTurn
		return original
	}
	crop.IsError, crop.Content = true, original.Content+"\nDiagnostic crop only; checkpoint failed. "+crop.Content
	return crop
}
