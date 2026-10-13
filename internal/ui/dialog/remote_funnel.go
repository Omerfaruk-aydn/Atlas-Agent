package dialog

import (
	"errors"
	"fmt"
	"runtime"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-style/v2"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/remotecontrol"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/tsfunnel"
)

// Tailscale actions of the main menu.
const (
	rcFunnel         = "funnel"
	rcFunnelStop     = "funnel-stop"
	rcFunnelPair     = "funnel-pair"
	rcFunnelCheck    = "funnel-check"
	rcFunnelInstall  = "funnel-install"
	rcFunnelDownload = "funnel-download"
	rcFunnelLogin    = "funnel-login"
	rcFunnelApp      = "funnel-app"
	rcFunnelDNS      = "funnel-dns"
	rcFunnelApprove  = "funnel-approve"
)

func (d *Remote) funnel() FunnelStatus {
	if d.env.Funnel == nil {
		return FunnelStatus{}
	}
	return d.env.Funnel()
}

func (d *Remote) funnelOn() bool { return d.funnel().On }

// approvalURL is the Tailscale page that turns on HTTPS or Funnel, from
// the running start or from a start that exited to ask for it.
func (f FunnelStatus) approvalURL() string {
	var approval *tsfunnel.ApprovalRequiredError
	if errors.As(f.Err, &approval) {
		return approval.URL
	}
	if f.Starting && f.Phase == tsfunnel.PhaseApproval {
		return f.ApprovalURL
	}
	return ""
}

func (f FunnelStatus) busy() bool { return f.Task != "" || f.Checking || f.Starting }

func (f FunnelStatus) lost() bool { return f.URL != "" && f.Phase == tsfunnel.PhaseLost }

// addFunnelActions lists only the steps that fit the checked state.
func (d *Remote) addFunnelActions(add func(id, label, hint string)) {
	f := d.funnel()
	if d.env.Funnel == nil {
		return
	}
	if !f.On {
		add(rcFunnel, "Connect with Tailscale", "No VPS or domain needed")
		return
	}
	if u := f.approvalURL(); u != "" {
		add(rcFunnelApprove, "Open the Tailscale approval page", "Turn on HTTPS and Funnel for this device there")
	}
	switch {
	case f.URL != "":
		add(rcFunnelPair, "Pair a phone / show QR", "")
		if f.lost() {
			add(rcFunnel, "Reconnect with Tailscale", "")
		}
	case f.busy():
	default:
		switch f.Ready.Problem {
		case tsfunnel.ProblemNotInstalled, tsfunnel.ProblemOldVersion:
			if runtime.GOOS == "windows" {
				add(rcFunnelInstall, "Install Tailscale", "Official installer, checked before it runs")
			} else {
				add(rcFunnelDownload, "Open the Tailscale download page", "")
			}
		case tsfunnel.ProblemServiceStopped, tsfunnel.ProblemStarting:
			add(rcFunnelApp, "Open the Tailscale app", "It starts the Tailscale service")
		case tsfunnel.ProblemNeedsLogin, tsfunnel.ProblemDisconnected:
			add(rcFunnelLogin, "Sign in to Tailscale", "Opens Tailscale's sign-in page in your browser")
		case tsfunnel.ProblemNeedsMagicDNS:
			add(rcFunnelDNS, "Open Tailscale DNS settings", "Turn on MagicDNS and HTTPS certificates")
		}
		if f.Ready.Ready() {
			add(rcFunnel, "Reconnect with Tailscale", "")
		}
		add(rcFunnelCheck, "Check Tailscale again", "")
	}
	add(rcFunnelStop, "Stop Tailscale connection", "The address stops reaching this computer")
}

func (d *Remote) runFunnel(id string) Action {
	f := d.funnel()
	switch id {
	case rcFunnel:
		if f.On {
			return ActionRemoteFunnel{}
		}
		d.ask(d.com.Text("Publish Atlas' phone panel at your Tailscale address on the internet? Only phones you pair and approve here can use it. A VPS connection that is on is turned off on this computer; its server is not changed."), func() Action { return ActionRemoteFunnel{} })
	case rcFunnelStop:
		return ActionRemoteFunnelStop{}
	case rcFunnelPair:
		return ActionOpenMobile{}
	case rcFunnelCheck:
		d.setNotice(remotecontrol.Msg("Checking Tailscale…"), true)
		return ActionRemoteFunnelCheck{}
	case rcFunnelInstall:
		d.ask(d.com.Text("Download Tailscale's official Windows installer from pkgs.tailscale.com, check its published SHA-256 and its Tailscale Inc. signature, then open it? Windows asks for administrator approval. You sign in to your own Tailscale account afterwards."), func() Action {
			return ActionRemoteFunnelTask{Task: FunnelTaskInstall}
		})
	case rcFunnelDownload:
		return ActionRemoteFunnelOpen{URL: tsfunnel.DownloadPage}
	case rcFunnelLogin:
		return ActionRemoteFunnelTask{Task: FunnelTaskLogin}
	case rcFunnelApp:
		return ActionRemoteFunnelTask{Task: FunnelTaskApp}
	case rcFunnelDNS:
		return ActionRemoteFunnelOpen{URL: tsfunnel.AdminDNSPage}
	case rcFunnelApprove:
		if u := f.approvalURL(); u != "" {
			return ActionRemoteFunnelOpen{URL: u}
		}
	}
	return nil
}

