package dialog

import (
	"strings"

	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	uv "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ultraviolet"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/help"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/key"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/textinput"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/common"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/list"
)

// SnippetsID is the identifier for the saved-prompt-snippets dialog.
const SnippetsID = "snippets"

// Snippet is a saved, reusable prompt.
type Snippet struct {
	Name string `json:"name"`
	Text string `json:"text"`
}

// ActionInsertSnippet is emitted when the user picks a snippet to insert
// into the editor.
type ActionInsertSnippet struct {
	Text string
}

// ActionSaveSnippet is emitted when the user names a new snippet to save
// from their current editor draft.
type ActionSaveSnippet struct {
	Name string
	Text string
}

// ActionDeleteSnippet is emitted when the user deletes a saved snippet.
type ActionDeleteSnippet struct {
	Index int
}

type snippetItem struct {
	*list.Versioned
	index   int
	snippet Snippet
	t       *common.Common
	focused bool
}

var _ list.Item = &snippetItem{}

func (e *snippetItem) Finished() bool { return true }
func (e *snippetItem) Filter() string { return e.snippet.Name }

func (e *snippetItem) SetFocused(focused bool) {
	if e.focused == focused {
		return
	}
	e.focused = focused
	if e.Versioned != nil {
		e.Bump()
	}
}

func (e *snippetItem) Render(width int) string {
	t := e.t.Styles
	itemStyles := ListItemStyles{
		ItemBlurred:     t.Dialog.NormalItem,
		ItemFocused:     t.Dialog.SelectedItem,
		InfoTextBlurred: t.Dialog.Sessions.InfoBlurred,
		InfoTextFocused: t.Dialog.Sessions.InfoFocused,
	}
	preview := strings.ReplaceAll(strings.TrimSpace(e.snippet.Text), "\n", " ")
	return renderItem(itemStyles, e.snippet.Name, preview, e.focused, width, nil, nil)
}

// Snippets lists saved prompt snippets. Enter inserts the selected one into
// the editor; n saves the current draft (passed in at construction) as a
// new snippet; x deletes the selected one.
type Snippets struct {
	com       *common.Common
	list      *list.FilterableList
	help      help.Model
	draftText string

	naming    bool
	nameInput textinput.Model

	keyMap struct {
		Next     key.Binding
		Previous key.Binding
		Select   key.Binding
		New      key.Binding
		Delete   key.Binding
		Close    key.Binding
	}
}

var _ Dialog = (*Snippets)(nil)

// NewSnippets creates a new Snippets dialog. draftText is the editor's
// current content at the time the dialog was opened, offered as the "save
// as snippet" source — empty if the editor was empty.
func NewSnippets(com *common.Common, snippets []Snippet, draftText string) *Snippets {
	d := &Snippets{com: com, draftText: draftText}

	items := make([]list.FilterableItem, 0, len(snippets))
	for i, s := range snippets {
		items = append(items, &snippetItem{Versioned: list.NewVersioned(), index: i, snippet: s, t: com})
	}
	d.list = list.NewFilterableList(items...)
	d.list.Focus()

	d.nameInput = textinput.New()
	d.nameInput.SetVirtualCursor(false)
	d.nameInput.Placeholder = com.Text("Snippet name...")
	d.nameInput.SetStyles(com.Styles.TextInput)

	h := help.New()
	h.Styles = com.Styles.DialogHelpStyles()
	d.help = h

	d.keyMap.Next = key.NewBinding(key.WithKeys("down", "ctrl+n"), key.WithHelp("↓", com.Text("next")))
	d.keyMap.Previous = key.NewBinding(key.WithKeys("up", "ctrl+p"), key.WithHelp("↑", com.Text("previous")))
	d.keyMap.Select = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "insert"))
	d.keyMap.New = key.NewBinding(key.WithKeys("n"), key.WithHelp("n", com.Text("save draft as snippet")))
	d.keyMap.Delete = key.NewBinding(key.WithKeys("x", "ctrl+x"), key.WithHelp("x", com.Text("delete")))
	d.keyMap.Close = CloseKey

	return d
}

// ID implements Dialog.
func (d *Snippets) ID() string {
	return SnippetsID
}

