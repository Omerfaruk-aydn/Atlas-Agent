package dialog

import (
	"context"
	"image"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	uv "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ultraviolet"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/i18n"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/mobile"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/remotecontrol"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/common"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/styles"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workspace"
	"github.com/stretchr/testify/require"
)

type remoteWorkspace struct {
	workspace.Workspace
	cfg *config.Config
}

func (w *remoteWorkspace) Config() *config.Config { return w.cfg }

type hostKeyDialer struct{}

func (hostKeyDialer) Dial(context.Context, config.RemoteProfile, []byte) (remotecontrol.Runner, error) {
	return nil, &remotecontrol.HostKeyUnknownError{Address: "203.0.113.10", Algorithm: "ssh-ed25519", Fingerprint: "SHA256:test-fingerprint"}
}

func newRemoteDialog(t *testing.T, lang string, profiles ...config.RemoteProfile) (*Remote, *remotecontrol.Manager) {
	t.Helper()
	sty := styles.AtlasPantera()
	sty.Locale = i18n.New(lang)
	cfg := &config.Config{}
	cfg.Tools.Devices.RemoteProfiles = profiles
	if len(profiles) > 0 {
		cfg.Tools.Devices.RemoteActive = profiles[0].ID
	}
	mgr := remotecontrol.NewManager(remotecontrol.Options{Dir: t.TempDir(), Dialer: hostKeyDialer{}, Logger: slog.New(slog.DiscardHandler)})
	com := &common.Common{Workspace: &remoteWorkspace{cfg: cfg}, Styles: &sty}
	d := NewRemote(com, RemoteEnv{Manager: mgr, Gateway: func() *mobile.Gateway { return nil }, Starting: func() bool { return false }, StartErr: func() error { return nil }})
	return d, mgr
}

func render(d *Remote, w, h int) string {
	scr := uv.NewScreenBuffer(w, h)
	d.Draw(scr, image.Rectangle{Max: image.Point{X: w, Y: h}})
	return scr.String()
}

func press(d *Remote, keys ...string) Action {
	var last Action
	for _, k := range keys {
		var msg tea.KeyPressMsg
		switch k {
		case "enter":
			msg = tea.KeyPressMsg{Code: tea.KeyEnter}
		case "tab":
			msg = tea.KeyPressMsg{Code: tea.KeyTab}
		case "esc":
			msg = tea.KeyPressMsg{Code: tea.KeyEscape}
		case "down":
			msg = tea.KeyPressMsg{Code: tea.KeyDown}
		case "right":
			msg = tea.KeyPressMsg{Code: tea.KeyRight}
		case "ctrl+s":
			msg = tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}
		default:
			for _, r := range k {
				last = d.HandleMsg(tea.KeyPressMsg{Code: r, Text: string(r)})
			}
			continue
		}
		last = d.HandleMsg(msg)
	}
	return last
}

func TestRemoteDialogFirstRunAddsAProfileWithInlineValidation(t *testing.T) {
	t.Parallel()
	d, _ := newRemoteDialog(t, "en")
	out := render(d, 100, 40)
	require.Contains(t, out, "Remote access: Not set up")
	require.Contains(t, out, "Connect my existing server")
	require.Contains(t, out, "Help me set up a server")

	press(d, "enter")
	require.Equal(t, remoteEditing, d.view)
	require.Nil(t, press(d, "ctrl+s"), "an incomplete form is not submitted")
	out = render(d, 100, 60)
	require.Contains(t, out, "Enter the server IP address or hostname.")

	// Name, host with a port (rejected in place), then fix it.
	press(d, "tab", "203.0.113.10:22")
	press(d, "tab")
	out = render(d, 100, 60)
	require.Contains(t, out, "Put the port in the port field.")
	d.form.fields[1].input.SetValue("203.0.113.10")
	d.form.fields[3].input.SetValue("ubuntu")
	d.form.fields[5].input.SetValue(filepath.Join(t.TempDir(), "id_ed25519"))
	d.form.fields[6].input.SetValue("key-passphrase-value")
	d.form.fields[7].input.SetValue("atlas.example.com")
	out = render(d, 100, 60)
	require.NotContains(t, out, "key-passphrase-value", "secrets are masked")
	action := press(d, "ctrl+s")
	save, ok := action.(ActionRemoteSaveProfile)
	require.True(t, ok, "%T", action)
	require.Equal(t, "203.0.113.10", save.Draft.SSHHost)
	require.Equal(t, remotecontrol.AuthKey, save.Draft.SSHAuth)
	require.Equal(t, "key-passphrase-value", save.Draft.Secret)

	d.ProfileSaved(nil, nil, true)
	require.Equal(t, remoteMain, d.view)
	require.Nil(t, d.form, "the form and its secret are dropped after saving")
}

func TestRemoteFormAuthenticationChoiceChangesFields(t *testing.T) {
	t.Parallel()
	d, _ := newRemoteDialog(t, "en")
	press(d, "enter")
	f := d.form
	f.focusField(4)
	press(d, "right")
	require.Equal(t, remotecontrol.AuthAgent, f.auth)
	for _, fld := range f.visible() {
		require.NotEqual(t, remotecontrol.FieldSecret, fld.key, "an agent needs no secret field")
		require.NotEqual(t, remotecontrol.FieldSSHKeyFile, fld.key)
	}
	press(d, "right")
	require.Equal(t, remotecontrol.AuthPassword, f.auth)
	require.Equal(t, remotecontrol.Draft{}.SSHKeyFile, f.Draft().SSHKeyFile)
	press(d, "esc")
	require.Equal(t, remoteMain, d.view)
	require.Nil(t, d.form)
}