// funnelState is the /rc headline in Tailscale mode.
func (d *Remote) funnelState() (string, lipgloss.Style, remotecontrol.Message) {
	t := d.com.Styles
	quiet, bad, good, accent := t.Dialog.SecondaryText.Padding(0), t.Dialog.OAuth.ErrorText, t.Dialog.OAuth.Success, t.Dialog.TitleAccent
	f := d.funnel()
	switch f.Task {
	case FunnelTaskInstall:
		return "Installing Tailscale…", quiet, remotecontrol.Msg(installStepText(f.InstallStep))
	case FunnelTaskLogin:
		if f.ApprovalURL != "" {
			return "Waiting for Tailscale sign-in", accent, remotecontrol.Msg("Finish signing in on Tailscale's page in your browser: %s", f.ApprovalURL)
		}
		return "Waiting for Tailscale sign-in", accent, remotecontrol.Msg("Finish signing in on Tailscale's page in your browser.")
	case FunnelTaskApp:
		return "Starting Tailscale…", quiet, remotecontrol.Message{}
	}
	if f.Starting {
		switch f.Phase {
		case tsfunnel.PhasePanel:
			return "Starting the phone panel…", quiet, remotecontrol.Message{}
		case tsfunnel.PhasePanelReady:
			return "Phone panel ready; setting up Funnel…", quiet, remotecontrol.Msg("The panel listens only on this computer. Waiting for Tailscale to list Atlas' Funnel entry.")
		case tsfunnel.PhaseApproval:
			return "Waiting for your approval", accent, remotecontrol.Msg("Turn on HTTPS and Funnel for this device on Tailscale's page; Atlas continues by itself: %s", f.ApprovalURL)
		}
		return "Checking Tailscale…", quiet, remotecontrol.Message{}
	}
	if f.Checking {
		return "Checking Tailscale…", quiet, remotecontrol.Message{}
	}
	if f.URL != "" {
		switch {
		case f.lost():
			return rcStateDisconnected, bad, remotecontrol.Msg("Tailscale Funnel stopped: %s Choose Reconnect.", f.Detail)
		case !f.Verified.IsZero():
			return "Phone connected over the internet", good, remotecontrol.Msg("Last request from %s at %s, through Tailscale Funnel.", f.Device, f.Verified.Format("15:04:05"))
		}
		return "Funnel set up; internet access not verified", accent, remotecontrol.Msg("Open the QR on a phone with Wi-Fi turned off. This changes once a paired phone reaches this computer through the Funnel.")
	}
	var conflict *tsfunnel.PortConflictError
	var approval *tsfunnel.ApprovalRequiredError
	switch {
	case f.Err == nil && !f.Checked:
		return "Checking Tailscale…", quiet, remotecontrol.Message{}
	case errors.As(f.Err, &conflict):
		return "All Funnel ports are in use", bad, remotecontrol.Msg("Other apps use Tailscale Serve or Funnel on 443, 8443 and 10000. Atlas does not change their entries: %s", conflict.Error())
	case errors.As(f.Err, &approval):
		return "Waiting for your approval", accent, remotecontrol.Msg("Approve HTTPS and Funnel on Tailscale's page, then choose Reconnect.")
	case errors.Is(f.Err, tsfunnel.ErrResidual):
		return rcStateError, bad, remotecontrol.Msg("Tailscale still lists Atlas' Funnel entry. Restart the Tailscale app or remove it with the Tailscale CLI; Atlas does not remove entries by force.")
	}
	if !f.Ready.Ready() {
		return d.funnelProblem(f.Ready)
	}
	if f.Err != nil {
		return rcStateError, bad, remotecontrol.Msg("The Tailscale connection failed: %s", f.Err.Error())
	}
	return "Tailscale is ready", good, remotecontrol.Msg("Choose Reconnect with Tailscale to publish the phone panel.")
}

