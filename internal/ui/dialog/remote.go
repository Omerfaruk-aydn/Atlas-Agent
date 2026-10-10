package dialog

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ansi"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-style/v2"
	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	uv "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ultraviolet"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/help"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/key"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/mobile"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/remotecontrol"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/common"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/util"
)

type remoteView int

const (
	remoteMain remoteView = iota
	remoteEditing
	remoteHostKey
	remoteProgress
	remoteDiagnostics
	remoteConfirm
	remoteProfiles
	remoteHomeNetwork
)

// Remote is the /rc dialog. Long operations run in the remote control
// manager and continue when the dialog closes; the dialog only shows the
// manager's latest snapshot.
type Remote struct {
	com      *common.Common
	env      RemoteEnv
	help     help.Model
	view     remoteView
	selected int
	scroll   int
	form     *remoteForm
	confirm  *remoteConfirmation
	notice   remotecontrol.Message
	noticeOK bool
	// lastOp repeats the attempted operation after a host key decision.
	lastOp       string
	profSel      int
	lanAddresses []mobile.HomeNetworkAddress
	lanSelected  int
	keyMap       struct {
		Up, Down, Enter, Back, Cancel, Trust, Replace, Retry key.Binding
	}
}

type remoteConfirmation struct {
	text   string
	action func() Action
}

type remoteAction struct {
	id    string
	label string
	hint  string
}

// Action identifiers of the main menu.
const (
	rcAdd      = "add"
	rcHelp     = "help"
	rcDraft    = "draft"
	rcTest     = "test"
	rcInstall  = "install"
	rcPair     = "pair"
	rcDevices  = "devices"
	rcDiag     = "diagnostics"
	rcToggle   = "toggle"
	rcEdit     = "edit"
	rcSwitch   = "switch"
	rcRollback = "rollback"
	rcRemove   = "remove"
	rcDelete   = "delete"
	rcProgress = "progress"
	rcLAN      = "lan"
	rcLANStop  = "lan-stop"
)

var _ Dialog = (*Remote)(nil)

// RemoteTick refreshes /rc. The UI routes it to the dialog even when another
// dialog, such as phone pairing, is in front.
type RemoteTick struct{}

// NewRemote creates the /rc dialog.
func NewRemote(com *common.Common, env RemoteEnv) *Remote {
	d := &Remote{com: com, env: env}
	d.help = help.New()
	d.help.Styles = com.Styles.DialogHelpStyles()
	d.keyMap.Up = key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑", com.Text("previous")))
	d.keyMap.Down = key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓", com.Text("next")))
	d.keyMap.Enter = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", com.Text("choose")))
	d.keyMap.Back = key.NewBinding(key.WithKeys("esc", "alt+esc"), key.WithHelp("esc", com.Text("back")))
	d.keyMap.Cancel = key.NewBinding(key.WithKeys("c"), key.WithHelp("c", com.Text("cancel operation")))
	d.keyMap.Trust = key.NewBinding(key.WithKeys("t"), key.WithHelp("t", com.Text("trust this key")))
	d.keyMap.Replace = key.NewBinding(key.WithKeys("R"), key.WithHelp("shift+r", com.Text("replace the trusted key")))
	d.keyMap.Retry = key.NewBinding(key.WithKeys("r"), key.WithHelp("r", com.Text("run again")))
	if snap, ok := d.snapshot(); ok && snap.Busy {
		d.view = remoteProgress
	}
	return d
}

// ID implements Dialog.
func (d *Remote) ID() string { return RemoteID }

// Init starts the refresh tick.
func (d *Remote) Init() tea.Cmd { return d.tick() }

func (d *Remote) tick() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg { return RemoteTick{} })
}

func (d *Remote) cfg() config.ToolDevices {
	if c := d.com.Config(); c != nil {
		return c.Tools.Devices
	}
	return config.ToolDevices{}
}

// active returns the active profile, falling back to the first one.
func (d *Remote) active() (config.RemoteProfile, bool) {
	cfg := d.cfg()
	for _, p := range cfg.RemoteProfiles {
		if p.ID == cfg.RemoteActive {
			return p, true
		}
	}
	if len(cfg.RemoteProfiles) > 0 {
		return cfg.RemoteProfiles[0], true
	}
	return config.RemoteProfile{}, false
}

// enabled reports whether remote access is configured for p.
func (d *Remote) enabled(p config.RemoteProfile) bool {
	if d.homeLAN() {
		return false
	}
	cfg := d.cfg()
	return p.PublicHost != "" && strings.TrimSuffix(cfg.MobileRelayURL, "/") == "https://"+p.PublicHost
}

