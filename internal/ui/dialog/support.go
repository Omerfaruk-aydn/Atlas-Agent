package dialog

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ansi"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-style/v2"
	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	uv "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ultraviolet"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/help"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/key"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/textinput"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/mobile"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/nodes"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/support"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/common"
)

// SupportID identifies the /support dialog.
const SupportID = "support"

// ActionOpenSupport opens /support.
type ActionOpenSupport struct{}

// SupportTick refreshes /support. The UI routes it to the dialog even when
// another dialog is in front.
type SupportTick struct{}

// SupportDone reports a finished /support command.
type SupportDone struct {
	Op      string
	Notice  string
	Err     error
	History []support.Summary
	Events  []support.Event
}

type supportView int

const (
	supportMenu supportView = iota
	supportHostSetup
	supportHost
	supportConnect
	supportSessions
	supportTransfers
	supportHistory
	supportHistoryDetail
	supportSettings
)

var supportDurations = []time.Duration{time.Hour, 2 * time.Hour, 4 * time.Hour, 8 * time.Hour}

// supportNetworks are the offer paths in cycle order.
var supportNetworks = []string{"lan", "internet", "both"}

// Support is the /support dialog. It renders snapshots of the support
// service; offers, joins, decisions and history reads run in commands.
type Support struct {
	com  *common.Common
	svc  *support.Service
	help help.Model
	view supportView

	menuSel int

	// Host setup.
	caps        map[support.Capability]bool
	capSel      int
	fileRoots   func() ([]nodes.Root, error)
	durationIdx int
	networkIdx  int
	showQR      bool

	// Connect.
	codeInput textinput.Model
	joining   bool

	// History.
	summaries []support.Summary
	histSel   int
	events    []support.Event

	// Settings.
	settings    [3]textinput.Model
	settingSel  int
	noDiscovery bool

	host    support.HostStatus
	hasHost bool
	ctl     support.ControllerStatus
	hasCtl  bool

	confirm   string
	notice    string
	noticeErr bool
	busy      bool
	keyMap    struct {
		Up, Down, Enter, Back, Toggle, Accept, Reject, Copy, CopyLink, QR, PauseAgent, PauseInput, Files, Disconnect, Grant, Deny, Duration, Network, Preset, Next key.Binding
	}
}

var _ Dialog = (*Support)(nil)

// NewSupport creates the /support dialog. svc is nil in client mode.
func NewSupport(com *common.Common, svc *support.Service) *Support {
	d := &Support{com: com, svc: svc, caps: map[support.Capability]bool{}, fileRoots: support.ComputerFileRoots}
	d.help = help.New()
	d.help.Styles = com.Styles.DialogHelpStyles()
	for _, c := range support.Presets["diagnose"] {
		d.caps[c] = true
	}
	d.durationIdx = 1
	newInput := func(placeholder string) textinput.Model {
		in := textinput.New()
		in.SetVirtualCursor(false)
		in.SetStyles(com.Styles.TextInput)
		in.Placeholder = placeholder
		return in
	}
	d.codeInput = newInput(com.Text("Code (XXXX-XXXX-XXXX), CODE@address or atlas-support:// link"))
	d.settings[0] = newInput(com.Text("Name shown to the other computer"))
	d.settings[1] = newInput("https://support.example.com")
	d.settings[2] = newInput(com.Text("Absolute path of the relay token file"))
	if cfg := supportConfig(com); cfg != nil {
		dev := cfg.Tools.Devices
		d.settings[0].SetValue(dev.SupportName)
		d.settings[1].SetValue(dev.SupportRelayURL)
		d.settings[2].SetValue(dev.SupportRelayTokenFile)
		d.noDiscovery = dev.SupportNoDiscovery
	}
	k := func(keys []string, h, desc string) key.Binding {
		return key.NewBinding(key.WithKeys(keys...), key.WithHelp(h, com.Text(desc)))
	}
	d.keyMap.Up = k([]string{"up", "k"}, "↑", "previous")
	d.keyMap.Down = k([]string{"down", "j"}, "↓", "next")
	d.keyMap.Enter = k([]string{"enter"}, "enter", "select")
	d.keyMap.Back = k([]string{"esc", "alt+esc"}, "esc", "back")
	d.keyMap.Toggle = k([]string{"space"}, "space", "toggle")
	d.keyMap.Accept = k([]string{"a"}, "a", "accept")
	d.keyMap.Reject = k([]string{"r"}, "r", "reject")
	d.keyMap.Copy = k([]string{"c"}, "c", "copy code")
	d.keyMap.CopyLink = k([]string{"l"}, "l", "copy link")
	d.keyMap.QR = k([]string{"q"}, "q", "QR")
	d.keyMap.PauseAgent = k([]string{"p"}, "p", "pause task")
	d.keyMap.PauseInput = k([]string{"s"}, "s", "pause screen control")
	d.keyMap.Files = k([]string{"f"}, "f", "file access")
	d.keyMap.Disconnect = k([]string{"x"}, "x", "disconnect")
	d.keyMap.Grant = k([]string{"y"}, "y", "grant")
	d.keyMap.Deny = k([]string{"n"}, "n", "deny")
	d.keyMap.Duration = k([]string{"t"}, "t", "duration")
	d.keyMap.Network = k([]string{"w"}, "w", "network")
	d.keyMap.Preset = k([]string{"1", "2", "3"}, "1-3", "preset")
	d.keyMap.Next = k([]string{"tab"}, "tab", "next field")
	d.refresh()
	return d
}

