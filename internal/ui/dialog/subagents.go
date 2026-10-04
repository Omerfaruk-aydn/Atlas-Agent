package dialog

import (
	"context"
	"fmt"
	"sort"

	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	uv "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ultraviolet"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/help"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/key"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/subagents"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/common"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/list"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/util"
)

// SubagentsID is the identifier for the subagents dialog.
const SubagentsID = "subagents"

// subagentEntry is one row: a discovered subagent definition (see
// internal/subagents and `atlas agent list`).
type subagentEntry struct {
	*list.Versioned
	sub     subagents.Subagent
	t       *common.Common
	focused bool
}

var _ list.Item = &subagentEntry{}

func (e *subagentEntry) Finished() bool { return true }

func (e *subagentEntry) Filter() string { return e.sub.Name }

func (e *subagentEntry) info() string {
	description := e.sub.Description
	if e.sub.Builtin {
		description = e.t.Text(firstSentence(description))
	}
	if e.sub.Model != "" {
		return description + " · " + e.sub.Model
	}
	return description
}

func (e *subagentEntry) SetFocused(focused bool) {
	if e.focused == focused {
		return
	}
	e.focused = focused
	if e.Versioned != nil {
		e.Bump()
	}
}

func (e *subagentEntry) Render(width int) string {
	t := e.t.Styles
	itemStyles := ListItemStyles{
		ItemBlurred:     t.Dialog.NormalItem,
		ItemFocused:     t.Dialog.SelectedItem,
		InfoTextBlurred: t.Dialog.Sessions.InfoBlurred,
		InfoTextFocused: t.Dialog.Sessions.InfoFocused,
	}
	return renderItem(itemStyles, e.sub.Name, e.info(), e.focused, width, nil, nil)
}

// Subagents lists every discovered subagent (see internal/subagents and
// `atlas agent list`): named, model-routable task definitions the
// agent/orchestrate/delegate/vibe tools can hand work to by name. From
// here one can be created, have its name/description/model role
// edited, have its instructions opened in $EDITOR, or be deleted --
// without hand editing or dropping to the CLI (`atlas agent new`).
type Subagents struct {
	com  *common.Common
	help help.Model
	list *list.FilterableList

	keyMap struct {
		Next     key.Binding
		Previous key.Binding
		Add      key.Binding
		Edit     key.Binding
		EditFile key.Binding
		Delete   key.Binding
		Close    key.Binding
	}
}

var _ Dialog = (*Subagents)(nil)

// NewSubagents creates a new Subagents dialog, listing what is
// currently discovered.
func NewSubagents(com *common.Common) *Subagents {
	d := &Subagents{com: com}
	d.list = list.NewFilterableList(d.buildItems()...)
	d.list.Focus()
	if d.list.Len() > 0 {
		d.list.SelectFirst()
	}

	h := help.New()
	h.Styles = com.Styles.DialogHelpStyles()
	d.help = h

	d.keyMap.Next = key.NewBinding(key.WithKeys("down", "ctrl+n"), key.WithHelp("↓", com.Text("next")))
	d.keyMap.Previous = key.NewBinding(key.WithKeys("up", "ctrl+p"), key.WithHelp("↑", com.Text("previous")))
	d.keyMap.Add = key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add"))
	d.keyMap.Edit = key.NewBinding(key.WithKeys("e"), key.WithHelp("e", com.Text("edit")))
	d.keyMap.EditFile = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", com.Text("edit instructions")))
	d.keyMap.Delete = key.NewBinding(key.WithKeys("x", "ctrl+x"), key.WithHelp("x", com.Text("delete")))
	d.keyMap.Close = CloseKey

	return d
}

func (d *Subagents) buildItems() []list.FilterableItem {
	discovered, err := d.com.Workspace.ListSubagents(context.TODO())
	if err != nil {
		return nil
	}
	sort.Slice(discovered, func(i, j int) bool { return discovered[i].Name < discovered[j].Name })

	items := make([]list.FilterableItem, 0, len(discovered))
	for _, sub := range discovered {
		items = append(items, &subagentEntry{Versioned: list.NewVersioned(), sub: sub, t: d.com})
	}
	return items
}

