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

func TestPipelineRejectsUnboundedIdentityBeforeDispatch(t *testing.T) {
	var steps []PipelineStep
	for i := range 160 {
		steps = append(steps, PipelineStep{ID: fmt.Sprintf("%d-%s", i, strings.Repeat("x", 512)), Tool: ViewToolName, Arguments: map[string]any{}})
	}
	require.NotEmpty(t, validatePipelinePlan(t.Context(), steps, nil), "oversized identities must be refused before any step is sent")
}

func TestPipelineStopTurnPreservesAppliedLedger(t *testing.T) {
	steps := []PipelineStep{
		{ID: "applied_mutation", Tool: WriteToolName, Arguments: map[string]any{}},
		{ID: "wait_for_user", Tool: QuestionToolName, Arguments: map[string]any{}},
		{ID: "not_run", Tool: ViewToolName, Arguments: map[string]any{}},
	}
	calls := 0
	run, err := executePipeline(t.Context(), steps, nil, fantasy.ToolCall{ID: "probe"}, func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		calls++
		if calls == 1 {
			return withToolOutcome(fantasy.NewTextResponse("Wrote the file."), ToolOutcome{Dispatch: "sent", Mutation: true}), nil
		}
		r := fantasy.NewTextResponse("Waiting for user.")
		r.StopTurn = true
		return r, nil
	})
	require.NoError(t, err)
	require.Len(t, run.results, 3)
	require.Equal(t, OutcomeApplied, run.results[0].Outcome)
	require.Equal(t, "sent", run.results[0].Dispatch)
	resp, err := pipelineResponse(run, nil)
	require.NoError(t, err)
	require.True(t, resp.StopTurn)
	require.Contains(t, resp.Content, "Waiting for user.")
	require.Contains(t, resp.Content, "applied_mutation")
	t.Logf("Returned %d step results with first outcome=%s", len(run.results), run.results[0].Outcome)
}

func TestPipelineBindingContinueRunsIndependentWork(t *testing.T) {
	steps := []PipelineStep{
		{ID: "source", Tool: ViewToolName, Arguments: map[string]any{}},
		{ID: "optional", Tool: ComputerToolName, OnError: "continue", Arguments: map[string]any{"action": "type"}, Bindings: map[string]string{"text": "source#/value"}},
		{ID: "independent", Tool: ViewToolName, Arguments: map[string]any{}},
	}
	var called []string
	run, err := executePipeline(t.Context(), steps, nil, fantasy.ToolCall{ID: "probe"}, func(_ context.Context, c fantasy.ToolCall) (fantasy.ToolResponse, error) {
		called = append(called, c.ID)
		return fantasy.NewTextResponse("no JSON"), nil
	})
	require.NoError(t, err)
	require.Len(t, called, 2)
	require.Equal(t, StepSucceeded, run.results[2].Status)
	resp, err := pipelineResponse(run, nil)
	require.NoError(t, err)
	require.False(t, resp.IsError)
	var results []PipelineResult
	require.NoError(t, json.Unmarshal([]byte(resp.Content), &results))
	t.Logf("only calls=%v; independent step status=%s; outer IsError=%v", called, results[2].Status, resp.IsError)
}

func TestPipelineCompactionBoundsAllAuxiliaryFields(t *testing.T) {
	var rows []PipelineResult
	for i := range 192 {
		rows = append(rows, PipelineResult{ID: fmt.Sprintf("step-%d", i), Status: StepUncertain, Outcome: OutcomeUnknown, Dispatch: "unknown", Reason: strings.Repeat("r", 2048), Resume: strings.Repeat("s", 2048), DesktopState: &DesktopEvidence{WindowID: strings.Repeat("w", 2048)}, Content: strings.Repeat("c", 2048)})
	}
	data, err := boundedPipelineJSON(rows)
	require.NoError(t, err)
	require.LessOrEqual(t, len(data), maxPipelineOutput)
	var got []PipelineResult
	require.NoError(t, json.Unmarshal(data, &got))
	require.Len(t, got, 192)
	require.Equal(t, "step-191", got[191].ID)
	require.Equal(t, OutcomeUnknown, got[191].Outcome)
	require.Equal(t, "unknown", got[191].Dispatch)
}

