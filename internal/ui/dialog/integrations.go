package dialog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ansi"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-style/v2"
	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	uv "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ultraviolet"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/help"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/key"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/textinput"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/i18n"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/integrations"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/common"
)

// IntegrationsID identifies the /integrations dialog.
const IntegrationsID = "integrations"

// ActionOpenIntegrations opens the integrations hub.
type ActionOpenIntegrations struct{}

// ActionIntegrationsMCPLogin starts the existing MCP sign-in flow for one
// server.
type ActionIntegrationsMCPLogin struct{ Name, URL string }

// IntegrationsTick refreshes /integrations. The UI routes it to the dialog
// even when another dialog is in front.
type IntegrationsTick struct{}

// IntegrationsLoaded carries a snapshot read off the UI loop.
type IntegrationsLoaded struct {
	Items   []integrations.Item
	Running int
}

// IntegrationsDone reports the outcome of a check or an action.
type IntegrationsDone struct {
	ID     string
	Action integrations.Action
	Msg    i18n.Message
	Err    error
}

type integrationsView int

const (
	integrationsList integrationsView = iota
	integrationsDetail
	integrationsConfirm
)

// Status filters, in cycle order.
var integrationStatusFilters = []string{"", "attention", string(integrations.StatusReady), string(integrations.StatusUnchecked), string(integrations.StatusDisabled)}

// Integrations is the /integrations dialog. It only renders snapshots; every
// read and check runs in a command.
type Integrations struct {
	com     *common.Common
	svc     *integrations.Service
	help    help.Model
	search  textinput.Model
	view    integrationsView
	items   []integrations.Item
	loaded  bool
	running int

	selected   int
	actionSel  int
	scroll     int
	kindFilter int
	statusIdx  int
	catalog    bool
	searching  bool
	detailID   string
	confirm    *integrationConfirm
	notice     string
	noticeOK   bool
	keyMap     struct {
		Up, Down, Enter, Back, Search, Test, TestAll, Cancel, Kind, Status, Catalog key.Binding
	}
}

type integrationConfirm struct {
	text string
	run  func() Action
}

var _ Dialog = (*Integrations)(nil)

// NewIntegrations creates the /integrations dialog. svc may be nil in
// client mode; the dialog then explains that the hub is unavailable.
func NewIntegrations(com *common.Common, svc *integrations.Service) *Integrations {
	d := &Integrations{com: com, svc: svc}
	d.help = help.New()
	d.help.Styles = com.Styles.DialogHelpStyles()
	d.search = textinput.New()
	d.search.SetVirtualCursor(false)
	d.search.SetStyles(com.Styles.TextInput)
	d.search.Placeholder = com.Text("Search by name, kind or capability")
	d.keyMap.Up = key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑", com.Text("previous")))
	d.keyMap.Down = key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓", com.Text("next")))
	d.keyMap.Enter = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", com.Text("details")))
	d.keyMap.Back = key.NewBinding(key.WithKeys("esc", "alt+esc"), key.WithHelp("esc", com.Text("back")))
	d.keyMap.Search = key.NewBinding(key.WithKeys("/"), key.WithHelp("/", com.Text("search")))
	d.keyMap.Test = key.NewBinding(key.WithKeys("t"), key.WithHelp("t", com.Text("test")))
	d.keyMap.TestAll = key.NewBinding(key.WithKeys("T"), key.WithHelp("shift+t", com.Text("test all shown")))
	d.keyMap.Cancel = key.NewBinding(key.WithKeys("x"), key.WithHelp("x", com.Text("cancel checks")))
	d.keyMap.Kind = key.NewBinding(key.WithKeys("f"), key.WithHelp("f", com.Text("kind")))
	d.keyMap.Status = key.NewBinding(key.WithKeys("s"), key.WithHelp("s", com.Text("status")))
	d.keyMap.Catalog = key.NewBinding(key.WithKeys("a"), key.WithHelp("a", com.Text("show catalog")))
	return d
}

// ID implements Dialog.
func (d *Integrations) ID() string { return IntegrationsID }

