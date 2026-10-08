package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/outcome"
	"github.com/stretchr/testify/require"
)

type mcpParams struct {
	Message string `json:"message"`
}

func outcomeCtx() context.Context {
	return context.WithValue(context.Background(), tools.SessionIDContextKey, "s")
}

func TestMCPErrorsAreNeverRecordedAsSuccess(t *testing.T) {
	t.Parallel()
	store := engineering.NewStore(t.TempDir())
	ctx := outcomeCtx()
	inner := fantasy.NewAgentTool("mcp_srv_send", "test", func(context.Context, mcpParams, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		return fantasy.NewTextErrorResponse("mcp_tool_error: quota"), nil
	})
	_, err := (&guardedTool{AgentTool: inner, store: store}).Run(ctx, fantasy.ToolCall{ID: "1", Name: "mcp_srv_send", Input: `{"message":"hi"}`})
	require.NoError(t, err)
	state, err := store.Read(ctx, "s")
	require.NoError(t, err)
	require.Equal(t, outcome.PhaseFailed, state.Outcomes.Records[0].Phase)
	require.Empty(t, state.Outcomes.Unresolved(), "a refusal changed nothing and may be retried differently")
}

func TestUnknownMCPOutcomeBlocksBlindResendUntilObserved(t *testing.T) {
	t.Parallel()
	store := engineering.NewStore(t.TempDir())
	ctx := outcomeCtx()
	sends := 0
	send := fantasy.NewAgentTool("mcp_srv_send", "test", func(context.Context, mcpParams, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		sends++
		return fantasy.WithResponseMetadata(fantasy.NewTextErrorResponse("mcp_outcome_unknown: reset"), map[string]any{"dispatch": "unknown"}), nil
	})
	read := fantasy.NewAgentTool("mcp_srv_list", "test", func(context.Context, mcpParams, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		return fantasy.NewTextResponse("nothing sent yet"), nil
	})
	sendGuard, readGuard := &guardedTool{AgentTool: send, store: store}, &guardedTool{AgentTool: read, store: store}
	call := fantasy.ToolCall{ID: "1", Name: "mcp_srv_send", Input: `{"message":"hi"}`}

	_, err := sendGuard.Run(ctx, call)
	require.NoError(t, err)
	state, _ := store.Read(ctx, "s")
	require.Equal(t, outcome.PhaseUnknown, state.Outcomes.Records[0].Phase)
	require.Contains(t, outcomePromptBlock(ctx, store, "s"), "never resend")

	resp, err := sendGuard.Run(ctx, call)
	require.NoError(t, err)
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "outcome_unknown")
	require.Equal(t, 1, sends, "the blind resend must not reach the server")

	_, err = readGuard.Run(ctx, fantasy.ToolCall{ID: "2", Name: "mcp_srv_list", Input: `{}`})
	require.NoError(t, err)
	_, err = sendGuard.Run(ctx, call)
	require.NoError(t, err)
	require.Equal(t, 1, sends, "an unrelated read must never authorize a resend")
	require.NoError(t, store.Resolve(ctx, "s", state.Outcomes.Records[0].OpID, "failed", "Inspected exact target: no message delivered"))
	_, err = sendGuard.Run(ctx, call)
	require.NoError(t, err)
	require.Equal(t, 2, sends, "explicit inspection reconciliation permits retry")
	require.Equal(t, 1, strings.Count(outcomePromptBlock(ctx, store, "s"), "mcp_srv_send#"), "the observed unknown is retired; only the new one remains")
}

func TestCancellationDuringMutationIsUnknownNotFailed(t *testing.T) {
	t.Parallel()
	store := engineering.NewStore(t.TempDir())
	ctx, cancel := context.WithCancel(outcomeCtx())
	inner := fantasy.NewAgentTool("mcp_srv_send", "test", func(context.Context, mcpParams, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		cancel()
		return fantasy.ToolResponse{}, context.Canceled
	})
	_, _ = (&guardedTool{AgentTool: inner, store: store}).Run(ctx, fantasy.ToolCall{ID: "1", Name: "mcp_srv_send", Input: `{}`})
	state, err := store.Read(context.Background(), "s")
	require.NoError(t, err)
	require.Equal(t, outcome.PhaseUnknown, state.Outcomes.Records[0].Phase)
}

func TestFileMutationVoidsEarlierCheckEvidence(t *testing.T) {
	t.Parallel()
	store := engineering.NewStore(t.TempDir())
	ctx := outcomeCtx()
	check := fantasy.NewAgentTool("test_run", "test", func(context.Context, struct{}, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		return fantasy.WithResponseMetadata(fantasy.NewTextResponse("ok"), tools.TestRunResponseMetadata{OK: true}), nil
	})
	edit := fantasy.NewAgentTool("edit", "test", func(context.Context, struct {
		FilePath string `json:"file_path"`
	}, fantasy.ToolCall,
	) (fantasy.ToolResponse, error) {
		return fantasy.NewTextResponse("edited"), nil
	})
	_, err := (&guardedTool{AgentTool: check, store: store}).Run(ctx, fantasy.ToolCall{ID: "1", Name: "test_run", Input: `{}`})
	require.NoError(t, err)
	state, _ := store.Read(ctx, "s")
	require.True(t, state.Outcomes.Supports(outcome.KindOutput, workspaceSubject, "check_passed:test_run"))

	_, err = (&guardedTool{AgentTool: edit, store: store}).Run(ctx, fantasy.ToolCall{ID: "2", Name: "edit", Input: `{"file_path":"a.go"}`})
	require.NoError(t, err)
	state, _ = store.Read(ctx, "s")
	require.False(t, state.Outcomes.Supports(outcome.KindOutput, workspaceSubject, "check_passed:test_run"), "a later edit makes the earlier passing run stale")
}

func TestOutcomeResultSeparatesReturnFromVerification(t *testing.T) {
	t.Parallel()
	ok := fantasy.NewTextResponse("done")
	require.Equal(t, outcome.PhaseApplied, outcome.Classify(outcomeResult("mcp_a_b", ok, nil, false, false)), "an MCP success is applied, not verified")
	require.Equal(t, outcome.PhaseUnknown, outcome.Classify(outcomeResult("mcp_a_b", fantasy.ToolResponse{}, errors.New("eof"), false, false)))
	require.Equal(t, outcome.PhaseFailed, outcome.Classify(outcomeResult("view", fantasy.ToolResponse{}, errors.New("eof"), false, false)))
	stop := fantasy.NewTextResponse("question")
	stop.StopTurn = true
	require.Equal(t, outcome.PhaseBlocked, outcome.Classify(outcomeResult("question", stop, nil, false, false)))
}