func TestRemoteDialogWithProfileShowsRealStatusAndHostKeyDecision(t *testing.T) {
	t.Parallel()
	p := config.RemoteProfile{ID: remotecontrol.NewProfileID(), Name: "hetzner", SSHHost: "203.0.113.10", SSHPort: 22, SSHUser: "root", SSHAuth: remotecontrol.AuthAgent, PublicHost: "atlas.example.com"}
	d, mgr := newRemoteDialog(t, "en", p)
	out := render(d, 110, 50)
	for _, want := range []string{"Remote access: Off", "Server: hetzner · SSH root@203.0.113.10", "Phone address: https://atlas.example.com", "SSH: not checked yet", "Last server check: never", "Test connection", "Turn remote access on"} {
		require.Contains(t, out, want)
	}
	require.NotContains(t, out, "Connected", "nothing claims a connection that was not checked")

	action := press(d, "enter")
	cmd, ok := action.(ActionCmd)
	require.True(t, ok)
	cmd.Cmd()
	mgr.Wait()
	d.HandleMsg(RemoteTick{})
	require.Equal(t, remoteHostKey, d.view)
	out = render(d, 110, 50)
	require.Contains(t, out, "SHA256:test-fingerprint")
	require.Contains(t, out, "~/.ssh/known_hosts is not changed")
	press(d, "esc")
	require.Equal(t, remoteMain, d.view)
	require.Contains(t, render(d, 110, 50), "nothing was sent to the server")
	require.Nil(t, mgr.Snapshot(p.ID).HostKey)

	// Pairing needs remote access on; the dialog says so instead of failing.
	d.selected = 2
	require.Nil(t, press(d, "enter"))
	require.Contains(t, render(d, 110, 50), "Turn remote access on first")
}

func TestRemoteDialogAsksBeforeDestructiveActions(t *testing.T) {
	t.Parallel()
	p := config.RemoteProfile{ID: remotecontrol.NewProfileID(), Name: "vps", SSHHost: "vps.example.com", SSHUser: "root", SSHAuth: remotecontrol.AuthAgent, PublicHost: "atlas.example.com"}
	d, _ := newRemoteDialog(t, "en", p)
	for i, a := range d.actions() {
		if a.id == rcDelete {
			d.selected = i
		}
	}
	require.Nil(t, press(d, "enter"))
	require.Equal(t, remoteConfirm, d.view)
	require.Contains(t, render(d, 110, 40), "The server is not contacted")
	require.Nil(t, press(d, "n"))
	require.Equal(t, remoteMain, d.view)
	press(d, "enter")
	require.Equal(t, ActionRemoteDeleteProfile{ID: p.ID}, press(d, "y"))
}

func TestRemoteDialogFitsSmallTerminalsAndIsLocalized(t *testing.T) {
	t.Parallel()
	p := config.RemoteProfile{ID: remotecontrol.NewProfileID(), Name: "vps", SSHHost: "2001:db8::1", SSHPort: 2222, SSHUser: "admin", SSHAuth: remotecontrol.AuthPassword, PublicHost: "atlas.example.com"}
	d, _ := newRemoteDialog(t, "tr", p)
	for _, size := range []image.Point{{X: 160, Y: 60}, {X: 80, Y: 24}, {X: 40, Y: 12}, {X: 20, Y: 6}, {X: 0, Y: 0}} {
		require.NotPanics(t, func() { render(d, size.X, size.Y) })
	}
	out := render(d, 120, 50)
	require.Contains(t, out, "Uzaktan erişim: Kapalı")
	require.Contains(t, out, "Bağlantıyı test et")
	require.Contains(t, out, "[2001:db8::1]:2222")
	for _, line := range strings.Split(render(d, 60, 30), "\n") {
		require.LessOrEqual(t, len([]rune(strings.TrimRight(line, " "))), 60)
	}
}

// Labels reach com.Text through variables, which the catalog's call-site
// test cannot see, so they are checked here.
func TestRemoteDialogLabelsAreTranslated(t *testing.T) {
	t.Parallel()
	p := config.RemoteProfile{ID: remotecontrol.NewProfileID(), Name: "vps", SSHHost: "vps.example.com", SSHUser: "root", SSHAuth: remotecontrol.AuthKey, PublicHost: "atlas.example.com"}
	without, _ := newRemoteDialog(t, "en")
	with, _ := newRemoteDialog(t, "en", p, p)
	var labels []string
	for _, d := range []*Remote{without, with} {
		for _, a := range d.actions() {
			labels = append(labels, a.label)
			if a.hint != "" {
				labels = append(labels, a.hint)
			}
		}
	}
	form := newRemoteForm(with.com, remotecontrol.DraftFromProfile(p))
	for _, f := range form.fields {
		labels = append(labels, f.label)
	}
	labels = append(labels, rcStateNotSetUp, rcStateOff, rcStateConnecting, rcStateConnected, rcStateDisconnected, rcStateError,
		authLabel(remotecontrol.AuthKey), authLabel(remotecontrol.AuthAgent), authLabel(remotecontrol.AuthPassword))
	for _, l := range labels {
		for _, lang := range i18n.Languages() {
			require.True(t, i18n.Has(lang.Code, l), "missing %s translation for %q", lang.Code, l)
		}
	}
}
