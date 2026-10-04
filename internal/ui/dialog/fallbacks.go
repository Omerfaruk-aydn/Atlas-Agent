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

// FallbacksID is the identifier for the model fallbacks dialog.
const FallbacksID = "fallbacks"

// fallbackRow is one row: either a model type's cooldown header
// (isCooldown true, Index unused) or one entry in its fallback chain
// (Index into Options.ModelFallbacks[ModelType]).
type fallbackRow struct {
	*list.Versioned
	modelType  config.SelectedModelType
	isCooldown bool
	index      int
	model      config.SelectedModel
	cooldown   int
	t          *common.Common
	focused    bool
}

var _ list.Item = &fallbackRow{}

func (r *fallbackRow) Finished() bool { return true }

func (r *fallbackRow) Filter() string {
	if r.isCooldown {
		return string(r.modelType) + r.t.Text(" cooldown")
	}
	return fmt.Sprintf("%s %s %s", r.modelType, r.model.Provider, r.model.Model)
}

func (r *fallbackRow) title() string {
	if r.isCooldown {
		return string(r.modelType) + r.t.Text(" — cooldown")
	}
	return fmt.Sprintf("%s #%d", r.modelType, r.index+1)
}

func (r *fallbackRow) info() string {
	if r.isCooldown {
		if r.cooldown <= 0 {
			return r.t.Text("returns to the primary model every turn")
		}
		return fmt.Sprintf(r.t.Text("%ds before returning to the primary model"), r.cooldown)
	}
	return fmt.Sprintf("%s / %s", r.model.Provider, r.model.Model)
}

func (r *fallbackRow) SetFocused(focused bool) {
	if r.focused == focused {
		return
	}
	r.focused = focused
	if r.Versioned != nil {
		r.Bump()
	}
}

func (r *fallbackRow) Render(width int) string {
	t := r.t.Styles
	itemStyles := ListItemStyles{
		ItemBlurred:     t.Dialog.NormalItem,
		ItemFocused:     t.Dialog.SelectedItem,
		InfoTextBlurred: t.Dialog.Sessions.InfoBlurred,
		InfoTextFocused: t.Dialog.Sessions.InfoFocused,
	}
	return renderItem(itemStyles, r.title(), r.info(), r.focused, width, nil, nil)
}

// Fallbacks lists, for each of the large/small model types, the
// cooldown setting and the ordered chain of provider/model pairs to
// fail over to when the primary model hits a 429/rate-limit response
// (see internal/config's ModelFallbacks and FallbackCooldown). From
// here an entry can be added, removed, or reordered is not supported
// (delete and re-add in the desired order) without hand editing the
// config file, and the cooldown can be edited in place.
type Fallbacks struct {
	com  *common.Common
	help help.Model
	list *list.FilterableList

	keyMap struct {
		Next     key.Binding
		Previous key.Binding
		Add      key.Binding
		Cooldown key.Binding
		Delete   key.Binding
		Close    key.Binding
	}
}

var _ Dialog = (*Fallbacks)(nil)

// NewFallbacks creates a new Fallbacks dialog, reading the current
// fallback configuration synchronously from com.Config().
func NewFallbacks(com *common.Common) *Fallbacks {
	d := &Fallbacks{com: com}
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
	d.keyMap.Cooldown = key.NewBinding(key.WithKeys("c"), key.WithHelp("c", com.Text("edit cooldown")))
	d.keyMap.Delete = key.NewBinding(key.WithKeys("x", "ctrl+x"), key.WithHelp("x", com.Text("delete")))
	d.keyMap.Close = CloseKey

	return d
}

// buildItems lays out a cooldown header row followed by every fallback
// entry, for large then small.
func (d *Fallbacks) buildItems() []list.FilterableItem {
	cfg := d.com.Config()
	if cfg == nil || cfg.Options == nil {
		return nil
	}

	var items []list.FilterableItem
	for _, t := range []config.SelectedModelType{config.SelectedModelTypeLarge, config.SelectedModelTypeSmall} {
		items = append(items, &fallbackRow{
			Versioned: list.NewVersioned(), modelType: t, isCooldown: true,
			cooldown: cfg.Options.FallbackCooldown, t: d.com,
		})
		for i, model := range cfg.Options.ModelFallbacks[t] {
			items = append(items, &fallbackRow{
				Versioned: list.NewVersioned(), modelType: t, index: i, model: model, t: d.com,
			})
		}
	}
	return items
}

