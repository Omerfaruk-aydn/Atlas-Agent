package model

import (
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ansi"
	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/dialog"
	"github.com/stretchr/testify/require"
)

func TestWorkflowViewsExposeRealBlocksAndKeepUnicodeWithinTerminal(t *testing.T) {
	t.Parallel()
	s := engineering.WorkflowSnapshot{Revision: "r", AgentLimit: 2, Tasks: []engineering.WorkflowTask{{ID: "base", Status: "in_progress", OwnedPaths: []string{"api"}}, {ID: "dependent", Content: strings.Repeat("界", 100), Status: "pending", DependsOn: []string{"base"}, OwnedPaths: []string{"api"}}}, Board: engineering.ControlBoard{HeldTasks: []string{"dependent"}}, Operations: []engineering.Operation{{ID: "op", TaskID: "base", Tool: "bash", Status: "running"}}}
	views := projectWorkflow(s)
	require.Contains(t, views[workflowGraph][1].Label, "waiting for base")
	require.Contains(t, views[workflowGraph][1].Label, "held by user")
	require.Contains(t, strings.Join(views[workflowOwnership][1].Details, " "), "Overlap with base")
	require.NotEmpty(t, views[workflowAttention])
	p := workflowPanel{snapshot: s, views: views, open: true}
	for tab := range workflowTabCount {
		p.tab = tab
		for _, size := range [][2]int{{120, 40}, {80, 24}, {40, 12}, {8, 2}, {1, 1}} {
			for _, details := range []bool{false, true} {
				p.details = details
				text := p.render(size[0], size[1])
				require.LessOrEqual(t, len(strings.Split(text, "\n")), size[1])
				for _, line := range strings.Split(text, "\n") {
					require.LessOrEqual(t, ansi.StringWidth(line), size[0])
				}
			}
		}
	}
}

func TestWorkflowContextAndBatchViewsUsePersistedRows(t *testing.T) {
	t.Parallel()
	views := projectWorkflow(engineering.WorkflowSnapshot{Context: engineering.ContextManifest{Model: "configured", EstimatedTokens: 100, Entries: []engineering.ContextEntry{{ID: "result", Kind: "tool-result", Name: "Result", EstimatedTokens: 20, Excluded: true}}}, Batches: []engineering.AgentBatchReport{{ID: "audit", Rows: []engineering.AgentBatchRow{{AgentBatchItem: engineering.AgentBatchItem{ID: "module", Input: "internal/config"}, Status: "failed", Attempts: 1, Output: "failure"}}}}})
	require.Len(t, views[workflowContext], 2)
	require.Contains(t, views[workflowContext][1].Label, "excluded")
	require.Equal(t, "result", views[workflowContext][1].ID)
	require.Len(t, views[workflowBatches], 2)
	require.Contains(t, views[workflowBatches][1].Label, "failed")
	require.Contains(t, views[workflowBatches][1].Details, "failure")
}

func TestWorkflowInputTargetsTaskAndParsesQueueDependencies(t *testing.T) {
	t.Parallel()
	i := workflowInput{mode: "queue", task: "ui", text: "[api,contract] Implement Türkçe界"}
	control, err := i.control()
	require.NoError(t, err)
	require.Equal(t, "ui", control.TaskID)
	require.Equal(t, []string{"api", "contract"}, control.DependsOn)
	require.Equal(t, "Implement Türkçe界", control.Text)
	i = workflowInput{text: "Ö界", cursor: 1}
	i.insert("ğ")
	require.Equal(t, "Öğ界", i.text)
	_, err = (workflowInput{mode: "budget", text: `{"max_tokens":10} {}`}).control()
	require.Error(t, err)
	_, err = (workflowInput{mode: "budget", text: `{"unknown":1}`}).control()
	require.Error(t, err)
}

func TestWorkflowDialogPriorityAndStaleOutputGuard(t *testing.T) {
	t.Parallel()
	m := &UI{session: &session.Session{ID: "session"}, dialog: dialog.NewOverlay(), workflow: workflowPanel{open: true, epoch: 2, tab: workflowOperations, outputRequest: 3, views: workflowViews{workflowOperations: {{ID: "op"}}}}}
	m.dialog.OpenDialog(&workflowTestDialog{})
	handled, _ := m.handleWorkflowPanel(tea.KeyPressMsg{Code: 's', Text: "s"})
	require.False(t, handled)
	handled, _ = m.handleWorkflowPanel(workflowOutputLoaded{epoch: 1, request: 3, sessionID: "session", operationID: "op", lines: []string{"stale"}})
	require.True(t, handled)
	require.Empty(t, m.workflow.output)
	handled, _ = m.handleWorkflowPanel(workflowOutputLoaded{epoch: 2, request: 3, sessionID: "session", operationID: "op", lines: []string{"observed"}})
	require.True(t, handled)
	require.Equal(t, []string{"observed"}, m.workflow.output)
}

type workflowTestDialog struct{ dialog.Dialog }

func (*workflowTestDialog) ID() string { return "workflow-test" }
