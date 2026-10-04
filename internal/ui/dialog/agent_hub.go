package dialog

import (
	"context"
	"fmt"

	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	uv "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ultraviolet"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/help"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/key"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/common"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/list"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/util"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workspace"
)

// AgentHubID is the identifier for the agent hub dialog.
const AgentHubID = "agent_hub"

// agentHubEntry is one row in the agent hub dialog: a sub-agent session
// spawned under the current top-level session, running or finished.
type agentHubEntry struct {
	*list.Versioned
	entry   workspace.AgentHubEntry
	t       *common.Common
	focused bool
}

var _ list.Item = &agentHubEntry{}

func (e *agentHubEntry) Finished() bool { return true }

// Filter returns the filterable value for this entry.
func (e *agentHubEntry) Filter() string { return e.title() }

func (e *agentHubEntry) title() string {
	if e.entry.Title != "" {
		return e.entry.Title
	}
	return e.t.Text("Sub-agent")
}

func (e *agentHubEntry) info() string {
	status := e.t.Text("idle")
	if e.entry.Busy {
		status = e.t.Text("running")
	}
	return fmt.Sprintf("%s · $%.4f · %s", status, e.entry.Cost, e.t.Styles.RelativeTime(e.entry.StartedAt))
}

func (e *agentHubEntry) SetFocused(focused bool) {
	if e.focused == focused {
		return
	}
	e.focused = focused
	if e.Versioned != nil {
		e.Bump()
	}
}

func (e *agentHubEntry) Render(width int) string {
	t := e.t.Styles
	itemStyles := ListItemStyles{
		ItemBlurred:     t.Dialog.NormalItem,
		ItemFocused:     t.Dialog.SelectedItem,
		InfoTextBlurred: t.Dialog.Sessions.InfoBlurred,
		InfoTextFocused: t.Dialog.Sessions.InfoFocused,
	}
	return renderItem(itemStyles, e.title(), e.info(), e.focused, width, nil, nil)
}

// AgentHub lists every sub-agent session spawned under the current
// top-level session, running or finished, with a key to jump into one
// and to cancel a still-running one. Unlike Jobs (currently in-flight
// runs only, alongside background shell jobs), this is the full history
// of sub-agent activity for the session.
type AgentHub struct {
	com  *common.Common
	help help.Model
	list *list.FilterableList

	keyMap struct {
		Next     key.Binding
		Previous key.Binding
		Kill     key.Binding
		View     key.Binding
		Close    key.Binding
	}
}

var _ Dialog = (*AgentHub)(nil)

// NewAgentHub creates a new AgentHub dialog, fetching the current set of
// sub-agent sessions for sessionID synchronously -- the same convention
// NewSessions uses, rather than the Jobs dialog's pre-memoized-poll
// approach, since Agent Hub is opened deliberately to inspect history
// rather than kept fresh in the background for a sidebar count.
func NewAgentHub(com *common.Common, sessionID string) *AgentHub {
	d := &AgentHub{com: com}

	entries := com.Workspace.AgentHubEntries(context.TODO(), sessionID)

	items := make([]list.FilterableItem, 0, len(entries))
	for _, entry := range entries {
		items = append(items, &agentHubEntry{Versioned: list.NewVersioned(), entry: entry, t: com})
	}

	h := help.New()
	h.Styles = com.Styles.DialogHelpStyles()
	d.help = h

	d.list = list.NewFilterableList(items...)
	d.list.Focus()
	if len(items) > 0 {
		d.list.SelectFirst()
	}

	d.keyMap.Next = key.NewBinding(key.WithKeys("down", "ctrl+n"), key.WithHelp("↓", com.Text("next")))
	d.keyMap.Previous = key.NewBinding(key.WithKeys("up", "ctrl+p"), key.WithHelp("↑", com.Text("previous")))
	d.keyMap.Kill = key.NewBinding(key.WithKeys("x", "ctrl+x"), key.WithHelp("x", com.Text("cancel")))
	d.keyMap.View = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", com.Text("view session")))
	d.keyMap.Close = CloseKey

	return d
}

// ID implements Dialog.
func (d *AgentHub) ID() string {
	return AgentHubID
}

// agentHubKilledMsg delivers the async result of a cancel action.
type agentHubKilledMsg struct {
	sessionID string
	err       error
}

func (d *AgentHub) killCmd(entry *agentHubEntry) tea.Cmd {
	workspace := d.com.Workspace
	sessionID := entry.entry.SessionID
	return func() tea.Msg {
		workspace.AgentCancel(sessionID)
		return agentHubKilledMsg{sessionID: sessionID}
	}
}

// HandleMsg implements Dialog.
func (d *AgentHub) HandleMsg(msg tea.Msg) Action {
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
		case key.Matches(msg, d.keyMap.Kill):
			item := d.list.SelectedItem()
			if item == nil {
				return nil
			}
			entry, ok := item.(*agentHubEntry)
			if !ok || !entry.entry.Busy {
				// Nothing to cancel on a sub-agent that already finished.
				return nil
			}
			return ActionCmd{d.killCmd(entry)}
		case key.Matches(msg, d.keyMap.View):
			item := d.list.SelectedItem()
			if item == nil {
				return nil
			}
			entry, ok := item.(*agentHubEntry)
			if !ok {
				return nil
			}
			return ActionViewSession{SessionID: entry.entry.SessionID}
		}
	case agentHubKilledMsg:
		if msg.err != nil {
			return ActionCmd{util.ReportError(fmt.Errorf(d.com.Text("failed to cancel: %w"), msg.err))}
		}
		d.markIdle(msg.sessionID)
	}
	return nil
}

// markIdle flips a cancelled entry's Busy flag off in place, so the
// dialog reflects the cancellation without a full re-fetch.
func (d *AgentHub) markIdle(sessionID string) {
	for _, listItem := range d.list.FilteredItems() {
		item, ok := listItem.(list.FilterableItem)
		if !ok {
			continue
		}
		entry, ok := item.(*agentHubEntry)
		if !ok || entry.entry.SessionID != sessionID {
			continue
		}
		entry.entry.Busy = false
		entry.Bump()
	}
}

// Cursor implements Dialog. The agent hub dialog has no text input.
func (d *AgentHub) Cursor() *tea.Cursor {
	return nil
}

// Draw implements [Dialog].
func (d *AgentHub) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := d.com.Styles
	width := max(0, min(defaultDialogMaxWidth, area.Dx()-t.Dialog.View.GetHorizontalBorderSize()))
	height := max(0, min(defaultDialogHeight, area.Dy()-t.Dialog.View.GetVerticalBorderSize()))
	innerWidth := width - t.Dialog.View.GetHorizontalFrameSize()

	rc := NewRenderContext(t, width)
	rc.Title = d.com.Text("Agent Hub")

	if len(d.list.FilteredItems()) == 0 {
		rc.AddPart(t.Dialog.Sessions.RenamingingMessage.Render(d.com.Text("No sub-agents have run in this session yet.")))
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
func (d *AgentHub) ShortHelp() []key.Binding {
	return []key.Binding{d.keyMap.Previous, d.keyMap.Next, d.keyMap.View, d.keyMap.Kill, d.keyMap.Close}
}

// FullHelp implements [help.KeyMap].
func (d *AgentHub) FullHelp() [][]key.Binding {
	return [][]key.Binding{d.ShortHelp()}
}
