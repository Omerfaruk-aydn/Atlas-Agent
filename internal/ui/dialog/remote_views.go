package dialog

import (
	"fmt"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ansi"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-style/v2"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/remotecontrol"
)

// Overall remote access states shown at the top of /rc.
const (
	rcStateNotSetUp     = "Not set up"
	rcStateOff          = "Off"
	rcStateConnecting   = "Connecting…"
	rcStateConnected    = "Connected"
	rcStateDisconnected = "Disconnected"
	rcStateError        = "Error"
)

// overallState derives the headline from configuration, the running
// gateway and the last checks; nothing here is assumed.
func (d *Remote) overallState() (string, lipgloss.Style, remotecontrol.Message) {
	t := d.com.Styles
	if d.funnelOn() && !d.homeLAN() {
		return d.funnelState()
	}
	if d.homeLAN() {
		switch {
		case d.env.Starting != nil && d.env.Starting():
			return rcStateConnecting, t.Dialog.SecondaryText.Padding(0), remotecontrol.Message{}
		case d.env.StartErr != nil && d.env.StartErr() != nil:
			return rcStateError, t.Dialog.OAuth.ErrorText, remotecontrol.Msg("Home network test failed: %s", d.env.StartErr().Error())
		case d.gateway() != nil:
			return "Home network test", t.Dialog.OAuth.Success, remotecontrol.Msg("Only available on the same home network. Saved server settings stay unchanged.")
		default:
			return rcStateOff, t.Dialog.SecondaryText.Padding(0), remotecontrol.Msg("Home network test stopped. Saved server settings stay unchanged.")
		}
	}
	p, has := d.active()
	switch {
	case !has:
		return rcStateNotSetUp, t.Dialog.SecondaryText.Padding(0), remotecontrol.Msg("Connect a server you own, or let the assistant help you set one up.")
	case !d.enabled(p):
		return rcStateOff, t.Dialog.SecondaryText.Padding(0), remotecontrol.Message{}
	case d.env.Starting != nil && d.env.Starting():
		return rcStateConnecting, t.Dialog.SecondaryText.Padding(0), remotecontrol.Message{}
	}
	gw := d.gateway()
	switch {
	case gw != nil && gw.RelayHost() == p.PublicHost && gw.RelayConnected():
		return rcStateConnected, t.Dialog.OAuth.Success, remotecontrol.Message{}
	case gw != nil && gw.RelayHost() == p.PublicHost:
		return rcStateDisconnected, t.Dialog.OAuth.ErrorText, remotecontrol.Msg("The relay connection dropped; Atlas reconnects automatically with increasing delays.")
	case gw != nil:
		return rcStateDisconnected, t.Dialog.OAuth.ErrorText, remotecontrol.Msg("Phone control runs without the relay. Turn remote access off and on again.")
	}
	if d.env.StartErr != nil {
		if err := d.env.StartErr(); err != nil {
			return rcStateError, t.Dialog.OAuth.ErrorText, remotecontrol.Msg("This computer could not connect to the relay: %s", err.Error())
		}
	}
	return rcStateDisconnected, t.Dialog.OAuth.ErrorText, remotecontrol.Msg("Atlas is not connected to the relay on this computer yet.")
}

// phoneCountLine summarizes the running gateway's phones.
func (d *Remote) phoneCountLine() string {
	gw := d.gateway()
	if gw == nil {
		return ""
	}
	paired, active := 0, 0
	for _, dev := range gw.Auth.Devices() {
		paired++
		if time.Since(time.UnixMilli(dev.LastSeen)) < 2*time.Minute {
			active++
		}
	}
	return d.com.Styles.Dialog.SecondaryText.Padding(0).Render(fmt.Sprintf(d.com.Text("Phones: %d paired · %d active in the last 2 minutes · %d waiting for approval"), paired, active, len(gw.Auth.PendingList())))
}

