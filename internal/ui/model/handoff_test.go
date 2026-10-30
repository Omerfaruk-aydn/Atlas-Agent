package model

import (
	"context"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/dialog"
	"github.com/stretchr/testify/require"
)

type handoffWorkspace struct {
	*freshSessionWorkspace
	source string
}

func (w *handoffWorkspace) AgentHandoff(_ context.Context, sessionID string) (session.Session, error) {
	w.source = sessionID
	return session.Session{ID: "child", ParentSessionID: sessionID}, nil
}

func TestActionHandoffLoadsLinkedFreshSession(t *testing.T) {
	t.Parallel()
	ws := &handoffWorkspace{freshSessionWorkspace: &freshSessionWorkspace{}}
	m := newFreshSessionTestUI(ws)
	m.session = &session.Session{ID: "parent"}

	cmd := m.dispatchDialogAction(dialog.ActionHandoff{SessionID: "parent"})
	require.Empty(t, ws.source, "handoff must not perform I/O during Update")
	msg := cmd()
	require.Equal(t, "parent", ws.source)
	require.Equal(t, handoffCompletedMsg{SessionID: "child"}, msg)

	_, cmd = m.Update(msg)
	require.Equal(t, "parent", m.previousSessionID)
	require.NotNil(t, cmd)
}

func TestActionHandoffRejectsBusySession(t *testing.T) {
	t.Parallel()
	ws := &countingWorkspace{ready: true, agentBusy: true}
	m := newBusyUI(ws)
	warmCaches(m, true)

	cmd := m.dispatchDialogAction(dialog.ActionHandoff{SessionID: "s1"})
	require.NotNil(t, cmd)
	_, ok := m.createSessionHandoff("s1")().(handoffCompletedMsg)
	require.False(t, ok, "the test workspace must reject unsupported handoffs explicitly")
}