// supportConfig is the configuration, or nil without a workspace.
func supportConfig(com *common.Common) *config.Config {
	if com.Workspace == nil {
		return nil
	}
	return com.Config()
}

// ID implements Dialog.
func (d *Support) ID() string { return SupportID }

// ShowHost switches to the owner's view, where requests are answered.
func (d *Support) ShowHost() {
	d.refresh()
	d.confirm = ""
	d.view = supportHost
}

// Init starts the refresh tick.
func (d *Support) Init() tea.Cmd { return d.tick() }

func (d *Support) tick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return SupportTick{} })
}

// refresh snapshots both ends without creating either one.
func (d *Support) refresh() {
	if h := d.svc.ActiveHost(); h != nil {
		d.host, d.hasHost = h.Status(), true
	}
	if c := d.svc.ActiveController(); c != nil {
		d.ctl, d.hasCtl = c.Status(), true
	}
}

func (d *Support) say(msg string, isErr bool) {
	d.notice, d.noticeErr = msg, isErr
}

// run executes fn off the UI loop.
func (d *Support) run(op string, fn func(ctx context.Context) SupportDone) Action {
	d.busy = true
	return ActionCmd{Cmd: func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		out := fn(ctx)
		out.Op = op
		return out
	}}
}

func (d *Support) errText(err error) string {
	if e := support.AsError(err); e != nil {
		msg := d.com.Text(e.Message)
		if e.NextStep != "" {
			msg += " " + d.com.Text(e.NextStep)
		}
		return msg
	}
	return err.Error()
}

// HandleMsg implements Dialog.
func (d *Support) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case SupportTick:
		d.refresh()
		return ActionCmd{Cmd: d.tick()}
	case SupportDone:
		d.busy = false
		d.refresh()
		switch {
		case msg.Err != nil:
			d.say(d.errText(msg.Err), true)
			if msg.Op == "join" {
				d.joining = false
			}
		case msg.Op == "history":
			d.summaries, d.histSel = msg.History, 0
		case msg.Op == "events":
			d.events, d.view = msg.Events, supportHistoryDetail
		case msg.Op == "offer":
			d.view = supportHost
			d.say(msg.Notice, false)
		case msg.Op == "join":
			d.joining = false
			d.say(msg.Notice, false)
		default:
			if msg.Notice != "" {
				d.say(msg.Notice, false)
			}
		}
		return nil
	case tea.PasteMsg:
		switch {
		case d.view == supportConnect && !d.joining:
			var cmd tea.Cmd
			d.codeInput, cmd = d.codeInput.Update(msg)
			return ActionCmd{Cmd: cmd}
		case d.view == supportSettings:
			var cmd tea.Cmd
			d.settings[d.settingSel], cmd = d.settings[d.settingSel].Update(msg)
			return ActionCmd{Cmd: cmd}
		}
	case tea.KeyPressMsg:
		if d.confirm != "" {
			return d.handleConfirm(msg)
		}
		return d.handleKey(msg)
	}
	return nil
}