func TestPipelineChildErrorKeepsPriorMutationAndUnknownDispatch(t *testing.T) {
	steps := []PipelineStep{{ID: "saved", Tool: WriteToolName}, {ID: "lost", Tool: WriteToolName}, {ID: "later", Tool: ViewToolName}}
	calls := 0
	tool := NewToolPipeline(func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		calls++
		if calls == 2 {
			return fantasy.ToolResponse{}, fmt.Errorf("transport lost after dispatch")
		}
		return fantasy.NewTextResponse("saved"), nil
	})
	data, _ := json.Marshal(PipelineParams{Steps: steps})
	resp, err := tool.Run(t.Context(), fantasy.ToolCall{ID: "p", Input: string(data)})
	require.NoError(t, err)
	require.True(t, resp.IsError)
	require.Equal(t, 2, calls)
	var got []PipelineResult
	require.NoError(t, json.Unmarshal([]byte(resp.Content), &got))
	require.Len(t, got, 3)
	require.Equal(t, OutcomeApplied, got[0].Outcome)
	require.Equal(t, OutcomeUnknown, got[1].Outcome)
	require.Equal(t, "unknown", got[1].Dispatch)
	require.Equal(t, OutcomeBlocked, got[2].Outcome)
}

func TestPipelineAssertionCannotOverrideHookStop(t *testing.T) {
	steps := []PipelineStep{{ID: "gate", Tool: ComputerToolName, Arguments: map[string]any{"action": "assert"}, RequirePassed: true, OnError: "continue"}, {ID: "write", Tool: WriteToolName}}
	calls := 0
	run, err := executePipeline(t.Context(), steps, nil, fantasy.ToolCall{ID: "p"}, func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		calls++
		r := fantasy.NewTextResponse("{\"passed\":true}\nPostToolUse halt: wait for user")
		r.StopTurn = true
		r.Metadata = `{"question":{"id":"q"}}`
		return r, nil
	})
	require.NoError(t, err)
	resp, err := pipelineResponse(run, nil)
	require.NoError(t, err)
	require.True(t, resp.StopTurn)
	require.Equal(t, 1, calls)
	require.Equal(t, `{"question":{"id":"q"}}`, resp.Metadata)
}

func TestPipelineStopTurnBoundsCompleteResponse(t *testing.T) {
	r := fantasy.NewTextResponse(strings.Repeat("hook halt ", 10000))
	r.StopTurn = true
	r.Metadata = `{"halt":"original"}`
	resp, err := pipelineResponse(pipelineRun{stopTurn: &r, results: []PipelineResult{{ID: "done", Status: StepSucceeded, Outcome: OutcomeApplied}, {ID: "halt", Status: StepStopped, Outcome: OutcomeBlocked}}}, nil)
	require.NoError(t, err)
	require.True(t, resp.StopTurn)
	require.Equal(t, r.Metadata, resp.Metadata)
	require.LessOrEqual(t, len(resp.Content), maxPipelineOutput)
	require.Contains(t, resp.Content, `"id":"done"`)
}

func TestPipelineTransportErrorPreservesNativeNoDispatch(t *testing.T) {
	run, err := executePipeline(t.Context(), []PipelineStep{{ID: "write", Tool: WriteToolName}}, nil, fantasy.ToolCall{}, func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		return withToolOutcome(fantasy.NewTextErrorResponse("canceled before dispatch"), ToolOutcome{Dispatch: "none", Mutation: true}), fmt.Errorf("driver refused")
	})
	require.Error(t, err)
	require.Len(t, run.results, 1)
	require.Equal(t, "none", run.results[0].Dispatch)
	require.Equal(t, OutcomeFailed, run.results[0].Outcome)
}
