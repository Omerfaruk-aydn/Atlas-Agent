package model

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/commands"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	editor "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-editor"
	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/subagents"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/dialog"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/util"
)

// This file wires the Model Roles, Model Fallbacks, and Subagents
// management dialogs into the UI: opening each list, transitioning
// into and back out of the shared Arguments form for add/edit, and
// (for subagents) handing the instructions body off to $EDITOR. Each
// list dialog handles its own delete flow internally (see
// dialog.ModelRoles/Fallbacks/Subagents); only the cross-dialog
// transitions live here, mirroring how ActionOpenFileDiff hands off
// from Files to FileDiff elsewhere in ui.go.

func (m *UI) openModelRolesDialog() tea.Cmd {
	if m.dialog.ContainsDialog(dialog.ModelRolesID) {
		m.dialog.BringToFront(dialog.ModelRolesID)
		return nil
	}
	m.dialog.OpenDialog(dialog.NewModelRoles(m.com))
	return nil
}

func (m *UI) openFallbacksDialog() tea.Cmd {
	if m.dialog.ContainsDialog(dialog.FallbacksID) {
		m.dialog.BringToFront(dialog.FallbacksID)
		return nil
	}
	m.dialog.OpenDialog(dialog.NewFallbacks(m.com))
	return nil
}

func (m *UI) openSubagentsDialog() tea.Cmd {
	if m.dialog.ContainsDialog(dialog.SubagentsID) {
		m.dialog.BringToFront(dialog.SubagentsID)
		return nil
	}
	m.dialog.OpenDialog(dialog.NewSubagents(m.com))
	return nil
}

func (m *UI) openToolSettingsDialog() tea.Cmd {
	if m.dialog.ContainsDialog(dialog.ToolSettingsID) {
		m.dialog.BringToFront(dialog.ToolSettingsID)
		return nil
	}
	m.dialog.OpenDialog(dialog.NewToolSettings(m.com))
	return nil
}

// handleSetMode switches the coder agent between the small model
// ("fast") and the large model ("quality"), and pushes that model's
// reasoning effort to the corresponding end of its supported range --
// lowest for fast, highest for quality. Reuses exactly the two
// primitives ActionSelectReasoningEffort already relies on
// (SetConfigField for the agent-model override, UpdatePreferredModel
// for the effort), just choosing both ends of an axis at once instead
// of asking the user to pick.
func (m *UI) handleSetMode(mode string) tea.Cmd {
	if m.isAgentBusy() {
		return util.ReportWarn(m.com.Text("Agent is busy, please wait..."))
	}
	if mode != "fast" && mode != "quality" {
		return util.ReportError(fmt.Errorf(m.com.Text("unknown mode %q"), mode))
	}
	return m.updateAgentModelCmd(func() tea.Msg {
		return m.applyMode(mode)
	})
}

// applyMode does the actual mode-switch work: it is a plain function
// (not a tea.Cmd factory) so a test can call it directly and inspect
// its return value, rather than having to unwrap the tea.Sequence
// handleSetMode wraps it in.
func (m *UI) applyMode(mode string) tea.Msg {
	targetType := config.SelectedModelTypeLarge
	if mode == "fast" {
		targetType = config.SelectedModelTypeSmall
	}

	ws := m.com.Workspace
	if err := ws.SetConfigField(config.ScopeGlobal, "options.agent_models."+config.AgentCoder, targetType); err != nil {
		return util.ReportError(fmt.Errorf(m.com.Text("switching mode: %w"), err))()
	}

	// SetConfigField reloads the config synchronously before returning,
	// so this read sees the agent-model override just written.
	cfg := ws.Config()
	if cfg == nil {
		return util.NewInfoMsg(modeLabel(mode) + m.com.Text(" mode: now running on the ") + string(targetType) + m.com.Text(" model."))
	}

	if model := cfg.GetModelByType(targetType); model != nil && len(model.ReasoningLevels) > 0 {
		effort := model.ReasoningLevels[0]
		if mode == "quality" {
			effort = model.ReasoningLevels[len(model.ReasoningLevels)-1]
		}
		selected := cfg.Models[targetType]
		selected.ReasoningEffort = effort
		if err := ws.UpdatePreferredModel(config.ScopeGlobal, targetType, selected); err != nil {
			return util.ReportError(fmt.Errorf(m.com.Text("setting reasoning effort: %w"), err))()
		}
		ws.UpdateAgentModel(context.Background())
		return util.NewInfoMsg(fmt.Sprintf(m.com.Text("%s mode: now running on the %s model at %s reasoning."), modeLabel(mode), targetType, effort))
	}

	ws.UpdateAgentModel(context.Background())
	return util.NewInfoMsg(modeLabel(mode) + m.com.Text(" mode: now running on the ") + string(targetType) + m.com.Text(" model."))
}