func (d *Support) handleConfirm(msg tea.KeyPressMsg) Action {
	what := d.confirm
	d.confirm = ""
	if msg.String() != "y" && msg.String() != "Y" {
		return nil
	}
	switch what {
	case "host-disconnect":
		return d.hostDo("disconnect", func(h *support.Host) (string, error) {
			h.Disconnect("the owner ended the session")
			return d.com.Text("Remote support ended; the other computer can no longer act here."), nil
		})
	case "ctl-disconnect":
		return d.run("ctl-disconnect", func(context.Context) SupportDone {
			if c := d.svc.ActiveController(); c != nil {
				c.Disconnect("the supporter ended the session")
			}
			return SupportDone{Notice: d.com.Text("Disconnected from the other computer.")}
		})
	}
	return nil
}

func (d *Support) hostDo(op string, fn func(*support.Host) (string, error)) Action {
	return d.run(op, func(context.Context) SupportDone {
		h := d.svc.ActiveHost()
		if h == nil {
			return SupportDone{Err: errors.New(d.com.Text("Remote support is not active on this computer."))}
		}
		notice, err := fn(h)
		return SupportDone{Notice: notice, Err: err}
	})
}

func (d *Support) handleKey(msg tea.KeyPressMsg) Action {
	if d.svc == nil {
		if key.Matches(msg, d.keyMap.Back) || key.Matches(msg, CloseKey) {
			return ActionClose{}
		}
		return nil
	}
	switch d.view {
	case supportMenu:
		return d.menuKey(msg)
	case supportHostSetup:
		return d.setupKey(msg)
	case supportHost:
		return d.hostKey(msg)
	case supportConnect:
		return d.connectKey(msg)
	case supportHistory:
		return d.historyKey(msg)
	case supportSettings:
		return d.settingsKey(msg)
	default:
		if key.Matches(msg, d.keyMap.Back) {
			d.back()
			return nil
		}
		if d.view == supportSessions && key.Matches(msg, d.keyMap.Disconnect) {
			if d.hasCtl && d.ctl.Remote != nil && !d.ctl.State.Terminal() {
				d.confirm = "ctl-disconnect"
			} else if d.hasHost && d.host.Session != nil {
				d.confirm = "host-disconnect"
			}
		}
	}
	return nil
}

func (d *Support) back() {
	d.notice = ""
	switch d.view {
	case supportHistoryDetail:
		d.view = supportHistory
	default:
		d.view = supportMenu
	}
}

var supportMenuItems = []string{
	"Get support for this computer",
	"Connect to another computer",
	"Active support sessions",
	"File transfers",
	"Support history",
	"Connection and privacy settings",
}

func (d *Support) menuKey(msg tea.KeyPressMsg) Action {
	switch {
	case key.Matches(msg, d.keyMap.Back), key.Matches(msg, CloseKey):
		return ActionClose{}
	case key.Matches(msg, d.keyMap.Up):
		d.menuSel = max(0, d.menuSel-1)
	case key.Matches(msg, d.keyMap.Down):
		d.menuSel = min(len(supportMenuItems)-1, d.menuSel+1)
	case key.Matches(msg, d.keyMap.Enter):
		return d.openMenu(d.menuSel)
	default:
		if s := msg.String(); len(s) == 1 && s[0] >= '1' && s[0] <= '6' {
			d.menuSel = int(s[0] - '1')
			return d.openMenu(d.menuSel)
		}
	}
	return nil
}

