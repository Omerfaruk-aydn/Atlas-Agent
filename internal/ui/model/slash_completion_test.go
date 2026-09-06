package model

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/common"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/dialog"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/styles"
	"github.com/stretchr/testify/require"
)

// slashCommandItems is what the "/" inline completion popup searches:
// it must offer the same commands the full Ctrl+P palette does, built
// fresh from the current session state rather than a stale snapshot.
func TestSlashCommandItemsOffersSystemCommands(t *testing.T) {
	sty := styles.AtlasPantera()
	ws := &modelManagementWorkspace{cfg: &config.Config{Options: &config.Options{}}}
	m := &UI{
		com:    &common.Common{Workspace: ws, Styles: &sty},
		dialog: dialog.NewOverlay(),
	}

	items := m.slashCommandItems()
	require.NotEmpty(t, items)

	var names, details []string
	for _, item := range items {
		names = append(names, item.Name)
		details = append(details, item.Detail)
		require.NotNil(t, item.Action, "every item must carry an action to dispatch on selection")
	}
	// The popup lists commands as they are typed, not by their palette
	// title, and glosses each one.
	require.Contains(t, names, "/new")
	require.Contains(t, details, "Start a new chat, clearing this one")
}
