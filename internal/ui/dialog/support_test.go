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

// The owner's whole path in the dialog: menu, permissions, code, the
// verified request with its verification code, acceptance and ending the
// session, against a real supporting endpoint with its own identity.
func TestSupportDialogOwnerFlow(t *testing.T) {
	t.Parallel()
	svc := support.NewService(t.TempDir(), func() support.ServiceConfig {
		return support.ServiceConfig{Name: "Ofis-PC", DiscoveryPort: -1, LANListen: "127.0.0.1:0", Overlay: quietOverlay{}}
	}, nil)
	t.Cleanup(svc.Close)
	d := NewSupport(supportCommon("tr"), svc)

	view := renderSupport(d)
	for _, item := range []string{"Bu bilgisayara destek al", "Başka bilgisayara bağlan", "Aktif destek oturumları", "Dosya aktarımları", "Destek geçmişi", "Bağlantı ve gizlilik ayarları"} {
		require.Contains(t, view, item)
	}

	supportPress(t, d, "1")
	view = renderSupport(d)
	require.Contains(t, view, "Sistem bilgisi ve tanılama")
	require.Contains(t, view, "[x]")
	root := t.TempDir()
	d.fileRoots = func() ([]nodes.Root, error) {
		return []nodes.Root{{Label: "Work", Path: root}}, nil
	}
	runAction(t, d, supportPress(t, d, "enter"))
	h := svc.ActiveHost()
	require.NotNil(t, h)
	st := h.Status()
	require.NotNil(t, st.Offer)
	view = renderSupport(d)
	require.Contains(t, view, st.Offer.Code)
	require.Contains(t, view, "tek kullanımlık")

	c, err := support.NewController(context.Background(), support.ControllerOptions{StateDir: t.TempDir(), Name: "Destekçi", DiscoveryPort: -1})
	require.NoError(t, err)
	t.Cleanup(c.Close)
	joined := make(chan error, 1)
	go func() {
		_, err := c.Join(context.Background(), st.Offer.Code+"@"+h.LANAddress(), nil)
		joined <- err
	}()
	require.Eventually(t, func() bool { return h.Status().Pending != nil }, 10*time.Second, 10*time.Millisecond)
	d.HandleMsg(SupportTick{})
	pending := h.Status().Pending
	view = renderSupport(d)
	require.Contains(t, view, "Bağlantı isteği")
	require.Contains(t, view, "Destekçi")
	require.Contains(t, view, "doğrulanmamış")
	require.Contains(t, view, pending.Verification)

	runAction(t, d, supportPress(t, d, "a"))
	require.NoError(t, <-joined)
	d.HandleMsg(SupportTick{})
	view = renderSupport(d)
	require.Contains(t, view, "Bağlı: Destekçi")
	require.Contains(t, view, "Sistem bilgisi ve tanılama")

	// Pausing the task is separate from ending the session.
	runAction(t, d, supportPress(t, d, "p"))
	require.True(t, h.Status().Session.AgentPaused)
	require.Equal(t, support.StatePaused, h.Status().State)
	runAction(t, d, supportPress(t, d, "p"))

	supportPress(t, d, "x")
	require.Contains(t, renderSupport(d), "sonlandırılsın mı")
	runAction(t, d, supportPress(t, d, "y"))
	require.Equal(t, support.StateRevoked, h.Status().State)
	require.Nil(t, h.Status().Session)
	require.Eventually(t, func() bool { return c.Status().State.Terminal() }, 15*time.Second, 20*time.Millisecond)
}

func TestSupportDialogConnectRejectsMalformedCodes(t *testing.T) {
	t.Parallel()
	svc := support.NewService(t.TempDir(), func() support.ServiceConfig {
		return support.ServiceConfig{DiscoveryPort: -1, Overlay: quietOverlay{}}
	}, nil)
	t.Cleanup(svc.Close)
	d := NewSupport(supportCommon("en"), svc)
	supportPress(t, d, "2")
	for _, r := range "not-a-code!" {
		d.HandleMsg(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	require.Nil(t, supportPress(t, d, "enter"))
	require.Contains(t, strings.ToLower(renderSupport(d)), "code")
	require.Nil(t, svc.ActiveController(), "nothing connects for a malformed code")
}

func TestSupportDialogOfferErrorVisibleInShortTerminal(t *testing.T) {
	t.Parallel()
	svc := support.NewService(t.TempDir(), nil, nil)
	t.Cleanup(svc.Close)
	d := NewSupport(supportCommon("en"), svc)
	d.fileRoots = func() ([]nodes.Root, error) {
		return nil, errors.New("Drive discovery unavailable")
	}
	supportPress(t, d, "1", "3")
	runAction(t, d, supportPress(t, d, "enter"))
	scr := uv.NewScreenBuffer(80, 24)
	d.Draw(scr, image.Rect(0, 0, 80, 24))
	require.Contains(t, scr.String(), "Drive discovery unavailable")
	require.Nil(t, svc.ActiveHost(), "no code starts without the requested file access")
}

func TestSupportDialogEveryPresetCreatesCodeWithoutFolderInput(t *testing.T) {
	t.Parallel()
	for _, preset := range []string{"1", "2", "3"} {
		t.Run(preset, func(t *testing.T) {
			t.Parallel()
			svc := support.NewService(t.TempDir(), func() support.ServiceConfig {
				return support.ServiceConfig{DiscoveryPort: -1, LANListen: "127.0.0.1:0", Overlay: quietOverlay{}}
			}, nil)
			t.Cleanup(svc.Close)
			d := NewSupport(supportCommon("tr"), svc)
			root := t.TempDir()
			d.fileRoots = func() ([]nodes.Root, error) {
				return []nodes.Root{{Label: "Work", Path: root}}, nil
			}
			supportPress(t, d, "1", preset)
			view := renderSupport(d)
			require.NotContains(t, view, "Mutlak klasör yolu")
			require.NotContains(t, view, "o ekle")
			require.NotContains(t, view, "en az bir klasör")
			require.Contains(t, view, "klasör seçmen gerekmez")
			require.Nil(t, supportPress(t, d, "o"), "the removed folder shortcut does nothing")
			runAction(t, d, supportPress(t, d, "enter"))
			require.Equal(t, supportHost, d.view)
			st := svc.ActiveHost().Status()
			require.NotNil(t, st.Offer)
			target, err := support.ParseTarget(st.Offer.Link)
			require.NoError(t, err)
			require.Equal(t, st.Offer.Code, target.Code.String())
			require.Equal(t, svc.ActiveHost().LANAddress(), target.Address)
		})
	}
}