func (d *Support) openMenu(i int) Action {
	d.notice = ""
	switch i {
	case 0:
		if d.hasHost && (d.host.Offer != nil || d.host.Pending != nil || d.host.Session != nil) {
			d.view = supportHost
			return nil
		}
		d.view = supportHostSetup
	case 1:
		d.view = supportConnect
		if !d.joining {
			return ActionCmd{Cmd: d.codeInput.Focus()}
		}
	case 2:
		d.view = supportSessions
	case 3:
		d.view = supportTransfers
	case 4:
		d.view = supportHistory
		return d.run("history", func(ctx context.Context) SupportDone {
			return SupportDone{History: d.loadHistory(ctx)}
		})
	case 5:
		d.view = supportSettings
		return ActionCmd{Cmd: d.settings[d.settingSel].Focus()}
	}
	return nil
}

// loadHistory merges both ends' session histories, newest first. It reads
// files and runs in a command.
func (d *Support) loadHistory(ctx context.Context) []support.Summary {
	var out []support.Summary
	if h, err := d.svc.Host(ctx); err == nil {
		if s, err := h.History().Sessions(); err == nil {
			out = append(out, s...)
		}
	}
	if c, err := d.svc.Controller(ctx); err == nil {
		if s, err := c.History().Sessions(); err == nil {
			out = append(out, s...)
		}
	}
	slices.SortFunc(out, func(a, b support.Summary) int { return int(b.Started - a.Started) })
	return out
}

func (d *Support) setupKey(msg tea.KeyPressMsg) Action {
	switch {
	case key.Matches(msg, d.keyMap.Back):
		d.back()
	case key.Matches(msg, d.keyMap.Up):
		d.capSel = max(0, d.capSel-1)
	case key.Matches(msg, d.keyMap.Down):
		d.capSel = min(len(support.AllCapabilities)-1, d.capSel+1)
	case key.Matches(msg, d.keyMap.Toggle):
		c := support.AllCapabilities[d.capSel]
		d.caps[c] = !d.caps[c]
	case key.Matches(msg, d.keyMap.Preset):
		name := map[string]string{"1": "diagnose", "2": "assist", "3": "full"}[msg.String()]
		d.caps = map[support.Capability]bool{}
		for _, c := range support.Presets[name] {
			d.caps[c] = true
		}
	case key.Matches(msg, d.keyMap.Duration):
		d.durationIdx = (d.durationIdx + 1) % len(supportDurations)
	case key.Matches(msg, d.keyMap.Network):
		d.networkIdx = (d.networkIdx + 1) % len(supportNetworks)
	case key.Matches(msg, d.keyMap.Enter):
		return d.createOffer()
	}
	return nil
}

func (d *Support) selectedCaps() []support.Capability {
	var out []support.Capability
	for _, c := range support.AllCapabilities {
		if d.caps[c] {
			out = append(out, c)
		}
	}
	return out
}

func (d *Support) createOffer() Action {
	if d.busy {
		return nil
	}
	caps := d.selectedCaps()
	if len(caps) == 0 {
		d.say(d.com.Text("Choose at least one permission."), true)
		return nil
	}
	network := supportNetworks[d.networkIdx]
	opts := support.OfferOptions{Capabilities: caps, SessionDuration: supportDurations[d.durationIdx], LAN: network != "internet", Internet: network != "lan"}
	return d.run("offer", func(ctx context.Context) SupportDone {
		roots, err := d.fileRoots()
		if err != nil && slices.ContainsFunc(caps, support.IsFileCapability) {
			return SupportDone{Err: err}
		}
		// Prepare the scope for later file permission requests without
		// granting any file capability that the owner did not select.
		opts.Roots = roots
		h, err := d.svc.Host(ctx)
		if err != nil {
			return SupportDone{Err: err}
		}
		info, err := h.Offer(opts)
		if err != nil {
			return SupportDone{Err: err}
		}
		return SupportDone{Notice: fmt.Sprintf(d.com.Text("Code %s created; it works once and expires at %s."), info.Code, info.ExpiresAt.Format("15:04"))}
	})
}