func (d *Remote) checkLine(label string, c remotecontrol.Check) string {
	t := d.com.Styles
	icon, style := "·", t.Dialog.SecondaryText.Padding(0)
	switch c.State {
	case remotecontrol.CheckOK:
		icon, style = "✓", t.Dialog.OAuth.Success
	case remotecontrol.CheckFailed:
		icon, style = "✗", t.Dialog.OAuth.ErrorText
	case remotecontrol.CheckPending:
		icon = "…"
	}
	detail := c.Detail.Localize(d.com.Text)
	if detail == "" {
		detail = d.com.Text("not checked yet")
	}
	return style.Render(icon+" "+d.com.Text(label)+": ") + t.Dialog.SecondaryText.Padding(0).Render(detail)
}

func (d *Remote) statusLines(width int) []string {
	t := d.com.Styles
	var lines []string
	state, style, note := d.overallState()
	lines = append(lines, t.Dialog.PrimaryText.Padding(0).Render(d.com.Text("Remote access")+": ")+style.Render(d.com.Text(state)))
	if !note.IsZero() {
		lines = append(lines, wrapLines(t.Dialog.SecondaryText.Padding(0), note.Localize(d.com.Text), width)...)
	}
	if d.homeLAN() {
		return lines
	}
	if d.funnelOn() {
		return append(lines, d.funnelLines(width)...)
	}
	p, has := d.active()
	if !has {
		return lines
	}
	lines = append(lines,
		t.Dialog.SecondaryText.Padding(0).Render(fmt.Sprintf(d.com.Text("Server: %s · SSH %s"), p.Name, p.SSHUser+"@"+remotecontrol.HostAddress(p.SSHHost, p.Port()))),
		t.Dialog.SecondaryText.Padding(0).Render(fmt.Sprintf(d.com.Text("Phone address: %s"), "https://"+p.PublicHost)),
	)
	snap, _ := d.snapshot()
	lines = append(lines,
		d.checkLine("SSH", snap.Checks[remotecontrol.CheckSSH]),
		d.checkLine("Relay service", snap.Checks[remotecontrol.CheckService]),
		d.checkLine("HTTPS", snap.Checks[remotecontrol.CheckHTTPS]),
		d.checkLine("Relay link", snap.Checks[remotecontrol.CheckLink]),
	)
	gw := d.gateway()
	desktop := d.com.Text("not running")
	if gw != nil && gw.RelayHost() == p.PublicHost {
		desktop = d.com.Text("reconnecting")
		if gw.RelayConnected() {
			desktop = d.com.Text("connected to the relay")
		}
	}
	lines = append(lines, t.Dialog.SecondaryText.Padding(0).Render(fmt.Sprintf(d.com.Text("Atlas on this computer: %s"), desktop)))
	if gw != nil {
		lines = append(lines, d.phoneCountLine())
	}
	last := d.com.Text("never")
	if !snap.LastCheck.IsZero() {
		last = snap.LastCheck.Format("15:04:05") + " (" + t.RelativeTime(snap.LastCheck) + ")"
	}
	lines = append(lines, t.Dialog.SecondaryText.Padding(0).Render(fmt.Sprintf(d.com.Text("Last server check: %s"), last)))
	switch {
	case snap.AwaitingApproval:
		lines = append(lines, wrapLines(t.Dialog.TitleAccent, d.com.Text("Waiting for your approval in the permission dialog."), width)...)
	case snap.Busy:
		lines = append(lines, t.Dialog.TitleAccent.Render(fmt.Sprintf(d.com.Text("Running: %s"), d.stepTitle(snap.OpStep))))
	case snap.Journal != nil && snap.Journal.Status == remotecontrol.StatusInterrupted:
		lines = append(lines, wrapLines(t.Dialog.OAuth.ErrorText, d.com.Text("An earlier operation was interrupted when Atlas closed. Run it again to continue; finished steps are checked and skipped."), width)...)
	}
	if snap.Err != nil && !snap.Busy {
		lines = append(lines, d.errorLines(snap.Err, width)...)
	}
	if snap.Assessment != nil && len(snap.Assessment.Blocking) > 0 && !snap.Busy {
		lines = append(lines, t.Dialog.OAuth.ErrorText.Render(d.com.Text("Before installing:")))
		for i, m := range snap.Assessment.Blocking {
			if i == 3 {
				lines = append(lines, t.Dialog.SecondaryText.Padding(0).Render("  "+d.com.Text("More in Diagnostics.")))
				break
			}
			lines = append(lines, wrapLines(t.Dialog.SecondaryText.Padding(0), "  • "+m.Localize(d.com.Text), width)...)
		}
	}
	return lines
}

