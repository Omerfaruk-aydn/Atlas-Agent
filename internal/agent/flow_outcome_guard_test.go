package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/outcome"
	"github.com/stretchr/testify/require"
)

func TestFlowUnknownOutcomeBlocksSharedGuardResend(t *testing.T) {
	t.Parallel()
	store := engineering.NewStore(t.TempDir())
	ctx := outcomeCtx()
	inputs := 0
	inner := tools.NewToolPipeline(func(_ context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		var p tools.ComputerParams
		require.NoError(t, json.Unmarshal([]byte(call.Input), &p))
		switch p.Action {
		case "windows":
			return fantasy.NewTextResponse(`{"result":[{"window_id":"11","name":"Untitled - Notepad","process_id":17,"process_name":"Notepad.exe","class_name":"Notepad","foreground":true}]}`), nil
		case "key":
			inputs++
			r := fantasy.NewTextErrorResponse("Native response lost after input")
			r.Metadata = `{"atlas_outcome":{"dispatch":"unknown","mutation":true,"subjects":["desktop:"]}}`
			return r, nil
		default:
			t.Fatalf("Unexpected action: %s", p.Action)
			return fantasy.ToolResponse{}, nil
		}
	}, store)
	guard := &guardedTool{AgentTool: inner, store: store}
	call := fantasy.ToolCall{ID: "flow", Name: "tool_pipeline", Input: `{"desktop":{"mode":"flow","flow":{"nodes":[{"id":"app","kind":"resolve","application":"Notepad","next":"press"},{"id":"press","kind":"operation","window_ref":"app","input":{"action":"key","key":"enter"},"checkpoint":{"name":"Field","condition":"value","expected":"changed"}}]}}}`}
	r, err := guard.Run(ctx, call)
	require.NoError(t, err)
	require.True(t, r.IsError)
	state, err := store.Read(ctx, "s")
	require.NoError(t, err)
	require.Len(t, state.Outcomes.Records, 1)
	require.Equal(t, outcome.PhaseUnknown, state.Outcomes.Records[0].Phase)
	require.Equal(t, []string{"desktop:11"}, state.Outcomes.Records[0].Subjects)
	require.Equal(t, 1, inputs)
	call.ID = "retry"
	r, err = guard.Run(ctx, call)
	require.NoError(t, err)
	require.True(t, r.IsError)
	require.Contains(t, r.Content, "outcome_unknown")
	require.Equal(t, 1, inputs, "an identical pipeline must not reach the desktop twice")
}

func TestWaitingNativeUnknownRemainsUnresolved(t *testing.T) {
	t.Parallel()
	r := fantasy.NewTextResponse("Waiting for the user's answer")
	r.StopTurn = true
	r.Metadata = `{"question":{"id":"q"},"atlas_outcome":{"dispatch":"unknown","mutation":true}}`
	require.Equal(t, outcome.PhaseUnknown, outcome.Classify(outcomeResult("tool_pipeline", r, nil, true, false)))
	r.Metadata = `{"question":{"id":"q"},"atlas_outcome":{"dispatch":"none","mutation":true}}`
	require.Equal(t, outcome.PhaseBlocked, outcome.Classify(outcomeResult("tool_pipeline", r, nil, true, false)))
	r.Metadata = `{"question":{"id":"q"},"atlas_outcome":{"dispatch":"sent","mutation":true}}`
	require.Equal(t, outcome.PhaseBlocked, outcome.Classify(outcomeResult("tool_pipeline", r, nil, true, false)), "known partial progress is not automatically uncertain")
}

