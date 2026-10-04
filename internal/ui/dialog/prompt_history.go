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

// PromptHistoryID is the identifier for the prompt-history search dialog
// (shell Ctrl+R style: fuzzy-search past prompts instead of walking them
// one at a time with up/down).
const PromptHistoryID = "prompt_history"

// ActionSelectPromptHistory is emitted when the user picks a past prompt to
// insert into the editor.
type ActionSelectPromptHistory struct {
	Text string
}

type promptHistoryItem struct {
	*list.Versioned
	text    string
	t       *common.Common
	focused bool
}

var _ list.Item = &promptHistoryItem{}

func (e *promptHistoryItem) Finished() bool { return true }
func (e *promptHistoryItem) Filter() string { return e.text }

func (e *promptHistoryItem) SetFocused(focused bool) {
	if e.focused == focused {
		return
	}
	e.focused = focused
	if e.Versioned != nil {
		e.Bump()
	}
}

func (e *promptHistoryItem) Render(width int) string {
	t := e.t.Styles
	itemStyles := ListItemStyles{
		ItemBlurred:     t.Dialog.NormalItem,
		ItemFocused:     t.Dialog.SelectedItem,
		InfoTextBlurred: t.Dialog.Sessions.InfoBlurred,
		InfoTextFocused: t.Dialog.Sessions.InfoFocused,
	}
	preview := strings.ReplaceAll(strings.TrimSpace(e.text), "\n", " ")
	return renderItem(itemStyles, preview, "", e.focused, width, nil, nil)
}

// PromptHistory is a fuzzy-searchable list of past prompts sent in the
// current session.
type PromptHistory struct {
	com   *common.Common
	list  *list.FilterableList
	input textinput.Model
	help  help.Model

	keyMap struct {
		Next     key.Binding
		Previous key.Binding
		Select   key.Binding
		Close    key.Binding
	}
}

var _ Dialog = (*PromptHistory)(nil)

// NewPromptHistory creates a new PromptHistory dialog. messages is the
// session's prompt history in chronological order (oldest first); it's
// shown most-recent-first so the freshest prompts are easiest to reach.
func NewPromptHistory(com *common.Common, messages []string) *PromptHistory {
	d := &PromptHistory{com: com}

	items := make([]list.FilterableItem, 0, len(messages))
	for i := len(messages) - 1; i >= 0; i-- {
		items = append(items, &promptHistoryItem{Versioned: list.NewVersioned(), text: messages[i], t: com})
	}
	d.list = list.NewFilterableList(items...)
	d.list.Focus()

	d.input = textinput.New()
	d.input.SetVirtualCursor(false)
	d.input.Placeholder = com.Text("Type to search past prompts...")
	d.input.SetStyles(com.Styles.TextInput)
	d.input.Focus()

	h := help.New()
	h.Styles = com.Styles.DialogHelpStyles()
	d.help = h

	d.keyMap.Next = key.NewBinding(key.WithKeys("down", "ctrl+n"), key.WithHelp("↓", com.Text("next")))
	d.keyMap.Previous = key.NewBinding(key.WithKeys("up", "ctrl+p"), key.WithHelp("↑", com.Text("previous")))
	d.keyMap.Select = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "insert"))
	d.keyMap.Close = CloseKey

	return d
}

// ID implements Dialog.
func (d *PromptHistory) ID() string {
	return PromptHistoryID
}

// HandleMsg implements Dialog.
func (d *PromptHistory) HandleMsg(msg tea.Msg) Action {
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
	case key.Matches(keyMsg, d.keyMap.Select):
		item := d.list.SelectedItem()
		if item == nil {
			return nil
		}
		pe, ok := item.(*promptHistoryItem)
		if !ok {
			return nil
		}
		return ActionSelectPromptHistory{Text: pe.text}
	default:
		prevValue := d.input.Value()
		var cmd tea.Cmd
		d.input, cmd = d.input.Update(msg)
		if d.input.Value() != prevValue {
			d.list.SetFilter(d.input.Value())
			d.list.ScrollToTop()
			d.list.SetSelected(0)
		}
		return ActionCmd{cmd}
	}
	return nil
}

// Cursor returns the cursor position relative to the dialog.
func (d *PromptHistory) Cursor() *tea.Cursor {
	return InputCursor(d.com.Styles, d.input.Cursor())
}

// Draw implements [Dialog].
func (d *PromptHistory) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := d.com.Styles
	width := max(0, min(defaultDialogMaxWidth, area.Dx()-t.Dialog.View.GetHorizontalBorderSize()))
	height := max(0, min(defaultDialogHeight, area.Dy()-t.Dialog.View.GetVerticalBorderSize()))
	innerWidth := width - t.Dialog.View.GetHorizontalFrameSize()

	d.input.SetWidth(max(0, innerWidth-1))

	rc := NewRenderContext(t, width)
	rc.Title = d.com.Text("Prompt history")
	rc.AddPart(t.Dialog.InputPrompt.Render(d.input.View()))

	if len(d.list.FilteredItems()) == 0 {
		rc.AddPart(t.Dialog.Sessions.RenamingingMessage.Render(d.com.Text("No matching prompts.")))
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
func (d *PromptHistory) ShortHelp() []key.Binding {
	return []key.Binding{d.keyMap.Previous, d.keyMap.Next, d.keyMap.Select, d.keyMap.Close}
}

// FullHelp implements [help.KeyMap].
func (d *PromptHistory) FullHelp() [][]key.Binding {
	return [][]key.Binding{d.ShortHelp()}
}
