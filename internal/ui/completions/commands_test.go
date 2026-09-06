package completions

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-style/v2"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/dialog"
	"github.com/stretchr/testify/require"
)

func TestSetCommandItemsOpensThePopup(t *testing.T) {
	t.Parallel()

	c := New(lipgloss.NewStyle(), lipgloss.NewStyle(), lipgloss.NewStyle())
	c.SetCommandItems([]CommandCompletionValue{
		{Name: "Session Mode", Action: dialog.ActionOpenDialog{DialogID: "modes"}},
		{Name: "Summarize Session", Action: dialog.ActionSummarize{SessionID: "s1"}},
	})

	require.True(t, c.IsOpen())
	require.Len(t, c.filtered, 2)
}

func TestSetCommandItemsFilterMatchesByLabel(t *testing.T) {
	t.Parallel()

	c := New(lipgloss.NewStyle(), lipgloss.NewStyle(), lipgloss.NewStyle())
	c.SetCommandItems([]CommandCompletionValue{
		{Name: "Session Mode", Action: dialog.ActionOpenDialog{DialogID: "modes"}},
		{Name: "Summarize Session", Action: dialog.ActionSummarize{SessionID: "s1"}},
	})

	c.Filter("summ")

	require.Len(t, c.filtered, 1)
	first, ok := c.filtered[0].(*CompletionItem)
	require.True(t, ok)
	require.Equal(t, "Summarize Session", first.Text())
}

// Selecting a command must hand back its own Action untouched, so the
// caller can dispatch it exactly as the full palette would -- not
// insert text the way a file or resource completion does.
func TestSelectCurrentReturnsTheCommandsAction(t *testing.T) {
	t.Parallel()

	c := New(lipgloss.NewStyle(), lipgloss.NewStyle(), lipgloss.NewStyle())
	action := dialog.ActionOpenDialog{DialogID: "modes"}
	c.SetCommandItems([]CommandCompletionValue{
		{Name: "Session Mode", Action: action},
	})

	msg := c.selectCurrent(false)
	sel, ok := msg.(SelectionMsg[CommandCompletionValue])
	require.True(t, ok, "expected SelectionMsg[CommandCompletionValue], got %T", msg)
	require.Equal(t, action, sel.Value.Action)
	require.False(t, c.IsOpen(), "selecting without KeepOpen must close the popup")
}
