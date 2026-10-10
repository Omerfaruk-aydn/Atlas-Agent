package dialog

import (
	"fmt"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ansi"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-style/v2"
	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	uv "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ultraviolet"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/help"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/key"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/mobile"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/common"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/util"
)

// MobileID identifies the phone control dialog.
const MobileID = "mobile"

// ActionStopMobile asks the UI to stop the phone gateway.
type ActionStopMobile struct{}

type mobileTick struct{}

type mobileRow struct {
	pending *mobile.Pending
	device  *mobile.Device
}

// Mobile shows the pairing QR code, pending pairings with their
// verification code, and paired phones. Every pairing must be accepted
// here; the QR only starts a request.
type Mobile struct {
	com         *common.Common
	help        help.Model
	gw          *mobile.Gateway
	qr          []string
	url         string
	expires     time.Time
	rows        []mobileRow
	selected    int
	homeNetwork bool
	// confirmRevokeAll is set after X, until y confirms or any key cancels.
	confirmRevokeAll bool
	keyMap           struct {
		Up, Down, Accept, Reject, Revoke, RevokeAll, Previews, NewQR, Stop, Close key.Binding
	}
}

var _ Dialog = (*Mobile)(nil)

// NewMobile creates the dialog for a running gateway.
func NewMobile(com *common.Common, gw *mobile.Gateway) *Mobile {
	d := &Mobile{com: com, gw: gw}
	h := help.New()
	h.Styles = com.Styles.DialogHelpStyles()
	d.help = h
	d.keyMap.Up = key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑", com.Text("previous")))
	d.keyMap.Down = key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓", com.Text("next")))
	d.keyMap.Accept = key.NewBinding(key.WithKeys("a"), key.WithHelp("a", com.Text("accept")))
	d.keyMap.Reject = key.NewBinding(key.WithKeys("r"), key.WithHelp("r", com.Text("reject")))
	d.keyMap.Revoke = key.NewBinding(key.WithKeys("d", "x"), key.WithHelp("d", com.Text("unpair")))
	d.keyMap.RevokeAll = key.NewBinding(key.WithKeys("X"), key.WithHelp("shift+x", com.Text("unpair all")))
	d.keyMap.Previews = key.NewBinding(key.WithKeys("p"), key.WithHelp("p", com.Text("previews")))
	d.keyMap.NewQR = key.NewBinding(key.WithKeys("n"), key.WithHelp("n", com.Text("new QR")))
	d.keyMap.Stop = key.NewBinding(key.WithKeys("s"), key.WithHelp("s", com.Text("stop server")))
	d.keyMap.Close = CloseKey
	d.newQR()
	d.refresh()
	return d
}

// Init starts the refresh tick that surfaces new pairing requests.
func (d *Mobile) Init() tea.Cmd { return d.tick() }

func (d *Mobile) tick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return mobileTick{} })
}

func (d *Mobile) newQR() {
	d.url, d.expires = d.gw.PairingURL()
	lines, err := mobile.QRLines(d.url)
	if err != nil {
		d.qr = []string{err.Error()}
		return
	}
	d.qr = lines
}

func (d *Mobile) refresh() {
	d.rows = d.rows[:0]
	for _, p := range d.gw.Auth.PendingList() {
		d.rows = append(d.rows, mobileRow{pending: &p})
	}
	for _, dev := range d.gw.Auth.Devices() {
		d.rows = append(d.rows, mobileRow{device: &dev})
	}
	d.selected = min(max(0, d.selected), max(0, len(d.rows)-1))
}

// ID implements Dialog.
func (d *Mobile) ID() string { return MobileID }

// SetHomeNetwork labels a temporary LAN connection in the pairing view.
func (d *Mobile) SetHomeNetwork(enabled bool) { d.homeNetwork = enabled }

