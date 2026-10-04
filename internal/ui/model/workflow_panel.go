package model

import (
	"context"
	"slices"
	"time"

	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	uv "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ultraviolet"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/history"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/i18n"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/pubsub"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/dialog"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workspace"
)

type (
	workflowPanel struct {
		locale                 *i18n.Translator
		open                   bool
		epoch, request         uint64
		selected               int
		snapshot               engineering.WorkflowSnapshot
		err                    string
		loading                bool
		timeline               bool
		details                bool
		tab                    int
		views                  workflowViews
		input                  workflowInput
		outputID               string
		output                 []string
		outputRequest          uint64
		detailOffset           int
		controlInFlight        bool
		diffLoading, diffDirty bool
	}
	workflowPanelLoaded struct {
		code           string
		epoch, request uint64
		sessionID      string
		snapshot       engineering.WorkflowSnapshot
		err            error
		views          workflowViews
	}
	workflowPanelTick struct{ epoch uint64 }
)

func workflowFetch(ws workspace.Workspace, id string, epoch, request uint64, codes ...string) tea.Cmd {
	code := "en"
	if len(codes) > 0 {
		code = codes[0]
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		snapshot, err := ws.WorkflowSnapshot(ctx, id)
		return workflowPanelLoaded{code: code, epoch: epoch, request: request, sessionID: id, snapshot: snapshot, views: projectWorkflow(snapshot, code), err: err}
	}
}

func workflowTick(epoch uint64) tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return workflowPanelTick{epoch: epoch} })
}

func (m *UI) openWorkflowPanel() tea.Cmd {
	if m.session == nil {
		return nil
	}
	m.workflow.open = true
	m.workflow.locale = m.com.Styles.Locale
	m.workflow.epoch++
	m.workflow.diffLoading, m.workflow.diffDirty = false, false
	m.workflow.request++
	m.workflow.loading = true
	return tea.Batch(workflowFetch(m.com.Workspace, m.session.ID, m.workflow.epoch, m.workflow.request, m.workflow.locale.Code()), workflowTick(m.workflow.epoch))
}