func (d *Remote) snapshot() (remotecontrol.Snapshot, bool) {
	p, ok := d.active()
	if !ok || d.env.Manager == nil {
		return remotecontrol.Snapshot{}, false
	}
	return d.env.Manager.Snapshot(p.ID), true
}

func (d *Remote) gateway() *mobile.Gateway {
	if d.env.Gateway == nil {
		return nil
	}
	return d.env.Gateway()
}

// ProfileSaved is called by the UI after a save attempt.
func (d *Remote) ProfileSaved(errs remotecontrol.FieldErrors, err error, persisted bool) {
	if d.form == nil {
		return
	}
	if len(errs) > 0 || err != nil {
		msg := remotecontrol.Message{}
		if err != nil {
			msg = remotecontrol.Msg("The profile could not be saved.")
		}
		d.form.SetErrors(errs, msg)
		return
	}
	d.form.clearSecret()
	d.form = nil
	d.view = remoteMain
	d.setNotice(remotecontrol.Msg("Profile saved. Test the connection next."), true)
	if !persisted {
		d.setNotice(remotecontrol.Msg("Profile saved, but this system could not seal the secret; it is kept in memory until Atlas exits."), false)
	}
}

// OpenDraft opens the form filled with details found by the assistant.
func (d *Remote) OpenDraft(draft remotecontrol.Draft) {
	if draft.ID == "" {
		draft.ID = remotecontrol.NewProfileID()
	}
	d.form = newRemoteForm(d.com, draft)
	d.form.banner = remotecontrol.Msg("Filled from the assistant's findings. Check every field before saving; nothing was saved yet.")
	d.view = remoteEditing
}

func (d *Remote) setNotice(m remotecontrol.Message, ok bool) {
	d.notice, d.noticeOK = m, ok
}

// SetNotice shows the outcome of an action handled by the UI.
func (d *Remote) SetNotice(m remotecontrol.Message, ok bool) { d.setNotice(m, ok) }

func (d *Remote) actions() []remoteAction {
	p, has := d.active()
	snap, _ := d.snapshot()
	var out []remoteAction
	add := func(id, label, hint string) { out = append(out, remoteAction{id: id, label: label, hint: hint}) }
	if snap.Busy || snap.Journal != nil {
		add(rcProgress, "Show progress and results", "")
	}
	addHomeNetwork := func() {
		if d.homeLAN() && (d.gateway() != nil || d.env.Starting != nil && d.env.Starting()) {
			add(rcLANStop, "Stop home network test", "Saved server settings stay unchanged")
			if d.gateway() != nil {
				add(rcLAN, "Show home network QR", "")
			}
		} else {
			add(rcLAN, "Try on home network", "Same Wi-Fi or Ethernet; no VPS needed")
		}
	}
	if !has {
		add(rcAdd, "Connect my existing server", "Enter SSH and HTTPS details")
		add(rcHelp, "Help me set up a server", "The assistant helps in your provider's web panel")
		add(rcDraft, "Use details found by the assistant", "")
		addHomeNetwork()
		add(rcDiag, "Diagnostics and logs", "")
		return out
	}
	add(rcTest, "Test connection", "Read-only checks over SSH and HTTPS")
	label := "Install on the server"
	if snap.Report != nil && snap.Report.Managed && snap.Report.State != nil {
		label = "Update the installation"
	}
	add(rcInstall, label, "Shows the plan and asks before changing anything")
	add(rcPair, "Pair a phone / show QR", "")
	add(rcDevices, "Manage devices", "")
	if d.enabled(p) {
		add(rcToggle, "Turn remote access off", "This computer disconnects; the server keeps running")
	} else {
		add(rcToggle, "Turn remote access on", "")
	}
	add(rcDiag, "Diagnostics and logs", "")
	add(rcEdit, "Edit server profile", "")
	add(rcAdd, "Connect another server", "")
	if len(d.cfg().RemoteProfiles) > 1 {
		add(rcSwitch, "Switch active server", "")
	}
	add(rcHelp, "Help me set up a server", "")
	add(rcDraft, "Use details found by the assistant", "")
	if snap.Report != nil && len(snap.Report.Backups) > 0 {
		add(rcRollback, "Restore the previous version", "")
	}
	add(rcRemove, "Remove the service from the server", "Asks for approval first")
	add(rcDelete, "Delete this profile from this computer", "The server is not changed")
	addHomeNetwork()
	return out
}

