package dialog

import (
	"strings"

	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	uv "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ultraviolet"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/help"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/key"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/textinput"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/common"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/list"
)

// SessionSearchID is the identifier for the content-based session search
// query dialog. Unlike the regular Sessions dialog (which fuzzy-filters
// already-loaded titles as you type), this searches message content on the
// server/DB, so it runs on submit rather than per keystroke.
const SessionSearchID = "session_search"

// ActionSearchSessions is emitted when the user submits a content search
// query; the UI model runs it (it has workspace access) and opens the
// results dialog.
type ActionSearchSessions struct {
	Query string
}

// SessionSearch is a single-line prompt for a session content search.
type SessionSearch struct {
	com   *common.Common
	input textinput.Model
	help  help.Model

	keyMap struct {
		Submit key.Binding
		Close  key.Binding
	}
}

var _ Dialog = (*SessionSearch)(nil)

// NewSessionSearch creates a new SessionSearch dialog.
func NewSessionSearch(com *common.Common) *SessionSearch {
	d := &SessionSearch{com: com}

	d.input = textinput.New()
	d.input.SetVirtualCursor(false)
	d.input.Placeholder = com.Text("Search all sessions by content...")
	d.input.SetStyles(com.Styles.TextInput)
	d.input.Focus()

	h := help.New()
	h.Styles = com.Styles.DialogHelpStyles()
	d.help = h

	d.keyMap.Submit = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", com.Text("search")))
	d.keyMap.Close = CloseKey

	return d
}

// ID implements Dialog.
func (d *SessionSearch) ID() string {
	return SessionSearchID
}

// HandleMsg implements Dialog.
func (d *SessionSearch) HandleMsg(msg tea.Msg) Action {
	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
		switch {
		case key.Matches(keyMsg, d.keyMap.Close):
			return ActionClose{}
		case key.Matches(keyMsg, d.keyMap.Submit):
			q := strings.TrimSpace(d.input.Value())
			if q == "" {
				return nil
			}
			return ActionSearchSessions{Query: q}
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
func (d *SessionSearch) Cursor() *tea.Cursor {
	return InputCursor(d.com.Styles, d.input.Cursor())
}

// Draw implements [Dialog].
func (d *SessionSearch) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := d.com.Styles
	width := max(0, min(defaultDialogMaxWidth, area.Dx()-t.Dialog.View.GetHorizontalBorderSize()))
	innerWidth := width - t.Dialog.View.GetHorizontalFrameSize()
	d.input.SetWidth(max(0, innerWidth-1))

	rc := NewRenderContext(t, width)
	rc.Title = d.com.Text("Search all sessions")
	rc.AddPart(t.Dialog.InputPrompt.Render(d.input.View()))
	rc.Help = renderDialogHelp(t, &d.help, d, innerWidth)

	view := rc.Render()
	cur := d.Cursor()
	DrawCenterCursor(scr, area, view, cur)
	return cur
}

// ShortHelp implements [help.KeyMap].
func (d *SessionSearch) ShortHelp() []key.Binding {
	return []key.Binding{d.keyMap.Submit, d.keyMap.Close}
}

// FullHelp implements [help.KeyMap].
func (d *SessionSearch) FullHelp() [][]key.Binding {
	return [][]key.Binding{d.ShortHelp()}
}

// SessionSearchResultsID is the identifier for the content-search results
// dialog.
const SessionSearchResultsID = "session_search_results"

type sessionSearchResultItem struct {
	*list.Versioned
	session session.Session
	t       *common.Common
	focused bool
}

var _ list.Item = &sessionSearchResultItem{}

func (e *sessionSearchResultItem) Finished() bool { return true }
func (e *sessionSearchResultItem) Filter() string { return e.session.Title }

func (e *sessionSearchResultItem) SetFocused(focused bool) {
	if e.focused == focused {
		return
	}
	e.focused = focused
	if e.Versioned != nil {
		e.Bump()
	}
}

func (e *sessionSearchResultItem) Render(width int) string {
	t := e.t.Styles
	itemStyles := ListItemStyles{
		ItemBlurred:     t.Dialog.NormalItem,
		ItemFocused:     t.Dialog.SelectedItem,
		InfoTextBlurred: t.Dialog.Sessions.InfoBlurred,
		InfoTextFocused: t.Dialog.Sessions.InfoFocused,
	}
	title := e.session.Title
	if title == "" {
		title = e.t.Text("Untitled")
	}
	return renderItem(itemStyles, title, "", e.focused, width, nil, nil)
}

// SessionSearchResults lists the sessions matched by a content search.
// Selecting one switches to it (via the existing ActionSelectSession, the
// same action the regular Sessions dialog emits).
type SessionSearchResults struct {
	com   *common.Common
	query string
	list  *list.FilterableList
	help  help.Model

	keyMap struct {
		Next     key.Binding
		Previous key.Binding
		Select   key.Binding
		Close    key.Binding
	}
}

var _ Dialog = (*SessionSearchResults)(nil)

// NewSessionSearchResults creates a new SessionSearchResults dialog.
func NewSessionSearchResults(com *common.Common, query string, sessions []session.Session) *SessionSearchResults {
	d := &SessionSearchResults{com: com, query: query}

	items := make([]list.FilterableItem, 0, len(sessions))
	for _, s := range sessions {
		items = append(items, &sessionSearchResultItem{Versioned: list.NewVersioned(), session: s, t: com})
	}
	d.list = list.NewFilterableList(items...)
	d.list.Focus()

	h := help.New()
	h.Styles = com.Styles.DialogHelpStyles()
	d.help = h

	d.keyMap.Next = key.NewBinding(key.WithKeys("down", "ctrl+n"), key.WithHelp("↓", com.Text("next")))
	d.keyMap.Previous = key.NewBinding(key.WithKeys("up", "ctrl+p"), key.WithHelp("↑", com.Text("previous")))
	d.keyMap.Select = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open"))
	d.keyMap.Close = CloseKey

	return d
}

// ID implements Dialog.
func (d *SessionSearchResults) ID() string {
	return SessionSearchResultsID
}

// HandleMsg implements Dialog.
func (d *SessionSearchResults) HandleMsg(msg tea.Msg) Action {
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
		se, ok := item.(*sessionSearchResultItem)
		if !ok {
			return nil
		}
		return ActionSelectSession{Session: se.session}
	}
	return nil
}