// Init loads the first snapshot and starts the refresh tick.
func (d *Integrations) Init() tea.Cmd { return tea.Batch(d.load(), d.tick()) }

func (d *Integrations) tick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return IntegrationsTick{} })
}

// load reads the snapshot in a command: adapters read config files and
// look up executables.
func (d *Integrations) load() tea.Cmd {
	svc := d.svc
	if svc == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return IntegrationsLoaded{Items: svc.Snapshot(ctx, integrations.Filter{}), Running: svc.Running()}
	}
}

func (d *Integrations) kinds() []integrations.Kind {
	return append([]integrations.Kind{""}, integrations.Kinds()...)
}

// visible applies the dialog filters to the last snapshot.
func (d *Integrations) visible() []integrations.Item {
	f := integrations.Filter{
		Kind:       d.kinds()[d.kindFilter],
		Status:     integrationStatusFilters[d.statusIdx],
		Configured: !d.catalog,
	}
	words := strings.Fields(strings.ToLower(d.search.Value()))
	var out []integrations.Item
	for _, it := range d.items {
		if !integrationMatches(f, it) {
			continue
		}
		if len(words) > 0 && !d.searchMatches(it, words) {
			continue
		}
		out = append(out, it)
	}
	return out
}

func integrationMatches(f integrations.Filter, it integrations.Item) bool {
	if f.Kind != "" && it.Kind != f.Kind {
		return false
	}
	switch f.Status {
	case "":
	case "attention":
		if !it.Status.NeedsAttention() {
			return false
		}
	default:
		if string(it.Status) != f.Status {
			return false
		}
	}
	return !f.Configured || it.Status != integrations.StatusNotConfigured
}

// searchMatches searches both the English and the displayed text.
func (d *Integrations) searchMatches(it integrations.Item, words []string) bool {
	parts := []string{it.Name, it.ID, string(it.Kind), d.com.Text(kindLabel(it.Kind)), it.Description.String(), it.Description.Localize(d.com.Text), it.Detail.String()}
	for _, c := range it.Capabilities {
		parts = append(parts, c.String(), c.Localize(d.com.Text))
	}
	hay := strings.ToLower(strings.Join(parts, " "))
	for _, w := range words {
		if !strings.Contains(hay, w) {
			return false
		}
	}
	return true
}

func (d *Integrations) find(id string) (integrations.Item, bool) {
	for _, it := range d.items {
		if it.ID == id {
			return it, true
		}
	}
	return integrations.Item{}, false
}

// HandleMsg implements Dialog.
func (d *Integrations) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case IntegrationsTick:
		return ActionCmd{Cmd: tea.Batch(d.load(), d.tick())}
	case IntegrationsLoaded:
		d.items, d.running, d.loaded = msg.Items, msg.Running, true
		return nil
	case IntegrationsDone:
		d.done(msg)
		return ActionCmd{Cmd: d.load()}
	case tea.PasteMsg:
		if d.searching {
			var cmd tea.Cmd
			d.search, cmd = d.search.Update(msg)
			d.selected = 0
			return ActionCmd{Cmd: cmd}
		}
	case tea.KeyPressMsg:
		if d.svc == nil {
			if key.Matches(msg, d.keyMap.Back) {
				return ActionClose{}
			}
			return nil
		}
		switch d.view {
		case integrationsConfirm:
			return d.handleConfirm(msg)
		case integrationsDetail:
			return d.handleDetail(msg)
		}
		if d.searching {
			return d.handleSearch(msg)
		}
		return d.handleList(msg)
	}
	return nil
}

func (d *Integrations) done(msg IntegrationsDone) {
	name := msg.ID
	if it, ok := d.find(msg.ID); ok {
		name = it.Name
	}
	switch {
	case errors.Is(msg.Err, context.Canceled):
		d.setNotice(fmt.Sprintf(d.com.Text("The check of %s was canceled; no result was kept."), name), false)
	case msg.Err != nil:
		d.setNotice(name+": "+msg.Err.Error(), false)
	case msg.Action == integrations.ActionTest:
		d.notice = ""
	default:
		d.setNotice(msg.Msg.Localize(d.com.Text), true)
	}
}

