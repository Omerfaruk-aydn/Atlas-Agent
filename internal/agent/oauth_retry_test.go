package agent

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

type expiredCredentialModel struct {
	finishStreamModel
	calls atomic.Int32
}

func (m *expiredCredentialModel) Stream(context.Context, fantasy.Call) (fantasy.StreamResponse, error) {
	m.calls.Add(1)
	return nil, &fantasy.ProviderError{StatusCode: http.StatusUnauthorized, Message: "Synthetic expired access token"}
}

func TestAgentRetriesWithRefreshedOAuthModel(t *testing.T) {
	t.Parallel()
	env := testEnv(t)
	expired := &expiredCredentialModel{}
	fresh := &finishStreamModel{text: "renewed successfully"}
	worker := testSessionAgent(env, expired, &finishStreamModel{text: "title"}, "fixture system").(*sessionAgent)
	sess, err := env.sessions.Create(t.Context(), "OAuth renewal fixture")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	refreshes := 0
	_, err = worker.Run(ctx, SessionAgentCall{
		SessionID: sess.ID, Prompt: "Reply after automatic credential renewal",
		OnAuthRefresh: func(context.Context, *fantasy.ProviderError) error {
			refreshes++
			updated := worker.largeModel.Get()
			updated.Model = fresh
			worker.SetModels(updated, worker.smallModel.Get(), nil, nil)
			return nil
		},
	})
	require.NoError(t, err)
	require.Equal(t, 1, refreshes)
	require.EqualValues(t, 1, expired.calls.Load(), "A successful refresh must stop retrying the expired client")
}
