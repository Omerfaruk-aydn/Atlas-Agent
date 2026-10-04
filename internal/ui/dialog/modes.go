package dialog

import (
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	uv "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ultraviolet"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/help"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/key"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/subagents"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/common"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/list"
)

// ModesID is the identifier for the session mode dialog.
const ModesID = "modes"

// noModeKey is the sentinel row that clears the session mode. It cannot
// collide with a real mode: subagent names are alphanumeric with hyphens
// (see subagents' namePattern), so no mode can be named with a space.
const noModeKey = " none"

// sessionModeCommandLabel names the command-palette entry, showing the
// active mode so the palette itself says which one is on without having
// to open the dialog.
func sessionModeCommandLabel(cfg *config.Config) string {
	if cfg != nil && cfg.Options != nil {
		if active := strings.TrimSpace(cfg.Options.SessionMode); active != "" {
			return "Session Mode (" + modeLabel(active) + ")"
		}
	}
	return "Session Mode"
}

// modeEntry is one row: a mode the session can wear, and whether it is
// the one currently worn.
type modeEntry struct {
	*list.Versioned
	name        string
	label       string
	description string
	roleModel   string
	active      bool
	t           *common.Common
	focused     bool
}

var _ list.Item = &modeEntry{}

func (e *modeEntry) Finished() bool { return true }

func (e *modeEntry) Filter() string { return e.label + " " + e.description }

func (e *modeEntry) info() string {
	var parts []string
	if e.active {
		parts = append(parts, e.t.Text("active"))
	}
	if e.name != noModeKey {
		if e.roleModel != "" {
			parts = append(parts, e.t.Text("runs on ")+e.roleModel)
		} else {
			parts = append(parts, e.t.Text("no model assigned -- uses the session's own"))
		}
	}
	parts = append(parts, e.description)
	return strings.Join(parts, " · ")
}

func (e *modeEntry) SetFocused(focused bool) {
	if e.focused == focused {
		return
	}
	e.focused = focused
	if e.Versioned != nil {
		e.Bump()
	}
}

func (e *modeEntry) Render(width int) string {
	t := e.t.Styles
	itemStyles := ListItemStyles{
		ItemBlurred:     t.Dialog.NormalItem,
		ItemFocused:     t.Dialog.SelectedItem,
		InfoTextBlurred: t.Dialog.Sessions.InfoBlurred,
		InfoTextFocused: t.Dialog.Sessions.InfoFocused,
	}
	return renderItem(itemStyles, e.label, e.info(), e.focused, width, nil, nil)
}

// Modes lets the session itself take on a mode: the chosen mode's
// instructions are folded into the main agent's system prompt and, when
// a model role shares the mode's name, the session switches to that
// model. The same catalog is what the agent tool delegates to, so a mode
// behaves the same whether it is handed work or worn.
type Modes struct {
	com  *common.Common
	help help.Model
	list *list.FilterableList

	keyMap struct {
		Next     key.Binding
		Previous key.Binding
		Select   key.Binding
		Close    key.Binding
	}
}

var _ Dialog = (*Modes)(nil)

// NewModes creates the session mode dialog, reading the available modes
// and the currently active one from com.Config().
func NewModes(com *common.Common) *Modes {
	d := &Modes{com: com}
	d.list = list.NewFilterableList(d.buildItems()...)
	d.list.Focus()
	d.selectActive()

	h := help.New()
	h.Styles = com.Styles.DialogHelpStyles()
	d.help = h

	d.keyMap.Next = key.NewBinding(key.WithKeys("down", "ctrl+n"), key.WithHelp("↓", com.Text("next")))
	d.keyMap.Previous = key.NewBinding(key.WithKeys("up", "ctrl+p"), key.WithHelp("↑", com.Text("previous")))
	d.keyMap.Select = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", com.Text("activate")))
	d.keyMap.Close = CloseKey

	return d
}

func (d *Modes) buildItems() []list.FilterableItem {
	cfg := d.com.Config()
	if cfg == nil {
		return nil
	}

	active := ""
	var paths []string
	if cfg.Options != nil {
		active = strings.TrimSpace(cfg.Options.SessionMode)
		paths = cfg.Options.SubagentsPaths
	}

	items := []list.FilterableItem{&modeEntry{
		Versioned: list.NewVersioned(), name: noModeKey, label: d.com.Text("No Mode"),
		description: d.com.Text("The ordinary coder prompt, on the session's own model."),
		active:      active == "", t: d.com,
	}}

	for _, mode := range subagents.Discover(paths) {
		description := firstSentence(mode.Description)
		if mode.Builtin {
			description = d.com.Text(description)
		}
		roleModel := ""
		if m, ok := cfg.ResolveRole(mode.Name); ok {
			roleModel = m.Provider + "/" + m.Model
		}
		items = append(items, &modeEntry{
			Versioned: list.NewVersioned(), name: mode.Name, label: modeLabel(mode.Name),
			description: description, roleModel: roleModel,
			active: strings.EqualFold(active, mode.Name), t: d.com,
		})
	}
	return items
}

// modeLabel title-cases a mode's name for display, so "code-review"
// reads as "Code Review" in the list without changing the name the
// config and the model roles are keyed by.
func modeLabel(name string) string {
	words := strings.Split(name, "-")
	for i, w := range words {
		if w == "" {
			continue
		}
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

// selectActive puts the cursor on the mode currently in effect, so
// opening the dialog shows where you are rather than the top of the list.
func (d *Modes) selectActive() {
	for i, item := range d.list.FilteredItems() {
		if e, ok := item.(*modeEntry); ok && e.active {
			d.list.SetSelected(i)
			d.list.ScrollToSelected()
			return
		}
	}
	d.list.SelectFirst()
}

// ID implements Dialog.
func (d *Modes) ID() string { return ModesID }

// HandleMsg implements Dialog.
func (d *Modes) HandleMsg(msg tea.Msg) Action {
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
		case key.Matches(msg, d.keyMap.Select):
			entry, ok := d.list.SelectedItem().(*modeEntry)
			if !ok {
				return nil
			}
			if entry.name == noModeKey {
				return ActionSelectSessionMode{}
			}
			return ActionSelectSessionMode{Mode: entry.name}
		}
	}
	return nil
}

// Cursor implements Dialog. The mode list has no text input of its own.
func (d *Modes) Cursor() *tea.Cursor { return nil }

// Draw implements [Dialog].
func (d *Modes) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := d.com.Styles
	width := max(0, min(defaultDialogMaxWidth, area.Dx()-t.Dialog.View.GetHorizontalBorderSize()))
	height := max(0, min(defaultDialogHeight, area.Dy()-t.Dialog.View.GetVerticalBorderSize()))
	innerWidth := width - t.Dialog.View.GetHorizontalFrameSize()

	rc := NewRenderContext(t, width)
	rc.Title = d.com.Text("Session Mode")

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
func (d *Modes) ShortHelp() []key.Binding {
	return []key.Binding{d.keyMap.Previous, d.keyMap.Next, d.keyMap.Select, d.keyMap.Close}
}

// FullHelp implements [help.KeyMap].
func (d *Modes) FullHelp() [][]key.Binding {
	return [][]key.Binding{d.ShortHelp()}
}
