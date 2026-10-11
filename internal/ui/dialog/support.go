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