func (d *Integrations) setNotice(text string, ok bool) { d.notice, d.noticeOK = text, ok }

func (d *Integrations) handleSearch(msg tea.KeyPressMsg) Action {
	switch msg.String() {
	case "esc", "alt+esc":
		d.search.SetValue("")
		d.search.Blur()
		d.searching = false
		return nil
	case "enter", "down", "up":
		d.search.Blur()
		d.searching = false
		return nil
	}
	prev := d.search.Value()
	var cmd tea.Cmd
	d.search, cmd = d.search.Update(msg)
	if prev != d.search.Value() {
		d.selected = 0
	}
	return ActionCmd{Cmd: cmd}
}

func (d *Integrations) handleList(msg tea.KeyPressMsg) Action {
	items := d.visible()
	d.notice = ""
	switch {
	case key.Matches(msg, d.keyMap.Back):
		return ActionClose{}
	case key.Matches(msg, d.keyMap.Up):
		d.selected = max(0, d.selected-1)
	case key.Matches(msg, d.keyMap.Down):
		d.selected = min(len(items)-1, d.selected+1)
	case key.Matches(msg, d.keyMap.Search):
		d.searching = true
		return ActionCmd{Cmd: d.search.Focus()}
	case key.Matches(msg, d.keyMap.Kind):
		d.kindFilter = (d.kindFilter + 1) % len(d.kinds())
		d.selected = 0
	case key.Matches(msg, d.keyMap.Status):
		d.statusIdx = (d.statusIdx + 1) % len(integrationStatusFilters)
		d.selected = 0
	case key.Matches(msg, d.keyMap.Catalog):
		d.catalog = !d.catalog
		d.selected = 0
	case key.Matches(msg, d.keyMap.Cancel):
		if n := d.svc.CancelAll(); n > 0 {
			d.setNotice(fmt.Sprintf(d.com.Text("Canceled %d running checks."), n), true)
		}
		return ActionCmd{Cmd: d.load()}
	case key.Matches(msg, d.keyMap.TestAll):
		var ids []string
		network := 0
		for _, it := range items {
			if it.Supports(integrations.ActionTest) && it.Status != integrations.StatusChecking {
				ids = append(ids, it.ID)
				if networkKind(it.Kind) {
					network++
				}
			}
		}
		if len(ids) == 0 {
			d.setNotice(d.com.Text("Nothing shown can be tested."), false)
			return nil
		}
		d.ask(fmt.Sprintf(d.com.Text("Test %d integrations? %d of them contact a provider or server. Every check is free: no model and no MCP tool is called."), len(ids), network), func() Action {
			return ActionCmd{Cmd: d.checkMany(ids)}
		})
	case key.Matches(msg, d.keyMap.Test):
		if d.selected >= 0 && d.selected < len(items) {
			it := items[d.selected]
			if !it.Supports(integrations.ActionTest) {
				d.setNotice(d.com.Text("This item has no check."), false)
				return nil
			}
			return ActionCmd{Cmd: d.check(it.ID)}
		}
	case key.Matches(msg, d.keyMap.Enter):
		if d.selected >= 0 && d.selected < len(items) {
			d.detailID, d.actionSel, d.scroll = items[d.selected].ID, 0, 0
			d.view = integrationsDetail
		}
	}
	return nil
}

func networkKind(k integrations.Kind) bool {
	return k == integrations.KindProvider || k == integrations.KindAccount || k == integrations.KindMCP
}

// check runs one check off the UI loop. The service bounds it with its own
// timeout; x cancels it.
func (d *Integrations) check(id string) tea.Cmd {
	svc := d.svc
	return tea.Batch(
		func() tea.Msg {
			_, err := svc.Check(context.Background(), id)
			return IntegrationsDone{ID: id, Action: integrations.ActionTest, Err: err}
		},
		// Show the "checking" state right away.
		func() tea.Msg {
			time.Sleep(50 * time.Millisecond)
			return IntegrationsTick{}
		},
	)
}

