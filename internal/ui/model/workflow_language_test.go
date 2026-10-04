package model

import (
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/i18n"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/stretchr/testify/require"
)

func TestWorkflowLanguagePreservesTaskContentAndIDs(t *testing.T) {
	t.Parallel()
	snapshot := engineering.WorkflowSnapshot{Revision: "revision", Paused: true, Tasks: []engineering.WorkflowTask{{ID: "task-id", Content: "Language", Status: "pending", OwnedPaths: []string{"D:\\Atlas"}}}}
	views := projectWorkflow(snapshot, "tr")
	row := views[workflowTasks][0]
	require.Equal(t, "task-id", row.ID)
	require.Equal(t, "paused", views[workflowAttention][0].ID)
	details := strings.Join(row.Details, "\n")
	require.Contains(t, details, "Atama: Language")
	require.Contains(t, details, "D:\\Atlas")
	require.Contains(t, details, "Görev: task-id")
	panel := workflowPanel{locale: i18n.New("tr"), snapshot: snapshot, views: views}
	require.Contains(t, panel.render(120, 40), "Ajan kontrolleri")
	panel.timeline = true
	require.Contains(t, panel.render(120, 40), "Sohbet zaman çizelgesi")
	require.Equal(t, "En fazla eşzamanlı ajan (1-16)", (workflowInput{mode: "team_limit"}).label("tr"))
}

func TestWorkflowLanguageValidationMessages(t *testing.T) {
	t.Parallel()
	for _, input := range []workflowInput{{mode: "queue"}, {mode: "team_limit", text: "17"}, {mode: "queue", text: "[missing"}, {mode: "queue", text: "[dep]"}, {mode: "budget", text: "{} {}"}} {
		_, english := input.control("en")
		_, turkish := input.control("tr")
		require.Error(t, english)
		require.Error(t, turkish)
		require.NotEqual(t, english.Error(), turkish.Error())
	}
	_, err := (workflowInput{mode: "budget", text: "{"}).control("tr")
	require.ErrorContains(t, err, "Geçersiz bütçe JSON")
	require.ErrorContains(t, err, "unexpected EOF")
}

func TestWorkflowLanguageDiscardsStaleProjectionWithoutChangingControls(t *testing.T) {
	t.Parallel()
	panel := workflowPanel{open: true, epoch: 2, locale: i18n.New("tr"), snapshot: engineering.WorkflowSnapshot{Revision: "latest"}, request: 7, loading: true, controlInFlight: true}
	m := &UI{workflow: panel, session: &session.Session{ID: "session"}}
	stale := workflowLanguageLoaded{epoch: 2, revision: "old", code: "tr"}
	handled, cmd := m.handleWorkflowPanel(stale)
	require.True(t, handled)
	require.Nil(t, cmd)
	result := workflowLanguageCmd(panel.snapshot, 2, "tr")().(workflowLanguageLoaded)
	m.handleWorkflowPanel(result)
	require.Equal(t, uint64(7), m.workflow.request)
	require.True(t, m.workflow.controlInFlight)
	require.True(t, m.workflow.loading)
}