func (m *UI) handleWorkflowPanel(msg tea.Msg) (bool, tea.Cmd) {
	p := &m.workflow
	switch value := msg.(type) {
	case workflowLanguageLoaded:
		if p.open && value.epoch == p.epoch && value.revision == p.snapshot.Revision && value.code == p.locale.Code() {
			p.views = value.views
		}
		return true, nil
	case workflowFilesLoaded:
		if !p.open || m.session == nil || value.sessionID != m.session.ID || value.epoch != p.epoch {
			return true, nil
		}
		p.diffLoading = false
		if value.err != nil {
			p.err = m.com.Text("Diff: ") + value.err.Error()
		}
		if d, ok := m.dialog.Dialog(dialog.FileDiffID).(*dialog.FileDiff); ok {
			for _, entry := range value.entries {
				d.Refresh(entry)
			}
		} else if d, ok := m.dialog.Dialog(dialog.FilesID).(*dialog.Files); ok {
			d.Refresh(value.entries)
		} else if value.openRequested {
			m.dialog.OpenDialog(dialog.NewFiles(m.com, value.entries))
		}
		if p.diffDirty {
			p.diffDirty = false
			return true, m.workflowFilesCmd()
		}
		return true, nil
	case pubsub.Event[history.File]:
		if p.open && m.dialog != nil && (m.dialog.ContainsDialog(dialog.FilesID) || m.dialog.ContainsDialog(dialog.FileDiffID)) {
			known := m.session != nil && value.Payload.SessionID == m.session.ID
			for _, run := range p.snapshot.Runners {
				known = known || run.SessionID == value.Payload.SessionID
			}
			for _, run := range p.snapshot.Executions {
				known = known || slices.Contains(run.SessionIDs, value.Payload.SessionID)
			}
			if !known {
				return false, nil
			}
			if p.diffLoading {
				p.diffDirty = true
				return false, nil
			}
			return false, m.workflowFilesCmd()
		}
	case workflowOutputLoaded:
		row, selected := p.selectedRow()
		if !p.open || m.session == nil || value.sessionID != m.session.ID || value.epoch != p.epoch || value.request != p.outputRequest || !selected || row.ID != value.operationID {
			return true, nil
		}
		p.outputID, p.output = value.operationID, value.lines
		return true, nil
	case pubsub.Event[engineering.WorkflowChanged]:
		if p.open && m.session != nil && value.Payload.SessionID == m.session.ID {
			if p.controlInFlight {
				return true, nil
			}
			p.request++
			p.loading = true
			return true, workflowFetch(m.com.Workspace, m.session.ID, p.epoch, p.request, p.locale.Code())
		}
	case workflowPanelLoaded:
		if !p.open || m.session == nil || value.sessionID != m.session.ID || value.epoch != p.epoch || value.request != p.request {
			return true, nil
		}
		p.loading = false
		wasControl := p.controlInFlight
		p.controlInFlight = false
		if value.err != nil {
			p.err = value.err.Error()
		} else {
			if wasControl {
				p.err = ""
			}
			oldRow, hadSelection := p.selectedRow()
			p.snapshot = value.snapshot
			p.views = value.views
			count := len(p.rows())
			if p.timeline {
				count = len(workflowTimeline(p.snapshot))
			}
			p.selected = min(p.selected, max(0, count-1))
			if hadSelection && !p.timeline {
				for i, row := range p.rows() {
					if row.ID == oldRow.ID {
						p.selected = i
						break
					}
				}
			}
		}
		if value.err == nil && value.code != "" && value.code != p.locale.Code() {
			return true, workflowLanguageCmd(p.snapshot, p.epoch, p.locale.Code())
		}
		return true, nil
	case workflowPanelTick:
		if !p.open || value.epoch != p.epoch || m.session == nil {
			return true, nil
		}
		if p.loading {
			return true, workflowTick(p.epoch)
		}
		p.request++
		p.loading = true
		return true, tea.Batch(workflowFetch(m.com.Workspace, m.session.ID, p.epoch, p.request, p.locale.Code()), workflowTick(p.epoch), m.workflowOutputCmd())
	case workspace.ConnectionEvent:
		if p.open && value.State == workspace.ConnectionRecovered {
			return false, m.openWorkflowPanel()
		}
	case tea.KeyPressMsg:
		if !p.open || m.dialog != nil && m.dialog.HasDialogs() {
			return false, nil
		}
		key := value.String()
		if p.input.mode != "" {
			return true, m.handleWorkflowInput(value)
		}
		if handled, cmd := m.workflowWorkspaceKey(key); handled {
			return true, cmd
		}
		switch key {
		case "t":
			p.timeline = !p.timeline
			p.details = false
			p.selected = 0
		case "enter":
			if p.timeline {
				p.details = !p.details
				return true, nil
			}
			p.details = !p.details
			p.detailOffset = 0
			return true, m.workflowOutputCmd()
		case "pgup":
			p.detailOffset = max(0, p.detailOffset-10)
		case "pgdown":
			p.detailOffset += 10
		case "esc", "q":
			p.open = false
			p.epoch++
			return true, nil
		case "up", "k", "shift+tab":
			p.detailOffset = 0
			p.selected = max(0, p.selected-1)
		case "down", "j", "tab":
			p.detailOffset = 0
			count := len(p.rows())
			if p.timeline {
				count = len(workflowTimeline(p.snapshot))
			}
			p.selected = min(max(0, count-1), p.selected+1)
		case "s", "p", "r":
			if m.session == nil || p.snapshot.Revision == "" {
				return true, nil
			}
			action := "stop"
			if key == "p" {
				action = "pause"
			}
			if key == "r" {
				action = "resume"
			}
			return true, m.workflowControlCmd(engineering.WorkflowControl{Action: action})
		}
		return true, nil
	case tea.PasteMsg:
		if p.open && p.input.mode != "" && (m.dialog == nil || !m.dialog.HasDialogs()) {
			p.input.insert(value.Content)
			return true, nil
		}
	}
	return false, nil
}

func (m *UI) workflowControlCmd(control engineering.WorkflowControl) tea.Cmd {
	p := &m.workflow
	if m.session == nil || p.snapshot.Revision == "" || p.loading {
		return nil
	}
	id, epoch, request := m.session.ID, p.epoch, p.request+1
	control.ExpectedRevision = p.snapshot.Revision
	p.request, p.loading = request, true
	p.controlInFlight = true
	code := p.locale.Code()
	ws := m.com.Workspace
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := ws.WorkflowControl(ctx, id, control)
		var snapshot engineering.WorkflowSnapshot
		if err == nil {
			snapshot, err = ws.WorkflowSnapshot(ctx, id)
		}
		return workflowPanelLoaded{code: code, epoch: epoch, request: request, sessionID: id, snapshot: snapshot, views: projectWorkflow(snapshot, code), err: err}
	}
}

func (p *workflowPanel) render(width, height int) string {
	if width < 1 || height < 1 {
		return ""
	}
	if p.timeline {
		return p.renderTimeline(width, height)
	}
	return p.renderWorkspace(width, height)
}

func (m *UI) drawWorkflowPanel(scr uv.Screen, area uv.Rectangle) {
	style := m.com.Styles.Dialog.View
	text := style.Render(m.workflow.render(max(0, area.Dx()-style.GetHorizontalFrameSize()), max(0, area.Dy()-style.GetVerticalFrameSize())))
	uv.NewStyledString(text).Draw(scr, area)
}
