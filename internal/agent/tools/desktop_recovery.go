package tools

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

// Desktop recovery collects evidence without replaying an uncertain input.
// Every recovery read uses the same permission- and hook-controlled dispatcher.
func desktopRecoveryInvoke(_ context.Context, parent fantasy.ToolCall, invoke func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error)) func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
	recovered := false
	return func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		r, err := invoke(ctx, call)
		if recovered || err != nil || !r.IsError || r.StopTurn || ctx.Err() != nil || call.Name != ComputerToolName {
			return r, err
		}
		code, _, _ := strings.Cut(r.Content, ":")
		guidance := ""
		switch code {
		case "wrong_window", "focus_denied":
			guidance = "Resolve the intended target in the fresh window list; explicitly focus and verify it before new input."
		case "target_missing", "stale_snapshot", "stale_element", "stale_observation":
			guidance = "Resolve a fresh target; do not reuse the disappeared handle or expired element."
		case "accessibility_unavailable", "unsupported_pattern":
			guidance = "Use the fresh supported patterns; if unavailable, use screenshot/OCR to resolve a precise target before new input."
		case "condition_timeout", "gate_failed":
			guidance = "Inspect the actual state and reconcile the failed checkpoint before planning dependent input."
		default:
			return r, err
		}
		recovered = true
		var input ComputerParams
		if json.Unmarshal([]byte(call.Input), &input) != nil {
			return r, err
		}
		// Prepare owns bounded focus re-selection; explicit handles stay pinned.
		// Do not add a second recovery enumeration or observation to that path.
		if input.Action == "focus" {
			recovered = false
			return r, err
		}
		recovery := map[string]any{"code": code, "guidance": guidance, "input_replayed": false}
		readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		read := func(suffix string, p ComputerParams) (fantasy.ToolResponse, error) {
			data, marshalErr := json.Marshal(p)
			if marshalErr != nil {
				return fantasy.ToolResponse{}, marshalErr
			}
			return invoke(readCtx, fantasy.ToolCall{ID: parent.ID + "/recovery/" + suffix, Name: ComputerToolName, Input: string(data)})
		}
		listed, listErr := read("windows", ComputerParams{Action: "windows"})
		if listErr == nil && !listed.IsError && !listed.StopTurn && len(listed.Content) <= 32*1024 {
			var envelope struct {
				Result []desktopWindowInfo `json:"result"`
			}
			if json.Unmarshal([]byte(listed.Content), &envelope) == nil {
				recovery["fresh_windows"] = envelope.Result
				for _, w := range envelope.Result {
					if w.ID != input.Automation.WindowID || w.ID == "" {
						continue
					}
					observed, observeErr := read("observe", ComputerParams{Action: "observe", Observation: "semantic", Automation: computer.AutomationRequest{WindowID: w.ID, MaxElements: 40}})
					if observeErr == nil && !observed.IsError && !observed.StopTurn && len(observed.Content) <= 32*1024 && json.Valid([]byte(observed.Content)) {
						recovery["fresh_observation"] = json.RawMessage(observed.Content)
					}
					if observed.StopTurn {
						r.StopTurn = true
					}
					break
				}
			}
		}
		if listed.StopTurn {
			r.StopTurn = true
		}
		metadata := map[string]any{}
		if r.Metadata != "" {
			if json.Unmarshal([]byte(r.Metadata), &metadata) != nil || metadata == nil {
				metadata = map[string]any{"original_metadata": r.Metadata}
			}
		}
		metadata["desktop_recovery"] = recovery
		data, marshalErr := json.Marshal(metadata)
		if marshalErr == nil {
			r.Metadata = string(data)
		}
		// Model tool messages consume Content; expose the evidence there as well.
		evidence, evidenceErr := json.Marshal(recovery)
		if evidenceErr == nil && len(r.Content)+len(evidence)+32 <= 64*1024 {
			r.Content += "\nDesktop recovery evidence: " + string(evidence)
		}
		return r, err
	}
}