func (d *Remote) errorLines(e *remotecontrol.Error, width int) []string {
	t := d.com.Styles
	lines := wrapLines(t.Dialog.OAuth.ErrorText, e.What.Localize(d.com.Text), width)
	if !e.Next.IsZero() {
		lines = append(lines, wrapLines(t.Dialog.SecondaryText.Padding(0), d.com.Text("What you can do: ")+e.Next.Localize(d.com.Text), width)...)
	}
	return lines
}

func (d *Remote) stepTitle(step string) string {
	switch step {
	case "", remotecontrol.CheckSSH:
		return d.com.Text("Connecting over SSH")
	case "preflight":
		return d.com.Text("Checking the server (read-only)")
	case "approval":
		return d.com.Text("Waiting for approval")
	}
	return remotecontrol.StepTitle(step).Localize(d.com.Text)
}

func (d *Remote) mainLines(width, height int) ([]string, int) {
	t := d.com.Styles
	lines := d.statusLines(width)
	if !d.notice.IsZero() {
		style := t.Dialog.OAuth.ErrorText
		if d.noticeOK {
			style = t.Dialog.OAuth.Success
		}
		lines = append(lines, wrapLines(style, d.notice.Localize(d.com.Text), width)...)
	}
	lines = append(lines, "")
	actions := d.actions()
	d.selected = min(max(0, d.selected), len(actions)-1)
	focus := len(lines)
	for i, a := range actions {
		label := d.com.Text(a.label)
		if a.hint != "" && width > 50 {
			label += t.Dialog.SecondaryText.Padding(0).Render(" — " + d.com.Text(a.hint))
		}
		if i == d.selected {
			focus = len(lines)
			lines = append(lines, t.Dialog.SelectedItem.Width(width).Render(stripStyles(label)))
		} else {
			lines = append(lines, t.Dialog.NormalItem.Width(width).Render(label))
		}
	}
	// Keep the whole status visible when it fits; otherwise follow the
	// selection.
	if len(lines) > height && focus < height {
		focus = 0
	}
	return lines, focus
}

func (d *Remote) hostKeyLines(width int) []string {
	t := d.com.Styles
	snap, _ := d.snapshot()
	k := snap.HostKey
	if k == nil {
		return []string{t.Dialog.SecondaryText.Padding(0).Render(d.com.Text("No host key decision is pending."))}
	}
	var lines []string
	if !k.Changed {
		lines = append(lines, wrapLines(t.Dialog.PrimaryText.Padding(0), fmt.Sprintf(d.com.Text("First connection to %s. Compare this fingerprint with the one your provider shows (web console or panel) before trusting it."), k.Address), width)...)
		lines = append(lines, "", t.Dialog.TitleAccent.Render("  "+k.Algorithm+" "+k.Fingerprint), "")
		lines = append(lines, wrapLines(t.Dialog.SecondaryText.Padding(0), d.com.Text("Trusting stores it in Atlas' own known_hosts file. ~/.ssh/known_hosts is not changed."), width)...)
		return lines
	}
	lines = append(lines, wrapLines(t.Dialog.OAuth.ErrorText, fmt.Sprintf(d.com.Text("The SSH host key of %s has changed. Reinstalling a server does this, but so can someone intercepting the connection. Nothing was sent to the server."), k.Address), width)...)
	lines = append(lines, "", t.Dialog.SecondaryText.Padding(0).Render(d.com.Text("Trusted until now:")))
	for _, prev := range k.Previous {
		lines = append(lines, t.Dialog.SecondaryText.Padding(0).Render("  "+prev))
	}
	lines = append(lines, t.Dialog.SecondaryText.Padding(0).Render(d.com.Text("Presented now:")), t.Dialog.TitleAccent.Render("  "+k.Algorithm+" "+k.Fingerprint), "")
	switch {
	case k.Revoked:
		lines = append(lines, wrapLines(t.Dialog.OAuth.ErrorText, d.com.Text("This key is marked as revoked and cannot be trusted."), width)...)
	case k.InUserFile:
		lines = append(lines, wrapLines(t.Dialog.SecondaryText.Padding(0), d.com.Text("The old key comes from ~/.ssh/known_hosts, which Atlas does not edit. Replacing records the new key for /rc only."), width)...)
		lines = append(lines, wrapLines(t.Dialog.SecondaryText.Padding(0), d.com.Text("Replace it only if your provider confirms the new fingerprint."), width)...)
	default:
		lines = append(lines, wrapLines(t.Dialog.SecondaryText.Padding(0), d.com.Text("Replace it only if your provider confirms the new fingerprint."), width)...)
	}
	return lines
}

