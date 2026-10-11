package model

import (
	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/support"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/dialog"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workspace"
)

// supportService returns the /support service when Atlas runs locally;
// client mode has none.
func (m *UI) supportService() *support.Service {
	aw, ok := m.com.Workspace.(*workspace.AppWorkspace)
	if !ok || aw.App() == nil {
		return nil
	}
	return aw.App().Support
}

// supportChangedMsg reports a change on either /support end.
type supportChangedMsg struct{}

func (m *UI) openSupport() tea.Cmd {
	m.dialog.CloseDialog(dialog.CommandsID)
	listen := m.watchSupport()
	if m.dialog.ContainsDialog(dialog.SupportID) {
		m.dialog.BringToFront(dialog.SupportID)
		return listen
	}
	d := dialog.NewSupport(m.com, m.supportService())
	m.dialog.OpenDialog(d)
	return tea.Batch(d.Init(), listen)
}

// watchSupport starts listening for /support changes once.
func (m *UI) watchSupport() tea.Cmd {
	svc := m.supportService()
	if svc == nil || m.devices.supportChanges != nil {
		return nil
	}
	m.devices.supportChanges = svc.Changes()
	return m.nextSupportChange()
}

func (m *UI) nextSupportChange() tea.Cmd {
	ch := m.devices.supportChanges
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		<-ch
		return supportChangedMsg{}
	}
}

// handleSupportChanged brings /support forward when the owner must decide:
// a verified connection request or a request for more permissions.
func (m *UI) handleSupportChanged() tea.Cmd {
	cmds := []tea.Cmd{m.nextSupportChange()}
	svc := m.supportService()
	if h := svc.ActiveHost(); h != nil {
		st := h.Status()
		if st.Pending != nil || st.Permission != nil {
			if d := m.supportDialog(); d != nil {
				d.ShowHost()
				m.dialog.BringToFront(dialog.SupportID)
			} else {
				cmds = append(cmds, m.openSupport())
				if d := m.supportDialog(); d != nil {
					d.ShowHost()
				}
			}
		}
	}
	return tea.Batch(cmds...)
}

func (m *UI) supportDialog() *dialog.Support {
	if d, ok := m.dialog.Dialog(dialog.SupportID).(*dialog.Support); ok {
		return d
	}
	return nil
}