func TestPipelineUnknownOutcomeBlocksSharedGuardResend(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, tool, arguments string }{
		{"files", "write", `{"file_path":"result.txt","content":"result"}`},
		{"browser", "browser", `{"action":"click","selector":"#submit"}`},
		{"cli_bridge", "cli_bridge", `{"action":"run","profile":"fixture","operation":"write"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := engineering.NewStore(t.TempDir())
			inputs := 0
			inner := tools.NewToolPipeline(func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
				inputs++
				r := fantasy.NewTextErrorResponse("Response lost after dispatch")
				r.Metadata = `{"atlas_outcome":{"dispatch":"unknown","mutation":true,"subjects":["target"]}}`
				return r, nil
			}, store)
			guard := &guardedTool{AgentTool: inner, store: store}
			call := fantasy.ToolCall{ID: "pipeline", Name: "tool_pipeline", Input: `{"steps":[{"id":"send","tool":"` + tc.tool + `","arguments":` + tc.arguments + `}]}`}
			r, err := guard.Run(outcomeCtx(), call)
			require.NoError(t, err)
			require.True(t, r.IsError)
			require.Equal(t, 1, inputs)
			state, err := store.Read(outcomeCtx(), "s")
			require.NoError(t, err)
			require.Len(t, state.Outcomes.Records, 1)
			require.Equal(t, outcome.PhaseUnknown, state.Outcomes.Records[0].Phase)
			require.Equal(t, []string{"target"}, state.Outcomes.Records[0].Subjects)
			call.ID = "retry"
			r, err = guard.Run(outcomeCtx(), call)
			require.NoError(t, err)
			require.True(t, r.IsError)
			require.Contains(t, r.Content, "outcome_unknown")
			require.Equal(t, 1, inputs, "an uncertain nested effect must block the entire envelope")
		})
	}
}

func TestPipelinePartialPauseInvalidatesEarlierEvidence(t *testing.T) {
	t.Parallel()
	for _, sendFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "before_input", true: "after_partial_write"}[sendFirst], func(t *testing.T) {
			t.Parallel()
			store := engineering.NewStore(t.TempDir())
			ctx := outcomeCtx()
			require.NoError(t, store.Update(ctx, "s", func(state *engineering.State) error {
				state.Outcomes.Add(outcome.Evidence{ID: "old", Kind: outcome.KindOutput, Source: "before", Subject: "target", Claim: "old content verified"})
				return nil
			}))
			calls := 0
			inner := tools.NewToolPipeline(func(_ context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
				calls++
				var input struct {
					FilePath string `json:"file_path"`
				}
				require.NoError(t, json.Unmarshal([]byte(call.Input), &input))
				if sendFirst && input.FilePath == "a" {
					response := fantasy.NewTextResponse("Written")
					response.Metadata = `{"atlas_outcome":{"dispatch":"sent","mutation":true,"subjects":["target"]}}`
					return response, nil
				}
				response := fantasy.NewTextResponse("Permission requested")
				response.StopTurn = true
				response.Metadata = `{"permission":{"id":"p"},"atlas_outcome":{"dispatch":"none","mutation":true}}`
				return response, nil
			}, store)
			guard := &guardedTool{AgentTool: inner, store: store}
			input := `{"steps":[{"id":"first","tool":"write","arguments":{"file_path":"a","content":"value"}},{"id":"second","tool":"write","arguments":{"file_path":"b","content":"value"}}]}`
			response, err := guard.Run(ctx, fantasy.ToolCall{ID: "pause", Name: "tool_pipeline", Input: input})
			require.NoError(t, err)
			require.True(t, response.StopTurn)
			state, err := store.Read(ctx, "s")
			require.NoError(t, err)
			require.Equal(t, outcome.PhaseBlocked, state.Outcomes.Records[0].Phase)
			require.Len(t, state.Outcomes.Evidence, 1)
			require.Equal(t, sendFirst, state.Outcomes.Evidence[0].StaleBy != "", "an earlier write must invalidate old proof even while the next step waits")
			if sendFirst {
				retry, err := guard.Run(ctx, fantasy.ToolCall{ID: "retry", Name: "tool_pipeline", Input: input})
				require.NoError(t, err)
				require.True(t, retry.IsError)
				require.Contains(t, retry.Content, "partial_progress")
				require.Equal(t, 2, calls, "restarting the entire paused pipeline would repeat the first write")
			}
		})
	}
}
