package dialog

import (
	"fmt"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ansiext"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ansi"

	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	uv "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ultraviolet"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/help"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/key"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/shell"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/common"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/list"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/util"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workspace"
)

// JobsID is the identifier for the background jobs dialog.
const JobsID = "jobs"

// jobEntry is one row in the jobs dialog: either a running background shell
// job or an in-flight sub-agent run. Exactly one of Job/SubAgent is set.
type jobEntry struct {
	*list.Versioned
	Job      *shell.BackgroundShellInfo
	SubAgent *workspace.SubAgentRunInfo
	t        *common.Common
	focused  bool
}

var _ list.Item = &jobEntry{}

func (e *jobEntry) Finished() bool { return true }

// Filter returns the filterable value for this entry.
func (e *jobEntry) Filter() string { return e.title() }

func (e *jobEntry) title() string {
	if e.Job != nil {
		if e.Job.Command != "" {
			return e.Job.Command
		}
		return e.Job.ID
	}
	if e.SubAgent.Title != "" {
		return e.SubAgent.Title
	}
	return e.t.Text("Sub-agent")
}

func (e *jobEntry) startedAt() time.Time {
	if e.Job != nil {
		return e.Job.StartedAt
	}
	return e.SubAgent.StartedAt
}

// killTarget returns what Jobs.confirmKill needs to cancel this entry:
// (jobID, sessionID) — exactly one is non-empty.
func (e *jobEntry) killTarget() (jobID, sessionID string) {
	if e.Job != nil {
		return e.Job.ID, ""
	}
	return "", e.SubAgent.SessionID
}

func (e *jobEntry) SetFocused(focused bool) {
	if e.focused == focused {
		return
	}
	e.focused = focused
	if e.Versioned != nil {
		e.Bump()
	}
}

func (e *jobEntry) Render(width int) string {
	t := e.t.Styles
	itemStyles := ListItemStyles{
		ItemBlurred:     t.Dialog.NormalItem,
		ItemFocused:     t.Dialog.SelectedItem,
		InfoTextBlurred: t.Dialog.Sessions.InfoBlurred,
		InfoTextFocused: t.Dialog.Sessions.InfoFocused,
	}
	info := e.t.Styles.RelativeTime(e.startedAt())
	return renderItem(itemStyles, e.title(), info, e.focused, width, nil, nil)
}

// Jobs lists currently-running background shell jobs and sub-agent runs
// for the current session, with a key to cancel the selected one.
type Jobs struct {
	com          *common.Common
	help         help.Model
	list         *list.FilterableList
	outputID     string
	output       []string
	outputOffset int

	keyMap struct {
		Next     key.Binding
		Previous key.Binding
		Kill     key.Binding
		View     key.Binding
		Close    key.Binding
	}
}

var _ Dialog = (*Jobs)(nil)

// NewJobs creates a new Jobs dialog from the UI's already-memoized job and
// sub-agent state (no workspace probe — this dialog is opened from the
// sidebar, which keeps that state fresh; see background_jobs.go).
func NewJobs(com *common.Common, jobs []shell.BackgroundShellInfo, subAgents []workspace.SubAgentRunInfo) *Jobs {
	d := &Jobs{com: com}

	var items []list.FilterableItem
	for i := range jobs {
		items = append(items, &jobEntry{Versioned: list.NewVersioned(), Job: &jobs[i], t: com})
	}
	for i := range subAgents {
		items = append(items, &jobEntry{Versioned: list.NewVersioned(), SubAgent: &subAgents[i], t: com})
	}

	h := help.New()
	h.Styles = com.Styles.DialogHelpStyles()
	d.help = h

	d.list = list.NewFilterableList(items...)
	d.list.Focus()

	d.keyMap.Next = key.NewBinding(key.WithKeys("down", "ctrl+n"), key.WithHelp("↓", com.Text("next")))
	d.keyMap.Previous = key.NewBinding(key.WithKeys("up", "ctrl+p"), key.WithHelp("↑", com.Text("previous")))
	d.keyMap.Kill = key.NewBinding(key.WithKeys("x", "ctrl+x"), key.WithHelp("x", com.Text("cancel")))
	d.keyMap.View = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "output/session"))
	d.keyMap.Close = CloseKey

	return d
}

// ID implements Dialog.
func (d *Jobs) ID() string {
	return JobsID
}

// jobKilledMsg delivers the async result of a cancel action.
type jobKilledMsg struct {
	id  string
	err error
}

func (d *Jobs) killCmd(entry *jobEntry) tea.Cmd {
	workspace := d.com.Workspace
	jobID, sessionID := entry.killTarget()
	return func() tea.Msg {
		var err error
		if jobID != "" {
			err = workspace.BackgroundJobKill(jobID)
		} else {
			workspace.AgentCancel(sessionID)
		}
		return jobKilledMsg{id: jobID + sessionID, err: err}
	}
}