// HandleMsg implements Dialog.
func (d *Remote) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case RemoteTick:
		if snap, ok := d.snapshot(); ok {
			if snap.HostKey != nil && d.view != remoteHostKey && d.view != remoteEditing {
				d.view = remoteHostKey
			}
			if snap.Busy && d.view == remoteMain {
				d.view = remoteProgress
			}
		}
		return ActionCmd{Cmd: d.tick()}
	case tea.PasteMsg:
		if d.view == remoteEditing && d.form != nil {
			return d.form.paste(msg)
		}
	case tea.KeyPressMsg:
		switch d.view {
		case remoteEditing:
			action, back := d.form.handleKey(msg)
			if back {
				d.form.clearSecret()
				d.form = nil
				d.view = remoteMain
				return nil
			}
			return action
		case remoteHostKey:
			return d.handleHostKey(msg)
		case remoteConfirm:
			switch msg.String() {
			case "y", "Y":
				c := d.confirm
				d.confirm, d.view = nil, remoteMain
				if c != nil && c.action != nil {
					return c.action()
				}
			case "n", "N", "esc":
				d.confirm, d.view = nil, remoteMain
			}
			return nil
		case remoteProgress, remoteDiagnostics:
			return d.handleScrollView(msg)
		case remoteProfiles:
			return d.handleProfiles(msg)
		case remoteHomeNetwork:
			return d.handleHomeNetwork(msg)
		}
		return d.handleMain(msg)
	}
	return nil
}

func (d *Remote) handleMain(msg tea.KeyPressMsg) Action {
	actions := d.actions()
	switch {
	case key.Matches(msg, d.keyMap.Back):
		return ActionClose{}
	case key.Matches(msg, d.keyMap.Up):
		d.selected = max(0, d.selected-1)
	case key.Matches(msg, d.keyMap.Down):
		d.selected = min(len(actions)-1, d.selected+1)
	case key.Matches(msg, d.keyMap.Enter):
		if d.selected >= len(actions) {
			d.selected = len(actions) - 1
		}
		if d.selected >= 0 {
			return d.run(actions[d.selected].id)
		}
	}
	return nil
}

func (d *Remote) run(id string) Action {
	d.notice = remotecontrol.Message{}
	p, has := d.active()
	mgr := d.env.Manager
	switch id {
	case rcLAN:
		if d.homeLAN() && d.gateway() != nil {
			return ActionOpenMobile{}
		}
		d.setNotice(remotecontrol.Msg("Finding home network addresses…"), true)
		return ActionRemoteLAN{}
	case rcLANStop:
		return ActionStopMobile{}
	case rcProgress:
		d.view, d.scroll = remoteProgress, 0
	case rcAdd:
		draft := remotecontrol.Draft{ID: remotecontrol.NewProfileID(), SSHPort: "22", SSHAuth: remotecontrol.AuthKey, Timeout: "15"}
		if len(d.cfg().RemoteProfiles) == 0 {
			draft.Name = "vps"
		}
		d.form = newRemoteForm(d.com, draft)
		d.view = remoteEditing
	case rcEdit:
		d.form = newRemoteForm(d.com, remotecontrol.DraftFromProfile(p))
		d.view = remoteEditing
	case rcHelp:
		return ActionRemoteBrowserHelp{}
	case rcDraft:
		return ActionRemoteImportDraft{}
	case rcDiag:
		d.view, d.scroll = remoteDiagnostics, 0
	case rcSwitch:
		d.view, d.profSel = remoteProfiles, 0
	case rcTest, rcInstall, rcRollback:
		if !has || mgr == nil {
			return nil
		}
		d.lastOp = id
		d.view, d.scroll = remoteProgress, 0
		return ActionCmd{Cmd: d.startOp(id, p)}
	case rcPair, rcDevices:
		if !d.enabled(p) {
			d.setNotice(remotecontrol.Msg("Turn remote access on first; phones pair through the relay."), false)
			return nil
		}
		return ActionRemotePair{}
	case rcToggle:
		if d.enabled(p) {
			d.ask(fmt.Sprintf(d.com.Text("Turn remote access off? Phones lose access until it is turned on again. The server at %s keeps running and billing."), p.SSHHost), func() Action { return ActionRemoteDisable{} })
			return nil
		}
		return ActionRemoteEnable{ID: p.ID}
	case rcRemove:
		d.ask(fmt.Sprintf(d.com.Text("Remove the Atlas relay service from %s? You will see the exact list and approve it in a permission dialog next. The server itself and its billing stay until you delete it at your provider."), p.SSHHost), func() Action {
			d.lastOp = rcRemove
			d.view, d.scroll = remoteProgress, 0
			return ActionCmd{Cmd: d.startOp(rcRemove, p)}
		})
	case rcDelete:
		d.ask(fmt.Sprintf(d.com.Text("Delete the profile %q from this computer? Its saved secret and connector key are erased here. The server is not contacted; remove the service first if you no longer need it."), p.Name), func() Action { return ActionRemoteDeleteProfile{ID: p.ID} })
	}
	return nil
}