func (d *Remote) opTitle(kind string, busy bool) string {
	switch {
	case kind == remotecontrol.OpInstall && busy:
		return d.com.Text("Installing…")
	case kind == remotecontrol.OpInstall:
		return d.com.Text("Installation")
	case kind == remotecontrol.OpRollback && busy:
		return d.com.Text("Restoring the previous version…")
	case kind == remotecontrol.OpRollback:
		return d.com.Text("Restore")
	case kind == remotecontrol.OpRemove && busy:
		return d.com.Text("Removing the service…")
	case kind == remotecontrol.OpRemove:
		return d.com.Text("Removal")
	case busy:
		return d.com.Text("Testing…")
	}
	return d.com.Text("Connection test")
}

func (d *Remote) journalStatus(status string) (string, lipgloss.Style) {
	t := d.com.Styles
	switch status {
	case remotecontrol.StatusDone:
		return "✓", t.Dialog.OAuth.Success
	case remotecontrol.StatusSkipped:
		return "–", t.Dialog.SecondaryText.Padding(0)
	case remotecontrol.StatusRunning:
		return "…", t.Dialog.TitleAccent
	case remotecontrol.StatusFailed, remotecontrol.StatusInterrupted, remotecontrol.StatusCanceled:
		return "✗", t.Dialog.OAuth.ErrorText
	}
	return "○", t.Dialog.SecondaryText.Padding(0)
}