func (d *Integrations) checkMany(ids []string) tea.Cmd {
	svc := d.svc
	return tea.Batch(
		func() tea.Msg {
			svc.CheckMany(context.Background(), ids)
			return IntegrationsDone{Action: integrations.ActionTest}
		},
		func() tea.Msg {
			time.Sleep(50 * time.Millisecond)
			return IntegrationsTick{}
		},
	)
}

func (d *Integrations) do(id string, a integrations.Action) tea.Cmd {
	svc := d.svc
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		msg, err := svc.Do(ctx, id, a)
		return IntegrationsDone{ID: id, Action: a, Msg: msg, Err: err}
	}
}

// detailActions lists what the user can do with the item.
func (d *Integrations) detailActions(it integrations.Item) []integrations.Action {
	var out []integrations.Action
	for _, a := range it.Actions {
		if a == integrations.ActionTest && it.Status == integrations.StatusChecking {
			continue
		}
		out = append(out, a)
	}
	return out
}

const integrationsActionCancelCheck integrations.Action = "cancel_check"

func (d *Integrations) handleDetail(msg tea.KeyPressMsg) Action {
	it, ok := d.find(d.detailID)
	if !ok {
		d.view = integrationsList
		return nil
	}
	actions := d.detailActions(it)
	if it.Status == integrations.StatusChecking {
		actions = append([]integrations.Action{integrationsActionCancelCheck}, actions...)
	}
	switch {
	case key.Matches(msg, d.keyMap.Back):
		d.view = integrationsList
		d.notice = ""
	case key.Matches(msg, d.keyMap.Up):
		d.actionSel = max(0, d.actionSel-1)
	case key.Matches(msg, d.keyMap.Down):
		d.actionSel = min(len(actions)-1, d.actionSel+1)
	case key.Matches(msg, d.keyMap.Test):
		if it.Supports(integrations.ActionTest) && it.Status != integrations.StatusChecking {
			return ActionCmd{Cmd: d.check(it.ID)}
		}
	case key.Matches(msg, d.keyMap.Enter):
		if d.actionSel < 0 || d.actionSel >= len(actions) {
			return nil
		}
		return d.runAction(it, actions[d.actionSel])
	}
	return nil
}

func (d *Integrations) runAction(it integrations.Item, a integrations.Action) Action {
	d.notice = ""
	switch a {
	case integrationsActionCancelCheck:
		d.svc.Cancel(it.ID)
		return ActionCmd{Cmd: d.load()}
	case integrations.ActionTest:
		return ActionCmd{Cmd: d.check(it.ID)}
	case integrations.ActionConfigure:
		return ActionOpenDialog{DialogID: ModelsID}
	case integrations.ActionLogin:
		if it.Kind == integrations.KindMCP {
			return ActionIntegrationsMCPLogin{Name: strings.TrimPrefix(it.ID, "mcp:")}
		}
		return ActionOpenDialog{DialogID: ModelsID}
	case integrations.ActionOpenApp:
		return ActionOpenApps{AppID: it.AppID}
	case integrations.ActionEnable:
		return ActionCmd{Cmd: d.do(it.ID, a)}
	}
	if a.Destructive() {
		var text string
		switch a {
		case integrations.ActionDisable:
			text = d.com.Text("Disable %s? Its settings and credentials stay in %s; enable it again at any time.")
		case integrations.ActionForgetCredentials:
			text = d.com.Text("Remove the saved credentials of %s from %s? Other settings stay. You will need to sign in or add a key again.")
		case integrations.ActionRemove:
			text = d.com.Text("Remove %s from %s? The entry is deleted from that file.")
		}
		d.ask(fmt.Sprintf(text, it.Name, it.Source.Path), func() Action { return ActionCmd{Cmd: d.do(it.ID, a)} })
		return nil
	}
	return ActionCmd{Cmd: d.do(it.ID, a)}
}

func (d *Integrations) ask(text string, run func() Action) {
	d.confirm = &integrationConfirm{text: text, run: run}
	d.view = integrationsConfirm
}