func modeLabel(mode string) string {
	if mode == "fast" {
		return "Fast"
	}
	return "Quality"
}

// -- Model roles --

func (m *UI) handleOpenModelRoleForm(msg dialog.ActionOpenModelRoleForm) tea.Cmd {
	m.dialog.CloseDialog(dialog.ModelRolesID)

	var args []commands.Argument
	title := m.com.Text("New Model Role")
	if msg.ExistingName == "" {
		args = append(args, commands.Argument{ID: "name", Title: m.com.Text("Name"), Description: m.com.Text("e.g. research"), Required: true})
	} else {
		title = m.com.Text("Edit Role: ") + msg.ExistingName
	}
	args = append(args,
		commands.Argument{ID: "provider", Title: m.com.Text("Provider"), Description: "e.g. openai", Required: true},
		commands.Argument{ID: "model", Title: m.com.Text("Model"), Description: "e.g. gpt-4o", Required: true},
		commands.Argument{ID: "reasoning_effort", Title: m.com.Text("Reasoning Effort"), Description: m.com.Text("e.g. low, medium, high -- leave empty for the model's own default")},
	)

	form := dialog.NewArguments(m.com, title, m.com.Text("A named model role a subagent, the advisor, or a vibe worker can run on -- see `atlas models roles`."),
		args, dialog.ActionSaveModelRole{ExistingName: msg.ExistingName})
	if msg.ExistingName != "" {
		form.SetValues(map[string]string{
			"provider":         msg.ExistingProvider,
			"model":            msg.ExistingModel,
			"reasoning_effort": msg.ExistingReasoningEffort,
		})
	}
	m.dialog.OpenDialog(form)
	return nil
}

// modelRoleSavedMsg delivers the async result of writing a model role.
type modelRoleSavedMsg struct {
	err error
}

func (m *UI) handleSaveModelRole(msg dialog.ActionSaveModelRole) tea.Cmd {
	m.dialog.CloseDialog(dialog.ArgumentsID)

	name := strings.TrimSpace(msg.ExistingName)
	if name == "" {
		name = strings.TrimSpace(msg.Args["name"])
	}
	if name == "" {
		return util.ReportWarn(m.com.Text("Role name is required."))
	}
	model := config.SelectedModel{
		Provider:        strings.TrimSpace(msg.Args["provider"]),
		Model:           strings.TrimSpace(msg.Args["model"]),
		ReasoningEffort: strings.TrimSpace(msg.Args["reasoning_effort"]),
	}

	ws := m.com.Workspace
	return func() tea.Msg {
		err := ws.SetConfigField(config.ScopeGlobal, "options.model_roles."+name, model)
		return modelRoleSavedMsg{err: err}
	}
}

// -- Model fallbacks --

func (m *UI) handleOpenFallbackEntryForm(msg dialog.ActionOpenFallbackEntryForm) tea.Cmd {
	m.dialog.CloseDialog(dialog.FallbacksID)
	form := dialog.NewArguments(m.com, m.com.Text("Add Fallback: ")+string(msg.ModelType),
		m.com.Text("Appended to the end of ")+string(msg.ModelType)+m.com.Text("'s fallback chain -- tried in order when the primary model hits a 429."),
		[]commands.Argument{
			{ID: "provider", Title: m.com.Text("Provider"), Description: "e.g. openai", Required: true},
			{ID: "model", Title: m.com.Text("Model"), Description: "e.g. gpt-4o-mini", Required: true},
		},
		dialog.ActionSaveFallbackEntry{ModelType: msg.ModelType},
	)
	m.dialog.OpenDialog(form)
	return nil
}

