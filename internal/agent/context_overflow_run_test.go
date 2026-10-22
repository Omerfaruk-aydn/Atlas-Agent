package agent

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/catwalk"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	"github.com/stretchr/testify/require"
)

// windowedModel records the size of every request that reaches the provider.
type windowedModel struct {
	finishStreamModel
	mu    sync.Mutex
	sizes []int64
}

func (m *windowedModel) Stream(ctx context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	m.mu.Lock()
	m.sizes = append(m.sizes, estimateMessageTokens(call.Prompt))
	m.mu.Unlock()
	return m.finishStreamModel.Stream(ctx, call)
}

func (m *windowedModel) largest() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out int64
	for _, size := range m.sizes {
		out = max(out, size)
	}
	return out
}

func seedHistory(t *testing.T, env fakeEnv, sessionID string) {
	t.Helper()
	for _, role := range []message.MessageRole{message.User, message.Assistant, message.User, message.Assistant} {
		_, err := env.messages.Create(t.Context(), sessionID, message.CreateMessageParams{
			Role:  role,
			Parts: []message.ContentPart{message.TextContent{Text: strings.Repeat("history ", 3000)}},
		})
		require.NoError(t, err)
	}
}

func shrinkActiveModel(worker SessionAgent, window, output int64) {
	impl := worker.(*sessionAgent)
	small := impl.largeModel.Get()
	small.CatwalkCfg = catwalk.Model{ContextWindow: window, DefaultMaxTokens: output}
	impl.largeModel.Set(small)
}

// A conversation too large for the active model is never sent to it. A summary
// model with room compacts it, the turn resumes, and every request the small
// model receives fits its window.
func TestRunCompactsAndResumesWhenRequestDoesNotFit(t *testing.T) {
	t.Parallel()

	env := testEnv(t)
	active := &windowedModel{finishStreamModel: finishStreamModel{text: "answer"}}
	summarizer := &windowedModel{finishStreamModel: finishStreamModel{text: "short summary"}}
	worker := testSessionAgent(env, active, &finishStreamModel{text: "title"}, "system")
	shrinkActiveModel(worker, 6000, 1000)
	worker.(*sessionAgent).compactModel.Set(ptrBox[Model]{v: &Model{
		Model:      summarizer,
		CatwalkCfg: catwalk.Model{ContextWindow: 200_000, DefaultMaxTokens: 10_000},
	}})

	sess, err := env.sessions.Create(t.Context(), "overflow")
	require.NoError(t, err)
	seedHistory(t, env, sess.ID)

	_, err = worker.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "continue the work"})
	require.NoError(t, err)

	require.Greater(t, summarizer.largest(), int64(6000), "the summary model read the whole history")
	require.NotEmpty(t, active.sizes, "the turn resumed after compaction")
	require.LessOrEqual(t, active.largest(), int64(6000-1000), "nothing larger than the active model's window was sent to it")
}

// When even the summary model cannot take the history, the turn ends with a
// stated reason and nothing oversized is sent anywhere.
func TestRunStopsWithAReasonWhenNoModelCanTakeTheRequest(t *testing.T) {
	t.Parallel()

	env := testEnv(t)
	active := &windowedModel{finishStreamModel: finishStreamModel{text: "answer"}}
	worker := testSessionAgent(env, active, &finishStreamModel{text: "title"}, "system")
	shrinkActiveModel(worker, 6000, 1000)

	sess, err := env.sessions.Create(t.Context(), "overflow-hard")
	require.NoError(t, err)
	seedHistory(t, env, sess.ID)

	_, err = worker.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "continue the work"})
	require.Error(t, err)
	require.Empty(t, active.sizes, "no request was sent to a model that cannot take it")
}