func (d *Integrations) handleConfirm(msg tea.KeyPressMsg) Action {
	back := integrationsList
	if d.detailID != "" {
		if _, ok := d.find(d.detailID); ok && d.confirm != nil {
			back = integrationsDetail
		}
	}
	switch msg.String() {
	case "y", "Y":
		c := d.confirm
		d.confirm, d.view = nil, back
		if c != nil && c.run != nil {
			return c.run()
		}
	case "n", "N", "esc", "alt+esc":
		d.confirm, d.view = nil, back
	}
	return nil
}

// Labels shown for kinds, statuses and actions. They are catalog literals.
func kindLabel(k integrations.Kind) string {
	switch k {
	case integrations.KindProvider:
		return "Model providers"
	case integrations.KindAccount:
		return "Accounts"
	case integrations.KindMCP:
		return "MCP servers"
	case integrations.KindTool:
		return "Local tools"
	case integrations.KindDependency:
		return "App runtimes"
	case integrations.KindApp:
		return "Local apps"
	}
	return "All kinds"
}

func statusLabel(s integrations.Status) string {
	switch s {
	case integrations.StatusNotConfigured:
		return "not configured"
	case integrations.StatusUnchecked:
		return "not checked"
	case integrations.StatusChecking:
		return "checking"
	case integrations.StatusReady:
		return "ready"
	case integrations.StatusNeedsLogin:
		return "needs sign-in"
	case integrations.StatusMissingDependency:
		return "missing dependency"
	case integrations.StatusConnectionError:
		return "connection error"
	case integrations.StatusLimited:
		return "limited"
	case integrations.StatusDisabled:
		return "disabled"
	}
	return string(s)
}

func statusFilterLabel(f string) string {
	switch f {
	case "":
		return "All statuses"
	case "attention":
		return "Needs attention"
	}
	return statusLabel(integrations.Status(f))
}

func actionLabel(a integrations.Action) string {
	switch a {
	case integrations.ActionTest:
		return "Test now (free)"
	case integrations.ActionConfigure:
		return "Set up in /model"
	case integrations.ActionLogin:
		return "Sign in"
	case integrations.ActionEnable:
		return "Enable"
	case integrations.ActionDisable:
		return "Disable (keeps settings)"
	case integrations.ActionForgetCredentials:
		return "Forget saved credentials"
	case integrations.ActionRemove:
		return "Remove from configuration"
	case integrations.ActionOpenApp:
		return "Open in /app"
	case integrationsActionCancelCheck:
		return "Cancel the running check"
	}
	return string(a)
}

func authLabelOf(a integrations.AuthKind) string {
	switch a {
	case integrations.AuthAPIKey:
		return "API key"
	case integrations.AuthOAuth:
		return "OAuth sign-in"
	case integrations.AuthHeaders:
		return "HTTP headers"
	case integrations.AuthEnv:
		return "environment variable"
	}
	return "none"
}

func (d *Integrations) statusStyle(s integrations.Status) (string, lipgloss.Style) {
	t := d.com.Styles
	switch s {
	case integrations.StatusReady:
		return "●", t.Dialog.OAuth.Success
	case integrations.StatusChecking:
		return "…", t.Dialog.TitleAccent
	case integrations.StatusNeedsLogin, integrations.StatusMissingDependency, integrations.StatusConnectionError:
		return "✗", t.Dialog.OAuth.ErrorText
	case integrations.StatusLimited:
		return "◐", t.Dialog.TitleAccent
	}
	return "○", t.Dialog.SecondaryText.Padding(0)
}

