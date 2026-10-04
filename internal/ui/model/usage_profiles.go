package model

import (
	"fmt"
	"slices"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/commands"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/dialog"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/util"
)

func (m *UI) openUsageProfiles() tea.Cmd {
	if m.isAgentBusy() {
		return util.ReportWarn(m.com.Text("Finish or stop the active turn before selecting a usage profile."))
	}
	cfg := m.com.Config()
	if cfg == nil {
		return nil
	}
	profiles := cfg.UsageProfiles()
	names := make([]string, 0, len(profiles))
	for name := range profiles {
		names = append(names, name)
	}
	slices.Sort(names)
	details := make([]string, 0, len(names))
	for _, name := range names {
		profile := profiles[name]
		details = append(details, fmt.Sprintf(m.com.Text("%s: %s (%d steps, %d agents)"), name, profile.Description, profile.MaxSteps, profile.MaxAgents))
	}
	form := dialog.NewArguments(m.com, m.com.Text("Usage Profiles"), strings.Join(details, "\n"), []commands.Argument{{ID: "name", Title: m.com.Text("Profile"), Description: strings.Join(names, ", "), Required: true}}, dialog.ActionSaveUsageProfile{})
	form.SetValues(map[string]string{"name": cfg.Options.UsageProfile})
	m.dialog.OpenDialog(form)
	return nil
}

func (m *UI) handleSaveUsageProfile(msg dialog.ActionSaveUsageProfile) tea.Cmd {
	if m.isAgentBusy() {
		return util.ReportWarn(m.com.Text("Finish or stop the active turn before selecting a usage profile."))
	}
	name := strings.TrimSpace(msg.Args["name"])
	if _, ok := m.com.Config().UsageProfiles()[name]; !ok {
		return util.ReportError(fmt.Errorf(m.com.Text("unknown usage profile %q"), name))
	}
	m.dialog.CloseDialog(dialog.ArgumentsID)
	ws := m.com.Workspace
	return m.updateAgentModelCmd(func() tea.Msg {
		if err := ws.SetConfigField(config.ScopeWorkspace, "options.usage_profile", name); err != nil {
			return util.ReportError(err)()
		}
		return util.NewInfoMsg(m.com.Text("Usage profile selected: ") + name)
	})
}
