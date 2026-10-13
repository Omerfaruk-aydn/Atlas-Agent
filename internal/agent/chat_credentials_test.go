package agent

import (
	"encoding/json"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/vault"
	"github.com/stretchr/testify/require"
)

func TestSuppliedChatCredentialRemovedBeforePersistenceAndProvider(t *testing.T) {
	env := testEnv(t)
	model := &promptRecordingModel{finishStreamModel: finishStreamModel{text: "done"}}
	worker := testSessionAgent(env, model, &finishStreamModel{text: "title"}, "test guidance")
	sess, err := env.sessions.Create(t.Context(), "credential ingestion")
	require.NoError(t, err)
	t.Cleanup(func() { vault.Ephemeral.PurgeSession(sess.ID) })
	secret := "Synthetic-Ingestion-44!"
	_, err = worker.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "gmail i açıp user@example.test hesabını ekle bu da şifresi " + secret})
	require.NoError(t, err)
	require.NotContains(t, model.observed, secret)
	require.Contains(t, model.observed, "credential_request")
	msgs, err := env.messages.List(t.Context(), sess.ID)
	require.NoError(t, err)
	stored, err := json.Marshal(msgs)
	require.NoError(t, err)
	require.NotContains(t, string(stored), secret)
	require.True(t, vault.Ephemeral.ChatHasCredential(sess.ID))
}
