package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func TestDesktopSequenceAvoidsDuplicateStableReadback(t *testing.T) {
	for _, kind := range []string{"stable", "visual", "truncated", "different_target", "pointer", "missing_actual", "wrong_window"} {
		t.Run(kind, func(t *testing.T) {
			steps := calculationSteps()
			mode := "auto"
			if kind == "visual" {
				mode = "visual"
			}
			if kind == "different_target" {
				steps[1].Checkpoint.ElementID = "other"
			}
			if kind == "pointer" {
				steps[1].Input = ComputerParams{Action: "click", X: 10, Y: 20, Automation: computer.AutomationRequest{WindowID: "11"}}
			}
			asserts, observations := 0, 0
			r, err := runDesktopWorkflow(t.Context(), DesktopWorkflowParams{Mode: "sequence", Steps: steps, Observation: mode}, fantasy.ToolCall{}, func(_ context.Context, c fantasy.ToolCall) (fantasy.ToolResponse, error) {
				var p ComputerParams
				require.NoError(t, json.Unmarshal([]byte(c.Input), &p))
				if p.Action == "assert" {
					actual := map[string]any{"passed": true, "actual": steps[asserts].Checkpoint.Expected, "window_id": "11", "actual_truncated": kind == "truncated"}
					if kind == "missing_actual" {
						delete(actual, "actual")
					}
					if kind == "wrong_window" {
						actual["window_id"] = "22"
					}
					asserts++
					data, marshalErr := json.Marshal(actual)
					require.NoError(t, marshalErr)
					return fantasy.NewTextResponse(string(data)), nil
				}
				if p.Action == "observe" {
					observations++
					return fantasy.NewTextResponse(`{"window_id":"11","snapshot_id":"final","elements":[]}`), nil
				}
				return fantasy.NewTextResponse(`{}`), nil
			})
			require.NoError(t, err)
			require.False(t, r.IsError, r.Content)
			require.Equal(t, 3, asserts)
			want := 3
			switch kind {
			case "stable":
				want = 1
			case "pointer":
				want = 2
			}
			require.Equal(t, want, observations)
			require.Contains(t, r.Content, `"snapshot_id":"final"`)
			var result struct {
				Checkpoints []json.RawMessage `json:"checkpoints"`
			}
			require.NoError(t, json.Unmarshal([]byte(r.Content), &result))
			require.Len(t, result.Checkpoints, 3)
		})
	}
}
