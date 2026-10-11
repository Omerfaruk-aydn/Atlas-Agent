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