// HandleMsg implements Dialog.
func (d *Mobile) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case mobileTick:
		if time.Now().After(d.expires) {
			d.newQR()
		}
		d.refresh()
		return ActionCmd{d.tick()}
	case tea.KeyPressMsg:
		if d.confirmRevokeAll {
			d.confirmRevokeAll = false
			if msg.String() != "y" && msg.String() != "Y" {
				return nil
			}
			n, err := d.gw.Auth.RevokeAll()
			if err != nil {
				return ActionCmd{util.ReportError(err)}
			}
			d.refresh()
			return ActionCmd{util.ReportInfo(fmt.Sprintf(d.com.Text("Unpaired %d phones; their sessions ended."), n))}
		}
		switch {
		case key.Matches(msg, d.keyMap.Close):
			return ActionClose{}
		case key.Matches(msg, d.keyMap.RevokeAll):
			if len(d.gw.Auth.Devices()) > 0 || len(d.gw.Auth.PendingList()) > 0 {
				d.confirmRevokeAll = true
			}
		case key.Matches(msg, d.keyMap.Up):
			d.selected = max(0, d.selected-1)
		case key.Matches(msg, d.keyMap.Down):
			d.selected = min(len(d.rows)-1, d.selected+1)
		case key.Matches(msg, d.keyMap.NewQR):
			d.newQR()
		case key.Matches(msg, d.keyMap.Stop):
			return ActionStopMobile{}
		case key.Matches(msg, d.keyMap.Accept, d.keyMap.Reject):
			if row := d.row(); row != nil && row.pending != nil {
				// refresh reuses the rows slice, so copy what is needed first.
				p := *row.pending
				accept := key.Matches(msg, d.keyMap.Accept)
				if _, err := d.gw.Auth.Decide(p.ID, accept); err != nil {
					return ActionCmd{util.ReportError(err)}
				}
				d.refresh()
				if accept {
					return ActionCmd{util.ReportInfo(fmt.Sprintf(d.com.Text("Phone %q paired"), p.Name))}
				}
			}
		case key.Matches(msg, d.keyMap.Revoke):
			if row := d.row(); row != nil && row.device != nil {
				dev := *row.device
				if err := d.gw.Auth.Revoke(dev.ID); err != nil {
					return ActionCmd{util.ReportError(err)}
				}
				d.refresh()
				return ActionCmd{util.ReportInfo(fmt.Sprintf(d.com.Text("Phone %q unpaired"), dev.Name))}
			}
		case key.Matches(msg, d.keyMap.Previews):
			if row := d.row(); row != nil && row.device != nil {
				if err := d.gw.Auth.SetPreviews(row.device.ID, !row.device.Previews); err != nil {
					return ActionCmd{util.ReportError(err)}
				}
				d.refresh()
			}
		}
	}
	return nil
}

func (d *Mobile) row() *mobileRow {
	if d.selected < 0 || d.selected >= len(d.rows) {
		return nil
	}
	return &d.rows[d.selected]
}

