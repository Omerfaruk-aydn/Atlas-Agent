package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/prompt"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/db"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/stretchr/testify/require"
)

type promptRecordingModel struct {
	finishStreamModel
	observed string
}

func (m *promptRecordingModel) Stream(ctx context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	data, err := json.Marshal(call.Prompt)
	if err != nil {
		return nil, err
	}
	m.observed = string(data)
	return m.finishStreamModel.Stream(ctx, call)
}

func TestAgentUsesRequestPromptInsteadOfMutableDefault(t *testing.T) {
	t.Parallel()
	env := testEnv(t)
	model := &promptRecordingModel{finishStreamModel: finishStreamModel{text: "done"}}
	worker := testSessionAgent(env, model, &finishStreamModel{text: "title"}, "different session guidance")
	sess, err := env.sessions.Create(t.Context(), "prompt isolation")
	require.NoError(t, err)
	_, err = worker.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "Inspect fixture", SystemPrompt: "frozen request guidance"})
	require.NoError(t, err)
	require.Contains(t, model.observed, "frozen request guidance")
	require.NotContains(t, model.observed, "different session guidance")
}

func TestPromptRecipeSurvivesRestartAndScopesSessions(t *testing.T) {
	dir := t.TempDir()
	conn, err := db.Connect(t.Context(), dir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Release(dir)) })
	sessions := session.NewService(db.New(conn), conn)
	c := &coordinator{sessions: sessions}
	_, err = c.preparePromptContext(t.Context(), "first", "UI arayüz tasarımı")
	require.NoError(t, err)
	c = &coordinator{sessions: sessions}
	_, err = c.preparePromptContext(t.Context(), "first", "devam et")
	require.NoError(t, err)
	var recipe promptRecipe
	_, err = c.stateStore().Get(t.Context(), "prompt_recipe/first", "active", &recipe)
	require.NoError(t, err)
	require.Equal(t, []string{"ui"}, recipe.Layers)
	require.Equal(t, prompt.ProtocolVersion, recipe.Version)
	_, err = c.preparePromptContext(t.Context(), "second", "devam et")
	require.NoError(t, err)
	_, err = c.stateStore().Get(t.Context(), "prompt_recipe/second", "active", &recipe)
	require.NoError(t, err)
	require.Empty(t, recipe.Layers)
}
