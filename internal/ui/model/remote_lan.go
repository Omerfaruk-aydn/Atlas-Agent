package model

import (
	"fmt"
	"net"
	"path/filepath"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/mobile"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/remotecontrol"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/dialog"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/util"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/version"
)

type homeNetworkMsg struct {
	generation uint64
	addresses  []mobile.HomeNetworkAddress
	err        error
}

func (m *UI) discoverHomeNetwork() tea.Cmd {
	var init tea.Cmd
	if m.remoteDialog() == nil {
		init = m.openRemote()
	}
	generation := m.devices.remote.generation.Load()
	discover := func() tea.Msg {
		addresses, err := mobile.HomeNetworkAddresses()
		return homeNetworkMsg{generation: generation, addresses: addresses, err: err}
	}
	if init != nil {
		return tea.Batch(init, discover)
	}
	return discover
}

func (m *UI) handleHomeNetwork(msg homeNetworkMsg) tea.Cmd {
	if msg.generation != m.devices.remote.generation.Load() {
		return nil
	}
	d := m.remoteDialog()
	if d == nil {
		return nil
	}
	if msg.err != nil {
		d.SetNotice(remotecontrol.Msg("Could not find a home network"), false)
		return util.ReportError(fmt.Errorf("%s: %w", m.com.Text("Could not find a home network"), msg.err))
	}
	if len(msg.addresses) == 0 {
		d.SetNotice(remotecontrol.Msg("No private IPv4 network found. Connect this computer to Wi-Fi or Ethernet and try again."), false)
		return util.ReportInfo(m.com.Text("No private IPv4 network found. Connect this computer to Wi-Fi or Ethernet and try again."))
	}
	if len(msg.addresses) == 1 {
		return m.startHomeNetwork(msg.addresses[0].Address)
	}
	d.ChooseHomeNetwork(msg.addresses)
	return nil
}

// homeNetworkOptions deliberately ignores saved listener, relay and TLS
// settings. Separate identity storage keeps a LAN test from renewing the
// production gateway's certificate or sharing its phone sessions.
func (m *UI) homeNetworkOptions(address string) mobile.Options {
	return mobile.Options{
		StateDir: filepath.Join(config.GlobalDevicesDir(), "mobile-home"),
		Listen:   net.JoinHostPort(address, "0"), LAN: true,
		Language: m.Language(), Version: version.Version,
	}
}

func (m *UI) startHomeNetwork(address string) tea.Cmd {
	ws := m.com.Workspace
	return m.startHomeNetworkWith(address, func(opts mobile.Options) (*mobile.Gateway, error) {
		return startGateway(ws, opts)
	})
}

// startHomeNetworkWith shares the serialized gateway lifecycle but makes
// no configuration writes. The callback runs outside the UI goroutine.
func (m *UI) startHomeNetworkWith(address string, start func(mobile.Options) (*mobile.Gateway, error)) tea.Cmd {
	if !mobile.IsHomeNetworkAddress(address) {
		return util.ReportError(fmt.Errorf("%s", m.com.Text("Choose a private IPv4 address on your home network")))
	}
	r := &m.devices.remote
	generation := r.generation.Add(1)
	old := m.devices.gateway
	m.setGateway(nil)
	m.dialog.CloseDialog(dialog.MobileID)
	m.devices.homeLAN = true
	if d := m.remoteDialog(); d != nil {
		d.SetNotice(remotecontrol.Message{}, true)
	}
	r.profileID, r.starting, r.startErr, r.retry = "", true, nil, 0
	opts := m.homeNetworkOptions(address)
	command := r.startCommand(generation, false, old, func() (*mobile.Gateway, error) {
		return start(opts)
	})
	return func() tea.Msg {
		if msg, ok := command().(remoteGatewayMsg); ok {
			return mobileReadyMsg{generation: msg.generation, gw: msg.gw, err: msg.err}
		}
		return nil
	}
}