// fallbackEntrySavedMsg delivers the async result of appending a
// fallback entry.
type fallbackEntrySavedMsg struct {
	err error
}

func (m *UI) handleSaveFallbackEntry(msg dialog.ActionSaveFallbackEntry) tea.Cmd {
	m.dialog.CloseDialog(dialog.ArgumentsID)

	entry := config.SelectedModel{
		Provider: strings.TrimSpace(msg.Args["provider"]),
		Model:    strings.TrimSpace(msg.Args["model"]),
	}

	var current []config.SelectedModel
	if cfg := m.com.Config(); cfg != nil && cfg.Options != nil {
		current = cfg.Options.ModelFallbacks[msg.ModelType]
	}
	updated := append(append([]config.SelectedModel{}, current...), entry)

	ws := m.com.Workspace
	return func() tea.Msg {
		err := ws.SetConfigField(config.ScopeGlobal, "options.model_fallbacks."+string(msg.ModelType), updated)
		return fallbackEntrySavedMsg{err: err}
	}
}

func (m *UI) handleOpenFallbackCooldownForm(msg dialog.ActionOpenFallbackCooldownForm) tea.Cmd {
	m.dialog.CloseDialog(dialog.FallbacksID)
	form := dialog.NewArguments(m.com, m.com.Text("Fallback Cooldown"),
		m.com.Text("Seconds a fallback stays active after a failover before the next turn returns to the primary model. 0 returns every turn."),
		[]commands.Argument{
			{ID: "seconds", Title: m.com.Text("Seconds"), Description: "e.g. 300", Required: true},
		},
		dialog.ActionSaveFallbackCooldown{},
	)
	form.SetValues(map[string]string{"seconds": strconv.Itoa(msg.Current)})
	m.dialog.OpenDialog(form)
	return nil
}

func (m *UI) handleSaveFallbackCooldown(msg dialog.ActionSaveFallbackCooldown) tea.Cmd {
	m.dialog.CloseDialog(dialog.ArgumentsID)

	seconds, err := strconv.Atoi(strings.TrimSpace(msg.Args[m.com.Text("seconds")]))
	if err != nil || seconds < 0 {
		return util.ReportWarn(m.com.Text("Cooldown must be a non-negative number of seconds."))
	}

	ws := m.com.Workspace
	return func() tea.Msg {
		saveErr := ws.SetConfigField(config.ScopeGlobal, "options.fallback_cooldown", seconds)
		return fallbackEntrySavedMsg{err: saveErr}
	}
}

// backToPreviousSession returns to the session the user stepped away
// from when they opened another one -- a sub-agent's run from the jobs
// dialog or the agent hub. The breadcrumb is cleared on the way out, so
// a second invocation does nothing instead of bouncing between the two.
func (m *UI) backToPreviousSession() tea.Cmd {
	if m.previousSessionID == "" {
		return nil
	}
	target := m.previousSessionID
	m.previousSessionID = ""
	return m.loadSession(target)
}

// -- Session mode --

func (m *UI) openModesDialog() tea.Cmd {
	if m.dialog.ContainsDialog(dialog.ModesID) {
		m.dialog.BringToFront(dialog.ModesID)
		return nil
	}
	m.dialog.OpenDialog(dialog.NewModes(m.com))
	return nil
}

