package agent

import (
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/teams"
)

// newTeamRegistry builds the team board registry. Its messages are saved in SQLite
// under the data directory, so a board survives a restart of the process.
func newTeamRegistry(dataDir string) *teams.Registry {
	registry := teams.NewRegistry()
	if dataDir != "" {
		if err := registry.SetStoreDir(filepath.Join(dataDir, "teams")); err != nil {
			slog.Warn("Team board will not survive a restart", "error", err)
		}
	}
	return registry
}

// boardName is the handle a sub-agent takes on the team board: the name of
// the specialist, or the session title without its "agent session" suffix.
func boardName(params subAgentParams) string {
	if params.BoardName != "" {
		return params.BoardName
	}
	title := strings.TrimSpace(params.SessionTitle)
	if suffix := "agent session"; strings.HasSuffix(strings.ToLower(title), suffix) {
		title = strings.TrimSpace(title[:len(title)-len(suffix)])
	}
	if title == "" || strings.EqualFold(title, "New") {
		return "agent"
	}
	return title
}
