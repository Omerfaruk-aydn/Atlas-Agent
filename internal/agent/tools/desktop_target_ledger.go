package tools

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

type desktopGuardContextKey struct{}

// requireObservedDesktopTarget refuses guessed handles before native input.
// Native focus and element freshness checks still run for known handles.
func requireObservedDesktopTarget(ctx context.Context, p ComputerParams) *computer.ContractError {
	if !desktopMutation(strings.ToLower(strings.TrimSpace(p.Action))) || p.Automation.WindowID == "" {
		return nil
	}
	state := desktopGuardFor(GetSessionFromContext(ctx))
	state.mu.Lock()
	at, known := state.windows[p.Automation.WindowID]
	state.mu.Unlock()
	if known && time.Since(at) < 5*time.Minute {
		return nil
	}
	err := computer.NewContractError("unobserved_target", "window_id", "The target window was not observed recently in this session. No input sent", "Call computer windows or observe/inspect, then select the intended live window from the returned evidence.", `{"action":"windows"}`)
	err.FreshObservation = true
	return err
}

// recordDesktopWindows accepts native discovery results, never request IDs.
// Complete enumeration also removes closed targets. Text and images are not kept.
func recordDesktopWindows(ctx context.Context, action string, resp fantasy.ToolResponse) {
	if ctx.Err() != nil || resp.IsError || resp.StopTurn {
		return
	}
	var root struct {
		WindowID string          `json:"window_id"`
		Result   json.RawMessage `json:"result"`
		Settle   *struct {
			New []string `json:"new_window_ids"`
		} `json:"input_settle"`
	}
	if json.Unmarshal([]byte(resp.Content), &root) != nil {
		root.WindowID, root.Result, root.Settle = "", nil, nil
	}
	// Native image captions are not JSON. Only adapter metadata may supply
	// their identities; never extract handles from free-form caption text.
	var meta struct {
		Settle *struct {
			New []string `json:"new_window_ids"`
		} `json:"input_settle"`
	}
	if json.Unmarshal([]byte(resp.Metadata), &meta) == nil && meta.Settle != nil {
		root.Settle = meta.Settle
	}
	state := desktopGuardFor(GetSessionFromContext(ctx))
	state.mu.Lock()
	defer state.mu.Unlock()
	now := time.Now()
	remember := func(id string) {
		if id != "" && id != "0" && computer.ValidateWindowID(id) == nil {
			if _, exists := state.windows[id]; !exists && len(state.windows) >= 512 {
				var oldest string
				var oldestTime time.Time
				for key, at := range state.windows {
					if oldestTime.IsZero() || at.Before(oldestTime) {
						oldest, oldestTime = key, at
					}
				}
				delete(state.windows, oldest)
			}
			state.windows[id] = now
		}
	}
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "windows":
		var windows []desktopWindowInfo
		if len(root.Result) == 0 || root.Result[0] != '[' || json.Unmarshal(root.Result, &windows) != nil {
			return
		}
		if len(windows) < 500 {
			clear(state.windows)
		}
		for _, w := range windows {
			remember(w.ID)
		}
	case "observe", "inspect", "find", "capture_window":
		remember(root.WindowID)
		if len(root.Result) > 0 && root.Result[0] == '{' {
			var result struct {
				WindowID string `json:"window_id"`
			}
			if json.Unmarshal(root.Result, &result) == nil {
				remember(result.WindowID)
			}
		}
	}
	if root.Settle != nil {
		// Windows a native enumeration found after an input.
		// TaskOpened is historical ownership, not a fresh observation.
		for _, id := range root.Settle.New {
			remember(id)
		}
	}
	for id, at := range state.windows {
		if now.Sub(at) >= 5*time.Minute {
			delete(state.windows, id)
		}
	}
}
