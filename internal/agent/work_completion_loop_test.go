package agent

import (
	"context"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	"github.com/stretchr/testify/require"
)

func TestUnverifiedWorkCannotEndAsNormalCompletion(t *testing.T) {
	t.Parallel()
	var store *engineering.Store
	workflow := fantasy.NewAgentTool("workflow", "Register the request.", func(ctx context.Context, _ probeInput, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
		_, err := store.UpdateWork(ctx, tools.GetSessionFromContext(ctx), func(w *engineering.WorkState, _ engineering.State) error {
			_, err := w.AddItem("behavior", "export works", engineering.OriginUser, []int{1}, time.Now())
			return err
		})
		return fantasy.NewTextResponse("registered"), err
	})
	model := &scriptedModel{steps: []scriptedStep{{tool: "workflow", input: `{"port":1}`}}, repeat: &scriptedStep{text: "All done."}}
	a, env, created := newWorkLoopAgent(t, model, workflow)
	store = created
	sess, err := env.sessions.Create(t.Context(), "unfinished work")
	require.NoError(t, err)
	_, err = a.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "fix export", NonInteractive: true, MaxOutputTokens: 1000})
	require.NoError(t, err)
	require.LessOrEqual(t, len(model.calls()), 5, "no-progress continuation is bounded")
	msgs, err := env.messages.List(t.Context(), sess.ID)
	require.NoError(t, err)
	var finish *message.Finish
	for i := range msgs {
		if msgs[i].Role == message.Assistant {
			finish = msgs[i].FinishPart()
		}
	}
	require.NotNil(t, finish)
	require.Equal(t, "Stopped with unverified requirements", finish.Message)
	require.Contains(t, finish.Details, "no_progress")
	st, err := store.Read(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Len(t, st.Work.Turns, 1, "internal completion checks must never become user requirements")
	require.Equal(t, engineering.ItemOpen, st.Work.Items[0].Status)
}