func (d *Support) hostKey(msg tea.KeyPressMsg) Action {
	st := d.host
	switch {
	case key.Matches(msg, d.keyMap.Back):
		d.back()
		return nil
	case st.Pending != nil && key.Matches(msg, d.keyMap.Accept, d.keyMap.Reject):
		id, accept := st.Pending.ID, key.Matches(msg, d.keyMap.Accept)
		return d.hostDo("decide", func(h *support.Host) (string, error) {
			if err := h.Decide(id, accept, nil); err != nil {
				return "", err
			}
			if accept {
				return d.com.Text("Connection accepted. The banner shows while support is active."), nil
			}
			return d.com.Text("Connection rejected; the code no longer works."), nil
		})
	case st.Permission != nil && key.Matches(msg, d.keyMap.Grant, d.keyMap.Deny):
		p, grant := *st.Permission, key.Matches(msg, d.keyMap.Grant)
		return d.hostDo("permission", func(h *support.Host) (string, error) {
			if err := h.AnswerPermission(p.ID, p.Revision, grant); err != nil {
				return "", err
			}
			if grant {
				return d.com.Text("Permission granted for this session."), nil
			}
			return d.com.Text("Permission denied."), nil
		})
	case key.Matches(msg, d.keyMap.Copy) && st.Offer != nil:
		return ActionCmd{Cmd: common.CopyToClipboard(st.Offer.Code, d.com.Text("Code copied to clipboard"))}
	case key.Matches(msg, d.keyMap.CopyLink) && st.Offer != nil:
		return ActionCmd{Cmd: common.CopyToClipboard(st.Offer.Link, d.com.Text("Link copied to clipboard"))}
	case key.Matches(msg, d.keyMap.QR):
		d.showQR = !d.showQR
	case key.Matches(msg, d.keyMap.PauseAgent) && st.Session != nil:
		paused := !st.Session.AgentPaused
		return d.hostDo("pause", func(h *support.Host) (string, error) {
			if err := h.PauseAgent(paused); err != nil {
				return "", err
			}
			if paused {
				return d.com.Text("Support task paused: no changes are accepted until you resume."), nil
			}
			return d.com.Text("Support task resumed."), nil
		})
	case key.Matches(msg, d.keyMap.PauseInput) && st.Session != nil:
		paused := !st.Session.InputPaused
		return d.hostDo("pause-input", func(h *support.Host) (string, error) {
			if err := h.PauseInput(paused); err != nil {
				return "", err
			}
			if paused {
				return d.com.Text("Screen control paused: no mouse or keyboard input until you resume."), nil
			}
			return d.com.Text("Screen control resumed."), nil
		})
	case key.Matches(msg, d.keyMap.Files) && st.Session != nil:
		caps := st.Session.Capabilities
		fileCaps := []support.Capability{support.CapFileRead, support.CapFileSend, support.CapFileReceive, support.CapFileModify}
		narrowed := slices.DeleteFunc(slices.Clone(caps), func(c support.Capability) bool { return slices.Contains(fileCaps, c) })
		if len(narrowed) == len(caps) {
			d.say(d.com.Text("File access is already off. A new permission request is needed to turn it on."), false)
			return nil
		}
		return d.hostDo("files", func(h *support.Host) (string, error) {
			return d.com.Text("File access turned off for this session."), h.SetCapabilities(narrowed)
		})
	case key.Matches(msg, d.keyMap.Disconnect):
		switch {
		case st.Session != nil:
			d.confirm = "host-disconnect"
		case st.Pending != nil || st.Offer != nil:
			return d.hostDo("cancel", func(h *support.Host) (string, error) {
				h.Disconnect("the owner withdrew the code")
				return d.com.Text("Code withdrawn."), nil
			})
		default:
			d.view = supportHostSetup
		}
	case key.Matches(msg, d.keyMap.Enter) && st.Session == nil && st.Offer == nil && st.Pending == nil:
		d.view = supportHostSetup
	}
	return nil
}

