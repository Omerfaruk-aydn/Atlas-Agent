package dialog

import (
	"fmt"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	uv "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ultraviolet"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/help"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/key"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/common"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/list"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/util"
)

// ToolSettingsID is the identifier for the tool settings dialog.
const ToolSettingsID = "tool_settings"

// toolSettingsCatalog lists every opt-in tool group (see internal/config's
// Tools struct): off by default because each registered tool costs
// context on every request, so this is where a user turns one on for a
// repository or task that actually needs it. The always-on core tools
// (ls, grep, glob, bash, view, ...) have no such switch and so are not
// listed here.
var toolSettingsCatalog = []struct {
	key, label, description string
}{
	{"git", "Git Tools", "Read-only git_status/log/blame -- compact answers without shelling out."},
	{"docs", "Docs Tools", "Index and search a tree's Markdown files by heading."},
	{"code_intel", "Code Intel Tools", "Go static analysis (dead_code and friends) via go/ast."},
	{"quality", "Quality Tools", "Secret scanning, coverage, and other code-quality checks."},
	{"teams", "Teams (sub-agent broadcast)", "team_send/team_read so sub-agents spawned by the same task can message each other."},
	{"debugger", "Debugger", "Drive a Go program under Delve (dlv dap): breakpoints, step, inspect variables. Requires dlv installed."},
	{"browser", "Browser", "Drive a real Chrome/Chromium tab: navigate, click, type, screenshot, read console. Requires Chrome/Chromium installed."},
	{"computer", "Computer-Use", "See the screen and drive the real desktop: screenshot, click, drag, scroll, type, hotkeys. Windows only. Actions run without per-action approval while on."},
	{"browser_visible", "Browser: Visible Window", "Show the Chrome window the Browser tool drives instead of running it headless (no window, no audio)."},
}

// toolEnabled reports whether the opt-in tool group named key is
// currently on. Mirrors each ToolX.IsEnabled() method one by one since
// they are distinct Go struct fields, not a map.
func toolEnabled(cfg *config.Config) map[string]bool {
	return map[string]bool{
		"git":        cfg.Tools.Git.IsEnabled(),
		"docs":       cfg.Tools.Docs.IsEnabled(),
		"code_intel": cfg.Tools.CodeIntel.IsEnabled(),
		"quality":    cfg.Tools.Quality.IsEnabled(),
		"teams":      cfg.Tools.Teams.IsEnabled(),
		"debugger":   cfg.Tools.Debugger.IsEnabled(),
		"browser":    cfg.Tools.Browser.IsEnabled(),
		"computer":   cfg.Tools.Computer.IsEnabled(),
		// Not a tool group switch like the others: this row reflects the
		// browser tool's Headless setting, inverted so "enabled" reads as
		// "the Chrome window is visible" -- the sense the user toggles.
		"browser_visible": !cfg.Tools.Browser.IsHeadless(),
	}
}

// toolSettingEntry is one row: an opt-in tool group and whether it is
// currently enabled.
type toolSettingEntry struct {
	*list.Versioned
	key         string
	label       string
	description string
	enabled     bool
	t           *common.Common
	focused     bool
}

var _ list.Item = &toolSettingEntry{}

func (e *toolSettingEntry) Finished() bool { return true }

func (e *toolSettingEntry) Filter() string { return e.label }

func (e *toolSettingEntry) info() string {
	status := "off"
	if e.enabled {
		status = "on"
	}
	return status + " -- " + e.description
}

func (e *toolSettingEntry) SetFocused(focused bool) {
	if e.focused == focused {
		return
	}
	e.focused = focused
	if e.Versioned != nil {
		e.Bump()
	}
}

func (e *toolSettingEntry) Render(width int) string {
	t := e.t.Styles
	itemStyles := ListItemStyles{
		ItemBlurred:     t.Dialog.NormalItem,
		ItemFocused:     t.Dialog.SelectedItem,
		InfoTextBlurred: t.Dialog.Sessions.InfoBlurred,
		InfoTextFocused: t.Dialog.Sessions.InfoFocused,
	}
	return renderItem(itemStyles, e.label, e.info(), e.focused, width, nil, nil)
}

// ToolSettings lists every opt-in tool group and lets each be turned on
// or off without hand-editing the config file. Restarting the session
// (not the whole app) is enough for a change to take effect, the same
// as any other config write in this app.
type ToolSettings struct {
	com  *common.Common
	help help.Model
	list *list.FilterableList

	keyMap struct {
		Next     key.Binding
		Previous key.Binding
		Toggle   key.Binding
		Close    key.Binding
	}
}

var _ Dialog = (*ToolSettings)(nil)