// handleSelectSessionMode writes the chosen mode and rebuilds the agent
// around it. The rebuild is what actually applies the change: it is
// where the mode's instructions join the system prompt and where the
// role sharing the mode's name becomes the session's model (see
// internal/agent's session_mode.go).
func (m *UI) handleSelectSessionMode(msg dialog.ActionSelectSessionMode) tea.Cmd {
	if m.isAgentBusy() {
		return util.ReportWarn(m.com.Text("Agent is busy, please wait..."))
	}
	m.dialog.CloseDialog(dialog.ModesID)

	mode := strings.TrimSpace(msg.Mode)
	ws := m.com.Workspace
	return m.updateAgentModelCmd(func() tea.Msg {
		if err := ws.SetConfigField(config.ScopeGlobal, "options.session_mode", mode); err != nil {
			return util.ReportError(fmt.Errorf(m.com.Text("switching mode: %w"), err))()
		}
		if mode == "" {
			return util.NewInfoMsg(m.com.Text("Session mode cleared -- back to the ordinary coder prompt."))
		}
		if cfg := ws.Config(); cfg != nil {
			if selected, ok := cfg.ResolveRole(mode); ok {
				return util.NewInfoMsg(fmt.Sprintf("Session mode: %s, on %s/%s.", mode, selected.Provider, selected.Model))
			}
		}
		return util.NewInfoMsg(fmt.Sprintf(m.com.Text("Session mode: %s. Assign a model to the %q role to give it its own model."), mode, mode))
	})
}

func (m *UI) handleOpenAutoCompactThresholdForm() tea.Cmd {
	m.dialog.CloseDialog(dialog.CommandsID)

	current := ""
	if cfg := m.com.Config(); cfg != nil && cfg.Options != nil && cfg.Options.AutoSummarizeAt > 0 && cfg.Options.AutoSummarizeAt < 1 {
		current = strconv.Itoa(int(cfg.Options.AutoSummarizeAt*100 + 0.5))
	}
	form := dialog.NewArguments(m.com, m.com.Text("Auto-Compact Threshold"),
		m.com.Text("Percentage of the model's context window that may be used before the session auto-summarizes. Leave empty to use the built-in thresholds."),
		[]commands.Argument{
			{ID: "percent", Title: m.com.Text("Percent"), Description: m.com.Text("e.g. 80 for 80% -- empty resets to built-in")},
		},
		dialog.ActionSaveAutoCompactThreshold{},
	)
	form.SetValues(map[string]string{"percent": current})
	m.dialog.OpenDialog(form)
	return nil
}

func (m *UI) handleSaveAutoCompactThreshold(msg dialog.ActionSaveAutoCompactThreshold) tea.Cmd {
	m.dialog.CloseDialog(dialog.ArgumentsID)

	raw := strings.TrimSpace(msg.Args["percent"])
	ws := m.com.Workspace

	if raw == "" {
		return func() tea.Msg {
			if err := ws.SetConfigField(config.ScopeGlobal, "options.auto_summarize_at", 0.0); err != nil {
				return util.ReportError(err)()
			}
			return util.NewInfoMsg(m.com.Text("Auto-compact threshold reset to the built-in defaults."))
		}
	}

	percent, err := strconv.ParseFloat(raw, 64)
	if err != nil || percent <= 0 || percent >= 100 {
		return util.ReportWarn(m.com.Text("Threshold must be a number between 1 and 99 (percent of context used)."))
	}

	return func() tea.Msg {
		if err := ws.SetConfigField(config.ScopeGlobal, "options.auto_summarize_at", percent/100); err != nil {
			return util.ReportError(err)()
		}
		return util.NewInfoMsg(fmt.Sprintf(m.com.Text("Auto-compact threshold set to %g%% of the context window."), percent))
	}
}

// -- Subagents --

func (m *UI) handleOpenSubagentForm(msg dialog.ActionOpenSubagentForm) tea.Cmd {
	m.dialog.CloseDialog(dialog.SubagentsID)

	var args []commands.Argument
	title := m.com.Text("New Subagent")
	if msg.ExistingName == "" {
		args = append(args, commands.Argument{ID: "name", Title: m.com.Text("Name"), Description: m.com.Text("e.g. research"), Required: true})
	} else {
		title = m.com.Text("Edit Subagent: ") + msg.ExistingName
	}
	args = append(args,
		commands.Argument{ID: "description", Title: m.com.Text("Description"), Description: m.com.Text("when this subagent should be used"), Required: true},
		commands.Argument{ID: "model", Title: m.com.Text("Model Role"), Description: m.com.Text("e.g. research; empty runs on the session's model")},
	)

	description := m.com.Text("Instructions are edited separately, in $EDITOR, after saving (see enter on the subagents list).")
	form := dialog.NewArguments(m.com, title, description, args,
		dialog.ActionSaveSubagentMeta{ExistingName: msg.ExistingName, UserScope: msg.UserScope})
	if msg.ExistingName != "" {
		form.SetValues(map[string]string{"description": msg.ExistingDescription, "model": msg.ExistingModel})
	}
	m.dialog.OpenDialog(form)
	return nil
}