func (d *Support) connectKey(msg tea.KeyPressMsg) Action {
	connected := d.hasCtl && d.ctl.Remote != nil && !d.ctl.State.Terminal()
	switch {
	case key.Matches(msg, d.keyMap.Back):
		d.codeInput.Blur()
		d.back()
		return nil
	case connected && key.Matches(msg, d.keyMap.Disconnect):
		d.confirm = "ctl-disconnect"
		return nil
	case connected || d.joining:
		return nil
	case key.Matches(msg, d.keyMap.Enter):
		input := strings.TrimSpace(d.codeInput.Value())
		if input == "" {
			return nil
		}
		if _, err := support.ParseTarget(input); err != nil {
			d.say(d.errText(err), true)
			return nil
		}
		d.joining = true
		d.notice = ""
		return d.run("join", func(ctx context.Context) SupportDone {
			c, err := d.svc.Controller(ctx)
			if err != nil {
				return SupportDone{Err: err}
			}
			r, err := c.Join(ctx, input, nil)
			if err != nil {
				return SupportDone{Err: err}
			}
			return SupportDone{Notice: fmt.Sprintf(d.com.Text("Connected to %s. Give tasks in the chat; they run on that computer."), r.RemoteName)}
		})
	}
	var cmd tea.Cmd
	d.codeInput, cmd = d.codeInput.Update(msg)
	return ActionCmd{Cmd: cmd}
}

func (d *Support) historyKey(msg tea.KeyPressMsg) Action {
	switch {
	case key.Matches(msg, d.keyMap.Back):
		d.back()
	case key.Matches(msg, d.keyMap.Up):
		d.histSel = max(0, d.histSel-1)
	case key.Matches(msg, d.keyMap.Down):
		d.histSel = min(len(d.summaries)-1, d.histSel+1)
	case key.Matches(msg, d.keyMap.Enter):
		if d.histSel < 0 || d.histSel >= len(d.summaries) {
			return nil
		}
		s := d.summaries[d.histSel]
		return d.run("events", func(ctx context.Context) SupportDone {
			var hist *support.History
			if s.Role == "host" {
				if h, err := d.svc.Host(ctx); err == nil {
					hist = h.History()
				}
			} else if c, err := d.svc.Controller(ctx); err == nil {
				hist = c.History()
			}
			if hist == nil {
				return SupportDone{Err: errors.New(d.com.Text("History is not available."))}
			}
			events, err := hist.Events(s.SessionID)
			return SupportDone{Events: events, Err: err}
		})
	}
	return nil
}

func (d *Support) settingsKey(msg tea.KeyPressMsg) Action {
	switch {
	case key.Matches(msg, d.keyMap.Back):
		d.settings[d.settingSel].Blur()
		d.back()
		return nil
	case key.Matches(msg, d.keyMap.Next), key.Matches(msg, d.keyMap.Down) && msg.String() == "down", key.Matches(msg, d.keyMap.Up) && msg.String() == "up":
		d.settings[d.settingSel].Blur()
		if msg.String() == "up" {
			d.settingSel = (d.settingSel + len(d.settings)) % (len(d.settings) + 1)
		} else {
			d.settingSel = (d.settingSel + 1) % (len(d.settings) + 1)
		}
		if d.settingSel < len(d.settings) {
			return ActionCmd{Cmd: d.settings[d.settingSel].Focus()}
		}
		return nil
	case d.settingSel == len(d.settings) && (key.Matches(msg, d.keyMap.Toggle) || key.Matches(msg, d.keyMap.Enter)):
		d.noDiscovery = !d.noDiscovery
		return d.saveSettings()
	case key.Matches(msg, d.keyMap.Enter):
		return d.saveSettings()
	}
	if d.settingSel < len(d.settings) {
		var cmd tea.Cmd
		d.settings[d.settingSel], cmd = d.settings[d.settingSel].Update(msg)
		return ActionCmd{Cmd: cmd}
	}
	return nil
}