// Refresh rebuilds the list from the current config -- called after a
// save or delete completes.
func (d *Fallbacks) Refresh() {
	d.list.SetItems(d.buildItems()...)
	if d.list.Len() > 0 && d.list.SelectedItem() == nil {
		d.list.SelectFirst()
	}
}

// ID implements Dialog.
func (d *Fallbacks) ID() string {
	return FallbacksID
}

// selectedModelType returns the currently selected row's model type, or
// "large" when nothing is selected yet (an empty list still needs a
// default target for Add).
func (d *Fallbacks) selectedModelType() config.SelectedModelType {
	if row, ok := d.list.SelectedItem().(*fallbackRow); ok {
		return row.modelType
	}
	return config.SelectedModelTypeLarge
}

// HandleMsg implements Dialog.
func (d *Fallbacks) HandleMsg(msg tea.Msg) Action {
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
			return ActionOpenFallbackEntryForm{ModelType: d.selectedModelType()}
		case key.Matches(msg, d.keyMap.Cooldown):
			row, ok := d.list.SelectedItem().(*fallbackRow)
			if !ok || !row.isCooldown {
				return nil
			}
			return ActionOpenFallbackCooldownForm{Current: row.cooldown}
		case key.Matches(msg, d.keyMap.Delete):
			row, ok := d.list.SelectedItem().(*fallbackRow)
			if !ok || row.isCooldown {
				return nil
			}
			return ActionCmd{d.deleteEntryCmd(row.modelType, row.index)}
		}
	case fallbackEntryDeletedMsg:
		if msg.err != nil {
			return ActionCmd{util.ReportError(fmt.Errorf(d.com.Text("failed to remove fallback entry: %w"), msg.err))}
		}
		d.Refresh()
	}
	return nil
}

// fallbackEntryDeletedMsg delivers the async result of removing one
// entry from a model type's fallback chain.
type fallbackEntryDeletedMsg struct {
	err error
}

// deleteEntryCmd removes the entry at index from modelType's chain and
// persists the whole updated chain -- config.Options.ModelFallbacks has
// no per-entry remove, only whole-slice replacement.
func (d *Fallbacks) deleteEntryCmd(modelType config.SelectedModelType, index int) tea.Cmd {
	ws := d.com.Workspace
	cfg := d.com.Config()
	var current []config.SelectedModel
	if cfg != nil && cfg.Options != nil {
		current = cfg.Options.ModelFallbacks[modelType]
	}
	updated := make([]config.SelectedModel, 0, max(0, len(current)-1))
	for i, m := range current {
		if i != index {
			updated = append(updated, m)
		}
	}
	return func() tea.Msg {
		err := ws.SetConfigField(config.ScopeGlobal, "options.model_fallbacks."+string(modelType), updated)
		return fallbackEntryDeletedMsg{err: err}
	}
}

// Cursor implements Dialog. The fallbacks list has no text input of its
// own; add/edit happen in a separate Arguments form.
func (d *Fallbacks) Cursor() *tea.Cursor {
	return nil
}

// Draw implements [Dialog].
func (d *Fallbacks) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := d.com.Styles
	width := max(0, min(defaultDialogMaxWidth, area.Dx()-t.Dialog.View.GetHorizontalBorderSize()))
	height := max(0, min(defaultDialogHeight, area.Dy()-t.Dialog.View.GetVerticalBorderSize()))
	innerWidth := width - t.Dialog.View.GetHorizontalFrameSize()

	rc := NewRenderContext(t, width)
	rc.Title = d.com.Text("Model Fallbacks")

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
func (d *Fallbacks) ShortHelp() []key.Binding {
	return []key.Binding{d.keyMap.Previous, d.keyMap.Next, d.keyMap.Add, d.keyMap.Cooldown, d.keyMap.Delete, d.keyMap.Close}
}

// FullHelp implements [help.KeyMap].
func (d *Fallbacks) FullHelp() [][]key.Binding {
	return [][]key.Binding{d.ShortHelp()}
}
