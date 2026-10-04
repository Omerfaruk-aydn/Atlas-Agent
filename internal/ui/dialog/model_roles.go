package dialog

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	uv "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ultraviolet"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/help"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/key"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/subagents"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/common"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/list"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/util"
)

// ModelRolesID is the identifier for the model roles dialog.
const ModelRolesID = "model_roles"

// presetRole is a role name the app offers before the user has
// configured anything, so a model can be assigned to a well-known
// purpose without having to invent and remember a name first.
type presetRole struct {
	name        string
	description string
}

// featureModelRoles are the names built-in features look up directly
// (see ModelRoles' doc comment in internal/config). They are offered
// whether or not any mode exists.
var featureModelRoles = []presetRole{
	{"compact", "Summarizes the session (auto or /summarize) instead of using the session's own model."},
	{"advisor", "Reviews each finished turn and leaves a note for the next prompt."},
	{"escalate", "A stronger model consulted when the primary agent gets stuck."},
}

// presetModelRoles returns every role the dialog offers up front: the
// feature roles above, then one per shipped mode. Each mode runs on the
// role sharing its name -- when it is delegated to as a subagent and
// when it is worn as the session's own mode -- so deriving this half
// from the mode catalog rather than repeating it means adding a mode can
// never leave its role missing here.
//
// Unassigned entries still show, reading "not set" until the user
// presses Edit and picks a provider/model.
func presetModelRoles() []presetRole {
	out := append([]presetRole(nil), featureModelRoles...)
	for _, mode := range subagents.Builtin() {
		out = append(out, presetRole{
			name:        mode.Name,
			description: "Mode: " + firstSentence(mode.Description),
		})
	}
	return out
}

// firstSentence trims a mode's description down to its opening sentence,
// which is the part that says what the mode does; the rest says when to
// reach for it and does not fit on a list row.
func firstSentence(s string) string {
	if i := strings.Index(s, ". "); i != -1 {
		return s[:i+1]
	}
	return s
}

// modelRoleEntry is one row: a named model role and the provider/model
// it resolves to. The two built-in roles (large/small) are shown for
// reference but cannot be edited or deleted here -- they already have
// their own dedicated Models dialog. A preset row without an assigned
// model still shows (see presetModelRoles) so it can be picked and
// filled in without typing its name.
type modelRoleEntry struct {
	*list.Versioned
	name        string
	description string
	model       config.SelectedModel
	builtin     bool
	assigned    bool
	t           *common.Common
	focused     bool
}

var _ list.Item = &modelRoleEntry{}

func (e *modelRoleEntry) Finished() bool { return true }

func (e *modelRoleEntry) Filter() string { return e.name }

func (e *modelRoleEntry) info() string {
	if !e.assigned {
		info := e.t.Text("not set")
		if e.description != "" {
			info += " -- " + e.description
		}
		return info
	}
	info := fmt.Sprintf("%s / %s", e.model.Provider, e.model.Model)
	if e.model.ReasoningEffort != "" {
		info += " · " + e.model.ReasoningEffort + e.t.Text(" reasoning")
	}
	if e.builtin {
		info += e.t.Text(" (built-in)")
	}
	return info
}

func (e *modelRoleEntry) SetFocused(focused bool) {
	if e.focused == focused {
		return
	}
	e.focused = focused
	if e.Versioned != nil {
		e.Bump()
	}
}

func (e *modelRoleEntry) Render(width int) string {
	t := e.t.Styles
	itemStyles := ListItemStyles{
		ItemBlurred:     t.Dialog.NormalItem,
		ItemFocused:     t.Dialog.SelectedItem,
		InfoTextBlurred: t.Dialog.Sessions.InfoBlurred,
		InfoTextFocused: t.Dialog.Sessions.InfoFocused,
	}
	return renderItem(itemStyles, e.name, e.info(), e.focused, width, nil, nil)
}