// Refresh rebuilds the list from disk -- called after a save or delete
// completes, or after returning from $EDITOR.
func (d *Subagents) Refresh() {
	selected := ""
	if item, ok := d.list.SelectedItem().(*subagentEntry); ok {
		selected = item.sub.Name
	}
	d.list.SetItems(d.buildItems()...)
	for i, item := range d.list.FilteredItems() {
		if e, ok := item.(*subagentEntry); ok && e.sub.Name == selected {
			d.list.SetSelected(i)
			return
		}
	}
	if d.list.Len() > 0 {
		d.list.SelectFirst()
	}
}

// ID implements Dialog.
func (d *Subagents) ID() string {
	return SubagentsID
}

// HandleMsg implements Dialog.
func (d *Subagents) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, d.keyMap.Close):
			return ActionClose{}
		case key.Matches(msg, d.keyMap.Previous):
			if d.list.IsSelectedFirst() {
				d.list.SelectLast()
			} else {
				d.list.SelectPrev()
			}
			d.list.ScrollToSelected()
		case key.Matches(msg, d.keyMap.Next):
			if d.list.IsSelectedLast() {
				d.list.SelectFirst()
			} else {
				d.list.SelectNext()
			}
			d.list.ScrollToSelected()
		case key.Matches(msg, d.keyMap.Add):
			return ActionOpenSubagentForm{}
		case key.Matches(msg, d.keyMap.Edit):
			entry, ok := d.selectedEntry()
			if !ok {
				return nil
			}
			return ActionOpenSubagentForm{ExistingName: entry.sub.Name, ExistingDescription: entry.sub.Description, ExistingModel: entry.sub.Model}
		case key.Matches(msg, d.keyMap.EditFile):
			entry, ok := d.selectedEntry()
			if !ok || entry.sub.Path == "" {
				return nil
			}
			return ActionEditSubagentFile{Name: entry.sub.Name, Path: entry.sub.Path}
		case key.Matches(msg, d.keyMap.Delete):
			entry, ok := d.selectedEntry()
			if !ok {
				return nil
			}
			return ActionCmd{d.deleteCmd(entry.sub.Name)}
		}
	case subagentDeletedMsg:
		if msg.err != nil {
			return ActionCmd{util.ReportError(fmt.Errorf(d.com.Text("failed to delete subagent %q: %w"), msg.name, msg.err))}
		}
		d.Refresh()
	}
	return nil
}

// subagentDeletedMsg delivers the async result of deleting a subagent.
type subagentDeletedMsg struct {
	name string
	err  error
}

func (d *Subagents) deleteCmd(name string) tea.Cmd {
	ws := d.com.Workspace
	return func() tea.Msg {
		err := ws.DeleteSubagent(context.Background(), name)
		return subagentDeletedMsg{name: name, err: err}
	}
}

func (d *Subagents) selectedEntry() (*subagentEntry, bool) {
	item := d.list.SelectedItem()
	if item == nil {
		return nil, false
	}
	entry, ok := item.(*subagentEntry)
	return entry, ok
}

// Cursor implements Dialog. The subagents list has no text input of its
// own; add/edit happen in a separate Arguments form, and instructions
// are edited in $EDITOR.
func (d *Subagents) Cursor() *tea.Cursor {
	return nil
}

// Draw implements [Dialog].
func (d *Subagents) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := d.com.Styles
	width := max(0, min(defaultDialogMaxWidth, area.Dx()-t.Dialog.View.GetHorizontalBorderSize()))
	height := max(0, min(defaultDialogHeight, area.Dy()-t.Dialog.View.GetVerticalBorderSize()))
	innerWidth := width - t.Dialog.View.GetHorizontalFrameSize()

	rc := NewRenderContext(t, width)
	rc.Title = d.com.Text("Subagents")

	if d.list.Len() == 0 {
		rc.AddPart(t.Dialog.Sessions.RenamingingMessage.Render(fmt.Sprintf(d.com.Text("No subagents configured yet. Press %s to create one."), d.keyMap.Add.Help().Key)))
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
func (d *Subagents) ShortHelp() []key.Binding {
	return []key.Binding{d.keyMap.Previous, d.keyMap.Next, d.keyMap.Add, d.keyMap.Edit, d.keyMap.EditFile, d.keyMap.Delete, d.keyMap.Close}
}

// FullHelp implements [help.KeyMap].
func (d *Subagents) FullHelp() [][]key.Binding {
	return [][]key.Binding{d.ShortHelp()}
}