// Cursor implements Dialog. The results list has no text input.
func (d *SessionSearchResults) Cursor() *tea.Cursor {
	return nil
}

// Draw implements [Dialog].
func (d *SessionSearchResults) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := d.com.Styles
	width := max(0, min(defaultDialogMaxWidth, area.Dx()-t.Dialog.View.GetHorizontalBorderSize()))
	height := max(0, min(defaultDialogHeight, area.Dy()-t.Dialog.View.GetVerticalBorderSize()))
	innerWidth := width - t.Dialog.View.GetHorizontalFrameSize()

	rc := NewRenderContext(t, width)
	rc.Title = d.com.Text("Results for \"") + d.query + "\""

	if len(d.list.FilteredItems()) == 0 {
		rc.AddPart(t.Dialog.Sessions.RenamingingMessage.Render(d.com.Text("No sessions matched.")))
	} else {
		listHeight, listTotalHeight, _ := sizeDialogList(t, d.list, innerWidth, height)
		bodyView := t.Dialog.List.Height(d.list.Height()).Render(d.list.Render())
		bodyView = joinScrollbar(t, bodyView, listHeight, listTotalHeight, listHeight, d.list.Offset())
		rc.AddPart(bodyView)
	}

	rc.Help = renderDialogHelp(t, &d.help, d, innerWidth)

	view := rc.Render()
	DrawCenterCursor(scr, area, view, nil)
	return nil
}

// ShortHelp implements [help.KeyMap].
func (d *SessionSearchResults) ShortHelp() []key.Binding {
	return []key.Binding{d.keyMap.Previous, d.keyMap.Next, d.keyMap.Select, d.keyMap.Close}
}

// FullHelp implements [help.KeyMap].
func (d *SessionSearchResults) FullHelp() [][]key.Binding {
	return [][]key.Binding{d.ShortHelp()}
}