func (d *Remote) ask(text string, action func() Action) {
	d.confirm = &remoteConfirmation{text: text, action: action}
	d.view = remoteConfirm
}

// startOp returns a command that starts a manager operation off the UI
// loop; progress is read from snapshots.
func (d *Remote) startOp(id string, p config.RemoteProfile) tea.Cmd {
	mgr := d.env.Manager
	return func() tea.Msg {
		var err error
		switch id {
		case rcTest:
			err = mgr.Test(p)
		case rcInstall:
			err = mgr.Install(p)
		case rcRollback:
			err = mgr.Rollback(p)
		case rcRemove:
			err = mgr.RemoveService(p)
		}
		switch {
		case errors.Is(err, remotecontrol.ErrBusy):
			return util.NewWarnMsg(d.com.Text("Another remote operation is still running."))
		case err != nil:
			return util.NewErrorMsg(err)
		}
		return nil
	}
}

func (d *Remote) handleHostKey(msg tea.KeyPressMsg) Action {
	snap, ok := d.snapshot()
	p, _ := d.active()
	if !ok || snap.HostKey == nil {
		d.view = remoteMain
		return nil
	}
	decision := *snap.HostKey
	trust := func() Action {
		mgr, op := d.env.Manager, d.lastOp
		if op == "" {
			op = rcTest
		}
		d.view = remoteProgress
		return ActionCmd{Cmd: func() tea.Msg {
			if err := mgr.TrustHostKey(p.ID, decision); err != nil {
				return util.NewErrorMsg(err)
			}
			return d.startOp(op, p)()
		}}
	}
	switch {
	case key.Matches(msg, d.keyMap.Back):
		d.env.Manager.DismissHostKey(p.ID)
		d.view = remoteMain
		d.setNotice(remotecontrol.Msg("The host key was not trusted; nothing was sent to the server."), false)
	case !decision.Changed && key.Matches(msg, d.keyMap.Trust):
		return trust()
	case decision.Changed && !decision.Revoked && key.Matches(msg, d.keyMap.Replace):
		return trust()
	}
	return nil
}

func (d *Remote) handleScrollView(msg tea.KeyPressMsg) Action {
	switch {
	case key.Matches(msg, d.keyMap.Back):
		d.view = remoteMain
	case key.Matches(msg, d.keyMap.Up):
		d.scroll = max(0, d.scroll-1)
	case key.Matches(msg, d.keyMap.Down):
		d.scroll++
	case msg.String() == "pgdown":
		d.scroll += 10
	case msg.String() == "pgup":
		d.scroll = max(0, d.scroll-10)
	case d.view == remoteProgress && key.Matches(msg, d.keyMap.Cancel):
		if snap, ok := d.snapshot(); ok && snap.Busy {
			d.env.Manager.Cancel()
		}
	case d.view == remoteProgress && key.Matches(msg, d.keyMap.Retry):
		snap, ok := d.snapshot()
		p, has := d.active()
		if ok && has && !snap.Busy && d.lastOp != "" {
			return ActionCmd{Cmd: d.startOp(d.lastOp, p)}
		}
	}
	return nil
}

func (d *Remote) handleProfiles(msg tea.KeyPressMsg) Action {
	profiles := d.cfg().RemoteProfiles
	switch {
	case key.Matches(msg, d.keyMap.Back):
		d.view = remoteMain
	case key.Matches(msg, d.keyMap.Up):
		d.profSel = max(0, d.profSel-1)
	case key.Matches(msg, d.keyMap.Down):
		d.profSel = min(len(profiles)-1, d.profSel+1)
	case key.Matches(msg, d.keyMap.Enter):
		if d.profSel >= 0 && d.profSel < len(profiles) {
			d.view = remoteMain
			return ActionRemoteSetActive{ID: profiles[d.profSel].ID}
		}
	}
	return nil
}

