package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/browser"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

// scriptedAdvancedSession answers semantic actions from a script, so the
// pipeline's recovery rules can be checked without a browser.
type scriptedAdvancedSession struct {
	*observingSession
	replies  []error
	requests []browser.Request
}

func (s *scriptedAdvancedSession) Advanced(_ context.Context, p browser.Request) (json.RawMessage, []byte, error) {
	s.requests = append(s.requests, p)
	if len(s.replies) > 0 {
		err := s.replies[0]
		s.replies = s.replies[1:]
		if err != nil {
			return nil, nil, err
		}
	}
	if p.Action == "find" {
		return json.RawMessage(`{"count":0,"matches":[]}`), nil, nil
	}
	return json.RawMessage(`{"action_sent":true,"verify_required":true}`), nil, nil
}

func runPipeline(t *testing.T, h *browserHarness, input string) []PipelineResult {
	t.Helper()
	pipeline := newToolPipeline(func(ctx context.Context, c fantasy.ToolCall) (fantasy.ToolResponse, error) {
		return h.tool.Run(h.ctx, c)
	})
	resp, err := pipeline.Run(h.ctx, fantasy.ToolCall{ID: "p", Name: "tool_pipeline", Input: input})
	require.NoError(t, err)
	var results []PipelineResult
	require.NoError(t, json.Unmarshal([]byte(resp.Content), &results), resp.Content)
	return results
}

// TestPipelineRetryOnlyAfterFreshObservation: a semantic click refused
// before input is retried once after an observe; a click that may have
// been sent is never retried.
func TestPipelineRetryOnlyAfterFreshObservation(t *testing.T) {
	t.Parallel()
	sess := &scriptedAdvancedSession{observingSession: newObservingSession(), replies: []error{errors.New("target_not_found: no visible element matches after 2001ms; nothing was sent")}}
	h := newBrowserHarness(t, sess)
	results := runPipeline(t, h, `{"steps":[{"id":"open","tool":"browser","arguments":{"action":"semantic_click","advanced":{"role":"button","name":"Share"}},"retry_on":["target_not_found"]}]}`)
	require.Len(t, results, 1)
	require.Equal(t, StepSucceeded, results[0].Status, results[0].Content)
	require.Equal(t, 2, results[0].Attempts)
	require.Len(t, sess.requests, 2)
	require.GreaterOrEqual(t, results[0].DurationMS, int64(0))

	// Sent input is not replayed even when its code is listed.
	sent := &scriptedAdvancedSession{observingSession: newObservingSession(), replies: []error{errors.New("condition_timeout: action_sent\":true but no change")}}
	hs := newBrowserHarness(t, sent)
	pipeline := newToolPipeline(func(ctx context.Context, c fantasy.ToolCall) (fantasy.ToolResponse, error) {
		return hs.tool.Run(hs.ctx, c)
	})
	resp, err := pipeline.Run(hs.ctx, fantasy.ToolCall{ID: "p", Name: "tool_pipeline", Input: `{"steps":[{"id":"open","tool":"browser","arguments":{"action":"semantic_click","advanced":{"role":"button","name":"Share"}},"retry_on":["condition_timeout"]}]}`})
	require.NoError(t, err)
	require.True(t, resp.IsError)
	require.Len(t, sent.requests, 1)
}

func TestPipelineRetryRequiresSemanticTarget(t *testing.T) {
	t.Parallel()
	h := newBrowserHarness(t, newObservingSession())
	pipeline := newToolPipeline(func(ctx context.Context, c fantasy.ToolCall) (fantasy.ToolResponse, error) {
		return h.tool.Run(h.ctx, c)
	})
	for _, input := range []string{
		`{"steps":[{"id":"c","tool":"browser","arguments":{"action":"click","ref":"e1"},"retry_on":["stale_ref"]}]}`,
		`{"steps":[{"id":"c","tool":"browser","arguments":{"action":"semantic_click","advanced":{"name":"Share"}},"retry_on":["value_not_applied"]}]}`,
	} {
		resp, err := pipeline.Run(h.ctx, fantasy.ToolCall{ID: "p", Name: "tool_pipeline", Input: input})
		require.NoError(t, err)
		require.True(t, resp.IsError, input)
		require.Contains(t, resp.Content, "retry_on")
	}
}

