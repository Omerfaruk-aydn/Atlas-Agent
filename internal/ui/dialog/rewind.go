package dialog

import (
	"context"
	"fmt"

	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	uv "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ultraviolet"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/help"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/key"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session/rewind"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/common"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/list"
)

// RewindID is the identifier for the rewind checkpoint dialog.
const RewindID = "rewind"

type rewindMode uint8

const (
	// rewindModePicking shows the list of the session's user messages to
	// pick a checkpoint from.
	rewindModePicking rewindMode = iota
	// rewindModeConfirming shows (once loaded) how many files a rewind to
	// the picked checkpoint would write/delete, and asks for explicit
	// confirmation before anything on disk actually changes.
	rewindModeConfirming
	// rewindModeApplying is a brief terminal state while the fork + file
	// restore is in flight; all input is ignored until it resolves.
	rewindModeApplying
)

// Rewind lets the user fork the current session at an earlier user message
// and restore the working directory's files to their state as of that
// message. The fork is non-destructive: the current session is never
// modified, only copied up to the chosen point.
type Rewind struct {
	com  *common.Common
	help help.Model
	list *list.FilterableList
	mode rewindMode

	sessionID     string
	sourceTitle   string
	targetID      string
	targetPreview string

	previewLoading bool
	previewErr     error
	filesToWrite   int
	filesToDelete  int

	applyErr error

	keyMap struct {
		Select   key.Binding
		Next     key.Binding
		Previous key.Binding
		Confirm  key.Binding
		Cancel   key.Binding
		Close    key.Binding
	}
}

var _ Dialog = (*Rewind)(nil)

// NewRewind creates a new Rewind dialog listing sessionID's user messages
// as candidate checkpoints.
func NewRewind(com *common.Common, sessionID, sessionTitle string) (*Rewind, error) {
	msgs, err := com.Workspace.ListUserMessages(context.TODO(), sessionID)
	if err != nil {
		return nil, err
	}

	r := &Rewind{
		com:         com,
		sessionID:   sessionID,
		sourceTitle: sessionTitle,
		mode:        rewindModePicking,
	}

	h := help.New()
	h.Styles = com.Styles.DialogHelpStyles()
	r.help = h

	r.list = list.NewFilterableList(rewindItems(com.Styles, msgs)...)
	r.list.Focus()
	if len(msgs) > 0 {
		r.list.SelectLast()
		r.list.ScrollToSelected()
	}

	r.keyMap.Select = key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "select checkpoint"),
	)
	r.keyMap.Next = key.NewBinding(
		key.WithKeys("down", "ctrl+n"),
		key.WithHelp("↓", "next"),
	)
	r.keyMap.Previous = key.NewBinding(
		key.WithKeys("up", "ctrl+p"),
		key.WithHelp("↑", "previous"),
	)
	r.keyMap.Confirm = key.NewBinding(
		key.WithKeys("y", "enter"),
		key.WithHelp("y", "confirm"),
	)
	r.keyMap.Cancel = key.NewBinding(
		key.WithKeys("n", "esc"),
		key.WithHelp("n", "cancel"),
	)
	r.keyMap.Close = CloseKey

	return r, nil
}

// ID implements Dialog.
func (r *Rewind) ID() string {
	return RewindID
}

// rewindPreviewMsg delivers the async result of an rewind.Preview call.
// Private: only Rewind.HandleMsg ever needs to see it.
type rewindPreviewMsg struct {
	filesToWrite  int
	filesToDelete int
	err           error
}

// rewindAppliedMsg delivers the async result of a rewind.ForkAt call.
type rewindAppliedMsg struct {
	result rewind.Result
	err    error
}

func (r *Rewind) previewCmd() tea.Cmd {
	workspace := r.com.Workspace
	sessionID, targetID := r.sessionID, r.targetID
	return func() tea.Msg {
		written, deleted, err := workspace.RewindPreview(context.Background(), sessionID, targetID)
		return rewindPreviewMsg{filesToWrite: written, filesToDelete: deleted, err: err}
	}
}

func (r *Rewind) applyCmd() tea.Cmd {
	workspace := r.com.Workspace
	sessionID, targetID := r.sessionID, r.targetID
	return func() tea.Msg {
		result, err := workspace.RewindTo(context.Background(), sessionID, targetID)
		return rewindAppliedMsg{result: result, err: err}
	}
}

// HandleMsg implements Dialog.
func (r *Rewind) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch r.mode {
		case rewindModePicking:
			return r.handlePickingKey(msg)
		case rewindModeConfirming:
			return r.handleConfirmingKey(msg)
		case rewindModeApplying:
			// All input ignored while the apply is in flight.
			return nil
		}
	case rewindPreviewMsg:
		r.previewLoading = false
		r.previewErr = msg.err
		r.filesToWrite = msg.filesToWrite
		r.filesToDelete = msg.filesToDelete
	case rewindAppliedMsg:
		if msg.err != nil {
			r.mode = rewindModeConfirming
			r.applyErr = msg.err
			return nil
		}
		return ActionRewindApplied{Result: msg.result}
	}
	return nil
}

