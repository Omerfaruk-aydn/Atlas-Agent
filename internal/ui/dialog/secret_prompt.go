package dialog

import (
	"strings"

	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	uv "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ultraviolet"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/help"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/key"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/textinput"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/secretprompt"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/common"
)

// SecretPromptID is the identifier for the masked secret dialog.
const SecretPromptID = "secret_prompt"

// SecretPrompt asks for a credential in a masked input. What is typed goes
// to OnSubmit only: it is never rendered, never sent as a message and
// never reaches the model.
type SecretPrompt struct {
	com     *common.Common
	request secretprompt.Request

	// OnSubmit receives the secret. OnCancel is called on dismissal.
	OnSubmit func(id, secret string)
	OnCancel func(id string)

	width  int
	input  textinput.Model
	help   help.Model
	keyMap struct {
		Submit key.Binding
		Close  key.Binding
	}
}

var _ Dialog = (*SecretPrompt)(nil)

// NewSecretPrompt creates the dialog for a pending secret request.
func NewSecretPrompt(com *common.Common, req secretprompt.Request) *SecretPrompt {
	m := &SecretPrompt{com: com, request: req}
	m.input = textinput.New()
	m.input.SetVirtualCursor(false)
	m.input.EchoMode = textinput.EchoPassword
	m.input.Placeholder = com.Text("Paste your API key...")
	m.input.SetStyles(com.Styles.TextInput)
	m.input.CharLimit = 16 * 1024
	m.input.Focus()

	m.help = help.New()
	m.help.Styles = com.Styles.DialogHelpStyles()
	m.keyMap.Submit = key.NewBinding(key.WithKeys("enter", "ctrl+y"), key.WithHelp("enter", "submit"))
	m.keyMap.Close = CloseKey
	return m
}

// ID implements Dialog.
func (m *SecretPrompt) ID() string { return SecretPromptID }

// RequestID is the pending request this dialog answers.
func (m *SecretPrompt) RequestID() string { return m.request.ID }

// HandleMsg implements Dialog.
func (m *SecretPrompt) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, m.keyMap.Close):
			if m.OnCancel != nil {
				m.OnCancel(m.request.ID)
			}
			return ActionClose{}
		case key.Matches(msg, m.keyMap.Submit):
			value := strings.TrimSpace(m.input.Value())
			if value == "" {
				return nil
			}
			if m.OnSubmit != nil {
				m.OnSubmit(m.request.ID, value)
			}
			m.input.SetValue("")
			return ActionClose{}
		default:
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			if cmd != nil {
				return ActionCmd{cmd}
			}
		}
	case tea.PasteMsg:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		if cmd != nil {
			return ActionCmd{cmd}
		}
	}
	return nil
}

// Draw implements Dialog.
func (m *SecretPrompt) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := m.com.Styles

	m.width = max(0, min(64, area.Dx()-t.Dialog.View.GetHorizontalBorderSize()))
	innerWidth := m.width - t.Dialog.View.GetHorizontalFrameSize() - 2
	m.input.SetWidth(max(0, innerWidth-t.Dialog.InputPrompt.GetHorizontalFrameSize()-1))

	dialogStyle := t.Dialog.View.Width(m.width)
	helpView := renderDialogHelp(t, &m.help, m, m.width-dialogStyle.GetHorizontalFrameSize())
	headerOffset := t.Dialog.Title.GetHorizontalFrameSize() + dialogStyle.GetHorizontalFrameSize()
	header := common.DialogTitle(t, t.Dialog.Title.Render(t.Dialog.TitleText.Render(m.request.Title)), m.width-headerOffset)

	m.input.Prompt = "> "
	lines := []string{header, t.Dialog.InputPrompt.Render(m.input.View())}
	if m.request.Detail != "" {
		lines = append(lines, t.Dialog.SecondaryText.Render(m.request.Detail))
	}
	lines = append(lines, t.Dialog.SecondaryText.Render(m.com.Text("Input is hidden and is not shown to the AI.")), "", helpView)

	cur := InputCursor(t, m.input.Cursor())
	DrawCenterCursor(scr, area, dialogStyle.Render(strings.Join(lines, "\n")), cur)
	return cur
}

// ShortHelp implements help.KeyMap.
func (m *SecretPrompt) ShortHelp() []key.Binding {
	return []key.Binding{m.keyMap.Submit, m.keyMap.Close}
}

// FullHelp implements help.KeyMap.
func (m *SecretPrompt) FullHelp() [][]key.Binding {
	return [][]key.Binding{{m.keyMap.Submit, m.keyMap.Close}}
}