// HandleMsg implements Dialog.
func (d *Snippets) HandleMsg(msg tea.Msg) Action {
	if d.naming {
		return d.handleNamingMsg(msg)
	}
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	switch {
	case key.Matches(keyMsg, d.keyMap.Close):
		return ActionClose{}
	case key.Matches(keyMsg, d.keyMap.Previous):
		if d.list.IsSelectedFirst() {
			d.list.SelectLast()
		} else {
			d.list.SelectPrev()
		}
		d.list.ScrollToSelected()
	case key.Matches(keyMsg, d.keyMap.Next):
		if d.list.IsSelectedLast() {
			d.list.SelectFirst()
		} else {
			d.list.SelectNext()
		}
		d.list.ScrollToSelected()
	case key.Matches(keyMsg, d.keyMap.New):
		if d.draftText == "" {
			return nil
		}
		d.naming = true
		d.nameInput.Focus()
	case key.Matches(keyMsg, d.keyMap.Select):
		item := d.list.SelectedItem()
		if item == nil {
			return nil
		}
		se, ok := item.(*snippetItem)
		if !ok {
			return nil
		}
		return ActionInsertSnippet{Text: se.snippet.Text}
	case key.Matches(keyMsg, d.keyMap.Delete):
		item := d.list.SelectedItem()
		if item == nil {
			return nil
		}
		se, ok := item.(*snippetItem)
		if !ok {
			return nil
		}
		return ActionDeleteSnippet{Index: se.index}
	}
	return nil
}

func (d *Snippets) handleNamingMsg(msg tea.Msg) Action {
	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
		switch {
		case key.Matches(keyMsg, d.keyMap.Close):
			d.naming = false
			return nil
		case keyMsg.String() == "enter":
			name := strings.TrimSpace(d.nameInput.Value())
			if name == "" {
				return nil
			}
			d.naming = false
			d.nameInput.SetValue("")
			return ActionSaveSnippet{Name: name, Text: d.draftText}
		}
	}
	var cmd tea.Cmd
	d.nameInput, cmd = d.nameInput.Update(msg)
	if cmd != nil {
		return ActionCmd{cmd}
	}
	return nil
}

// Cursor implements Dialog.
func (d *Snippets) Cursor() *tea.Cursor {
	if d.naming {
		return InputCursor(d.com.Styles, d.nameInput.Cursor())
	}
	return nil
}

// Draw implements [Dialog].
func (d *Snippets) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := d.com.Styles
	width := max(0, min(defaultDialogMaxWidth, area.Dx()-t.Dialog.View.GetHorizontalBorderSize()))
	height := max(0, min(defaultDialogHeight, area.Dy()-t.Dialog.View.GetVerticalBorderSize()))
	innerWidth := width - t.Dialog.View.GetHorizontalFrameSize()

	rc := NewRenderContext(t, width)
	rc.Title = d.com.Text("Snippets")

	if d.naming {
		d.nameInput.SetWidth(max(0, innerWidth-1))
		rc.AddPart(t.Dialog.InputPrompt.Render(d.nameInput.View()))
	} else if len(d.list.FilteredItems()) == 0 {
		msg := d.com.Text("No saved snippets yet.")
		if d.draftText != "" {
			msg += d.com.Text(" Press n to save your current draft.")
		}
		rc.AddPart(t.Dialog.Sessions.RenamingingMessage.Render(msg))
	} else {
		listHeight, listTotalHeight, _ := sizeDialogList(t, d.list, innerWidth, height)
		bodyView := t.Dialog.List.Height(d.list.Height()).Render(d.list.Render())
		bodyView = joinScrollbar(t, bodyView, listHeight, listTotalHeight, listHeight, d.list.Offset())
		rc.AddPart(bodyView)
	}

	rc.Help = renderDialogHelp(t, &d.help, d, innerWidth)

	view := rc.Render()
	cur := d.Cursor()
	DrawCenterCursor(scr, area, view, cur)
	return cur
}

// ShortHelp implements [help.KeyMap].
func (d *Snippets) ShortHelp() []key.Binding {
	if d.naming {
		return []key.Binding{d.keyMap.Close}
	}
	return []key.Binding{d.keyMap.Previous, d.keyMap.Next, d.keyMap.Select, d.keyMap.New, d.keyMap.Delete, d.keyMap.Close}
}

// FullHelp implements [help.KeyMap].
func (d *Snippets) FullHelp() [][]key.Binding {
	return [][]key.Binding{d.ShortHelp()}
}
