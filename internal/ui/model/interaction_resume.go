package model

import (
	"context"
	"time"

	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/util"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workspace"
)

type interactionResumedMsg struct {
	sessionID string
	resumed   bool
	// reason is the step the handoff left to the user.
	reason string
	// canceled means the user canceled the task while control was handed
	// to them; returning control must not restart it.
	canceled bool
	err      error
}

// resumeInteractionCmd uses the normal revision-checked workspace API.
// The command is issued only by a direct user action in the TUI.
func resumeInteractionCmd(ws workspace.Workspace, id string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		snapshot, err := ws.WorkflowSnapshot(ctx, id)
		if err != nil || !snapshot.Interactions.Paused {
			return interactionResumedMsg{sessionID: id, err: err}
		}
		err = ws.WorkflowControl(ctx, id, engineering.WorkflowControl{Action: "interaction_resume", ExpectedRevision: snapshot.Revision})
		return interactionResumedMsg{sessionID: id, resumed: err == nil, reason: snapshot.Interactions.Reason, canceled: snapshot.Interactions.Canceled, err: err}
	}
}

func (m *UI) resumeInteraction() tea.Cmd {
	if !m.hasSession() {
		return util.ReportInfo(m.com.Text("Open the paused conversation first"))
	}
	return resumeInteractionCmd(m.com.Workspace, m.session.ID)
}

func (m *UI) handleInteractionResumed(msg interactionResumedMsg) tea.Cmd {
	if msg.err != nil {
		return util.ReportError(msg.err)
	}
	if !msg.resumed {
		return util.ReportInfo(m.com.Text("Browser and desktop control is already active"))
	}
	if !m.hasSession() || m.session.ID != msg.sessionID {
		return util.ReportInfo(m.com.Text("Control resumed in the previous conversation; return there to continue"))
	}
	if msg.canceled {
		return util.ReportInfo(m.com.Text("Control returned. The task was canceled, so Atlas will not restart it"))
	}
	m.workflow.open = false
	return m.sendMessage(resumeContinuationPrompt(m.com.Text, msg.reason))
}

// resumeContinuationPrompt returns control without asserting that the
// manual step succeeded: the user only said control is back, so the agent
// must verify the step from a fresh observation.
func resumeContinuationPrompt(text func(string) string, reason string) string {
	prompt := text("I returned browser and desktop control to Atlas. Continue the previous task from a fresh observation of the current page or window.")
	if reason != "" {
		prompt += " " + text("The step that was left to me:") + " " + reason
	}
	return prompt + " " + text("Check whether that step is actually complete before relying on it; if it is not, tell me exactly what is still needed instead of assuming it succeeded.")
}
