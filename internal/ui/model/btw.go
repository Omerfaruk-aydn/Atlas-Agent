package model

import (
	"context"
	"strings"

	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/dialog"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/util"
)

// "/btw <question>" asks a side question. It is answered by a separate
// read-only agent (see internal/agent/btw.go) and shown in a panel that
// is dismissed with esc. Nothing about it is added to the session, and
// the main agent is not waiting on it, so it works while the main agent
// is busy.

// sideQuestionCommand is the command word.
const sideQuestionCommand = "/btw"

// sideQuestionState tracks the one side question in flight. Only one runs
// at a time: asking another replaces it.
type sideQuestionState struct {
	// gen identifies the current question, so an answer that arrives
	// after its panel was dismissed or replaced is dropped.
	gen    uint64
	cancel context.CancelFunc
}

// sideQuestionDoneMsg carries the outcome of a side question.
type sideQuestionDoneMsg struct {
	gen    uint64
	answer string
	model  string
	err    error
}

// parseSideQuestionCommand returns the question after "/btw", and whether
// the line is that command at all. A bare "/btw" is a command with no
// question, and "/btwx" is not the command.
func parseSideQuestionCommand(content string) (string, bool) {
	trimmed := strings.TrimSpace(content)
	if !strings.HasPrefix(trimmed, sideQuestionCommand) {
		return "", false
	}
	after := trimmed[len(sideQuestionCommand):]
	if after != "" && !isSpace(after[0]) {
		return "", false
	}
	return strings.TrimSpace(after), true
}

// handleShowSideQuestionHelp says how to ask one. Picking /btw from the
// command list has no question to ask.
func (m *UI) handleShowSideQuestionHelp() tea.Cmd {
	m.dialog.CloseDialog(dialog.CommandsID)
	return util.ReportInfo(m.com.Text("Type ") + sideQuestionCommand + m.com.Text(" followed by a question. It is answered on the side and never added to this conversation."))
}

// interceptSideQuestionCommand handles "/btw ..." typed into the editor.
// It reports whether it consumed the prompt.
func (m *UI) interceptSideQuestionCommand(content string) (tea.Cmd, bool) {
	question, ok := parseSideQuestionCommand(content)
	if !ok {
		return nil, false
	}
	if question == "" {
		return m.handleShowSideQuestionHelp(), true
	}
	return m.startSideQuestion(question), true
}

// startSideQuestion opens the panel and starts answering. A question
// already in flight is stopped first.
func (m *UI) startSideQuestion(question string) tea.Cmd {
	m.stopSideQuestion()
	m.dialog.CloseDialog(dialog.SideQuestionID)

	// Before the first message there is no session; the question is then
	// asked without conversation background rather than creating a
	// session just to hold nothing.
	sessionID := ""
	if m.hasSession() {
		sessionID = m.session.ID
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.sideQuestion.gen++
	m.sideQuestion.cancel = cancel
	gen := m.sideQuestion.gen

	m.dialog.OpenDialog(dialog.NewSideQuestion(m.com, question))

	ws := m.com.Workspace
	return func() tea.Msg {
		answer, model, err := ws.AgentAskSideQuestion(ctx, sessionID, question)
		return sideQuestionDoneMsg{gen: gen, answer: answer, model: model, err: err}
	}
}

// stopSideQuestion cancels the question in flight, if any, and makes its
// eventual answer stale.
func (m *UI) stopSideQuestion() {
	if m.sideQuestion.cancel != nil {
		m.sideQuestion.cancel()
		m.sideQuestion.cancel = nil
	}
	m.sideQuestion.gen++
}

// handleSideQuestionDone shows an answer in its panel.
func (m *UI) handleSideQuestionDone(msg sideQuestionDoneMsg) tea.Cmd {
	if msg.gen != m.sideQuestion.gen {
		return nil
	}
	m.sideQuestion.cancel = nil
	panel, ok := m.dialog.Dialog(dialog.SideQuestionID).(*dialog.SideQuestion)
	if !ok {
		return nil
	}
	if msg.err != nil {
		panel.SetError(msg.err)
		return nil
	}
	panel.SetAnswer(msg.answer, msg.model)
	return nil
}

// dismissSideQuestion closes the panel and stops the question if it is
// still being answered.
func (m *UI) dismissSideQuestion() {
	m.stopSideQuestion()
	m.dialog.CloseDialog(dialog.SideQuestionID)
}
