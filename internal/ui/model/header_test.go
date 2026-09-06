package model

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/common"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/styles"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workspace"
	"github.com/stretchr/testify/require"
)

// headerTestWorkspace supplies just enough of workspace.Workspace for
// renderHeaderDetails: a config to read model info from and a working
// directory to render. Everything else is left nil and must not be
// called by the code under test.
type headerTestWorkspace struct {
	workspace.Workspace
	cfg *config.Config
}

func (w *headerTestWorkspace) Config() *config.Config { return w.cfg }
func (w *headerTestWorkspace) WorkingDir() string      { return "/repo" }

// The only other trace of a goal used to be a status-bar toast that
// clears itself after a few seconds -- indistinguishable, once gone,
// from the goal never having been set. The header is where it has to
// stay visible instead.
func TestRenderHeaderDetailsShowsTheGoalUntilItIsCleared(t *testing.T) {
	sty := styles.AtlasPantera()
	com := &common.Common{
		Workspace: &headerTestWorkspace{cfg: &config.Config{Options: &config.Options{}}},
		Styles:    &sty,
	}

	withGoal := renderHeaderDetails(com, &session.Session{Goal: "ship the release"}, 0, false, 200, nil)
	require.Contains(t, withGoal, "ship the release")

	withoutGoal := renderHeaderDetails(com, &session.Session{}, 0, false, 200, nil)
	require.NotContains(t, withoutGoal, "🎯")
}

// A goal long enough to blow out the header is truncated to a preview
// rather than pushed off the end of the line entirely by the outer
// width truncation, so at least some of it always survives.
func TestRenderHeaderDetailsTruncatesALongGoal(t *testing.T) {
	sty := styles.AtlasPantera()
	com := &common.Common{
		Workspace: &headerTestWorkspace{cfg: &config.Config{Options: &config.Options{}}},
		Styles:    &sty,
	}

	long := "migrate every last consumer off the deprecated v1 endpoints before the freeze"
	got := renderHeaderDetails(com, &session.Session{Goal: long}, 0, false, 200, nil)
	require.Contains(t, got, "…")
	require.NotContains(t, got, long, "the full goal must not survive into the header verbatim")
}
