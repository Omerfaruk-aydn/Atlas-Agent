package dialog

import (
	"context"
	"image"
	"strings"
	"testing"

	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	uv "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ultraviolet"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/i18n"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/mobile"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/common"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/styles"
	"github.com/stretchr/testify/require"
)

// idleBackend answers only what the gateway asks without phone requests.
type idleBackend struct{ mobile.Backend }

func (idleBackend) ListSessions(context.Context) ([]session.Session, error) { return nil, nil }
func (idleBackend) AgentIsSessionBusy(string) bool                          { return false }

func TestMobileDialogAnnouncesRenewedCertificate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	gw, err := mobile.Start(t.Context(), mobile.Options{StateDir: dir, Listen: "127.0.0.1:0", LAN: true, Backend: idleBackend{}})
	require.NoError(t, err)
	previous := gw.Fingerprint()
	require.NoError(t, gw.Close())
	gw, err = mobile.Start(t.Context(), mobile.Options{StateDir: dir, Listen: "127.0.0.1:0", LAN: true, Hosts: []string{"ev-pc.tail1234.ts.net"}, Backend: idleBackend{}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = gw.Close() })
	require.Equal(t, previous, gw.RenewedFrom())

	sty := styles.AtlasPantera()
	sty.Locale = i18n.New("tr")
	d := NewMobile(&common.Common{Styles: &sty}, gw)
	scr := uv.NewScreenBuffer(120, 60)
	d.Draw(scr, image.Rectangle{Max: image.Point{X: 120, Y: 60}})
	require.Contains(t, scr.String(), "Sertifika yenilendi")
	require.Contains(t, scr.String(), shortFingerprint(previous))
	require.Contains(t, d.url, "https://ev-pc.tail1234.ts.net:", "the QR uses the configured name")
}

func TestMobileDialogPairsOnlyAfterDesktopAcceptance(t *testing.T) {
	t.Parallel()
	gw, err := mobile.Start(t.Context(), mobile.Options{StateDir: t.TempDir(), Listen: "127.0.0.1:0", Backend: idleBackend{}, Language: "tr"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = gw.Close() })
	sty := styles.AtlasPantera()
	sty.Locale = i18n.New("tr")
	d := NewMobile(&common.Common{Styles: &sty}, gw)

	for _, size := range []image.Point{{X: 120, Y: 60}, {X: 80, Y: 24}, {X: 30, Y: 10}, {X: 0, Y: 0}} {
		scr := uv.NewScreenBuffer(size.X, size.Y)
		require.NotPanics(t, func() { d.Draw(scr, image.Rectangle{Max: size}) })
	}
	big := uv.NewScreenBuffer(120, 60)
	d.Draw(big, image.Rectangle{Max: image.Point{X: 120, Y: 60}})
	require.Contains(t, big.String(), "████████", "a large terminal shows the QR code")
	small := uv.NewScreenBuffer(80, 24)
	d.Draw(small, image.Rectangle{Max: image.Point{X: 80, Y: 24}})
	require.NotContains(t, small.String(), "████████", "a QR that does not fit is replaced by the address")
	require.Contains(t, small.String(), "https://")

	// A phone scans the QR: the request appears with its code and is not
	// paired until accepted here.
	nonce := d.url[strings.Index(d.url, "#pair=")+len("#pair="):]
	pending, _, err := gw.Auth.Redeem(nonce, "Pixel 8", "test", "127.0.0.1")
	require.NoError(t, err)
	require.NotNil(t, d.HandleMsg(mobileTick{}).(ActionCmd).Cmd, "the tick keeps refreshing")
	require.Len(t, d.rows, 1)
	d.Draw(big, image.Rectangle{Max: image.Point{X: 120, Y: 60}})
	require.Contains(t, big.String(), pending.Code[:3]+" "+pending.Code[3:])
	require.Empty(t, gw.Auth.Devices())

	action := d.HandleMsg(tea.KeyPressMsg{Code: 'a', Text: "a"})
	require.IsType(t, ActionCmd{}, action)
	require.Len(t, gw.Auth.Devices(), 1)
	require.Nil(t, d.rows[0].pending)

	// Unpairing removes the phone; s asks the UI to stop the server.
	d.HandleMsg(tea.KeyPressMsg{Code: 'd', Text: "d"})
	require.Empty(t, gw.Auth.Devices())
	require.Equal(t, ActionStopMobile{}, d.HandleMsg(tea.KeyPressMsg{Code: 's', Text: "s"}))
}