// Draw implements Dialog.
func (d *Remote) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := d.com.Styles
	width := max(0, min(88, area.Dx()-t.Dialog.View.GetHorizontalBorderSize()))
	inner := max(10, width-t.Dialog.View.GetHorizontalFrameSize())
	// Text is inset by one column, like the dialog title.
	text := inner - 1
	rc := NewRenderContext(t, width)
	rc.Title = d.com.Text("Remote Control")
	if p, ok := d.active(); ok {
		rc.TitleInfo = t.Dialog.SecondaryText.Padding(0).Render(" " + ansi.Truncate(p.Name, 24, "…"))
	}
	rc.Help = renderDialogHelp(t, &d.help, d, inner)
	// Title, help and frame take about five rows.
	height := max(4, area.Dy()-t.Dialog.View.GetVerticalFrameSize()-4)

	var lines []string
	focus := 0
	switch d.view {
	case remoteEditing:
		if d.form != nil {
			lines, focus = d.form.lines(text)
		}
	case remoteHostKey:
		lines = d.hostKeyLines(text)
	case remoteProgress:
		lines = d.progressLines(text)
		focus = -1
	case remoteDiagnostics:
		lines = d.diagnosticLines(text)
		focus = -1
	case remoteConfirm:
		if d.confirm != nil {
			lines = wrapLines(t.Dialog.PrimaryText.Padding(0), d.confirm.text, text)
			lines = append(lines, "", t.Dialog.SecondaryText.Padding(0).Render(d.com.Text("y confirm · n cancel")))
		}
	case remoteProfiles:
		lines, focus = d.profileLines(text)
	case remoteHomeNetwork:
		lines, focus = d.homeNetworkLines(text)
	default:
		lines, focus = d.mainLines(text, height)
	}
	if focus < 0 {
		d.scroll = min(d.scroll, max(0, len(lines)-height))
		lines = window(lines, d.scroll, height)
	} else {
		lines = windowAround(lines, focus, height)
	}
	for i, l := range lines {
		lines[i] = " " + ansi.Truncate(l, text, "…")
	}
	rc.AddPart(strings.Join(lines, "\n"))
	DrawCenterCursor(scr, area, rc.Render(), nil)
	return nil
}

// window returns at most height lines starting at offset.
func window(lines []string, offset, height int) []string {
	if len(lines) <= height {
		return lines
	}
	offset = min(max(0, offset), len(lines)-height)
	return lines[offset : offset+height]
}

// windowAround keeps the focused line and a few after it in view.
func windowAround(lines []string, focus, height int) []string {
	if len(lines) <= height {
		return lines
	}
	start := max(0, min(focus-height/3, len(lines)-height))
	return lines[start : start+height]
}

// wrapLines renders text in style at width and splits it into lines.
func wrapLines(style lipgloss.Style, text string, width int) []string {
	return strings.Split(style.Width(width).Render(text), "\n")
}

// ShortHelp implements help.KeyMap.
func (d *Remote) ShortHelp() []key.Binding {
	switch d.view {
	case remoteEditing:
		return []key.Binding{
			key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", d.com.Text("next field"))),
			key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", d.com.Text("save"))),
			d.keyMap.Back,
		}
	case remoteHostKey:
		snap, _ := d.snapshot()
		if snap.HostKey != nil && snap.HostKey.Changed {
			if snap.HostKey.Revoked {
				return []key.Binding{d.keyMap.Back}
			}
			return []key.Binding{d.keyMap.Replace, d.keyMap.Back}
		}
		return []key.Binding{d.keyMap.Trust, d.keyMap.Back}
	case remoteProgress:
		snap, _ := d.snapshot()
		if snap.Busy {
			return []key.Binding{d.keyMap.Cancel, d.keyMap.Up, d.keyMap.Down, d.keyMap.Back}
		}
		return []key.Binding{d.keyMap.Retry, d.keyMap.Up, d.keyMap.Down, d.keyMap.Back}
	case remoteDiagnostics:
		return []key.Binding{d.keyMap.Up, d.keyMap.Down, d.keyMap.Back}
	case remoteConfirm:
		return []key.Binding{d.keyMap.Back}
	}
	return []key.Binding{d.keyMap.Up, d.keyMap.Down, d.keyMap.Enter, CloseKey}
}

// FullHelp implements help.KeyMap.
func (d *Remote) FullHelp() [][]key.Binding { return [][]key.Binding{d.ShortHelp()} }
