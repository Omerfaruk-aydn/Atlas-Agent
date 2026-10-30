package model

import (
	"context"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	uv "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ultraviolet"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/textarea"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/history"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/common"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/dialog"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workspace"
	"github.com/stretchr/testify/require"
)

// freshSessionActionDialog is a fake Dialog (the same trick
// dialog_mouse_test.go's mouseActionDialog uses) that turns a
// tea.MouseClickMsg into dialog.ActionFreshSession, so the test can
// drive handleDialogMsg's real switch without needing the Commands
// dialog's own selection machinery.
type freshSessionActionDialog struct {
	sessionID string
}

func (*freshSessionActionDialog) ID() string { return "fresh-session-action" }

func (d *freshSessionActionDialog) HandleMsg(msg tea.Msg) dialog.Action {
	if _, ok := msg.(tea.MouseClickMsg); !ok {
		return nil
	}
	return dialog.ActionFreshSession{SessionID: d.sessionID}
}

func (*freshSessionActionDialog) Draw(uv.Screen, uv.Rectangle) *tea.Cursor { return nil }

var _ dialog.Dialog = (*freshSessionActionDialog)(nil)

// freshSessionWorkspace is a minimal workspace.Workspace stub for
// TestActionFreshSessionCancelsAndReloads: it records AgentCancel calls
// and answers just enough of loadSession's calls (GetSession,
// ListSessionHistory, FileTrackerListReadFiles, SetCurrentSession,
// ListMessages) for that reload to complete without touching a real
// backend.
type freshSessionWorkspace struct {
	workspace.Workspace
	cancelled []string
}

func (w *freshSessionWorkspace) AgentCancel(sessionID string) {
	w.cancelled = append(w.cancelled, sessionID)
}

func (w *freshSessionWorkspace) Config() *config.Config {
	return nil
}

func (w *freshSessionWorkspace) GetSession(context.Context, string) (session.Session, error) {
	return session.Session{ID: "s1", Title: "Fresh me"}, nil
}

func (w *freshSessionWorkspace) ListSessionHistory(context.Context, string) ([]history.File, error) {
	return nil, nil
}

func (w *freshSessionWorkspace) FileTrackerListReadFiles(context.Context, string) ([]string, error) {
	return nil, nil
}

func (w *freshSessionWorkspace) SetCurrentSession(context.Context, string) error {
	return nil
}

func (w *freshSessionWorkspace) ListMessages(context.Context, string) ([]message.Message, error) {
	return nil, nil
}

func newFreshSessionTestUI(ws workspace.Workspace) *UI {
	com := common.DefaultCommon(ws)

	ta := textarea.New()
	ta.SetStyles(com.Styles.Editor.Textarea)
	ta.SetVirtualCursor(false)

	return &UI{
		com:      com,
		status:   NewStatus(com, nil),
		chat:     NewChat(com, config.ScrollbarDefault),
		textarea: ta,
		state:    uiChat,
		focus:    uiFocusEditor,
		width:    140,
		height:   45,
		dialog:   dialog.NewOverlay(),
	}
}

// TestActionFreshSessionCancelsAndReloads verifies /fresh's two safe,
// already-proven primitives fire in the right order: the active run is
// cancelled synchronously (a no-op if nothing was running), and the
// session is reloaded from the backend, the same reload a session
// switch already does -- rather than adding any new recovery mechanism
// of its own.
func TestActionFreshSessionCancelsAndReloads(t *testing.T) {
	t.Parallel()

	ws := &freshSessionWorkspace{}
	m := newFreshSessionTestUI(ws)
	m.dialog.OpenDialog(&freshSessionActionDialog{sessionID: "s1"})

	_, cmd := m.Update(tea.MouseClickMsg(tea.Mouse{Button: tea.MouseLeft}))

	require.Equal(t, []string{"s1"}, ws.cancelled, "AgentCancel must fire synchronously, not deferred into the returned command")
	require.NotNil(t, cmd, "expected a command batch to reload the session")
}