// Draw implements Dialog.
func (d *Integrations) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := d.com.Styles
	width := max(0, min(100, area.Dx()-t.Dialog.View.GetHorizontalBorderSize()))
	inner := max(10, width-t.Dialog.View.GetHorizontalFrameSize())
	text := inner - 1
	rc := NewRenderContext(t, width)
	rc.Title = d.com.Text("Integrations")
	if d.running > 0 {
		rc.TitleInfo = t.Dialog.SecondaryText.Padding(0).Render(" " + fmt.Sprintf(d.com.Text("%d checks running"), d.running))
	}
	rc.Help = renderDialogHelp(t, &d.help, d, inner)
	height := max(4, area.Dy()-t.Dialog.View.GetVerticalFrameSize()-6)

	var cur *tea.Cursor
	if d.view == integrationsList && d.svc != nil {
		d.search.SetWidth(dialogInputTextWidth(t, d.search, inner))
		rc.AddPart(t.Dialog.InputPrompt.Render(d.search.View()))
		if d.searching {
			cur = InputCursor(t, d.search.Cursor())
		}
		height--
	}

	var lines []string
	focus := -1
	switch {
	case d.svc == nil:
		lines = wrapLines(t.Dialog.PrimaryText.Padding(0), d.com.Text("Integrations are only available when Atlas runs locally."), text)
	case d.view == integrationsConfirm && d.confirm != nil:
		lines = wrapLines(t.Dialog.PrimaryText.Padding(0), d.confirm.text, text)
		lines = append(lines, "", t.Dialog.SecondaryText.Padding(0).Render(d.com.Text("y confirm · n cancel")))
	case d.view == integrationsDetail:
		lines, focus = d.detailLines(text)
	default:
		lines, focus = d.listLines(text)
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
	DrawCenterCursor(scr, area, rc.Render(), cur)
	return cur
}

func (d *Integrations) listLines(width int) ([]string, int) {
	t := d.com.Styles
	sec := t.Dialog.SecondaryText.Padding(0)
	filters := d.com.Text(kindLabel(d.kinds()[d.kindFilter])) + " · " + d.com.Text(statusFilterLabel(integrationStatusFilters[d.statusIdx]))
	if d.catalog {
		filters += " · " + d.com.Text("catalog shown")
	}
	lines := []string{sec.Render(filters)}
	if d.notice != "" {
		style := t.Dialog.OAuth.ErrorText
		if d.noticeOK {
			style = t.Dialog.OAuth.Success
		}
		lines = append(lines, wrapLines(style, d.notice, width)...)
	}
	if !d.loaded {
		return append(lines, sec.Render(d.com.Text("Loading…"))), -1
	}
	items := d.visible()
	if len(items) == 0 {
		return append(lines, "", sec.Render(d.com.Text("Nothing matches these filters."))), -1
	}
	d.selected = min(max(0, d.selected), len(items)-1)
	focus := 0
	var kind integrations.Kind = "-"
	for i, it := range items {
		if it.Kind != kind {
			kind = it.Kind
			lines = append(lines, "", t.Dialog.TitleAccent.Render(d.com.Text(kindLabel(kind))))
		}
		icon, style := d.statusStyle(it.Status)
		status := d.com.Text(statusLabel(it.Status))
		detail := ""
		switch {
		case !it.LastError.IsZero() && it.Status != integrations.StatusReady:
			detail = it.LastError.Localize(d.com.Text)
		case !it.Detail.IsZero():
			detail = it.Detail.Localize(d.com.Text)
		case it.Previous != nil:
			detail = fmt.Sprintf(d.com.Text("before restart: %s"), d.com.Text(statusLabel(it.Previous.Status)))
		}
		row := style.Render(icon+" ") + it.Name + " " + style.Render(status)
		if detail != "" && width > 50 {
			row += sec.Render(" — " + firstLine(detail))
		}
		if i == d.selected {
			focus = len(lines)
			lines = append(lines, t.Dialog.SelectedItem.Width(width).Render(stripStyles(row)))
		} else {
			lines = append(lines, t.Dialog.NormalItem.Width(width).Render(row))
		}
	}
	return lines, focus
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	return s
}

