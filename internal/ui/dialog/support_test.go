package dialog

import (
	"context"
	"errors"
	"image"
	"strings"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	uv "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ultraviolet"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/i18n"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/nodes"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/support"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/common"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/styles"
	"github.com/stretchr/testify/require"
)

type quietOverlay struct{}

func (quietOverlay) Start(string, string, func())            {}
func (quietOverlay) Operation(string, int, int, bool) func() { return func() {} }
func (quietOverlay) End()                                    {}

func supportCommon(lang string) *common.Common {
	sty := styles.AtlasPantera()
	sty.Locale = i18n.New(lang)
	ws := &modelRolesWorkspace{cfg: &config.Config{Options: &config.Options{}}}
	return &common.Common{Styles: &sty, Workspace: ws}
}

func renderSupport(d *Support) string {
	scr := uv.NewScreenBuffer(110, 60)
	d.Draw(scr, image.Rect(0, 0, 110, 60))
	return scr.String()
}

func supportPress(t *testing.T, d *Support, keys ...string) Action {
	t.Helper()
	var last Action
	for _, k := range keys {
		var msg tea.KeyPressMsg
		switch k {
		case "enter":
			msg = tea.KeyPressMsg{Code: tea.KeyEnter}
		case "esc":
			msg = tea.KeyPressMsg{Code: tea.KeyEscape}
		default:
			msg = tea.KeyPressMsg{Code: rune(k[0]), Text: k}
		}
		last = d.HandleMsg(msg)
	}
	return last
}

// runAction executes a dialog command the way the UI would and feeds its
// result back.
func runAction(t *testing.T, d *Support, a Action) {
	t.Helper()
	cmd, ok := a.(ActionCmd)
	require.True(t, ok, "expected a command, got %T", a)
	require.NotNil(t, cmd.Cmd)
	msg := cmd.Cmd()
	done, ok := msg.(SupportDone)
	require.True(t, ok, "expected SupportDone, got %T", msg)
	d.HandleMsg(done)
}

func TestSupportDialogWithoutLocalAtlasExplainsItself(t *testing.T) {
	t.Parallel()
	d := NewSupport(testCommon("tr"), nil)
	require.Contains(t, renderSupport(d), "Uzaktan destek yalnızca Atlas yerelde çalışırken kullanılabilir")
	require.IsType(t, ActionClose{}, supportPress(t, d, "esc"))
}