// TestPipelineWhenUsesEarlierResult: a step runs only when an earlier
// result matches; the skip names why.
func TestPipelineWhenUsesEarlierResult(t *testing.T) {
	t.Parallel()
	sess := &scriptedAdvancedSession{observingSession: newObservingSession()}
	h := newBrowserHarness(t, sess)
	results := runPipeline(t, h, `{"steps":[
		{"id":"found","tool":"browser","arguments":{"action":"find","advanced":{"role":"dialog","name":"Upgrade"}}},
		{"id":"close","tool":"browser","arguments":{"action":"semantic_click","advanced":{"role":"button","name":"Close"}},"when":{"source":"found#/count","equals":"1"}},
		{"id":"go","tool":"browser","arguments":{"action":"semantic_click","advanced":{"role":"button","name":"Share"}},"when":{"source":"found#/count","equals":"0"}}]}`)
	require.Len(t, results, 3)
	require.Equal(t, StepSkipped, results[1].Status)
	require.Contains(t, results[1].Reason, `is "0", not "1"`)
	require.Equal(t, StepSucceeded, results[2].Status)
	require.Len(t, sess.requests, 2, "find and the matching branch only")
}

// TestPipelineBindsReadValue: a value read by one step is applied by a
// later one, and the plan check accepts the bound value.
func TestPipelineBindsReadValue(t *testing.T) {
	t.Parallel()
	sess := newObservingSession()
	sess.readValue = "390"
	h := newBrowserHarness(t, sess)
	h.run(`{"action":"observe","advanced":{"mode":"full"}}`)
	results := runPipeline(t, h, `{"steps":[
		{"id":"read","tool":"browser","arguments":{"action":"read_field","ref":"e1"}},
		{"id":"copy","tool":"browser","arguments":{"action":"set_field","ref":"e2","advanced":{"commit":"enter"}},"bindings":{"advanced.value":"read#/accepted_value"}}]}`)
	require.Equal(t, StepSucceeded, results[1].Status, results[1].Content)
	require.Len(t, sess.fields, 1)
	require.Equal(t, "390", sess.fields[0].Value)
}

func TestPipelineRetryHonorsMetadataDispatchAndStopTurn(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name        string
		dispatch    string
		stopInitial bool
		stopObserve bool
		wantCalls   int
	}{
		{name: "metadata_sent", dispatch: "sent", wantCalls: 1},
		{name: "metadata_partial", dispatch: "partial", wantCalls: 1},
		{name: "metadata_unknown", dispatch: "unknown", wantCalls: 1},
		{name: "hook_stops_initial", dispatch: "none", stopInitial: true, wantCalls: 1},
		{name: "hook_stops_observation", dispatch: "none", stopObserve: true, wantCalls: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			steps := []PipelineStep{{
				ID: "send", Tool: BrowserToolName,
				Arguments: map[string]any{"action": "semantic_click", "advanced": map[string]any{"role": "button", "name": "Save"}},
				RetryOn:   []string{"condition_timeout"},
			}}
			calls := 0
			run, err := executePipeline(t.Context(), steps, nil, fantasy.ToolCall{ID: "p"}, func(_ context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
				calls++
				if call.ID == "p/send/0/observe" {
					resp := fantasy.NewTextResponse("Waiting for user")
					resp.StopTurn = test.stopObserve
					return resp, nil
				}
				resp := fantasy.NewTextErrorResponse("condition_timeout: check did not pass")
				resp.Metadata = `{"dispatch": "` + test.dispatch + `"}`
				resp.StopTurn = test.stopInitial
				return resp, nil
			})
			require.NoError(t, err)
			require.Equal(t, test.wantCalls, calls)
			if test.stopInitial || test.stopObserve {
				require.NotNil(t, run.stopTurn)
				require.True(t, run.stopTurn.StopTurn)
			}
		})
	}
}

func TestPipelineDispatchDoesNotHidePartialEffectsBehindLaterRefusal(t *testing.T) {
	t.Parallel()
	require.Equal(t, "sent", pipelineDispatch(`{"fields":[{"dispatch":"sent"},{"dispatch":"none"}]}`))
	require.Equal(t, "unknown", pipelineDispatch(`{"dispatch":"none"}`, `{"dispatch": "unknown"}`))
	require.Equal(t, "sent", pipelineDispatch(`{"action_sent": true}`, `{"dispatch":"none"}`))
}