func (d *Integrations) detailLines(width int) ([]string, int) {
	t := d.com.Styles
	sec := t.Dialog.SecondaryText.Padding(0)
	prim := t.Dialog.PrimaryText.Padding(0)
	it, ok := d.find(d.detailID)
	if !ok {
		return []string{sec.Render(d.com.Text("This item is no longer configured."))}, -1
	}
	icon, style := d.statusStyle(it.Status)
	lines := []string{
		t.Dialog.TitleAccent.Render(it.Name) + sec.Render("  "+d.com.Text(kindLabel(it.Kind))),
		style.Render(icon + " " + d.com.Text(statusLabel(it.Status))),
	}
	add := func(label, value string) {
		if value == "" {
			return
		}
		lines = append(lines, wrapLines(prim, d.com.Text(label)+" "+value, width)...)
	}
	if !it.Description.IsZero() {
		lines = append(lines, wrapLines(sec, it.Description.Localize(d.com.Text), width)...)
	}
	lines = append(lines, "")
	add("Detail:", it.Detail.Localize(d.com.Text))
	add("Error:", it.LastError.Localize(d.com.Text))
	add("Next step:", it.Remedy.Localize(d.com.Text))
	if !it.CheckedAt.IsZero() {
		add("Last check:", t.RelativeTime(it.CheckedAt))
	} else if it.Status != integrations.StatusNotConfigured && it.Status != integrations.StatusDisabled && !it.Live {
		add("Last check:", d.com.Text("not checked since Atlas started"))
	}
	if it.Previous != nil {
		prev := d.com.Text(statusLabel(it.Previous.Status)) + " (" + t.RelativeTime(it.Previous.CheckedAt) + ")"
		if !it.Previous.Detail.IsZero() {
			prev += " — " + it.Previous.Detail.Localize(d.com.Text)
		}
		add("Before restart:", prev)
	}
	add("Note:", it.Stale.Localize(d.com.Text))
	if len(it.Capabilities) > 0 {
		caps := make([]string, len(it.Capabilities))
		for i, c := range it.Capabilities {
			caps[i] = c.Localize(d.com.Text)
		}
		add("Can do:", strings.Join(caps, ", "))
	}
	src := it.Source.Origin
	if it.Source.Path != "" {
		src += " — " + it.Source.Path
	}
	add("Source:", src)
	if it.Source.Origin != "" {
		editable := d.com.Text("Atlas can change this entry.")
		if !it.Source.Editable {
			editable = d.com.Text("Atlas does not edit this source.")
		}
		lines = append(lines, wrapLines(sec, editable, width)...)
	}
	add("Authentication:", d.com.Text(authLabelOf(it.Auth)))
	add("Check cost:", it.CheckNote.Localize(d.com.Text))
	if d.notice != "" {
		style := t.Dialog.OAuth.ErrorText
		if d.noticeOK {
			style = t.Dialog.OAuth.Success
		}
		lines = append(lines, "")
		lines = append(lines, wrapLines(style, d.notice, width)...)
	}
	actions := d.detailActions(it)
	if it.Status == integrations.StatusChecking {
		actions = append([]integrations.Action{integrationsActionCancelCheck}, actions...)
	}
	if len(actions) == 0 {
		return lines, -1
	}
	lines = append(lines, "")
	d.actionSel = min(max(0, d.actionSel), len(actions)-1)
	focus := len(lines)
	for i, a := range actions {
		label := d.com.Text(actionLabel(a))
		if i == d.actionSel {
			focus = len(lines)
			lines = append(lines, t.Dialog.SelectedItem.Width(width).Render(label))
		} else {
			lines = append(lines, t.Dialog.NormalItem.Width(width).Render(label))
		}
	}
	return lines, focus
}

// ShortHelp implements help.KeyMap.
func (d *Integrations) ShortHelp() []key.Binding {
	switch {
	case d.svc == nil, d.view == integrationsConfirm:
		return []key.Binding{d.keyMap.Back}
	case d.view == integrationsDetail:
		return []key.Binding{d.keyMap.Up, d.keyMap.Down, key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", d.com.Text("run"))), d.keyMap.Test, d.keyMap.Back}
	case d.searching:
		return []key.Binding{key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", d.com.Text("done"))), key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", d.com.Text("clear")))}
	}
	return []key.Binding{d.keyMap.Enter, d.keyMap.Search, d.keyMap.Kind, d.keyMap.Status, d.keyMap.Catalog, d.keyMap.Test, d.keyMap.TestAll, d.keyMap.Cancel, CloseKey}
}

// FullHelp implements help.KeyMap.
func (d *Integrations) FullHelp() [][]key.Binding { return [][]key.Binding{d.ShortHelp()} }