// ModelRoles lists the model roles a subagent's model field, the
// advisor, and vibe workers can reference by name (see
// internal/config's ModelRoles and `atlas models roles`): the two
// built-in ones (large/small) plus any custom roles configured. From
// here a custom role can be added, edited, or removed without hand
// editing the config file.
type ModelRoles struct {
	com  *common.Common
	help help.Model
	list *list.FilterableList

	keyMap struct {
		Next     key.Binding
		Previous key.Binding
		Add      key.Binding
		Edit     key.Binding
		Delete   key.Binding
		Close    key.Binding
	}
}

var _ Dialog = (*ModelRoles)(nil)

// NewModelRoles creates a new ModelRoles dialog, reading the current
// role set synchronously from com.Config() -- already a live, locally
// cached snapshot in both local and client/server mode (see
// Workspace.Config), so no separate fetch is needed.
func NewModelRoles(com *common.Common) *ModelRoles {
	d := &ModelRoles{com: com}
	d.list = list.NewFilterableList(d.buildItems()...)
	d.list.Focus()
	if d.list.Len() > 0 {
		d.list.SelectFirst()
	}

	h := help.New()
	h.Styles = com.Styles.DialogHelpStyles()
	d.help = h

	d.keyMap.Next = key.NewBinding(key.WithKeys("down", "ctrl+n"), key.WithHelp("↓", com.Text("next")))
	d.keyMap.Previous = key.NewBinding(key.WithKeys("up", "ctrl+p"), key.WithHelp("↑", com.Text("previous")))
	d.keyMap.Add = key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add"))
	d.keyMap.Edit = key.NewBinding(key.WithKeys("e"), key.WithHelp("e", com.Text("edit")))
	d.keyMap.Delete = key.NewBinding(key.WithKeys("x", "ctrl+x"), key.WithHelp("x", com.Text("delete")))
	d.keyMap.Close = CloseKey

	return d
}

// buildItems reads the current role set: large/small (builtin) first,
// then custom roles sorted by name.
func (d *ModelRoles) buildItems() []list.FilterableItem {
	cfg := d.com.Config()
	if cfg == nil {
		return nil
	}

	var items []list.FilterableItem
	for _, t := range []config.SelectedModelType{config.SelectedModelTypeLarge, config.SelectedModelTypeSmall} {
		if model, ok := cfg.Models[t]; ok {
			items = append(items, &modelRoleEntry{Versioned: list.NewVersioned(), name: string(t), model: model, builtin: true, assigned: true, t: d.com})
		}
	}

	var roles map[string]config.SelectedModel
	if cfg.Options != nil {
		roles = cfg.Options.ModelRoles
	}

	// Presets first, in a fixed (not alphabetical) order, whether or not
	// a model has been assigned yet -- picking one and pressing Edit is
	// how a user assigns it for the first time.
	presets := presetModelRoles()
	isPreset := make(map[string]bool, len(presets))
	for _, p := range presets {
		description := d.com.Text(p.description)
		if strings.HasPrefix(p.description, "Mode: ") {
			description = d.com.Text("Mode: ") + d.com.Text(strings.TrimPrefix(p.description, "Mode: "))
		}
		isPreset[p.name] = true
		model, ok := roles[p.name]
		items = append(items, &modelRoleEntry{
			Versioned: list.NewVersioned(), name: p.name, description: description,
			model: model, assigned: ok, t: d.com,
		})
	}

	// Then any other custom role the user named themselves, sorted.
	var names []string
	for name := range roles {
		if !isPreset[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		items = append(items, &modelRoleEntry{Versioned: list.NewVersioned(), name: name, model: roles[name], assigned: true, t: d.com})
	}
	return items
}

// Refresh rebuilds the list from the current config -- called after a
// save or delete completes, so the dialog reflects its own change
// without the user having to close and reopen it.
func (d *ModelRoles) Refresh() {
	selected := ""
	if item, ok := d.list.SelectedItem().(*modelRoleEntry); ok {
		selected = item.name
	}
	d.list.SetItems(d.buildItems()...)
	for i, item := range d.list.FilteredItems() {
		if e, ok := item.(*modelRoleEntry); ok && e.name == selected {
			d.list.SetSelected(i)
			break
		}
	}
}

// ID implements Dialog.
func (d *ModelRoles) ID() string {
	return ModelRolesID
}

// HandleMsg implements Dialog.
func (d *ModelRoles) HandleMsg(msg tea.Msg) Action {
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
		case key.Matches(msg, d.keyMap.Add):
			return ActionOpenModelRoleForm{}
		case key.Matches(msg, d.keyMap.Edit):
			entry, ok := d.selectedCustomEntry()
			if !ok {
				return nil
			}
			return ActionOpenModelRoleForm{
				ExistingName:            entry.name,
				ExistingProvider:        entry.model.Provider,
				ExistingModel:           entry.model.Model,
				ExistingReasoningEffort: entry.model.ReasoningEffort,
			}
		case key.Matches(msg, d.keyMap.Delete):
			entry, ok := d.selectedCustomEntry()
			if !ok || !entry.assigned {
				return nil
			}
			return ActionCmd{d.deleteCmd(entry.name)}
		}
	case modelRoleDeletedMsg:
		if msg.err != nil {
			return ActionCmd{util.ReportError(fmt.Errorf(d.com.Text("failed to delete role %q: %w"), msg.name, msg.err))}
		}
		d.Refresh()
	}
	return nil
}

