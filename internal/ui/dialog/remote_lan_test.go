package dialog

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/mobile"
	"github.com/stretchr/testify/require"
)

func TestRemoteHomeNetworkAvailableWithoutServer(t *testing.T) {
	t.Parallel()
	d, _ := newRemoteDialog(t, "tr")
	require.Contains(t, render(d, 110, 40), "Ev ağında dene")
	require.Equal(t, ActionRemoteLAN{}, d.run(rcLAN))
	d.ChooseHomeNetwork([]mobile.HomeNetworkAddress{{Interface: "Ethernet", Address: "192.168.1.2"}, {Interface: "VPN", Address: "10.0.0.2"}})
	require.Contains(t, render(d, 110, 40), "Ethernet · 192.168.1.2")
	require.Equal(t, ActionRemoteLAN{Address: "10.0.0.2"}, press(d, "down", "enter"))
	require.Equal(t, remoteMain, d.view)
	d.ChooseHomeNetwork([]mobile.HomeNetworkAddress{{Interface: "Ethernet", Address: "192.168.1.2"}})
	require.Nil(t, press(d, "esc"))
	require.Equal(t, remoteMain, d.view)
}

func TestRemoteHomeNetworkStartupCanBeStopped(t *testing.T) {
	t.Parallel()
	d, _ := newRemoteDialog(t, "tr")
	d.env.HomeLAN = func() bool { return true }
	d.env.Starting = func() bool { return true }
	require.Contains(t, render(d, 110, 40), "Ev ağı denemesini durdur")
	require.Equal(t, ActionStopMobile{}, d.run(rcLANStop))
	d.env.Starting = func() bool { return false }
	require.Contains(t, render(d, 110, 40), "Ev ağı denemesi durduruldu")
}
