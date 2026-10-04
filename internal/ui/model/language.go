package model

import (
	"fmt"
	"reflect"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/key"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/i18n"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/dialog"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/util"
)

type languageSavedMsg struct {
	code string
	err  error
}

// Language returns the selected interface language for post-TUI output.
func (m *UI) Language() string { return m.com.Styles.Language() }

func (m *UI) localizeKeys() {
	defaults := DefaultKeyMap()
	var visit func(reflect.Value, reflect.Value)
	visit = func(current, source reflect.Value) {
		if current.Type() == reflect.TypeFor[key.Binding]() {
			binding := current.Addr().Interface().(*key.Binding)
			help := source.Interface().(key.Binding).Help()
			binding.SetHelp(binding.Help().Key, m.com.Text(help.Desc))
			return
		}
		for i := range current.NumField() {
			visit(current.Field(i), source.Field(i))
		}
	}
	visit(reflect.ValueOf(&m.keyMap).Elem(), reflect.ValueOf(defaults))
}

func (m *UI) saveLanguage(code string) tea.Cmd {
	if !i18n.Supported(code) {
		return util.ReportError(fmt.Errorf("unsupported interface language %q", code))
	}
	ws := m.com.Workspace
	return func() tea.Msg {
		return languageSavedMsg{code: code, err: ws.SetConfigField(config.ScopeGlobal, "options.tui.language", code)}
	}
}

func (m *UI) handleLanguageSaved(msg languageSavedMsg) tea.Cmd {
	if msg.err != nil {
		if picker, ok := m.dialog.Dialog(dialog.LanguagesID).(*dialog.Languages); ok {
			picker.SetSaving(false)
		}
		return util.ReportError(msg.err)
	}
	if m.com.Styles.Locale == nil {
		m.com.Styles.Locale = i18n.New(msg.code)
	} else {
		m.com.Styles.Locale.Set(msg.code)
	}
	m.dialog.CloseDialog(dialog.LanguagesID)
	m.dialog.CloseDialog(dialog.CommandsID)
	// Style refresh invalidates cached labels but never rewrites message content.
	if m.chat != nil && m.header != nil {
		m.refreshStyles()
	}
	m.localizeKeys()
	previousReady, previousWorking := m.readyPlaceholder, m.workingPlaceholder
	m.randomizePlaceholders()
	switch m.textarea.Placeholder {
	case previousReady:
		m.textarea.Placeholder = m.readyPlaceholder
	case previousWorking:
		m.textarea.Placeholder = m.workingPlaceholder
	}
	if m.workflow.open && m.session != nil {
		m.workflow.locale = m.com.Styles.Locale
		return tea.Batch(util.ReportSuccess(m.com.Text("Language saved")), workflowLanguageCmd(m.workflow.snapshot, m.workflow.epoch, msg.code))
	}
	return util.ReportSuccess(m.com.Text("Language saved"))
}
