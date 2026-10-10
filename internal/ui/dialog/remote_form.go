package dialog

import (
	"slices"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ansi"
	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/textinput"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/remotecontrol"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/common"
)

type remoteFieldKind int

const (
	fieldText remoteFieldKind = iota
	fieldSecret
	fieldChoice
	fieldToggle
)

type remoteField struct {
	key   string
	label string
	kind  remoteFieldKind
	input textinput.Model
}

// remoteForm edits one server profile. Secrets typed here leave the form
// only inside ActionRemoteSaveProfile and are cleared after saving.
type remoteForm struct {
	com         *common.Common
	base        remotecontrol.Draft
	fields      []*remoteField
	focus       int
	auth        string
	ignoreKnown bool
	errs        remotecontrol.FieldErrors
	touched     map[string]bool
	showAll     bool
	saving      bool
	banner      remotecontrol.Message
	saveErr     remotecontrol.Message
}

var authChoices = []string{remotecontrol.AuthKey, remotecontrol.AuthAgent, remotecontrol.AuthPassword}

func newRemoteForm(com *common.Common, d remotecontrol.Draft) *remoteForm {
	f := &remoteForm{com: com, base: d, auth: d.SSHAuth, ignoreKnown: d.IgnoreUserKnownHosts, touched: map[string]bool{}}
	if !slices.Contains(authChoices, f.auth) {
		f.auth = remotecontrol.AuthKey
	}
	add := func(key, label string, kind remoteFieldKind, value, placeholder string) {
		in := textinput.New()
		in.SetVirtualCursor(true)
		in.SetStyles(com.Styles.TextInput)
		in.Prompt = "> "
		in.Placeholder = placeholder
		in.CharLimit = 512
		if kind == fieldSecret {
			in.EchoMode = textinput.EchoPassword
			in.CharLimit = 4096
		}
		in.SetValue(value)
		in.Blur()
		f.fields = append(f.fields, &remoteField{key: key, label: label, kind: kind, input: in})
	}
	add(remotecontrol.FieldName, "Profile name", fieldText, d.Name, "my-vps")
	add(remotecontrol.FieldSSHHost, "Server IP address or SSH hostname", fieldText, d.SSHHost, "203.0.113.10")
	add(remotecontrol.FieldSSHPort, "SSH port", fieldText, d.SSHPort, "22")
	add(remotecontrol.FieldSSHUser, "SSH user", fieldText, d.SSHUser, "ubuntu")
	add(remotecontrol.FieldSSHAuth, "Authentication", fieldChoice, "", "")
	add(remotecontrol.FieldSSHKeyFile, "Private key file", fieldText, d.SSHKeyFile, "C:/Users/me/.ssh/id_ed25519")
	add(remotecontrol.FieldSecret, "Password or key passphrase", fieldSecret, "", "")
	add(remotecontrol.FieldPublicHost, "Hostname phones open (HTTPS)", fieldText, d.PublicHost, "atlas.example.com")
	add(remotecontrol.FieldACMEEmail, "Certificate e-mail (optional)", fieldText, d.ACMEEmail, "you@example.com")
	add(remotecontrol.FieldTimeout, "SSH timeout in seconds (advanced)", fieldText, d.Timeout, "15")
	add(fieldIgnoreKnown, "Ignore ~/.ssh/known_hosts (advanced)", fieldToggle, "", "")
	f.updatePlaceholders()
	f.focusField(0)
	f.validate()
	return f
}

const fieldIgnoreKnown = "ignore_known_hosts"

func (f *remoteForm) updatePlaceholders() {
	for _, fld := range f.fields {
		if fld.kind != fieldSecret {
			continue
		}
		switch {
		case f.base.HasSecret:
			fld.input.Placeholder = f.com.Text("saved; leave empty to keep it")
		case f.auth == remotecontrol.AuthKey:
			fld.input.Placeholder = f.com.Text("only if the key has a passphrase")
		default:
			fld.input.Placeholder = ""
		}
	}
}

