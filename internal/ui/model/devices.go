package model

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/marking"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/mobile"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/dialog"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/util"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/version"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workspace"
)

// devicesState holds the phone gateway, which outlives its dialog, and the
// /rc remote control state.
type devicesState struct {
	gateway *mobile.Gateway
	remote  remoteState
	homeLAN bool
	// funnel is the /rc Tailscale Funnel mode.
	funnel funnelState
	// supportChanges delivers /support changes so a connection request or
	// a permission request reaches the owner even with /support closed.
	supportChanges <-chan struct{}
}

type (
	screenMarkMsg struct {
		mark marking.Mark
		crop []byte
		err  error
	}
	mobileReadyMsg struct {
		generation uint64
		gw         *mobile.Gateway
		err        error
	}
	mobileStoppedMsg struct{ generation uint64 }
)

// screenMarkCmd runs the marking overlay off the UI goroutine and stores
// the mark. The overlay shows a frozen capture, so marking itself never
// clicks anything in the application underneath.
func (m *UI) screenMarkCmd() tea.Cmd {
	return func() tea.Msg {
		mark, crop, err := marking.CaptureMark(context.Background(), "")
		if err != nil {
			return screenMarkMsg{err: err}
		}
		saved, err := marking.Store{Dir: config.GlobalMarksDir()}.Save(mark, crop)
		return screenMarkMsg{mark: saved, crop: crop, err: err}
	}
}

// handleScreenMark attaches the crop and its manifest to the prompt. Both
// show as attachment chips; removing them removes the mark from the
// message.
func (m *UI) handleScreenMark(msg screenMarkMsg) tea.Cmd {
	if errors.Is(msg.err, marking.ErrCanceled) {
		return util.ReportInfo(m.com.Text("Marking canceled"))
	}
	if msg.err != nil {
		return util.ReportError(fmt.Errorf("%s: %w", m.com.Text("screen mark failed"), msg.err))
	}
	_ = m.attachments.Update(message.Attachment{FileName: msg.mark.ID + ".png", MimeType: "image/png", Content: msg.crop})
	_ = m.attachments.Update(message.Attachment{FileName: msg.mark.ID + ".md", MimeType: "text/markdown", Content: []byte(marking.Manifest(msg.mark))})
	where := msg.mark.Mode
	if msg.mark.Window != nil && msg.mark.Window.Title != "" {
		where += " · " + msg.mark.Window.Title
	}
	return util.ReportInfo(fmt.Sprintf(m.com.Text("Marked %dx%d (%s); describe what to do with it"), msg.mark.Rect.W, msg.mark.Rect.H, strings.TrimSpace(where)))
}

// openMobile starts the gateway on first use, then shows pairing.
func (m *UI) openMobile() tea.Cmd {
	r := &m.devices.remote
	if gw := m.devices.gateway; gw != nil {
		generation := r.generation.Load()
		return func() tea.Msg { return mobileReadyMsg{generation: generation, gw: gw} }
	}
	if r.starting {
		return nil
	}
	if m.devices.homeLAN {
		return m.discoverHomeNetwork()
	}
	if m.devices.funnel.on {
		// Tailscale mode publishes only its own gateway; a plain local one
		// would not be reachable and would hide the real state.
		return util.ReportInfo(m.com.Text("The Tailscale connection is not ready yet; see /rc"))
	}
	generation := r.generation.Add(1)
	r.starting, r.startErr = true, nil
	ws := m.com.Workspace
	opts := m.gatewayOptions()
	start := r.startCommand(generation, false, nil, func() (*mobile.Gateway, error) {
		return startGateway(ws, opts)
	})
	return func() tea.Msg {
		if msg, ok := start().(remoteGatewayMsg); ok {
			return mobileReadyMsg{generation: msg.generation, gw: msg.gw, err: msg.err}
		}
		return nil
	}
}

// gatewayOptions reads the phone gateway settings on the UI goroutine.
func (m *UI) gatewayOptions() mobile.Options {
	opts := mobile.Options{
		StateDir: filepath.Join(config.GlobalDevicesDir(), "mobile"),
		Language: m.Language(), Version: version.Version,
	}
	if cfg := m.com.Config(); cfg != nil {
		dev := cfg.Tools.Devices
		opts.Listen, opts.LAN, opts.Hosts = dev.MobileListen, dev.MobileLAN, dev.MobileHosts
		opts.TLSCert, opts.TLSKey = dev.MobileTLSCert, dev.MobileTLSKey
		opts.RelayURL, opts.RelayTokenFile = dev.MobileRelayURL, dev.MobileRelayTokenFile
	}
	return opts
}

// startGateway starts the phone gateway for the local workspace; it runs
// inside a command, never on the UI goroutine.
func startGateway(ws workspace.Workspace, opts mobile.Options) (*mobile.Gateway, error) {
	backend, events, preview, err := mobile.FromWorkspace(ws)
	if err != nil {
		return nil, err
	}
	opts.Backend, opts.Events, opts.Preview = backend, events, preview
	if preview != nil {
		opts.Screen = mobile.DesktopScreen()
	}
	return mobile.Start(context.Background(), opts)
}

func (m *UI) handleMobileReady(msg mobileReadyMsg) tea.Cmd {
	if msg.generation != m.devices.remote.generation.Load() {
		return closeGatewayCmd(msg.gw)
	}
	m.devices.remote.starting = false
	m.devices.remote.startErr = msg.err
	if msg.err != nil {
		return util.ReportError(fmt.Errorf("%s: %w", m.com.Text("phone control unavailable"), msg.err))
	}
	if cur := m.devices.gateway; cur != nil && cur != msg.gw {
		// Two quick /mobile commands started two gateways; keep the first.
		_ = msg.gw.Close()
		msg.gw = cur
	}
	m.setGateway(msg.gw)
	if m.dialog.ContainsDialog(dialog.MobileID) {
		return nil
	}
	d := dialog.NewMobile(m.com, msg.gw)
	d.SetHomeNetwork(m.devices.homeLAN)
	m.dialog.OpenDialog(d)
	return d.Init()
}

func (m *UI) stopMobile() tea.Cmd {
	if m.devices.funnel.on {
		return m.stopFunnel()
	}
	gw := m.devices.gateway
	r := &m.devices.remote
	generation := r.generation.Add(1)
	r.starting, r.startErr, r.retry = false, nil, 0
	m.setGateway(nil)
	return func() tea.Msg {
		r.lifecycle.Lock()
		defer r.lifecycle.Unlock()
		if gw != nil {
			_ = gw.Close()
		}
		return mobileStoppedMsg{generation: generation}
	}
}