// modelRoleDeletedMsg delivers the async result of deleting a custom
// model role.
type modelRoleDeletedMsg struct {
	name string
	err  error
}

func (d *ModelRoles) deleteCmd(name string) tea.Cmd {
	ws := d.com.Workspace
	return func() tea.Msg {
		err := ws.RemoveConfigField(config.ScopeGlobal, "options.model_roles."+name)
		return modelRoleDeletedMsg{name: name, err: err}
	}
}

// selectedCustomEntry returns the selected row if it is a deletable
// custom role, not one of the built-in large/small entries.
func (d *ModelRoles) selectedCustomEntry() (*modelRoleEntry, bool) {
	item := d.list.SelectedItem()
	if item == nil {
		return nil, false
	}
	entry, ok := item.(*modelRoleEntry)
	if !ok || entry.builtin {
		return nil, false
	}
	return entry, true
}

// Cursor implements Dialog. The model roles list has no text input of
// its own; add/edit happen in a separate Arguments form.
func (d *ModelRoles) Cursor() *tea.Cursor {
	return nil
}

// Draw implements [Dialog].
func (d *ModelRoles) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := d.com.Styles
	width := max(0, min(defaultDialogMaxWidth, area.Dx()-t.Dialog.View.GetHorizontalBorderSize()))
	height := max(0, min(defaultDialogHeight, area.Dy()-t.Dialog.View.GetVerticalBorderSize()))
	innerWidth := width - t.Dialog.View.GetHorizontalFrameSize()

	rc := NewRenderContext(t, width)
	rc.Title = d.com.Text("Model Roles")

	if d.list.Len() == 0 {
		rc.AddPart(t.Dialog.Sessions.RenamingingMessage.Render(d.com.Text("No model roles configured yet.")))
	} else {
		listHeight, listTotalHeight, _ := sizeDialogList(t, d.list, innerWidth, height)
		bodyView := t.Dialog.List.Height(d.list.Height()).Render(d.list.Render())
		bodyView = joinScrollbar(t, bodyView, listHeight, listTotalHeight, listHeight, d.list.Offset())
		rc.AddPart(bodyView)
	}

	rc.Help = renderDialogHelp(t, &d.help, d, innerWidth)

	view := rc.Render()
	DrawCenterCursor(scr, area, view, nil)
	return nil
}

// ShortHelp implements [help.KeyMap].
func (d *ModelRoles) ShortHelp() []key.Binding {
	return []key.Binding{d.keyMap.Previous, d.keyMap.Next, d.keyMap.Add, d.keyMap.Edit, d.keyMap.Delete, d.keyMap.Close}
}

// FullHelp implements [help.KeyMap].
func (d *ModelRoles) FullHelp() [][]key.Binding {
	return [][]key.Binding{d.ShortHelp()}
}