func (d *Support) saveSettings() Action {
	relay := strings.TrimSpace(d.settings[1].Value())
	if relay != "" && !strings.HasPrefix(relay, "https://") && !strings.HasPrefix(relay, "http://localhost") && !strings.HasPrefix(relay, "http://127.0.0.1") {
		d.say(d.com.Text("The relay address must start with https://."), true)
		return nil
	}
	token := strings.TrimSpace(d.settings[2].Value())
	if token != "" && !filepath.IsAbs(token) {
		d.say(d.com.Text("The token file must be an absolute path."), true)
		return nil
	}
	fields := map[string]any{
		"tools.devices.support_name":             strings.TrimSpace(d.settings[0].Value()),
		"tools.devices.support_relay_url":        relay,
		"tools.devices.support_relay_token_file": token,
		"tools.devices.support_no_discovery":     d.noDiscovery,
	}
	ws := d.com.Workspace
	if ws == nil {
		d.say(d.com.Text("Remote support is only available when Atlas runs locally."), true)
		return nil
	}
	return d.run("settings", func(context.Context) SupportDone {
		for k, v := range fields {
			if err := ws.SetConfigField(config.ScopeGlobal, k, v); err != nil {
				return SupportDone{Err: err}
			}
		}
		return SupportDone{Notice: d.com.Text("Settings saved; they apply to the next code or connection.")}
	})
}

// Draw implements Dialog.
func (d *Support) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := d.com.Styles
	width := max(0, min(92, area.Dx()-t.Dialog.View.GetHorizontalBorderSize()))
	inner := max(10, width-t.Dialog.View.GetHorizontalFrameSize())
	text := inner - 1
	rc := NewRenderContext(t, width)
	rc.Title = d.com.Text("Remote support")
	if sub := d.subtitle(); sub != "" {
		rc.TitleInfo = t.Dialog.SecondaryText.Padding(0).Render(" · " + sub)
	}
	height := max(4, area.Dy()-t.Dialog.View.GetVerticalFrameSize()-6)

	var cur *tea.Cursor
	var input *textinput.Model
	switch {
	case d.view == supportConnect && !d.joining && !d.connected():
		input = &d.codeInput
	case d.view == supportSettings && d.settingSel < len(d.settings):
		input = &d.settings[d.settingSel]
	}
	if input != nil {
		input.SetWidth(dialogInputTextWidth(t, *input, inner))
		rc.AddPart(t.Dialog.InputPrompt.Render(input.View()))
		cur = InputCursor(t, input.Cursor())
		height--
	}

	var lines []string
	if d.svc == nil {
		lines = wrapLines(t.Dialog.PrimaryText.Padding(0), d.com.Text("Remote support is only available when Atlas runs locally."), text)
	} else {
		lines = d.body(text)
	}
	// Feedback must stay visible when the terminal clips a long body.
	var feedback []string
	if d.confirm != "" {
		q := d.com.Text("End remote support now? Running commands are stopped and recorded. y confirm · any other key cancels")
		feedback = append(feedback, wrapLines(t.Dialog.OAuth.ErrorText, q, text)...)
		feedback = append(feedback, "")
	}
	if d.notice != "" {
		style := t.Dialog.SecondaryText.Padding(0)
		if d.noticeErr {
			style = t.Dialog.OAuth.ErrorText
		}
		feedback = append(feedback, wrapLines(style, d.notice, text)...)
		feedback = append(feedback, "")
	}
	lines = append(feedback, lines...)
	if len(lines) > height {
		lines = lines[:height]
	}
	for i, l := range lines {
		lines[i] = " " + ansi.Truncate(l, text, "…")
	}
	rc.AddPart(strings.Join(lines, "\n"))
	rc.Help = renderDialogHelp(t, &d.help, d, inner)
	DrawCenterCursor(scr, area, rc.Render(), cur)
	return cur
}