func (d *Remote) funnelProblem(r tsfunnel.Readiness) (string, lipgloss.Style, remotecontrol.Message) {
	bad, accent := d.com.Styles.Dialog.OAuth.ErrorText, d.com.Styles.Dialog.TitleAccent
	switch r.Problem {
	case tsfunnel.ProblemNotInstalled:
		return "Tailscale is not installed", accent, remotecontrol.Msg("Install Tailscale, then sign in to your own account. No VPS, domain or router setting is needed.")
	case tsfunnel.ProblemServiceStopped:
		return "The Tailscale service is not running", accent, remotecontrol.Msg("Open the Tailscale app; it starts the service.")
	case tsfunnel.ProblemOldVersion:
		return "Tailscale is too old", accent, remotecontrol.Msg("Atlas needs Tailscale %s or newer. Update it, then check again.", tsfunnel.MinVersion)
	case tsfunnel.ProblemNeedsLogin:
		return "Tailscale sign-in needed", accent, remotecontrol.Msg("Sign in to your own Tailscale account in the browser.")
	case tsfunnel.ProblemMachineAuth:
		return "Waiting for device approval", accent, remotecontrol.Msg("An admin of your tailnet must approve this computer in the Tailscale admin console.")
	case tsfunnel.ProblemDisconnected:
		return "Tailscale is disconnected", accent, remotecontrol.Msg("Sign in to Tailscale to connect it again.")
	case tsfunnel.ProblemStarting:
		return "Tailscale is starting", accent, remotecontrol.Msg("Wait a moment, then check again.")
	case tsfunnel.ProblemNeedsMagicDNS:
		return "MagicDNS and HTTPS are needed", accent, remotecontrol.Msg("Turn on MagicDNS and HTTPS certificates in the Tailscale admin console, then check again.")
	case tsfunnel.ProblemNoFunnelPort:
		return "Funnel is not allowed on any port", bad, remotecontrol.Msg("Your tailnet policy allows Funnel on none of 443, 8443 or 10000.")
	}
	return rcStateError, bad, remotecontrol.Msg("Tailscale did not answer as expected: %s", r.Detail)
}

func installStepText(step string) string {
	switch tsfunnel.InstallStep(step) {
	case tsfunnel.StepDownload:
		return "Downloading the official installer…"
	case tsfunnel.StepVerify:
		return "Checking the SHA-256 and the Tailscale signature…"
	case tsfunnel.StepInstall:
		return "Follow the Tailscale installer; Windows asks for approval."
	}
	return "Reading Tailscale's package list…"
}

// funnelLines are the status lines below the headline in Tailscale mode.
func (d *Remote) funnelLines(width int) []string {
	t := d.com.Styles
	quiet := t.Dialog.SecondaryText.Padding(0)
	f := d.funnel()
	var lines []string
	if r := f.Ready; r.CLIPath != "" {
		parts := []string{orDash(r.Version), orDash(r.Backend)}
		if r.Host != "" {
			parts = append(parts, r.Host)
		}
		lines = append(lines, quiet.Render(fmt.Sprintf(d.com.Text("Tailscale: %s"), strings.Join(parts, " · "))))
	}
	if f.URL != "" {
		lines = append(lines, quiet.Render(fmt.Sprintf(d.com.Text("Phone address: %s"), f.URL)))
	}
	if f.LocalPort > 0 {
		lines = append(lines, quiet.Render(fmt.Sprintf(d.com.Text("Phone panel: only on this computer (127.0.0.1:%d)"), f.LocalPort)))
	}
	if gw := d.gateway(); gw != nil && gw.UsesFunnel() {
		lines = append(lines, d.phoneCountLine())
	}
	lines = append(lines, wrapLines(quiet, d.com.Text("Keep this computer on and awake. Tailscale Funnel has bandwidth limits and serves only ts.net addresses on ports 443, 8443 and 10000."), width)...)
	return lines
}

// funnelDiagnostics adds the checked Tailscale facts to Diagnostics.
func (d *Remote) funnelDiagnostics() []string {
	f := d.funnel()
	if !f.On && !f.Checked {
		return nil
	}
	t := d.com.Styles
	kv := func(k, v string) string { return t.Dialog.SecondaryText.Padding(0).Render(d.com.Text(k) + ": " + v) }
	r := f.Ready
	var ports []string
	for _, p := range r.FunnelPorts {
		ports = append(ports, fmt.Sprint(p))
	}
	lines := []string{
		"", t.Dialog.PrimaryText.Padding(0).Render(d.com.Text("Tailscale")),
		kv("CLI", orDash(r.CLIPath)),
		kv("Version", orDash(r.Version)),
		kv("State", orDash(r.Backend)),
		kv("Device name", orDash(r.Host)),
		kv("MagicDNS", fmt.Sprint(r.MagicDNS)),
		kv("HTTPS certificates", fmt.Sprint(r.HTTPS)),
		kv("Funnel allowed", fmt.Sprint(r.Funnel)),
		kv("Funnel ports", orDash(strings.Join(ports, ", "))),
		kv("Phase", orDash(string(f.Phase))),
	}
	if r.Detail != "" {
		lines = append(lines, kv("Detail", r.Detail))
	}
	if f.Detail != "" {
		lines = append(lines, kv("Funnel", f.Detail))
	}
	if !f.Seen.IsZero() {
		lines = append(lines, kv("Last internet request", f.Seen.Format("15:04:05")))
	}
	return lines
}
