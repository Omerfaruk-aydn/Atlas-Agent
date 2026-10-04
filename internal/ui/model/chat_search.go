package model

import (
	"fmt"
	"strings"

	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/dialog"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/util"
)

// resetChatSearch clears search state, e.g. when a different session loads.
// A search's matches are message IDs from the previous session's list and
// would otherwise silently point at the wrong messages (or none) after a
// switch.
func (m *UI) resetChatSearch() {
	m.chatSearchQuery = ""
	m.chatSearchMatches = nil
	m.chatSearchIdx = 0
}

// openChatSearchDialog opens the chat search prompt, prefilled with the
// last query if there is one — pressing enter immediately then just jumps
// to the next match instead of retyping the same text.
func (m *UI) openChatSearchDialog() tea.Cmd {
	if !m.hasSession() {
		return nil
	}
	d := dialog.NewChatSearch(m.com)
	if m.chatSearchQuery != "" {
		d.SetValue(m.chatSearchQuery)
	}
	m.dialog.OpenDialog(d)
	return nil
}

// runChatSearch executes (or repeats) a search over the current session's
// messages and jumps the chat selection to a match. Repeating the same
// query advances to the next match (wrapping); a new query starts over.
func (m *UI) runChatSearch(query string) tea.Cmd {
	if query == m.chatSearchQuery && len(m.chatSearchMatches) > 0 {
		m.chatSearchIdx = (m.chatSearchIdx + 1) % len(m.chatSearchMatches)
	} else {
		m.chatSearchQuery = query
		m.chatSearchMatches = findChatMatches(m.sessionMessages, query)
		m.chatSearchIdx = 0
	}

	if len(m.chatSearchMatches) == 0 {
		return util.ReportWarn(fmt.Sprintf(m.com.Text("No matches for %q"), query))
	}

	id := m.chatSearchMatches[m.chatSearchIdx]
	idx, ok := m.chat.IndexForID(id)
	if !ok {
		return util.ReportWarn(fmt.Sprintf(m.com.Text("No matches for %q"), query))
	}
	m.chat.SetSelected(idx)
	var cmds []tea.Cmd
	cmds = append(cmds, m.chat.ScrollToSelected())
	cmds = append(cmds, util.ReportInfo(fmt.Sprintf(
		"Match %d/%d for %q", m.chatSearchIdx+1, len(m.chatSearchMatches), query,
	)))
	return tea.Batch(cmds...)
}

// findChatMatches returns the IDs of user/assistant messages whose text
// content contains query (case-insensitive), in session order.
func findChatMatches(msgs []message.Message, query string) []string {
	needle := strings.ToLower(query)
	var ids []string
	for _, msg := range msgs {
		if msg.Role != message.User && msg.Role != message.Assistant {
			continue
		}
		if strings.Contains(strings.ToLower(msg.Content().Text), needle) {
			ids = append(ids, msg.ID)
		}
	}
	return ids
}