// subagentSavedMsg delivers the async result of saving a subagent's
// metadata.
type subagentSavedMsg struct {
	err error
}

// newSubagentInstructionsTemplate mirrors atlas agent new's own
// placeholder body (internal/cmd/agent_new.go), so a subagent created
// from the UI reads the same way as one scaffolded from the CLI until
// its instructions are actually written.
func newSubagentInstructionsTemplate(name string) string {
	return fmt.Sprintf("# %s\n\nTODO: the instructions this subagent runs with.\n", name)
}

func (m *UI) handleSaveSubagentMeta(msg dialog.ActionSaveSubagentMeta) tea.Cmd {
	m.dialog.CloseDialog(dialog.ArgumentsID)

	name := strings.TrimSpace(msg.ExistingName)
	isNew := name == ""
	if isNew {
		name = strings.TrimSpace(msg.Args["name"])
	}
	if name == "" {
		return util.ReportWarn(m.com.Text("Subagent name is required."))
	}
	description := strings.TrimSpace(msg.Args["description"])
	if description == "" {
		return util.ReportWarn(m.com.Text("Subagent description is required."))
	}

	sub := subagents.Subagent{
		Name:        name,
		Description: description,
		Model:       strings.TrimSpace(msg.Args["model"]),
	}
	if isNew {
		sub.Instructions = newSubagentInstructionsTemplate(name)
	}

	ws := m.com.Workspace
	return func() tea.Msg {
		if isNew {
			_, err := ws.SaveSubagent(context.Background(), sub, msg.UserScope)
			return subagentSavedMsg{err: err}
		}
		// Preserve role policy and instructions when editing only metadata.
		existing, err := ws.ListSubagents(context.Background())
		if err != nil {
			return subagentSavedMsg{err: err}
		}
		for _, e := range existing {
			if e.Name == name {
				sub.Instructions = e.Instructions
				sub.Contract = e.Contract
				sub.Tools = e.Tools
				sub.ReadOnly = e.ReadOnly
				sub.AllowCommands = e.AllowCommands
				sub.PreferredSkills = e.PreferredSkills
				sub.InheritModel = e.InheritModel
				_, err = ws.SaveSubagent(context.Background(), sub, msg.UserScope)
				return subagentSavedMsg{err: err}
			}
		}
		return subagentSavedMsg{err: fmt.Errorf(m.com.Text("subagent %q no longer exists"), name)}
	}
}

// subagentFileEditedMsg signals that $EDITOR closed for a subagent's
// definition file, so the subagents list should refresh. Deliberately
// separate from openEditorMsg (used for the composer's own external-
// editor flow): that message always feeds its text into m.textarea,
// which is not what closing an unrelated file's editor should do.
type subagentFileEditedMsg struct{}

func (m *UI) handleEditSubagentFile(msg dialog.ActionEditSubagentFile) tea.Cmd {
	cmd, err := editor.Command("atlas", msg.Path)
	if err != nil {
		return util.ReportError(err)
	}
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil {
			return util.ReportError(err)()
		}
		return subagentFileEditedMsg{}
	})
}

// refreshSubagentsDialogIfOpen re-lists subagents into the Subagents
// dialog if it is still open -- used after an $EDITOR round trip on a
// subagent's file, left open (unlike the save/delete flows, which
// close and reopen fresh) so the user lands back exactly where they
// were.
func (m *UI) refreshSubagentsDialogIfOpen() {
	if d, ok := m.dialog.Dialog(dialog.SubagentsID).(*dialog.Subagents); ok {
		d.Refresh()
	}
}
