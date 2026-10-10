package model

import (
	"fmt"
	"net/url"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools/mcp"
	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/integrations"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/localapps"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/dialog"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/util"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workspace"
	"github.com/pkg/browser"
)

// localServices returns the integrations hub and the app manager when
// Atlas runs locally; client mode has neither.
func (m *UI) localServices() (*integrations.Service, *localapps.Manager) {
	aw, ok := m.com.Workspace.(*workspace.AppWorkspace)
	if !ok || aw.App() == nil {
		return nil, nil
	}
	return aw.App().Integrations, aw.App().LocalApps
}

func (m *UI) openIntegrations() tea.Cmd {
	m.dialog.CloseDialog(dialog.CommandsID)
	if m.dialog.ContainsDialog(dialog.IntegrationsID) {
		m.dialog.BringToFront(dialog.IntegrationsID)
		return nil
	}
	svc, _ := m.localServices()
	d := dialog.NewIntegrations(m.com, svc)
	m.dialog.OpenDialog(d)
	return d.Init()
}

func (m *UI) integrationsDialog() *dialog.Integrations {
	if d, ok := m.dialog.Dialog(dialog.IntegrationsID).(*dialog.Integrations); ok {
		return d
	}
	return nil
}

func (m *UI) openApps(appID string) tea.Cmd {
	m.dialog.CloseDialog(dialog.CommandsID)
	if m.dialog.ContainsDialog(dialog.AppsID) {
		if appID == "" {
			m.dialog.BringToFront(dialog.AppsID)
			return nil
		}
		m.dialog.CloseDialog(dialog.AppsID)
	}
	_, mgr := m.localServices()
	d := dialog.NewApps(m.com, mgr, appID)
	m.dialog.OpenDialog(d)
	return d.Init()
}

func (m *UI) appsDialog() *dialog.Apps {
	if d, ok := m.dialog.Dialog(dialog.AppsID).(*dialog.Apps); ok {
		return d
	}
	return nil
}

// integrationsMCPLogin reuses the MCP sign-in dialog for one server.
func (m *UI) integrationsMCPLogin(name string) tea.Cmd {
	if m.dialog.ContainsDialog(dialog.MCPAuthID) {
		m.dialog.BringToFront(dialog.MCPAuthID)
		return nil
	}
	serverURL := ""
	if cfg := m.com.Config(); cfg != nil {
		serverURL = cfg.MCP[name].URL
	}
	dlg, cmd := dialog.NewMCPAuth(m.com, []mcp.PendingAuthServer{{Name: name, URL: serverURL}}, m.com.Workspace.MCPAuthURL)
	m.dialog.OpenDialog(dlg)
	return cmd
}

// The prompts hand app work to the assistant, which uses the local_app tool
// and reports in the chat. The user's own words follow unchanged.
const (
	appCreatePrompt = "Build a local app with the local_app tool and follow its create steps: check the runtime, plan, scaffold, adapt, verify, start, check every acceptance criterion and report. My request:"
	appUpdatePrompt = "Update the local app %s with the local_app tool: update_begin, make the change, update_finish; roll back if it cannot pass. The change I want:"
)

func (m *UI) createApp(request string) tea.Cmd {
	m.dialog.CloseDialog(dialog.AppsID)
	return m.sendMessage(m.com.Text(appCreatePrompt) + "\n\n" + request)
}

func (m *UI) updateApp(id, request string) tea.Cmd {
	m.dialog.CloseDialog(dialog.AppsID)
	return m.sendMessage(fmt.Sprintf(m.com.Text(appUpdatePrompt), id) + "\n\n" + request)
}

// openAppURL opens a local app in the default browser. Only loopback URLs
// from the app manager reach here.
func (m *UI) openAppURL(raw string) tea.Cmd {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost") {
		return util.ReportWarn(m.com.Text("Only local app addresses can be opened."))
	}
	return func() tea.Msg {
		if err := browser.OpenURL(raw); err != nil {
			return util.NewErrorMsg(err)
		}
		return nil
	}
}
