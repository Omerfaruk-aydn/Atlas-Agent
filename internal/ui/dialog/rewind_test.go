package dialog

import (
	"context"
	"testing"

	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session/rewind"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/common"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/styles"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workspace"
	"github.com/stretchr/testify/require"
)

type rewindWorkspace struct {
	workspace.Workspace
	messages []message.Message
}

// ListUserMessages returns newest first, as the real workspace does.
func (w *rewindWorkspace) ListUserMessages(context.Context, string) ([]message.Message, error) {
	return w.messages, nil
}

func userMessage(id, text string) message.Message {
	return message.Message{ID: id, Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: text}}}
}

// The picker starts at the latest message and moving up goes back in
// time, so going back two messages is two presses of up.
func TestRewindPickerGoesBackFromTheLatestMessage(t *testing.T) {
	t.Parallel()

	sty := styles.AtlasPantera()
	ws := &rewindWorkspace{messages: []message.Message{
		userMessage("m3", "third"),
		userMessage("m2", "second"),
		userMessage("m1", "first"),
	}}
	r, err := NewRewind(&common.Common{Workspace: ws, Styles: &sty}, "s", "title")
	require.NoError(t, err)

	selected := func() string { return r.list.SelectedItem().(*RewindItem).ID() }
	require.Equal(t, "m3", selected())
	r.HandleMsg(tea.KeyPressMsg{Code: tea.KeyUp})
	require.Equal(t, "m2", selected())
	r.HandleMsg(tea.KeyPressMsg{Code: tea.KeyUp})
	require.Equal(t, "m1", selected())

	r.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.Equal(t, "m1", r.targetID)
	action := r.HandleMsg(rewindAppliedMsg{result: rewind.Result{}})
	applied, ok := action.(ActionRewindApplied)
	require.True(t, ok)
	require.Equal(t, "first", applied.Prompt, "the chosen message goes back to the editor")
}
