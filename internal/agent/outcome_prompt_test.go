package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/outcome"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/stretchr/testify/require"
)

func TestRuntimeOutcomesRefreshWithoutAccumulating(t *testing.T) {
	t.Parallel()
	base := []fantasy.Message{fantasy.NewSystemMessage("base"), fantasy.NewUserMessage("request")}
	first := withRuntimeOutcomes(base, "unknown old")
	second := withRuntimeOutcomes(first, "verified current")
	require.Len(t, base, 2)
	require.Len(t, second, 3)
	require.Contains(t, second[2].Content[0].(fantasy.TextPart).Text, "verified current")
	require.NotContains(t, second[2].Content[0].(fantasy.TextPart).Text, "unknown old")
	require.Len(t, withRuntimeOutcomes(second, ""), 2)
}

func TestNestedPipelineCannotBypassTaskDependencyVerification(t *testing.T) {
	h := newPayloadCoordinator(t, wireOpenAI, "", automationTools)
	coord := h.coord.(*coordinator)
	require.NoError(t, coord.readyWg.Wait())
	a := coord.agents[config.AgentCoder].(*sessionAgent)
	sessionID := newSession(t, h)
	current, err := h.env.sessions.Get(t.Context(), sessionID)
	require.NoError(t, err)
	current.Todos = []session.Todo{
		{ID: "upstream", Content: "Verify upstream acceptance", Status: session.TodoStatusCompleted},
		{ID: "dependent", Content: "Write only after upstream verification", Status: session.TodoStatusPending, DependsOn: []string{"upstream"}},
	}
	_, err = h.env.sessions.Save(t.Context(), current)
	require.NoError(t, err)
	ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, sessionID)
	ctx = engineering.WithScope(ctx, sessionID, "dependent")
	path := filepath.Join(h.env.workingDir, "must-not-write.txt")
	input, err := json.Marshal(map[string]any{"steps": []map[string]any{{"id": "write", "tool": "write", "arguments": map[string]any{"file_path": path, "content": "must not be sent"}}}})
	require.NoError(t, err)
	var pipeline fantasy.AgentTool
	for _, tool := range a.tools.Copy() {
		if tool.Info().Name == "tool_pipeline" {
			pipeline = tool
		}
	}
	require.NotNil(t, pipeline)
	response, err := pipeline.Run(ctx, fantasy.ToolCall{ID: "pipeline", Name: "tool_pipeline", Input: string(input)})
	require.NoError(t, err)
	require.True(t, response.IsError)
	require.Contains(t, response.Content, "Verify its upstream requirements")
	require.NoFileExists(t, path)
}

type failedFinalMessageStore struct {
	message.Service
	Reason message.FinishReason
}

func (s failedFinalMessageStore) Update(ctx context.Context, msg message.Message) error {
	if msg.Role == message.Assistant && msg.FinishReason() == s.Reason {
		return errors.New("final message persistence failed")
	}
	return s.Service.Update(ctx, msg)
}

func TestFinalMessageSaveFailureCannotClaimDelivery(t *testing.T) {
	h := newPayloadCoordinator(t, wireOpenAI, "", "", openAIText("Completed."))
	coord := h.coord.(*coordinator)
	a := coord.agents[config.AgentCoder].(*sessionAgent)
	a.messages = failedFinalMessageStore{Service: a.messages, Reason: message.FinishReasonEndTurn}
	sessionID := newSession(t, h)
	_, err := h.coord.Run(t.Context(), sessionID, "merhaba")
	require.ErrorContains(t, err, "final message persistence failed")
	state, readErr := a.engineering.Read(t.Context(), sessionID)
	require.NoError(t, readErr)
	require.False(t, state.Outcomes.Supports(outcome.KindDelivery, "final_message", "answer_sent"))
	require.Len(t, h.recorder.stepRequests(), 1)
}

