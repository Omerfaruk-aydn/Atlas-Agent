package model

import (
	"context"
	"errors"

	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/util"
)

func (m *UI) createSessionHandoff(sessionID string) tea.Cmd {
	ws := m.com.Workspace
	return func() tea.Msg {
		handoff, ok := ws.(interface {
			AgentHandoff(context.Context, string) (session.Session, error)
		})
		if !ok {
			return util.ReportError(errors.New("session handoff is not supported by this workspace"))()
		}
		child, err := handoff.AgentHandoff(context.Background(), sessionID)
		if err != nil {
			return util.ReportError(err)()
		}
		return handoffCompletedMsg{SessionID: child.ID}
	}
}