// Draw implements Dialog.
func (d *Mobile) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := d.com.Styles
	width := max(0, min(78, area.Dx()-t.Dialog.View.GetHorizontalBorderSize()))
	innerWidth := width - t.Dialog.View.GetHorizontalFrameSize()
	rc := NewRenderContext(t, width)
	rc.Title = d.com.Text("Phone control")

	// A QR code needs fixed dark-on-light contrast to scan, whatever the
	// terminal theme, so it does not use theme colors.
	qrStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#000000")).Background(lipgloss.Color("#FFFFFF"))
	renewed := d.gw.RenewedFrom()
	reserve := 14
	if d.homeNetwork {
		reserve += 2
	}
	if renewed != "" {
		reserve += 2
	}
	if d.gw.UsesRelay() {
		reserve++
	}
	qrFits := len(d.qr) > 0 && ansi.StringWidth(d.qr[0]) <= innerWidth && len(d.qr)+reserve <= area.Dy()
	if qrFits {
		var b strings.Builder
		for i, line := range d.qr {
			if i > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(qrStyle.Render(line))
		}
		rc.AddPart(lipgloss.PlaceHorizontal(innerWidth, lipgloss.Center, b.String()))
	} else {
		rc.AddPart(t.Dialog.Sessions.InfoBlurred.Render(d.com.Text("Enlarge the terminal to show the QR code, or open this address on the phone:")))
	}
	info := []string{
		ansi.Truncate(d.url, innerWidth, "…"),
		fmt.Sprintf(d.com.Text("Code valid until %s, single use. Certificate %s"), d.expires.Format("15:04:05"), shortFingerprint(d.gw.Fingerprint())),
		d.com.Text("The QR holds no password: you will compare a 6-digit code and accept here."),
	}
	if d.homeNetwork {
		info = append(info,
			d.com.Text("Home network test: same Wi-Fi or Ethernet; s stops the connection."),
			d.com.Text("On the phone, accept the local HTTPS certificate only after checking its fingerprint."))
	}
	if d.gw.UsesRelay() {
		status := "Remote connection: reconnecting"
		if d.gw.RelayConnected() {
			status = "Remote connection: connected"
		}
		info = append(info, d.com.Text(status))
	}
	if renewed != "" {
		// Paired phones keep their pairing but see a certificate warning
		// again; they must accept only the new fingerprint.
		info = append(info,
			fmt.Sprintf(d.com.Text("Certificate renewed; previous one was %s."), shortFingerprint(renewed)),
			d.com.Text("Phones stay paired; on the phone accept only the certificate above."))
	}
	for i, line := range info {
		info[i] = ansi.Truncate(line, innerWidth, "…")
	}
	rc.AddPart(t.Dialog.Sessions.InfoBlurred.Render(strings.Join(info, "\n")))

	var rows []string
	if len(d.rows) == 0 {
		rows = append(rows, t.Dialog.Sessions.InfoBlurred.Render(d.com.Text("No paired phones yet.")))
	}
	for i, r := range d.rows {
		var line string
		if r.pending != nil {
			// The name is whatever the phone sent; only the code proves which
			// phone is asking.
			line = fmt.Sprintf("● %s %s — %s %s %s", r.pending.Name, d.com.Text("(name sent by the phone, unverified)"), d.com.Text("code"), r.pending.Code[:3]+" "+r.pending.Code[3:], d.com.Text("(a accept / r reject)"))
		} else {
			prev := d.com.Text("previews off")
			if r.device.Previews {
				prev = d.com.Text("previews on")
			}
			line = fmt.Sprintf("  %s · %s · %s %s", r.device.Name, prev, d.com.Text("seen"), t.RelativeTime(time.UnixMilli(r.device.LastSeen)))
		}
		line = ansi.Truncate(line, innerWidth, "…")
		if i == d.selected {
			line = t.Dialog.SelectedItem.Width(innerWidth).Render(line)
		} else {
			line = t.Dialog.NormalItem.Width(innerWidth).Render(line)
		}
		rows = append(rows, line)
	}
	if d.confirmRevokeAll {
		rows = append(rows, t.Dialog.OAuth.ErrorText.Render(ansi.Truncate(fmt.Sprintf(d.com.Text("Press y to unpair all %d phones and end their sessions; any other key cancels."), len(d.gw.Auth.Devices())), innerWidth, "…")))
	}
	rc.AddPart(strings.Join(rows, "\n"))
	rc.Help = renderDialogHelp(t, &d.help, d, innerWidth)
	DrawCenterCursor(scr, area, rc.Render(), nil)
	return nil
}

func shortFingerprint(fp string) string {
	fp = strings.TrimPrefix(fp, "sha256:")
	if len(fp) > 16 {
		return fp[:8] + "…" + fp[len(fp)-8:]
	}
	return fp
}

// ShortHelp implements help.KeyMap.
func (d *Mobile) ShortHelp() []key.Binding {
	return []key.Binding{d.keyMap.Accept, d.keyMap.Reject, d.keyMap.Revoke, d.keyMap.RevokeAll, d.keyMap.Previews, d.keyMap.NewQR, d.keyMap.Stop, d.keyMap.Close}
}

// FullHelp implements help.KeyMap.
func (d *Mobile) FullHelp() [][]key.Binding { return [][]key.Binding{d.ShortHelp()} }
