package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func runPipelineSteps(t *testing.T, invoke func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error), p PipelineParams) (fantasy.ToolResponse, []PipelineResult) {
	t.Helper()
	data, err := json.Marshal(p)
	require.NoError(t, err)
	resp, err := NewToolPipeline(invoke).Run(t.Context(), fantasy.ToolCall{ID: "p", Name: ToolPipelineName, Input: string(data)})
	require.NoError(t, err)
	var results []PipelineResult
	require.NoError(t, json.Unmarshal([]byte(resp.Content), &results), resp.Content)
	return resp, results
}

func outcomesByID(results []PipelineResult) map[string]string {
	out := map[string]string{}
	for _, r := range results {
		out[r.ID] = r.Outcome
	}
	return out
}

// A successful pipeline step is applied, a passed gate is verified, and the
// steps left behind by a failure are blocked, not "skipped" as if they were
// optional. Conditional branches that were never selected stay planned.
func TestPipelineStepOutcomesSeparateAppliedVerifiedAndBlocked(t *testing.T) {
	t.Parallel()
	invoke := func(_ context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		var p ComputerParams
		require.NoError(t, json.Unmarshal([]byte(call.Input), &p))
		switch p.Action {
		case "assert":
			return withToolOutcome(fantasy.NewTextResponse(`{"passed":true,"actual":"Ekran değeri 9.000"}`), ToolOutcome{Dispatch: "none", Verified: true}), nil
		case "hotkey":
			return withToolOutcome(fantasy.NewTextErrorResponse("wrong_window: focus the intended window"), ToolOutcome{Dispatch: "none", Mutation: true}), nil
		}
		return withToolOutcome(fantasy.NewTextResponse("Typed the text."), ToolOutcome{Dispatch: "sent", Mutation: true}), nil
	}
	equals := "0"
	resp, results := runPipelineSteps(t, invoke, PipelineParams{Steps: []PipelineStep{
		{ID: "type", Tool: ComputerToolName, Arguments: map[string]any{"action": "type", "text": "8638+362", "automation": map[string]any{"window_id": "11"}}},
		{ID: "check", Tool: ComputerToolName, RequirePassed: true, Arguments: map[string]any{"action": "assert", "automation": map[string]any{"window_id": "11", "element_id": "r", "condition": "text", "expected": "Ekran değeri 9.000"}}},
		{ID: "maybe", Tool: ComputerToolName, When: &PipelineCondition{Source: "check#/passed", Equals: &equals}, Arguments: map[string]any{"action": "key", "key": "esc", "automation": map[string]any{"window_id": "11"}}},
		{ID: "save", Tool: ComputerToolName, Arguments: map[string]any{"action": "hotkey", "key": "s", "modifiers": "ctrl", "automation": map[string]any{"window_id": "11"}}},
		{ID: "after", Tool: ComputerToolName, Arguments: map[string]any{"action": "windows"}},
	}})
	require.True(t, resp.IsError, "a failed step fails the pipeline")
	got := outcomesByID(results)
	require.Equal(t, OutcomeApplied, got["type"])
	require.Equal(t, OutcomeVerified, got["check"])
	require.Equal(t, OutcomePlanned, got["maybe"], "an unselected branch is not blocked work")
	require.Equal(t, OutcomeFailed, got["save"])
	require.Equal(t, OutcomeBlocked, got["after"])
}

// An input whose effect is unknown is reported as unknown and blocks later
// mutations; a read-only recovery branch may still observe the target.
func TestPipelineUnknownMutationAllowsOnlyObservation(t *testing.T) {
	t.Parallel()
	var sent []string
	invoke := func(_ context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		var p ComputerParams
		require.NoError(t, json.Unmarshal([]byte(call.Input), &p))
		sent = append(sent, p.Action)
		if p.Action == "key" {
			return withToolOutcome(fantasy.NewTextErrorResponse(`computer action "key" timed out after 30s; the result may be unknown. Inspect before repeating input.`), ToolOutcome{Dispatch: "unknown", Mutation: true}), nil
		}
		return fantasy.NewTextResponse(`{"window_id":"11","result":{"elements":[]}}`), nil
	}
	_, results := runPipelineSteps(t, invoke, PipelineParams{Steps: []PipelineStep{
		{ID: "enter", Tool: ComputerToolName, Arguments: map[string]any{"action": "key", "key": "enter", "automation": map[string]any{"window_id": "11"}}},
		{ID: "look", Tool: ComputerToolName, IfFailed: "enter", Arguments: map[string]any{"action": "observe", "observation": "semantic", "automation": map[string]any{"window_id": "11"}}},
		{ID: "again", Tool: ComputerToolName, Arguments: map[string]any{"action": "key", "key": "enter", "automation": map[string]any{"window_id": "11"}}},
	}})
	got := outcomesByID(results)
	require.Equal(t, OutcomeUnknown, got["enter"])
	require.Equal(t, OutcomeApplied, got["look"])
	require.Equal(t, OutcomeBlocked, got["again"])
	require.Equal(t, []string{"key", "observe"}, sent, "the unknown Enter is never replayed")
}