// NewToolSettings creates a new ToolSettings dialog, reading the current
// enabled state synchronously from com.Config().
func NewToolSettings(com *common.Common) *ToolSettings {
	d := &ToolSettings{com: com}
	d.list = list.NewFilterableList(d.buildItems()...)
	d.list.Focus()
	d.list.SelectFirst()

	h := help.New()
	h.Styles = com.Styles.DialogHelpStyles()
	d.help = h

	d.keyMap.Next = key.NewBinding(key.WithKeys("down", "ctrl+n"), key.WithHelp("↓", "next"))
	d.keyMap.Previous = key.NewBinding(key.WithKeys("up", "ctrl+p"), key.WithHelp("↑", "previous"))
	d.keyMap.Toggle = key.NewBinding(key.WithKeys("enter", " "), key.WithHelp("enter", "toggle"))
	d.keyMap.Close = CloseKey

	return d
}

func (d *ToolSettings) buildItems() []list.FilterableItem {
	cfg := d.com.Config()
	if cfg == nil {
		return nil
	}
	enabled := toolEnabled(cfg)

	items := make([]list.FilterableItem, 0, len(toolSettingsCatalog))
	for _, entry := range toolSettingsCatalog {
		items = append(items, &toolSettingEntry{
			Versioned: list.NewVersioned(), key: entry.key, label: entry.label,
			description: entry.description, enabled: enabled[entry.key], t: d.com,
		})
	}
	return items
}

// ID implements Dialog.
func (d *ToolSettings) ID() string {
	return ToolSettingsID
}

// toolToggledMsg delivers the async result of flipping one tool's
// enabled state.
type toolToggledMsg struct {
	key     string
	enabled bool
	err     error
}

func (d *ToolSettings) toggleCmd(key string, newState bool) tea.Cmd {
	ws := d.com.Workspace
	return func() tea.Msg {
		// browser_visible isn't its own tool group -- it flips the
		// Browser tool's Headless field, inverted (visible = !headless).
		field := "tools." + key + ".enabled"
		value := any(newState)
		if key == "browser_visible" {
			field = "tools.browser.headless"
			value = !newState
		}
		err := ws.SetConfigField(config.ScopeGlobal, field, value)
		return toolToggledMsg{key: key, enabled: newState, err: err}
	}
}

// HandleMsg implements Dialog.
func (d *ToolSettings) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, d.keyMap.Close):
			return ActionClose{}
		case key.Matches(msg, d.keyMap.Previous):
			if d.list.IsSelectedFirst() {
				d.list.SelectLast()
			} else {
				d.list.SelectPrev()
			}
			d.list.ScrollToSelected()
		case key.Matches(msg, d.keyMap.Next):
			if d.list.IsSelectedLast() {
				d.list.SelectFirst()
			} else {
				d.list.SelectNext()
			}
			d.list.ScrollToSelected()
		case key.Matches(msg, d.keyMap.Toggle):
			entry, ok := d.list.SelectedItem().(*toolSettingEntry)
			if !ok {
				return nil
			}
			return ActionCmd{d.toggleCmd(entry.key, !entry.enabled)}
		}
	case toolToggledMsg:
		if msg.err != nil {
			return ActionCmd{util.ReportError(fmt.Errorf("failed to update %s: %w", msg.key, msg.err))}
		}
		for _, item := range d.list.FilteredItems() {
			if e, ok := item.(*toolSettingEntry); ok && e.key == msg.key {
				e.enabled = msg.enabled
				e.Bump()
				break
			}
		}
	}
	return nil
}

// Cursor implements Dialog. The tool settings list has no text input.
func (d *ToolSettings) Cursor() *tea.Cursor {
	return nil
}

// Draw implements [Dialog].
func (d *ToolSettings) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := d.com.Styles
	width := max(0, min(defaultDialogMaxWidth, area.Dx()-t.Dialog.View.GetHorizontalBorderSize()))
	height := max(0, min(defaultDialogHeight, area.Dy()-t.Dialog.View.GetVerticalBorderSize()))
	innerWidth := width - t.Dialog.View.GetHorizontalFrameSize()

	rc := NewRenderContext(t, width)
	rc.Title = "Tool Settings"

	listHeight, listTotalHeight, _ := sizeDialogList(t, d.list, innerWidth, height)
	bodyView := t.Dialog.List.Height(d.list.Height()).Render(d.list.Render())
	bodyView = joinScrollbar(t, bodyView, listHeight, listTotalHeight, listHeight, d.list.Offset())
	rc.AddPart(bodyView)

	rc.Help = renderDialogHelp(t, &d.help, d, innerWidth)

	view := rc.Render()
	DrawCenterCursor(scr, area, view, nil)
	return nil
}

// ShortHelp implements [help.KeyMap].
func (d *ToolSettings) ShortHelp() []key.Binding {
	return []key.Binding{d.keyMap.Previous, d.keyMap.Next, d.keyMap.Toggle, d.keyMap.Close}
}

// FullHelp implements [help.KeyMap].
func (d *ToolSettings) FullHelp() [][]key.Binding {
	return [][]key.Binding{d.ShortHelp()}
}
