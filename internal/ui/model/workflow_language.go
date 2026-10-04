package model

import (
	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/i18n"
)

type workflowLanguageLoaded struct {
	epoch          uint64
	revision, code string
	views          workflowViews
}

func workflowLanguageCmd(snapshot engineering.WorkflowSnapshot, epoch uint64, code string) tea.Cmd {
	return func() tea.Msg {
		return workflowLanguageLoaded{epoch: epoch, revision: snapshot.Revision, code: code, views: projectWorkflow(snapshot, code)}
	}
}

// Capture a locale for off-thread projections so a single view is consistent.
func workflowTranslator(codes ...string) func(string) string {
	code := "en"
	if len(codes) > 0 {
		code = codes[0]
	}
	return func(source string) string { return i18n.Text(code, source) }
}

// Translate known state labels without changing protocol or custom values.
func workflowStatus(text func(string) string, status string) string {
	switch status {
	case "pending", "in_progress", "completed", "failed", "blocked", "paused", "cancelled", "ready", "included", "excluded", "queued", "received", "uncertain", "idle", "running":
		return text(status)
	default:
		return status
	}
}
