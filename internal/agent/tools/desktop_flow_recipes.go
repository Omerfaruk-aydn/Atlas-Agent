package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

func desktopFlowEffect(kind string) bool {
	return kind == "operation" || kind == "prepare" || kind == "rename" || kind == "close"
}

func desktopFlowIntent(n DesktopFlowNode, state *desktopFlowState, save func(*desktopFlowState) error) error {
	if state.Attempts[n.ID] != 0 {
		return fmt.Errorf("uncertain_effect: recipe intent was already recorded; no automatic replay")
	}
	state.Pending = n.ID
	state.Attempts[n.ID]++
	return save(state)
}

// Nested recipes still dispatch every child through the flow's bounded guards.
func desktopFlowInvoke(child desktopFlowChild) func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
	return func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		var p ComputerParams
		if call.Name != ComputerToolName || json.Unmarshal([]byte(call.Input), &p) != nil {
			return fantasy.NewTextErrorResponse("invalid nested desktop child"), nil
		}
		return child(ctx, p)
	}
}

func desktopFlowPrepare(ctx context.Context, n DesktopFlowNode, parent fantasy.ToolCall, child desktopFlowChild) (desktopWindowInfo, fantasy.ToolResponse, error) {
	r, err := runDesktopWorkflow(ctx, DesktopWorkflowParams{Mode: "prepare", Application: n.Application, WaitMS: n.WaitMS}, parent, desktopFlowInvoke(child))
	if desktopRecipeStopped(r, err) {
		return desktopWindowInfo{}, r, err
	}
	var observed struct {
		WindowID string `json:"window_id"`
	}
	if json.Unmarshal([]byte(r.Content), &observed) != nil || observed.WindowID == "" {
		return desktopWindowInfo{}, fantasy.NewTextErrorResponse("prepare_unverified: missing observed window identity"), nil
	}
	windows, r, err := desktopFlowWindows(ctx, child)
	if desktopRecipeStopped(r, err) {
		return desktopWindowInfo{}, r, err
	}
	for _, w := range windows {
		if w.ID == observed.WindowID && w.ProcessID > 0 && w.WindowClass != "" && w.Foreground && !w.Minimized && desktopApplicationWindowMatches(w, n.Application) {
			return w, r, nil
		}
	}
	return desktopWindowInfo{}, fantasy.NewTextErrorResponse("prepare_unverified: prepared application identity changed; stop without replay"), nil
}

func desktopFlowRecipe(ctx context.Context, n DesktopFlowNode, w desktopWindowInfo, parent fantasy.ToolCall, child desktopFlowChild) (fantasy.ToolResponse, error) {
	if n.Kind == "rename" {
		return runDesktopRename(ctx, DesktopWorkflowParams{Mode: "rename", WindowID: w.ID, Rename: n.Rename}, parent, desktopFlowInvoke(child))
	}
	r, err := child(ctx, ComputerParams{Action: "hotkey", Key: "f4", Modifiers: "alt", Automation: computer.AutomationRequest{WindowID: w.ID}})
	if desktopRecipeStopped(r, err) {
		return r, err
	}
	// One close request only; subsequent reads prove absence or expose a dialog.
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	for {
		windows, r, err := desktopFlowWindows(ctx, child)
		if desktopRecipeStopped(r, err) {
			return r, err
		}
		present := false
		for _, current := range windows {
			if current.ID == w.ID {
				present = true
			}
			if current.Owner == w.ID && current.ProcessID == w.ProcessID && current.Foreground {
				data, _ := json.Marshal(current)
				return fantasy.NewTextErrorResponse("close_blocked: owned dialog requires a new grounded decision; no further key sent. " + string(data)), nil
			}
		}
		if !present {
			data, _ := json.Marshal(map[string]any{"window_id": w.ID, "closed": true, "absence_verified": true})
			return fantasy.NewTextResponse(string(data)), nil
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fantasy.NewTextErrorResponse("close_unverified: requested window remains present; Alt+F4 was not replayed"), nil
		case <-timer.C:
		}
	}
}