// visible lists the fields that apply to the chosen authentication.
func (f *remoteForm) visible() []*remoteField {
	var out []*remoteField
	for _, fld := range f.fields {
		switch fld.key {
		case remotecontrol.FieldSSHKeyFile:
			if f.auth != remotecontrol.AuthKey {
				continue
			}
		case remotecontrol.FieldSecret:
			if f.auth == remotecontrol.AuthAgent {
				continue
			}
		}
		out = append(out, fld)
	}
	return out
}

func (f *remoteForm) focused() *remoteField {
	v := f.visible()
	f.focus = min(max(0, f.focus), len(v)-1)
	return v[f.focus]
}

func (f *remoteForm) focusField(i int) {
	v := f.visible()
	if cur := f.focus; cur >= 0 && cur < len(v) {
		v[cur].input.Blur()
		f.touched[v[cur].key] = true
	}
	n := len(v)
	f.focus = ((i % n) + n) % n
	v[f.focus].input.Focus()
	f.validate()
}

// Draft returns the form content as typed.
func (f *remoteForm) Draft() remotecontrol.Draft {
	d := f.base
	get := func(key string) string {
		for _, fld := range f.fields {
			if fld.key == key {
				return fld.input.Value()
			}
		}
		return ""
	}
	d.Name, d.SSHHost, d.SSHPort, d.SSHUser = get(remotecontrol.FieldName), get(remotecontrol.FieldSSHHost), get(remotecontrol.FieldSSHPort), get(remotecontrol.FieldSSHUser)
	d.SSHAuth, d.SSHKeyFile, d.Secret = f.auth, get(remotecontrol.FieldSSHKeyFile), get(remotecontrol.FieldSecret)
	d.PublicHost, d.ACMEEmail, d.Timeout = get(remotecontrol.FieldPublicHost), get(remotecontrol.FieldACMEEmail), get(remotecontrol.FieldTimeout)
	d.IgnoreUserKnownHosts = f.ignoreKnown
	if d.SSHAuth != remotecontrol.AuthKey {
		d.SSHKeyFile = ""
	}
	if d.SSHAuth == remotecontrol.AuthAgent {
		d.Secret = ""
	}
	return d
}

func (f *remoteForm) validate() {
	_, f.errs = f.Draft().Validate()
}

// SetErrors shows errors found while saving, such as a missing key file.
func (f *remoteForm) SetErrors(errs remotecontrol.FieldErrors, msg remotecontrol.Message) {
	f.saving, f.showAll, f.saveErr = false, true, msg
	for k, v := range errs {
		if f.errs == nil {
			f.errs = remotecontrol.FieldErrors{}
		}
		f.errs[k] = v
	}
}

func (f *remoteForm) cycleAuth(delta int) {
	i := slices.Index(authChoices, f.auth)
	f.auth = authChoices[(i+delta+len(authChoices))%len(authChoices)]
	f.updatePlaceholders()
	f.validate()
}

// handleKey returns an action when the form is submitted or canceled.
func (f *remoteForm) handleKey(msg tea.KeyPressMsg) (Action, bool) {
	fld := f.focused()
	switch msg.String() {
	case "esc":
		return nil, true
	case "ctrl+s":
		return f.submit(), false
	case "tab", "down":
		f.focusField(f.focus + 1)
		return nil, false
	case "shift+tab", "up":
		f.focusField(f.focus - 1)
		return nil, false
	case "enter":
		switch {
		case fld.kind == fieldToggle:
			f.ignoreKnown = !f.ignoreKnown
		case f.focus == len(f.visible())-1:
			return f.submit(), false
		default:
			f.focusField(f.focus + 1)
		}
		return nil, false
	}
	switch fld.kind {
	case fieldChoice:
		switch msg.String() {
		case "left", "h":
			f.cycleAuth(-1)
		case "right", "l", "space", " ":
			f.cycleAuth(1)
		}
		return nil, false
	case fieldToggle:
		if msg.String() == "space" || msg.String() == " " {
			f.ignoreKnown = !f.ignoreKnown
		}
		return nil, false
	}
	var cmd tea.Cmd
	fld.input, cmd = fld.input.Update(msg)
	f.validate()
	if cmd != nil {
		return ActionCmd{Cmd: cmd}, false
	}
	return nil, false
}

