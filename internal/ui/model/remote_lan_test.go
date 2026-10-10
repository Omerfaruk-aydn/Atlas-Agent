package model

import (
	"errors"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/mobile"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/dialog"
	"github.com/stretchr/testify/require"
)

func TestHomeNetworkPreservesSavedConfigurationAndStops(t *testing.T) {
	t.Setenv("ATLAS_DEVICES_DIR", t.TempDir())
	m, ws := newRemoteLifecycleUI()
	dev := &ws.cfg.Tools.Devices
	dev.MobileRelayURL, dev.MobileRelayTokenFile = "https://atlas.example.com", "production-token"
	dev.MobileListen, dev.MobileHosts = "127.0.0.1:7777", []string{"pc.example.com"}
	dev.MobileTLSCert, dev.MobileTLSKey = "production-cert", "production-key"
	before := *dev
	old := newRemoteLifecycleGateway(t)
	m.setGateway(old)
	gw := newRemoteLifecycleGateway(t)
	start := m.startHomeNetworkWith("192.168.1.135", func(opts mobile.Options) (*mobile.Gateway, error) {
		require.Equal(t, "192.168.1.135:0", opts.Listen)
		require.True(t, opts.LAN)
		require.Empty(t, opts.Hosts)
		require.Empty(t, opts.RelayURL)
		require.Empty(t, opts.RelayTokenFile)
		require.Empty(t, opts.TLSCert)
		require.Empty(t, opts.TLSKey)
		require.NotEqual(t, m.gatewayOptions().StateDir, opts.StateDir)
		return gw, nil
	})
	require.True(t, m.devices.homeLAN)
	require.True(t, m.devices.remote.starting)
	msg := start().(mobileReadyMsg)
	m.handleMobileReady(msg)
	requireRemoteGatewayClosed(t, old)
	require.Same(t, gw, m.devices.gateway)
	require.True(t, m.dialog.ContainsDialog(dialog.MobileID))
	require.Equal(t, before, *dev)
	require.Empty(t, ws.set)
	require.Empty(t, ws.removed)
	stop := m.disableRemote()
	require.Nil(t, m.devices.gateway)
	stop()
	requireRemoteGatewayClosed(t, gw)
	require.Equal(t, before, *dev)
	require.Empty(t, ws.set)
	require.Empty(t, ws.removed)
	require.Nil(t, m.autoStartRemote(), "a stopped home test must not activate the saved VPS")
}

func TestHomeNetworkStopRejectsDelayedStartup(t *testing.T) {
	t.Parallel()
	m, ws := newRemoteLifecycleUI()
	gw := newRemoteLifecycleGateway(t)
	start := m.startHomeNetworkWith("10.0.0.2", func(mobile.Options) (*mobile.Gateway, error) { return gw, nil })
	msg := start().(mobileReadyMsg)
	m.stopMobile()()
	closeOld := m.handleMobileReady(msg)
	require.NotNil(t, closeOld)
	closeOld()
	requireRemoteGatewayClosed(t, gw)
	require.Nil(t, m.devices.gateway)
	require.Empty(t, ws.set)
	require.Empty(t, ws.removed)
}

func TestHomeNetworkRejectsInvalidBindBeforeReplacingGateway(t *testing.T) {
	t.Parallel()
	m, _ := newRemoteLifecycleUI()
	gw := newRemoteLifecycleGateway(t)
	m.setGateway(gw)
	for _, address := range []string{"0.0.0.0", "8.8.8.8", "127.0.0.1", "100.64.1.1"} {
		cmd := m.startHomeNetworkWith(address, func(mobile.Options) (*mobile.Gateway, error) {
			t.Fatal("invalid bind must not start")
			return nil, nil
		})
		require.NotNil(t, cmd())
		require.Same(t, gw, m.devices.gateway)
		require.False(t, m.devices.homeLAN)
	}
}

func TestHomeNetworkFailureDoesNotRewriteSettings(t *testing.T) {
	t.Parallel()
	m, ws := newRemoteLifecycleUI()
	failure := errors.New("address no longer available")
	cmd := m.startHomeNetworkWith("10.0.0.2", func(mobile.Options) (*mobile.Gateway, error) { return nil, failure })
	m.handleMobileReady(cmd().(mobileReadyMsg))
	require.ErrorIs(t, m.devices.remote.startErr, failure)
	require.False(t, m.devices.remote.starting)
	require.Nil(t, m.devices.gateway)
	require.Empty(t, ws.set)
	require.Empty(t, ws.removed)
}

func TestHomeNetworkDiscoveryIgnoredAfterStopOrDialogClose(t *testing.T) {
	t.Parallel()
	m, _ := newRemoteLifecycleUI()
	m.openRemote()
	msg := homeNetworkMsg{generation: m.devices.remote.generation.Load(), addresses: []mobile.HomeNetworkAddress{{Interface: "Ethernet", Address: "10.0.0.2"}}}
	m.stopMobile()()
	require.Nil(t, m.handleHomeNetwork(msg))
	msg.generation = m.devices.remote.generation.Load()
	m.dialog.CloseDialog(dialog.RemoteID)
	require.Nil(t, m.handleHomeNetwork(msg))
	require.Nil(t, m.devices.gateway)
}
