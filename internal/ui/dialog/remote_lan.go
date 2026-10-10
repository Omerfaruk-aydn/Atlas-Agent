package dialog

import (
	"fmt"
	"slices"

	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/key"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/mobile"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/remotecontrol"
)

func (d *Remote) homeLAN() bool { return d.env.HomeLAN != nil && d.env.HomeLAN() }

// ChooseHomeNetwork shows interface names instead of guessing between a
// home network, a VPN and a virtual machine adapter.
func (d *Remote) ChooseHomeNetwork(addresses []mobile.HomeNetworkAddress) {
	d.notice = remotecontrol.Message{}
	d.lanAddresses = slices.Clone(addresses)
	d.lanSelected, d.view = 0, remoteHomeNetwork
}

func (d *Remote) handleHomeNetwork(msg tea.KeyPressMsg) Action {
	switch {
	case key.Matches(msg, d.keyMap.Back):
		d.view, d.lanAddresses = remoteMain, nil
	case key.Matches(msg, d.keyMap.Up):
		d.lanSelected = max(0, d.lanSelected-1)
	case key.Matches(msg, d.keyMap.Down):
		d.lanSelected = min(len(d.lanAddresses)-1, d.lanSelected+1)
	case key.Matches(msg, d.keyMap.Enter):
		if d.lanSelected >= 0 && d.lanSelected < len(d.lanAddresses) {
			address := d.lanAddresses[d.lanSelected].Address
			d.view, d.lanAddresses = remoteMain, nil
			return ActionRemoteLAN{Address: address}
		}
	}
	return nil
}

func (d *Remote) homeNetworkLines(width int) ([]string, int) {
	t := d.com.Styles
	lines := wrapLines(t.Dialog.PrimaryText.Padding(0), d.com.Text("Choose the network your phone uses"), width)
	lines = append(lines, wrapLines(t.Dialog.SecondaryText.Padding(0), d.com.Text("Use your Ethernet or Wi-Fi adapter. Saved server settings stay unchanged."), width)...)
	lines = append(lines, "")
	focus := len(lines)
	for i, address := range d.lanAddresses {
		style := t.Dialog.NormalItem
		if i == d.lanSelected {
			focus, style = len(lines), t.Dialog.SelectedItem
		}
		lines = append(lines, style.Width(width).Render(fmt.Sprintf("%s · %s", address.Interface, address.Address)))
	}
	return lines, focus
}
