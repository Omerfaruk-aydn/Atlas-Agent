package model

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ansiext"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ansi"
	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	uv "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ultraviolet"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/pubsub"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workspace"
)

type (
	workflowPanel struct {
		open           bool
		epoch, request uint64
		selected       int
		snapshot       engineering.WorkflowSnapshot
		err            string
		loading        bool
		assigning      bool
		role           string
	}
	workflowPanelLoaded struct {
		epoch, request uint64
		sessionID      string
		snapshot       engineering.WorkflowSnapshot
		err            error
	}
	workflowPanelTick struct{ epoch uint64 }
)

func workflowFetch(ws workspace.Workspace, id string, epoch, request uint64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		snapshot, err := ws.WorkflowSnapshot(ctx, id)
		return workflowPanelLoaded{epoch: epoch, request: request, sessionID: id, snapshot: snapshot, err: err}
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
	m.workflow.epoch++
	m.workflow.request++
	m.workflow.loading = true
	return tea.Batch(workflowFetch(m.com.Workspace, m.session.ID, m.workflow.epoch, m.workflow.request), workflowTick(m.workflow.epoch))
}

func (m *UI) handleWorkflowPanel(msg tea.Msg) (bool, tea.Cmd) {
	p := &m.workflow
	switch value := msg.(type) {
	case pubsub.Event[engineering.WorkflowChanged]:
		if p.open && m.session != nil && value.Payload.SessionID == m.session.ID {
			p.request++
			p.loading = true
			return true, workflowFetch(m.com.Workspace, m.session.ID, p.epoch, p.request)
		}
	case workflowPanelLoaded:
		if !p.open || m.session == nil || value.sessionID != m.session.ID || value.epoch != p.epoch || value.request != p.request {
			return true, nil
		}
		p.loading = false
		if value.err != nil {
			p.err = value.err.Error()
		} else {
			p.err = ""
			p.snapshot = value.snapshot
			p.selected = min(p.selected, max(0, len(p.snapshot.Tasks)-1))
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
		return true, tea.Batch(workflowFetch(m.com.Workspace, m.session.ID, p.epoch, p.request), workflowTick(p.epoch))
	case workspace.ConnectionEvent:
		if p.open && value.State == workspace.ConnectionRecovered {
			return false, m.openWorkflowPanel()
		}
	case tea.KeyPressMsg:
		if !p.open {
			return false, nil
		}
		key := value.String()
		if p.assigning {
			switch key {
			case "esc":
				p.assigning = false
			case "backspace":
				runes := []rune(p.role)
				p.role = string(runes[:max(0, len(runes)-1)])
			case "enter":
				if p.role != "" && p.selected < len(p.snapshot.Tasks) {
					p.assigning = false
					return true, m.workflowControlCmd(engineering.WorkflowControl{Action: "reassign", TaskID: p.snapshot.Tasks[p.selected].ID, Agent: p.role})
				}
			default:
				if len([]rune(key)) == 1 && len(p.role) < 128 {
					p.role += key
				}
			}
			return true, nil
		}
		switch key {
		case "a", "enter":
			if p.selected < len(p.snapshot.Tasks) && !p.loading {
				p.assigning = true
				p.role = p.snapshot.Tasks[p.selected].Agent
			}
		case "esc", "q":
			p.open = false
			p.epoch++
			return true, nil
		case "up", "k", "shift+tab":
			p.selected = max(0, p.selected-1)
		case "down", "j", "tab":
			p.selected = min(max(0, len(p.snapshot.Tasks)-1), p.selected+1)
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
	ws := m.com.Workspace
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := ws.WorkflowControl(ctx, id, control)
		var snapshot engineering.WorkflowSnapshot
		if err == nil {
			snapshot, err = ws.WorkflowSnapshot(ctx, id)
		}
		return workflowPanelLoaded{epoch: epoch, request: request, sessionID: id, snapshot: snapshot, err: err}
	}
}

func (p *workflowPanel) render(width, height int) string {
	if width < 1 || height < 1 {
		return ""
	}
	lines := []string{"Agent controls", "Esc close | s stop | p pause | r resume | a assign"}
	status := "running"
	if p.snapshot.Paused {
		status = "dispatch paused"
	}
	if !p.snapshot.Busy && !p.snapshot.Paused {
		status = "idle"
	}
	lines = append(lines, fmt.Sprintf("%s | stage %d | calls %d | tokens %d", status, p.snapshot.Stage, p.snapshot.Usage.ToolCalls, p.snapshot.Usage.Tokens))
	lines = append(lines, fmt.Sprintf("Limits: calls %d | tokens %d | $%.2f / $%.2f", p.snapshot.Limits.MaxToolCalls, p.snapshot.Limits.MaxTokens, p.snapshot.Usage.Cost, p.snapshot.Limits.MaxCost))
	active := 0
	for _, op := range p.snapshot.Operations {
		if op.Status == "running" || op.Status == "background" {
			active++
		}
	}
	if active > 0 {
		lines = append(lines, fmt.Sprintf("Unresolved operations: %d; stop requests cancellation", active))
	}
	if p.assigning {
		lines = append(lines, "Role: "+p.role+" | Enter apply, Esc cancel")
	}
	if p.err != "" {
		lines = append(lines, "Error: "+p.err)
	} else if p.loading && p.snapshot.Revision == "" {
		lines = append(lines, "Loading workflow…")
	}
	available := max(0, height-len(lines)-2)
	if p.selected < len(p.snapshot.Tasks) && height >= 24 {
		task := p.snapshot.Tasks[p.selected]
		lines = append(lines, "Depends on: "+strings.Join(task.DependsOn, ", "), "Owned paths: "+strings.Join(task.OwnedPaths, ", "))
		for _, run := range p.snapshot.Executions {
			if run.TaskID == task.ID {
				status := "verification pending"
				if run.Passed && run.TaskFingerprint == task.SpecFingerprint {
					status = "historical checks passed; source gates remain authoritative"
				}
				lines = append(lines, "Execution "+run.Agent+": "+status)
				break
			}
		}
		available = max(0, height-len(lines)-2)
	}
	start := max(0, p.selected-available+1)
	for i := start; i < len(p.snapshot.Tasks) && len(lines) < height-2; i++ {
		task := p.snapshot.Tasks[i]
		pointer := " "
		if i == p.selected {
			pointer = ">"
		}
		lines = append(lines, fmt.Sprintf("%s %s [%s] %s: %s", pointer, task.ID, task.Status, task.Agent, task.Content))
	}
	lines = append(lines, fmt.Sprintf("Findings %d | checks %d | checkpoints %d", len(p.snapshot.Findings), len(p.snapshot.Checks), len(p.snapshot.Checkpoints)), "Up/down select; pause blocks new agent dispatch")
	if len(lines) > height {
		lines = lines[:height]
	}
	for i, line := range lines {
		lines[i] = ansi.Truncate(ansiext.Escape(strings.ReplaceAll(strings.ReplaceAll(line, "\n", " "), "\r", " ")), width, "")
	}
	return strings.Join(lines, "\n")
}

func (m *UI) drawWorkflowPanel(scr uv.Screen, area uv.Rectangle) {
	style := m.com.Styles.Dialog.View
	text := style.Render(m.workflow.render(max(0, area.Dx()-style.GetHorizontalFrameSize()), max(0, area.Dy()-style.GetVerticalFrameSize())))
	uv.NewStyledString(text).Draw(scr, area)
}