func (d *Remote) progressLines(width int) []string {
	t := d.com.Styles
	snap, _ := d.snapshot()
	kind := snap.OpKind
	if kind == "" && snap.Journal != nil {
		kind = snap.Journal.Kind
	}
	if kind == "" {
		kind = remotecontrol.OpTest
	}
	var lines []string
	lines = append(lines, t.Dialog.TitleAccent.Render(d.opTitle(kind, snap.Busy)))
	if snap.Busy {
		lines = append(lines, t.Dialog.SecondaryText.Padding(0).Render(fmt.Sprintf(d.com.Text("Current step: %s (started %s)"), d.stepTitle(snap.OpStep), t.RelativeTime(snap.OpStarted))))
	}
	if snap.AwaitingApproval {
		lines = append(lines, wrapLines(t.Dialog.PrimaryText.Padding(0), d.com.Text("Review the plan below and answer the permission dialog. Nothing on the server changes before you approve."), width)...)
	}
	if snap.Plan != nil && (snap.AwaitingApproval || kind == remotecontrol.OpInstall) {
		lines = append(lines, "", t.Dialog.PrimaryText.Padding(0).Render(d.com.Text("Plan")))
		for l := range strings.SplitSeq(snap.Plan.Describe(d.com.Text), "\n") {
			lines = append(lines, wrapLines(t.Dialog.SecondaryText.Padding(0), l, width)...)
		}
	}
	if snap.Journal != nil && snap.Journal.Kind == kind && kind != remotecontrol.OpTest {
		lines = append(lines, "", t.Dialog.PrimaryText.Padding(0).Render(d.com.Text("Steps")))
		for _, s := range snap.Journal.Steps {
			icon, style := d.journalStatus(s.Status)
			line := style.Render(icon+" ") + d.stepTitle(s.ID)
			if !s.Detail.IsZero() {
				line += t.Dialog.SecondaryText.Padding(0).Render(" — " + s.Detail.Localize(d.com.Text))
			}
			lines = append(lines, line)
		}
	}
	if kind == remotecontrol.OpTest || snap.Journal == nil {
		lines = append(lines, "",
			d.checkLine("SSH", snap.Checks[remotecontrol.CheckSSH]),
			d.checkLine("Relay service", snap.Checks[remotecontrol.CheckService]),
			d.checkLine("HTTPS", snap.Checks[remotecontrol.CheckHTTPS]),
			d.checkLine("Relay link", snap.Checks[remotecontrol.CheckLink]))
	}
	if a := snap.Assessment; a != nil && !snap.Busy {
		if len(a.Blocking) > 0 {
			lines = append(lines, "", t.Dialog.OAuth.ErrorText.Render(d.com.Text("Server check: needs attention")))
			for _, m := range a.Blocking {
				lines = append(lines, wrapLines(t.Dialog.SecondaryText.Padding(0), "  • "+m.Localize(d.com.Text), width)...)
			}
		} else if snap.Report != nil {
			lines = append(lines, "", t.Dialog.OAuth.Success.Render(d.com.Text("Server check: supported")))
		}
		for _, m := range a.Warnings {
			lines = append(lines, wrapLines(t.Dialog.SecondaryText.Padding(0), "  ! "+m.Localize(d.com.Text), width)...)
		}
	}
	if !snap.Busy {
		lines = append(lines, "")
		switch {
		case snap.Err != nil:
			lines = append(lines, d.errorLines(snap.Err, width)...)
		case snap.Journal != nil && snap.Journal.Kind == kind && snap.Journal.Status == remotecontrol.StatusSucceeded && kind == remotecontrol.OpInstall:
			lines = append(lines, wrapLines(t.Dialog.OAuth.Success, d.com.Text("Ready: HTTPS and the relay link are verified. Turn remote access on and pair a phone."), width)...)
		case snap.Journal != nil && snap.Journal.Kind == kind && snap.Journal.Status == remotecontrol.StatusSucceeded:
			lines = append(lines, t.Dialog.OAuth.Success.Render(d.com.Text("Done.")))
		case kind == remotecontrol.OpTest && !snap.LastCheck.IsZero():
			lines = append(lines, t.Dialog.OAuth.Success.Render(d.com.Text("Checks finished.")))
		}
	}
	return lines
}