func (r *Rewind) handlePickingKey(msg tea.KeyPressMsg) Action {
	switch {
	case key.Matches(msg, r.keyMap.Close):
		return ActionClose{}
	case key.Matches(msg, r.keyMap.Previous):
		if r.list.IsSelectedFirst() {
			r.list.SelectLast()
		} else {
			r.list.SelectPrev()
		}
		r.list.ScrollToSelected()
	case key.Matches(msg, r.keyMap.Next):
		if r.list.IsSelectedLast() {
			r.list.SelectFirst()
		} else {
			r.list.SelectNext()
		}
		r.list.ScrollToSelected()
	case key.Matches(msg, r.keyMap.Select):
		item := r.list.SelectedItem()
		if item == nil {
			return nil
		}
		ri, ok := item.(*RewindItem)
		if !ok {
			return nil
		}
		r.targetID = ri.Message.ID
		r.targetPreview = ri.preview()
		r.mode = rewindModeConfirming
		r.previewLoading = true
		r.previewErr = nil
		return ActionCmd{r.previewCmd()}
	}
	return nil
}

func (r *Rewind) handleConfirmingKey(msg tea.KeyPressMsg) Action {
	// Any key dismisses a preview error back to picking; the failed
	// request can simply be retried by re-selecting the checkpoint.
	if r.previewErr != nil {
		r.mode = rewindModePicking
		r.previewErr = nil
		return nil
	}
	if r.previewLoading {
		return nil
	}
	switch {
	case key.Matches(msg, r.keyMap.Confirm):
		r.mode = rewindModeApplying
		r.applyErr = nil
		return ActionCmd{r.applyCmd()}
	case key.Matches(msg, r.keyMap.Cancel):
		r.mode = rewindModePicking
		r.applyErr = nil
	}
	return nil
}

// Cursor returns the cursor position relative to the dialog. The rewind
// dialog has no text input, so there is never a cursor to show.
func (r *Rewind) Cursor() *tea.Cursor {
	return nil
}

// Draw implements [Dialog].
func (r *Rewind) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := r.com.Styles
	width := max(0, min(defaultDialogMaxWidth, area.Dx()-t.Dialog.View.GetHorizontalBorderSize()))
	height := max(0, min(defaultDialogHeight, area.Dy()-t.Dialog.View.GetVerticalBorderSize()))
	innerWidth := width - t.Dialog.View.GetHorizontalFrameSize()

	rc := NewRenderContext(t, width)
	rc.Title = "Rewind"

	switch r.mode {
	case rewindModePicking:
		if len(r.list.FilteredItems()) == 0 {
			rc.AddPart(t.Dialog.Sessions.DeletingMessage.Render("No checkpoints yet — send a message first."))
			break
		}
		rc.AddPart(t.Dialog.Sessions.RenamingingMessage.Render("Pick a message to rewind to:"))
		listHeight, listTotalHeight, _ := sizeDialogList(t, r.list, innerWidth, height)
		bodyView := t.Dialog.List.Height(r.list.Height()).Render(r.list.Render())
		bodyView = joinScrollbar(t, bodyView, listHeight, listTotalHeight, listHeight, r.list.Offset())
		rc.AddPart(bodyView)
	case rewindModeConfirming:
		rc.TitleStyle = t.Dialog.Sessions.DeletingTitle
		rc.ViewStyle = t.Dialog.Sessions.DeletingView
		rc.AddPart(t.Dialog.Sessions.DeletingMessage.Render(r.confirmMessage()))
	case rewindModeApplying:
		rc.AddPart(t.Dialog.Sessions.RenamingingMessage.Render("Rewinding…"))
	}

	rc.Help = renderDialogHelp(t, &r.help, r, innerWidth)

	view := rc.Render()
	DrawCenterCursor(scr, area, view, nil)
	return nil
}

func (r *Rewind) confirmMessage() string {
	if r.previewErr != nil {
		return fmt.Sprintf("Could not preview this rewind: %v\n\nPress any key to go back.", r.previewErr)
	}
	if r.previewLoading {
		return "Checking what this rewind would change…"
	}
	if r.applyErr != nil {
		return fmt.Sprintf("Rewind failed: %v\n\nPress y to retry, n to go back.", r.applyErr)
	}
	return fmt.Sprintf(
		"Rewind to: %q\n\nThis creates a new session (\"Rewind: %s\") with the conversation up to that message, and restores %d file(s) to their content at that point. %d file(s) created after that point will be deleted. The current session is not changed.\n\nConfirm? (y/n)",
		r.targetPreview, r.sourceTitle, r.filesToWrite, r.filesToDelete,
	)
}

// ShortHelp implements [help.KeyMap].
func (r *Rewind) ShortHelp() []key.Binding {
	switch r.mode {
	case rewindModeConfirming:
		return []key.Binding{r.keyMap.Confirm, r.keyMap.Cancel}
	case rewindModeApplying:
		return nil
	default:
		return []key.Binding{r.keyMap.Previous, r.keyMap.Next, r.keyMap.Select, r.keyMap.Close}
	}
}

// FullHelp implements [help.KeyMap].
func (r *Rewind) FullHelp() [][]key.Binding {
	return [][]key.Binding{r.ShortHelp()}
}
