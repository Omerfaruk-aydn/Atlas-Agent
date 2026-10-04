package dialog

import (
	"strings"

	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	uv "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ultraviolet"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/help"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/key"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/textinput"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/common"
)

// ChatSearchID is the identifier for the in-chat search dialog.
const ChatSearchID = "chat_search"

// ActionChatSearch is emitted when the user submits a search query. The UI
// model owns the actual match-finding (it has the session's messages);
// this dialog only collects the query text.
type ActionChatSearch struct {
	Query string
}

// ChatSearch is a single-line text prompt for searching the current
// session's messages.
type ChatSearch struct {
	com   *common.Common
	input textinput.Model
	help  help.Model
	width int

	keyMap struct {
		Submit key.Binding
		Close  key.Binding
	}
}

var _ Dialog = (*ChatSearch)(nil)

// NewChatSearch creates a new ChatSearch dialog.
func NewChatSearch(com *common.Common) *ChatSearch {
	d := &ChatSearch{com: com}

	d.input = textinput.New()
	d.input.SetVirtualCursor(false)
	d.input.Placeholder = com.Text("Search this chat...")
	d.input.SetStyles(com.Styles.TextInput)
	d.input.Focus()

	h := help.New()
	h.Styles = com.Styles.DialogHelpStyles()
	d.help = h

	d.keyMap.Submit = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", com.Text("search")))
	d.keyMap.Close = CloseKey

	return d
}

// SetValue prefills the query, e.g. with the last search so pressing enter
// immediately repeats it (jumping to the next match).
func (d *ChatSearch) SetValue(v string) {
	d.input.SetValue(v)
	d.input.CursorEnd()
}

// ID implements Dialog.
func (d *ChatSearch) ID() string {
	return ChatSearchID
}

// HandleMsg implements Dialog.
func (d *ChatSearch) HandleMsg(msg tea.Msg) Action {
	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
		switch {
		case key.Matches(keyMsg, d.keyMap.Close):
			return ActionClose{}
		case key.Matches(keyMsg, d.keyMap.Submit):
			q := strings.TrimSpace(d.input.Value())
			if q == "" {
				return nil
			}
			return ActionChatSearch{Query: q}
		}
	}
	var cmd tea.Cmd
	d.input, cmd = d.input.Update(msg)
	if cmd != nil {
		return ActionCmd{cmd}
	}
	return nil
}

// Cursor returns the cursor position relative to the dialog.
func (d *ChatSearch) Cursor() *tea.Cursor {
	return InputCursor(d.com.Styles, d.input.Cursor())
}

// Draw implements [Dialog].
func (d *ChatSearch) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := d.com.Styles
	d.width = max(0, min(defaultDialogMaxWidth, area.Dx()-t.Dialog.View.GetHorizontalBorderSize()))
	innerWidth := d.width - t.Dialog.View.GetHorizontalFrameSize()
	d.input.SetWidth(max(0, innerWidth-1))

	rc := NewRenderContext(t, d.width)
	rc.Title = d.com.Text("Search chat")
	rc.AddPart(t.Dialog.InputPrompt.Render(d.input.View()))
	rc.Help = renderDialogHelp(t, &d.help, d, innerWidth)

	view := rc.Render()
	cur := d.Cursor()
	DrawCenterCursor(scr, area, view, cur)
	return cur
}

// ShortHelp implements [help.KeyMap].
func (d *ChatSearch) ShortHelp() []key.Binding {
	return []key.Binding{d.keyMap.Submit, d.keyMap.Close}
}

// FullHelp implements [help.KeyMap].
func (d *ChatSearch) FullHelp() [][]key.Binding {
	return [][]key.Binding{d.ShortHelp()}
}