func (d *Remote) diagnosticLines(width int) []string {
	t := d.com.Styles
	h := func(s string) string { return t.Dialog.PrimaryText.Padding(0).Render(s) }
	kv := func(k, v string) string { return t.Dialog.SecondaryText.Padding(0).Render(d.com.Text(k) + ": " + v) }
	var lines []string
	p, has := d.active()
	if has {
		lines = append(lines, h(d.com.Text("Profile")),
			kv("Name", p.Name),
			kv("SSH", p.SSHUser+"@"+remotecontrol.HostAddress(p.SSHHost, p.Port())),
			kv("Authentication", d.com.Text(authLabel(p.SSHAuth))),
			kv("Phone address", "https://"+p.PublicHost))
		if p.SSHKeyFile != "" {
			lines = append(lines, kv("Key file", p.SSHKeyFile))
		}
		secret := d.com.Text("none")
		if p.SecretRef != "" {
			secret = d.com.Text("stored sealed (not shown)")
		}
		lines = append(lines, kv("Secret", secret))
	}
	snap, _ := d.snapshot()
	if r := snap.Report; r != nil {
		lines = append(lines, "", h(d.com.Text("Server")),
			kv("System", fmt.Sprintf("%s (%s, %s)", r.OSName, r.Machine, r.Kernel)),
			kv("Privileges", r.Privilege),
			kv("Docker", orDash(r.DockerVersion)+" / Compose "+orDash(r.ComposeVersion)),
			kv("Free disk", fmt.Sprintf("%d MiB", r.DiskKB/1024)),
			kv("Memory", fmt.Sprintf("%d MiB", r.MemKB/1024)),
			kv("Firewall", "ufw "+orDash(r.UFW)+", firewalld "+orDash(r.Firewalld)),
			kv("Addresses", orDash(strings.Join(r.Addresses, " "))),
			kv("DNS", fmt.Sprintf("%s → %s", r.DNS.Host, orDash(strings.Join(r.DNS.Addresses, ", ")))))
		if r.State != nil {
			lines = append(lines, kv("Installed relay", r.State.Version+" "+short12(r.State.SHA256)+" ("+r.State.InstalledAt+")"))
		}
		if len(r.Backups) > 0 {
			lines = append(lines, kv("Backups", strings.Join(r.Backups, " ")))
		}
		var svc []string
		for s, st := range r.Services {
			svc = append(svc, s+"="+st)
		}
		if len(svc) > 0 {
			lines = append(lines, kv("Services", strings.Join(svc, " ")))
		}
	}
	if a := snap.Assessment; a != nil {
		for _, m := range a.Blocking {
			lines = append(lines, wrapLines(t.Dialog.OAuth.ErrorText, "✗ "+m.Localize(d.com.Text), width)...)
		}
		for _, m := range a.Warnings {
			lines = append(lines, wrapLines(t.Dialog.SecondaryText.Padding(0), "! "+m.Localize(d.com.Text), width)...)
		}
	}
	if j := snap.Journal; j != nil {
		lines = append(lines, "", h(fmt.Sprintf(d.com.Text("Last operation: %s, %s"), j.Kind, j.Status)))
		lines = append(lines, kv("Started", j.Started.Format(time.DateTime)))
		for _, s := range j.Steps {
			line := "  " + s.ID + ": " + s.Status
			if !s.Detail.IsZero() {
				line += " — " + s.Detail.Localize(d.com.Text)
			}
			lines = append(lines, t.Dialog.SecondaryText.Padding(0).Render(line))
		}
		if j.Error != nil {
			lines = append(lines, d.errorLines(j.Error, width)...)
		}
	}
	if gw := d.gateway(); gw != nil {
		lines = append(lines, "", h(d.com.Text("Phone gateway")),
			kv("Relay", orDash(gw.RelayHost())),
			kv("Connected", fmt.Sprint(gw.RelayConnected())),
			kv("Local certificate", shortFingerprint(gw.Fingerprint())))
	}
	lines = append(lines, d.funnelDiagnostics()...)
	if len(snap.Diagnostics) > 0 {
		lines = append(lines, "", h(d.com.Text("Server output (secrets removed)")))
		for _, l := range snap.Diagnostics {
			lines = append(lines, t.Dialog.SecondaryText.Padding(0).Render(l))
		}
	}
	if len(lines) == 0 {
		lines = append(lines, t.Dialog.SecondaryText.Padding(0).Render(d.com.Text("Nothing to show yet. Add a server and test the connection.")))
	}
	return lines
}

func (d *Remote) profileLines(width int) ([]string, int) {
	t := d.com.Styles
	cfg := d.cfg()
	lines := []string{t.Dialog.PrimaryText.Padding(0).Render(d.com.Text("Choose the active server")), ""}
	focus := 0
	for i, p := range cfg.RemoteProfiles {
		mark := "  "
		if p.ID == cfg.RemoteActive {
			mark = "● "
		}
		line := mark + p.Name + " · " + p.PublicHost
		if i == d.profSel {
			focus = len(lines)
			lines = append(lines, t.Dialog.SelectedItem.Width(width).Render(line))
		} else {
			lines = append(lines, t.Dialog.NormalItem.Width(width).Render(line))
		}
	}
	return lines, focus
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func short12(s string) string {
	if len(s) > 12 {
		return s[:12] + "…"
	}
	return s
}

// stripStyles removes styling so a selected row is drawn in one style.
func stripStyles(s string) string { return ansi.Strip(s) }