// HandleMsg implements Dialog.
func (d *Jobs) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case jobOutputMsg:
		if msg.id != d.outputID {
			return nil
		}
		d.output = msg.lines
		if msg.done {
			return nil
		}
		id := msg.id
		return ActionCmd{tea.Tick(time.Second, func(time.Time) tea.Msg { return jobOutputTick{id: id} })}
	case jobOutputTick:
		if msg.id == d.outputID {
			return ActionCmd{d.outputCmd(msg.id)}
		}
	case tea.KeyPressMsg:
		if d.outputID != "" {
			switch msg.String() {
			case "esc", "enter":
				d.outputID, d.output, d.outputOffset = "", nil, 0
			case "up", "k":
				d.outputOffset = max(0, d.outputOffset-1)
			case "down", "j":
				d.outputOffset++
			case "pgup":
				d.outputOffset = max(0, d.outputOffset-10)
			case "pgdown":
				d.outputOffset += 10
			case "x":
				if item, ok := d.list.SelectedItem().(*jobEntry); ok {
					return ActionCmd{d.killCmd(item)}
				}
			}
			return nil
		}
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
			entry, ok := item.(*jobEntry)
			if !ok {
				return nil
			}
			return ActionCmd{d.killCmd(entry)}
		case key.Matches(msg, d.keyMap.View):
			item := d.list.SelectedItem()
			if item == nil {
				return nil
			}
			entry, ok := item.(*jobEntry)
			if !ok {
				return nil
			}
			if entry.Job != nil {
				d.outputID, d.output = entry.Job.ID, []string{d.com.Text("Loading actual process output…")}
				return ActionCmd{d.outputCmd(entry.Job.ID)}
			}
			return ActionViewSession{SessionID: entry.SubAgent.SessionID}
		}
	case jobKilledMsg:
		if msg.err != nil {
			return ActionCmd{util.ReportError(fmt.Errorf(d.com.Text("failed to cancel: %w"), msg.err))}
		}
		d.removeByKillID(msg.id)
		if d.outputID == msg.id {
			d.outputID, d.output = "", nil
		}
	}
	return nil
}

func (d *Jobs) removeByKillID(id string) {
	var kept []list.FilterableItem
	for _, listItem := range d.list.FilteredItems() {
		item, ok := listItem.(list.FilterableItem)
		if !ok {
			continue
		}
		entry, ok := item.(*jobEntry)
		if !ok {
			continue
		}
		jobID, sessionID := entry.killTarget()
		if jobID+sessionID == id {
			continue
		}
		kept = append(kept, item)
	}
	d.list.SetItems(kept...)
}

// Cursor implements Dialog. The jobs dialog has no text input.
func (d *Jobs) Cursor() *tea.Cursor {
	return nil
}

// Draw implements [Dialog].
func (d *Jobs) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := d.com.Styles
	width := max(0, min(defaultDialogMaxWidth, area.Dx()-t.Dialog.View.GetHorizontalBorderSize()))
	height := max(0, min(defaultDialogHeight, area.Dy()-t.Dialog.View.GetVerticalBorderSize()))
	innerWidth := width - t.Dialog.View.GetHorizontalFrameSize()

	rc := NewRenderContext(t, width)
	rc.Title = d.com.Text("Jobs")

	if d.outputID != "" {
		rc.Title = "Process output | " + d.outputID
		available := max(0, height-t.Dialog.View.GetVerticalFrameSize()-4)
		offset := min(max(0, d.outputOffset), max(0, len(d.output)-available))
		lines := append([]string(nil), d.output[offset:min(len(d.output), offset+available)]...)
		for i, line := range lines {
			lines[i] = ansi.Truncate(ansiext.Escape(strings.ReplaceAll(line, "\r", " ")), max(0, innerWidth), "")
		}
		rc.AddPart(strings.Join(lines, "\n"))
		rc.AddPart("Esc back | arrows/PgUp/PgDown scroll | x cancel")
	} else if len(d.list.FilteredItems()) == 0 {
		rc.AddPart(t.Dialog.Sessions.RenamingingMessage.Render(d.com.Text("Nothing running.")))
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
func (d *Jobs) ShortHelp() []key.Binding {
	return []key.Binding{d.keyMap.Previous, d.keyMap.Next, d.keyMap.View, d.keyMap.Kill, d.keyMap.Close}
}

// FullHelp implements [help.KeyMap].
func (d *Jobs) FullHelp() [][]key.Binding {
	return [][]key.Binding{d.ShortHelp()}
}