func (f *remoteForm) paste(msg tea.PasteMsg) Action {
	fld := f.focused()
	if fld.kind == fieldChoice || fld.kind == fieldToggle {
		return nil
	}
	var cmd tea.Cmd
	fld.input, cmd = fld.input.Update(msg)
	f.validate()
	if cmd != nil {
		return ActionCmd{Cmd: cmd}
	}
	return nil
}

func (f *remoteForm) submit() Action {
	f.showAll = true
	f.saveErr = remotecontrol.Message{}
	f.validate()
	if len(f.errs) > 0 || f.saving {
		return nil
	}
	f.saving = true
	return ActionRemoteSaveProfile{Draft: f.Draft()}
}

// clearSecret wipes the typed secret after it was handed over.
func (f *remoteForm) clearSecret() {
	for _, fld := range f.fields {
		if fld.kind == fieldSecret {
			fld.input.SetValue("")
		}
	}
}

// lines renders the form; focusLine is the first line of the focused field.
func (f *remoteForm) lines(width int) (lines []string, focusLine int) {
	t := f.com.Styles
	if !f.banner.IsZero() {
		lines = append(lines, wrapLines(t.Dialog.SecondaryText.Padding(0), f.banner.Localize(f.com.Text), width)...)
		lines = append(lines, "")
	}
	for i, fld := range f.visible() {
		focused := i == f.focus
		label := t.Dialog.Arguments.InputLabelBlurred
		if focused {
			label = t.Dialog.Arguments.InputLabelFocused
			focusLine = len(lines)
		}
		lines = append(lines, label.Render(ansi.Truncate(f.com.Text(fld.label), width, "…")))
		switch fld.kind {
		case fieldChoice:
			var parts []string
			for _, c := range authChoices {
				// The marker shows the choice without relying on color.
				name := f.com.Text(authLabel(c))
				if c == f.auth {
					parts = append(parts, t.Dialog.SelectedItem.Render("◉ "+name))
				} else {
					parts = append(parts, t.Dialog.NormalItem.Render("○ "+name))
				}
			}
			lines = append(lines, "  "+strings.Join(parts, " "))
		case fieldToggle:
			box := "[ ]"
			if f.ignoreKnown {
				box = "[x]"
			}
			style := t.Dialog.NormalItem
			if focused {
				style = t.Dialog.SelectedItem
			}
			lines = append(lines, "  "+style.Render(box+" "+f.com.Text("Only trust host keys accepted in /rc")))
		default:
			// The prompt and the virtual cursor take three columns.
			fld.input.SetWidth(max(10, width-4))
			lines = append(lines, fld.input.View())
		}
		if msg, ok := f.errs[fld.key]; ok && (f.showAll || f.touched[fld.key]) {
			lines = append(lines, wrapLines(t.Dialog.OAuth.ErrorText, "  "+msg.Localize(f.com.Text), width)...)
		}
	}
	if !f.saveErr.IsZero() {
		lines = append(lines, "")
		lines = append(lines, wrapLines(t.Dialog.OAuth.ErrorText, f.saveErr.Localize(f.com.Text), width)...)
	}
	if f.saving {
		lines = append(lines, "", t.Dialog.SecondaryText.Padding(0).Render(f.com.Text("Saving…")))
	}
	return lines, focusLine
}

func authLabel(method string) string {
	switch method {
	case remotecontrol.AuthAgent:
		return "SSH agent"
	case remotecontrol.AuthPassword:
		return "Password"
	}
	return "Key file"
}