func TestFailedToolStepPersistenceStopsNextProviderRequest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixture.txt")
	require.NoError(t, os.WriteFile(path, []byte("fixture"), 0o600))
	h := newPayloadCoordinator(t, wireOpenAI, "", "",
		openAIToolCall("read", "view", map[string]any{"file_path": path}), openAIText("Completed."))
	coord := h.coord.(*coordinator)
	a := coord.agents[config.AgentCoder].(*sessionAgent)
	a.messages = failedFinalMessageStore{Service: a.messages, Reason: message.FinishReasonToolUse}
	_, err := h.coord.Run(t.Context(), newSession(t, h), "Read the fixture file")
	require.ErrorContains(t, err, "final message persistence failed")
	require.Len(t, h.recorder.stepRequests(), 1, "a failed save must stop before another model turn")
}

func TestCanonicalJSONCannotBypassUnknownResendGuard(t *testing.T) {
	t.Parallel()
	store := engineering.NewStore(t.TempDir())
	ctx := context.Background()
	original := fantasy.ToolCall{Name: "mcp_send", Input: `{"amount":1,"to":"target"}`}
	require.NoError(t, store.Update(ctx, "s", func(st *engineering.State) error {
		st.Outcomes.Record(outcome.Record{OpID: "op", Tool: original.Name, Phase: outcome.PhaseUnknown, Mutation: true, Fingerprint: outcomeFingerprint(original)})
		return nil
	}))
	reordered := fantasy.ToolCall{Name: "mcp_send", Input: `{ "to": "target", "amount": 1 }`}
	require.Contains(t, (&guardedTool{store: store}).resendRefusal(ctx, "s", reordered), "outcome_unknown")
}

func TestPaddedActionCannotBypassUnknownMutationGuard(t *testing.T) {
	t.Parallel()
	store := engineering.NewStore(t.TempDir())
	ctx := context.Background()
	call := fantasy.ToolCall{Name: "computer", Input: `{"action":"key","key":"enter"}`}
	require.NoError(t, store.Update(ctx, "s", func(st *engineering.State) error {
		st.Outcomes.Record(outcome.Record{OpID: "key", Phase: outcome.PhaseUnknown, Mutation: true, Fingerprint: outcomeFingerprint(call)})
		return nil
	}))
	call.Input = `{"key":"enter","action":" Key "}`
	require.Contains(t, (&guardedTool{store: store}).resendRefusal(ctx, "s", call), "outcome_unknown")
}

func TestLateNativeResponseCannotAddEvidenceAfterReconciliation(t *testing.T) {
	t.Parallel()
	store := engineering.NewStore(t.TempDir())
	ctx := context.Background()
	require.NoError(t, store.Update(ctx, "s", func(st *engineering.State) error {
		st.Outcomes.Record(outcome.Record{OpID: "old", Phase: outcome.PhaseUnknown, Mutation: true})
		require.True(t, st.Outcomes.Resolve("old", outcome.PhaseFailed))
		return nil
	}))
	response := fantasy.WithResponseMetadata(fantasy.NewTextResponse("old result"), map[string]any{
		"atlas_outcome": tools.ToolOutcome{Dispatch: "sent", Mutation: true, Verified: true, Kind: outcome.KindOutput, Subjects: []string{"target"}, Claim: "changed"},
	})
	guard := &guardedTool{store: store}
	require.NoError(t, guard.recordOutcome(ctx, "s", "old", fantasy.ToolCall{Name: "computer", Input: `{"action":"click"}`}, outcomeResult("computer", response, nil, true, false), response))
	st, err := store.Read(ctx, "s")
	require.NoError(t, err)
	require.Empty(t, st.Outcomes.Evidence)
	require.Equal(t, outcome.PhaseFailed, st.Outcomes.Records[0].Phase)
}

func TestFinalDeliveryPersistenceFailureIsReturned(t *testing.T) {
	t.Parallel()
	store := engineering.NewStore(t.TempDir())
	require.NoError(t, recordDelivery(store, context.Background(), "s", "msg"))
	st, err := store.Read(context.Background(), "s")
	require.NoError(t, err)
	require.True(t, st.Outcomes.Supports(outcome.KindDelivery, "final_message", "answer_sent"))
	bad := engineering.NewStore(t.TempDir())
	require.NoError(t, os.WriteFile(bad.Dir(), []byte("file obstructs state directory"), 0o600))
	require.Error(t, recordDelivery(bad, context.Background(), "s", "msg"))
}