// A generic pipeline can open an application and address the window that
// the launch reported without another model turn; a launch that reported no
// single new window stops before any input.
func TestPipelineBindsLaunchedWindowIntoLaterInput(t *testing.T) {
	t.Parallel()
	var calls []ComputerParams
	newWindows := `["591766"]`
	invoke := func(_ context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		var p ComputerParams
		require.NoError(t, json.Unmarshal([]byte(call.Input), &p))
		calls = append(calls, p)
		if p.Action == "launch_app" {
			return fantasy.NewTextResponse(`{"input_settle":{"after":"launch_app","state":"changed","new_window_ids":` + newWindows + `},"result":{"launch_requested":true}}`), nil
		}
		return fantasy.NewTextResponse("ok"), nil
	}
	steps := []PipelineStep{
		{ID: "launch", Tool: ComputerToolName, Arguments: map[string]any{"action": "launch_app", "automation": map[string]any{"name": "Hesap Makinesi"}}},
		{ID: "focus", Tool: ComputerToolName, Bindings: map[string]string{"automation.window_id": "launch#/input_settle/new_window_ids/0"}, Arguments: map[string]any{"action": "focus"}},
		{ID: "type", Tool: ComputerToolName, Bindings: map[string]string{"automation.window_id": "launch#/input_settle/new_window_ids/0"}, Arguments: map[string]any{"action": "type", "text": "1234*56"}},
	}
	resp, results := runPipelineSteps(t, invoke, PipelineParams{Steps: steps})
	require.False(t, resp.IsError, resp.Content)
	require.Len(t, calls, 3)
	require.Equal(t, "591766", calls[1].Automation.WindowID)
	require.Equal(t, "591766", calls[2].Automation.WindowID)
	require.Equal(t, OutcomeApplied, outcomesByID(results)["type"])

	calls, newWindows = nil, `["591766","1050808"]`
	resp, results = runPipelineSteps(t, invoke, PipelineParams{Steps: steps})
	require.True(t, resp.IsError)
	require.Len(t, calls, 1, "an ambiguous window binding sends no input")
	got := outcomesByID(results)
	require.Equal(t, OutcomeFailed, got["focus"])
	require.Equal(t, OutcomeBlocked, got["type"])
}

// Output over the 64 KiB bound used to replace the whole result with an
// error after every step had already run, hiding what was applied. The
// ledger now stays complete and only large contents are shortened.
func TestPipelineOversizedOutputKeepsStepLedger(t *testing.T) {
	t.Parallel()
	invoke := func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		return withToolOutcome(fantasy.NewTextResponse(strings.Repeat("x", 6000)), ToolOutcome{Dispatch: "sent", Mutation: true}), nil
	}
	var steps []PipelineStep
	for i := range 30 {
		steps = append(steps, PipelineStep{ID: fmt.Sprintf("s%d", i), Tool: ComputerToolName, Arguments: map[string]any{"action": "key", "key": "tab", "automation": map[string]any{"window_id": "11"}}})
	}
	resp, results := runPipelineSteps(t, invoke, PipelineParams{Steps: steps})
	require.False(t, resp.IsError, "an applied run is not reported as failed because of size")
	require.LessOrEqual(t, len(resp.Content), 64*1024)
	require.Len(t, results, 30)
	for _, r := range results {
		require.Equal(t, OutcomeApplied, r.Outcome)
		require.Contains(t, r.Content, "bytes omitted")
	}
}
