package dialog

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/common"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/styles"
	"github.com/stretchr/testify/require"
)

func newTestSideQuestion(t *testing.T) *SideQuestion {
	t.Helper()
	sty := styles.AtlasPantera()
	return NewSideQuestion(&common.Common{Styles: &sty}, "what does it do?")
}

func TestSideQuestionLifecycle(t *testing.T) {
	t.Parallel()

	d := newTestSideQuestion(t)
	require.Equal(t, SideQuestionID, d.ID())
	require.False(t, d.Done())

	waiting := strings.Join(d.body(60), "\n")
	require.Contains(t, waiting, "what does it do?")
	require.Contains(t, waiting, "Thinking")

	d.SetAnswer("It parses **tokens**.", "some-model")
	require.True(t, d.Done())
	answered := strings.Join(d.body(60), "\n")
	require.Contains(t, answered, "tokens")
	require.NotContains(t, answered, "Thinking")

	d.SetError(errors.New("provider down"))
	require.Contains(t, strings.Join(d.body(60), "\n"), "provider down")
}

func TestSideQuestionKeys(t *testing.T) {
	t.Parallel()

	d := newTestSideQuestion(t)
	require.IsType(t, ActionDismissSideQuestion{}, d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEscape}))

	require.Nil(t, d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyDown}))
	require.Equal(t, 1, d.yOffset)
	require.Nil(t, d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyUp}))
	require.Nil(t, d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyUp}))
	require.Equal(t, 0, d.yOffset, "scrolling must not go above the top")
}
