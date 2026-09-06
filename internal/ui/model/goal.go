package model

import (
	"context"
	"fmt"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-style/v2"
	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/common"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/dialog"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/util"
)

// A goal turns the session autonomous: it keeps taking turns towards
// what the user asked for instead of waiting to be told to carry on.
// The engine is in internal/agent/goal.go; this is the way in.
//
// The whole command is one line -- "/goal <what to reach>" -- because a
// goal is one sentence and a dialog to type one sentence into is a form
// standing between the user and the thing they already said.

// goalCommand is the command word, kept in one place so the parser and
// the length arithmetic below cannot drift apart.
const goalCommand = "/goal"

// goalStopWords end a run instead of setting one. "clear" matches what
// the rest of the UI calls it; "stop" is what people type.
var goalStopWords = []string{"clear", "stop", "off", "none"}

// handleShowGoal reports what the session is working towards. It is what
// picking /goal out of the command list does, and what a bare "/goal"
// does: with no argument there is nothing to set, so the useful answer
// is what is already set.
func (m *UI) handleShowGoal(msg dialog.ActionShowGoal) tea.Cmd {
	m.dialog.CloseDialog(dialog.CommandsID)

	sessionID := msg.SessionID
	if sessionID == "" && m.hasSession() {
		sessionID = m.session.ID
	}
	if sessionID == "" {
		return util.ReportInfo("No goal set. Type " + goalCommand + " followed by what this session should work towards.")
	}

	goal := m.currentGoal(sessionID)
	if goal == "" {
		return util.ReportInfo("No goal set. Type " + goalCommand + " followed by what this session should work towards.")
	}
	return util.ReportInfo("Working towards: " + goal + " — " + goalCommand + " clear stops it.")
}

// currentGoal returns the goal already set on the session, or empty.
func (m *UI) currentGoal(sessionID string) string {
	if sessionID == "" {
		return ""
	}
	sess, err := m.com.Workspace.GetSession(context.Background(), sessionID)
	if err != nil {
		return ""
	}
	return sess.Goal
}

// interceptGoalCommand handles "/goal ..." typed straight into the
// editor. Reports whether it consumed the prompt.
//
// This lives on the submit path rather than in the completions popup
// because the popup filters on a single word: by the time a goal has a
// space in it, the popup is no longer what is receiving the keystrokes.
func (m *UI) interceptGoalCommand(content string) (tea.Cmd, bool) {
	rest, ok := parseGoalCommand(content)
	if !ok {
		return nil, false
	}

	// A goal set on the welcome screen opens the session it belongs to,
	// the same way sending a prompt there does. Refusing here made the
	// command work after the first message and not before it, which
	// looks like a bug from the outside because it is one.
	loadCmd, err := m.ensureSession()
	if err != nil {
		return util.ReportError(err), true
	}
	sessionID := m.session.ID

	clearing := false
	for _, word := range goalStopWords {
		if strings.EqualFold(rest, word) {
			clearing = true
			break
		}
	}
	if clearing {
		rest = ""
	}

	// The command is consumed either way, so the editor is emptied
	// before the work happens rather than after it succeeds: leaving
	// "/goal ..." sitting in the prompt after acting on it would invite
	// sending it a second time.
	prevHeight := m.textarea.Height()
	m.textarea.SetValue("")
	heightCmd := m.handleTextareaHeightChange(prevHeight)

	prompt := rest
	return tea.Batch(loadCmd, heightCmd, func() tea.Msg {
		if err := m.com.Workspace.AgentSetGoal(context.Background(), sessionID, rest); err != nil {
			return goalSetMsg{err: err}
		}
		return goalSetMsg{prompt: prompt}
	}), true
}

// goalSetMsg reports the outcome of AgentSetGoal. Handled in ui.go's
// Update rather than folded into interceptGoalCommand's own closure
// because sending the prompt on afterwards means calling m.sendMessage,
// which mutates UI model fields (busy cache, generation counters) --
// safe from Update, not from the background goroutine a tea.Cmd runs on.
type goalSetMsg struct {
	prompt string
	err    error
}

// parseGoalCommand returns the argument to "/goal", and whether the line
// is that command at all.
//
// A bare "/goal" is not this path's business: it reports the goal rather
// than setting one, which the command list already does. And "/goal" has
// to be the whole word -- "/goalkeeper wins" is a sentence someone meant
// to send, not a command with an argument.
func parseGoalCommand(content string) (string, bool) {
	trimmed := strings.TrimSpace(content)
	if !strings.HasPrefix(trimmed, goalCommand) {
		return "", false
	}
	after := trimmed[len(goalCommand):]
	if after == "" {
		return "", false
	}
	if !isSpace(after[0]) {
		return "", false
	}
	rest := strings.TrimSpace(after)
	if rest == "" {
		return "", false
	}
	return rest, true
}

func isSpace(b byte) bool {
	return b == ' ' || b == '\t'
}

// goalInfo renders the sidebar's "Goal" section, in the same shape as
// filesInfo/jobsInfo/lspInfo beside it. It exists because the only other
// trace of a goal used to be a status-bar toast that clears itself after
// a few seconds -- and a confirmation that vanishes looks, from the
// outside, exactly like nothing having happened. The sidebar has room to
// hold the whole thing rather than the header's single truncated line.
func (m *UI) goalInfo(width int, isSection bool) string {
	t := m.com.Styles
	title := t.Resource.Heading.Render("Goal")
	if isSection {
		title = common.Section(t, title, width)
	}

	var goal string
	if m.session != nil {
		goal = strings.TrimSpace(m.session.Goal)
	}
	if goal == "" {
		body := t.Resource.AdditionalText.Render("None")
		return lipgloss.NewStyle().Width(width).Render(fmt.Sprintf("%s\n\n%s", title, body))
	}

	body := lipgloss.NewStyle().Width(width).Render(goal + " " + t.Resource.AdditionalText.Render("("+goalCommand+" clear stops it)"))
	return lipgloss.NewStyle().Width(width).Render(fmt.Sprintf("%s\n\n%s", title, body))
}
