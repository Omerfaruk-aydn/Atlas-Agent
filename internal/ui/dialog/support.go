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