// connected reports an active session from this computer to another.
func (d *Support) connected() bool {
	return d.hasCtl && d.ctl.Remote != nil && !d.ctl.State.Terminal()
}

func (d *Support) subtitle() string {
	switch {
	case d.hasHost && d.host.Session != nil:
		return d.com.Text("receiving support")
	case d.hasCtl && d.ctl.Remote != nil && !d.ctl.State.Terminal():
		return fmt.Sprintf(d.com.Text("supporting %s"), d.ctl.Remote.RemoteName)
	}
	return ""
}

func (d *Support) body(text int) []string {
	switch d.view {
	case supportHostSetup:
		return d.setupLines(text)
	case supportHost:
		return d.hostLines(text)
	case supportConnect:
		return d.connectLines(text)
	case supportSessions:
		return d.sessionLines(text)
	case supportTransfers:
		return d.transferLines(text)
	case supportHistory:
		return d.historyLines(text)
	case supportHistoryDetail:
		return d.eventLines()
	case supportSettings:
		return d.settingsLines(text)
	}
	return d.menuLines(text)
}

func (d *Support) item(selected bool, s string, text int) string {
	t := d.com.Styles
	s = ansi.Truncate(s, text, "…")
	if selected {
		return t.Dialog.SelectedItem.Width(text).Render(s)
	}
	return t.Dialog.NormalItem.Width(text).Render(s)
}

func (d *Support) muted(s string) string {
	return d.com.Styles.Dialog.SecondaryText.Padding(0).Render(s)
}

func (d *Support) strong(s string) string {
	return d.com.Styles.Dialog.PrimaryText.Padding(0).Render(s)
}

func (d *Support) menuLines(text int) []string {
	var lines []string
	for i, label := range supportMenuItems {
		lines = append(lines, d.item(i == d.menuSel, fmt.Sprintf("%d. %s", i+1, d.com.Text(label)), text))
	}
	lines = append(lines, "")
	lines = append(lines, wrapLines(d.com.Styles.Dialog.SecondaryText.Padding(0), d.com.Text("Support sessions are visible, started by the person at each computer and end when either side disconnects. Atlas never sets up hidden or permanent access."), text)...)
	return lines
}

func (d *Support) setupLines(text int) []string {
	name := d.host.Name
	if name == "" {
		if cfg := supportConfig(d.com); cfg != nil {
			name = cfg.Tools.Devices.SupportName
		}
	}
	if name == "" {
		name = d.com.Text("this computer's name")
	}
	network := map[string]string{"lan": "Local network only", "internet": "Internet (through your relay)", "both": "Local network and internet"}[supportNetworks[d.networkIdx]]
	lines := []string{
		d.strong(fmt.Sprintf(d.com.Text("Shown to the supporter as: %s"), name)),
		fmt.Sprintf(d.com.Text("Connection: %s (w changes)"), d.com.Text(network)),
	}
	lines = append(lines, wrapLines(d.com.Styles.Dialog.SecondaryText.Padding(0), d.com.Text("File permissions apply to this computer's accessible drives; no folder selection is needed."), text)...)
	lines = append(lines, d.muted(d.com.Text("Permissions (space toggles, 1 diagnose · 2 assist · 3 full):")))
	for i, c := range support.AllCapabilities {
		mark := "[ ]"
		if d.caps[c] {
			mark = "[x]"
		}
		label := d.com.Text(c.Label())
		if c.HighImpact() {
			label += " " + d.com.Text("(high impact)")
		}
		lines = append(lines, d.item(i == d.capSel, mark+" "+label, text))
	}
	lines = append(lines, "",
		fmt.Sprintf(d.com.Text("Session length: %s (t changes) · Code valid for 10 minutes, single use"), fmt.Sprintf("%dh", int(supportDurations[d.durationIdx].Hours()))),
		d.muted(d.com.Text("enter creates the code. Nothing is reachable until you accept the request.")),
	)
	return lines
}

