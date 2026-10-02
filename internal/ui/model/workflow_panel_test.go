package model

import (
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ansi"
	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/stretchr/testify/require"
)

func TestWorkflowPanelResizeAndOldResponses(t *testing.T) {
	t.Parallel()
	p := workflowPanel{open: true, snapshot: engineering.WorkflowSnapshot{Tasks: []engineering.WorkflowTask{{ID: "task", Content: "Long task " + strings.Repeat("界", 80), Status: "pending"}}}}
	for _, size := range [][2]int{{80, 24}, {120, 40}, {40, 12}} {
		text := p.render(size[0], size[1])
		require.Contains(t, text, "s stop")
		require.Contains(t, text, "Esc close")
		require.LessOrEqual(t, len(strings.Split(text, "\n")), size[1])
		for _, line := range strings.Split(text, "\n") {
			require.LessOrEqual(t, ansi.StringWidth(line), size[0])
		}
	}
	m := &UI{session: &session.Session{ID: "session"}, workflow: workflowPanel{open: true, epoch: 2, request: 3, snapshot: engineering.WorkflowSnapshot{Revision: "current"}}}
	handled, cmd := m.handleWorkflowPanel(workflowPanelLoaded{epoch: 1, request: 2, sessionID: "session", snapshot: engineering.WorkflowSnapshot{Revision: "old"}})
	require.True(t, handled)
	require.Nil(t, cmd)
	require.Equal(t, "current", m.workflow.snapshot.Revision)
	handled, cmd = m.handleWorkflowPanel(tea.KeyPressMsg{Code: tea.KeyEscape})
	require.True(t, handled)
	require.Nil(t, cmd)
	require.False(t, m.workflow.open)
}
