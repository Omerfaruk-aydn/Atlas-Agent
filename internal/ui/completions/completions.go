package completions

import (
	"cmp"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools/mcp"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ansi"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ordered"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-style/v2"
	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/key"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/fsext"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/list"
)

const (
	minHeight = 1
	maxHeight = 10
	minWidth  = 10
	maxWidth  = 100

	// maxNameColumn caps the shared width of the "/" popup's name
	// column, so one unusually long command cannot crowd out every
	// gloss beside it.
	maxNameColumn = 28

	tierExactName = iota
	tierPrefixName
	tierPathSegment
	tierFallback
)

// SelectionMsg is sent when a completion is selected.
type SelectionMsg[T any] struct {
	Value    T
	KeepOpen bool // If true, insert without closing.
}

// ClosedMsg is sent when the completions are closed.
type ClosedMsg struct{}

// CompletionItemsLoadedMsg is sent when files have been loaded for completions.
type CompletionItemsLoadedMsg struct {
	Files     []FileCompletionValue
	Resources []ResourceCompletionValue
}

// Completions represents the completions popup component.
type Completions struct {
	// Popup dimensions
	width  int
	height int

	// State
	open  bool
	query string

	// Key bindings
	keyMap KeyMap

	// List component
	list *list.FilterableList

	// commands is set while the popup lists slash commands rather than
	// files. A command row is a name plus a gloss plus a shortcut, so
	// the popup takes the whole width of the editor it hangs off,
	// instead of shrinking to its longest line the way a column of file
	// paths sensibly does.
	commands bool

	// available is the widest the popup may grow, set from the window
	// size. Command rows carry a gloss now, so the popup wants far more
	// room than the bare file paths it used to list and would otherwise
	// run off the right edge of a narrow terminal.
	available int

	// Styling
	normalStyle  lipgloss.Style
	focusedStyle lipgloss.Style
	matchStyle   lipgloss.Style
	infoStyle    lipgloss.Style

	allItems []list.FilterableItem
	filtered []list.FilterableItem
}

type namePriorityRule struct {
	tier  int
	match func(pathLower, baseLower, stemLower, queryLower string) bool
}

var namePriorityRules = []namePriorityRule{
	{
		tier: tierExactName,
		match: func(_ string, baseLower, stemLower, queryLower string) bool {
			return baseLower == queryLower || stemLower == queryLower
		},
	},
	{
		tier: tierPrefixName,
		match: func(_ string, baseLower, _ string, queryLower string) bool {
			return strings.HasPrefix(baseLower, queryLower)
		},
	},
	{
		tier: tierPathSegment,
		match: func(pathLower, _ string, _ string, queryLower string) bool {
			return hasPathSegment(pathLower, queryLower)
		},
	},
}

// New creates a new completions component.
func New(normalStyle, focusedStyle, matchStyle lipgloss.Style) *Completions {
	l := list.NewFilterableList()
	l.SetGap(0)
	l.SetReverse(true)

	return &Completions{
		keyMap:       DefaultKeyMap(),
		list:         l,
		normalStyle:  normalStyle,
		focusedStyle: focusedStyle,
		matchStyle:   matchStyle,
	}
}

// SetAvailableWidth caps how wide the popup may grow. Zero means
// uncapped, which is the behaviour before a window size is known.
func (c *Completions) SetAvailableWidth(width int) {
	if c.available == width {
		return
	}
	c.available = width
	if c.open {
		c.updateSize()
	}
}

// SpansWidth reports whether the popup wants the full width of the
// editor rather than only as much as its contents need.
func (c *Completions) SpansWidth() bool {
	return c.commands
}

// SetInfoStyle sets the style of the trailing hint column, where a
// slash command shows its keyboard shortcut.
func (c *Completions) SetInfoStyle(style lipgloss.Style) {
	c.infoStyle = style
}

// SetStyles updates the styles used when rendering completion items.
// Existing items are not restyled; subsequent SetItems calls pick up the
// new styles.
func (c *Completions) SetStyles(normalStyle, focusedStyle, matchStyle lipgloss.Style) {
	c.normalStyle = normalStyle
	c.focusedStyle = focusedStyle
	c.matchStyle = matchStyle
}

// IsOpen returns whether the completions popup is open.
func (c *Completions) IsOpen() bool {
	return c.open
}

// Query returns the current filter query.
func (c *Completions) Query() string {
	return c.query
}

// Size returns the visible size of the popup.
func (c *Completions) Size() (width, height int) {
	visible := len(c.filtered)
	return c.width, min(visible, c.height)
}

// KeyMap returns the key bindings.
func (c *Completions) KeyMap() KeyMap {
	return c.keyMap
}

// Open opens the completions with file items from the filesystem.
func (c *Completions) Open(depth, limit int) tea.Cmd {
	return func() tea.Msg {
		var msg CompletionItemsLoadedMsg
		var wg sync.WaitGroup
		wg.Go(func() {
			msg.Files = loadFiles(depth, limit)
		})
		wg.Go(func() {
			msg.Resources = loadMCPResources()
		})
		wg.Wait()
		return msg
	}
}

// SetItems sets the files and MCP resources and rebuilds the merged list.
func (c *Completions) SetItems(files []FileCompletionValue, resources []ResourceCompletionValue) {
	c.commands = false
	items := make([]list.FilterableItem, 0, len(files)+len(resources))

	// Add files first.
	for _, file := range files {
		item := NewCompletionItem(
			file.Path,
			file,
			c.normalStyle,
			c.focusedStyle,
			c.matchStyle,
		)
		items = append(items, item)
	}

	// Add MCP resources.
	for _, resource := range resources {
		item := NewCompletionItem(
			resource.MCPName+"/"+cmp.Or(resource.Title, resource.URI),
			resource,
			c.normalStyle,
			c.focusedStyle,
			c.matchStyle,
		)
		items = append(items, item)
	}

	c.finishOpen(items)
}

// SetCommandItems opens the popup with commands to run instead of text
// to insert -- the "/" trigger's source, built synchronously from the
// command palette's own item list rather than loaded asynchronously
// the way file/resource completions are.
func (c *Completions) SetCommandItems(cmds []CommandCompletionValue) {
	// The name column is measured across every command, not just the
	// eight or so on screen, so the gloss column stays put while the
	// reader scrolls. It is capped so one long command cannot push
	// every gloss off the right edge.
	label := 0
	for _, cmd := range cmds {
		label = max(label, ansi.StringWidth(cmd.Name))
	}
	label = min(label, maxNameColumn)

	c.commands = true

	// Commands sit on the terminal's own ground rather than on a raised
	// panel: the popup already spans the editor, and a filled slab that
	// wide reads as a second window rather than as the next thing the
	// editor is about to say. The rows still paint their full width, so
	// they cover the chat underneath.
	normal := c.normalStyle.UnsetBackground()

	// A row is not banded either. Taking the accent as foreground marks
	// the selection just as clearly and lets the whole line -- name,
	// gloss and shortcut -- move to it at once.
	focused := normal.Foreground(c.focusedStyle.GetBackground())
	info := c.infoStyle.UnsetBackground()

	items := make([]list.FilterableItem, 0, len(cmds))
	for _, cmd := range cmds {
		item := NewCompletionItem(
			cmd.Name,
			cmd,
			normal,
			focused,
			c.matchStyle,
		)
		// The name leads the filter text so a match on it scores ahead
		// of one buried in an alias.
		filter := strings.Join(append([]string{cmd.Name}, cmd.Aliases...), " ")
		items = append(items, item.WithCommandColumns(cmd.Detail, cmd.Hint, filter, label, info))
	}
	c.finishOpen(items)
}

// finishOpen finishes opening the popup with the given items, shared
// by SetItems and SetCommandItems.
func (c *Completions) finishOpen(items []list.FilterableItem) {
	c.open = true
	c.query = ""
	c.allItems = items
	c.filtered = append([]list.FilterableItem(nil), items...)
	c.list.SetItems(c.filtered...)
	c.list.SetFilter("")
	c.list.Focus()

	c.width = maxWidth
	c.height = ordered.Clamp(len(items), int(minHeight), int(maxHeight))
	c.list.SetSize(c.width, c.height)
	c.list.SelectFirst()
	c.list.ScrollToSelected()

	c.updateSize()
}

// Close closes the completions popup.
func (c *Completions) Close() {
	c.open = false
}

// Filter filters the completions with the given query.
func (c *Completions) Filter(query string) {
	if !c.open {
		return
	}

	if query == c.query {
		return
	}

	c.query = query
	c.applyNamePriorityFilter(query)

	c.updateSize()
}

func (c *Completions) applyNamePriorityFilter(query string) {
	if query == "" {
		c.filtered = append([]list.FilterableItem(nil), c.allItems...)
		c.list.SetItems(c.filtered...)
		return
	}

	c.list.SetItems(c.allItems...)
	c.list.SetFilter(query)
	raw := c.list.FilteredItems()
	filtered := make([]list.FilterableItem, 0, len(raw))
	for _, item := range raw {
		filterable, ok := item.(list.FilterableItem)
		if !ok {
			continue
		}
		filtered = append(filtered, filterable)
	}

	queryLower := strings.ToLower(strings.TrimSpace(query))
	slices.SortStableFunc(filtered, func(a, b list.FilterableItem) int {
		return namePriorityTier(a.Filter(), queryLower) - namePriorityTier(b.Filter(), queryLower)
	})
	c.filtered = filtered
	c.list.SetItems(c.filtered...)
}

func namePriorityTier(path, queryLower string) int {
	if queryLower == "" {
		return tierFallback
	}

	pathLower := strings.ToLower(path)
	baseLower := strings.ToLower(filepath.Base(strings.ReplaceAll(path, `\`, `/`)))
	stemLower := strings.TrimSuffix(baseLower, filepath.Ext(baseLower))
	for _, rule := range namePriorityRules {
		if rule.match(pathLower, baseLower, stemLower, queryLower) {
			return rule.tier
		}
	}
	return tierFallback
}

func hasPathSegment(pathLower, queryLower string) bool {
	return slices.Contains(strings.FieldsFunc(pathLower, func(r rune) bool {
		return r == '/' || r == '\\'
	}), queryLower)
}

func (c *Completions) updateSize() {
	items := c.filtered
	start, end := c.list.VisibleItemIndices()
	width := 0
	for i := start; i <= end; i++ {
		item := c.list.ItemAt(i)
		if item == nil {
			continue
		}
		if sized, ok := item.(interface{ FullWidth() int }); ok {
			width = max(width, sized.FullWidth())
			continue
		}
		s := item.(interface{ Text() string }).Text()
		width = max(width, ansi.StringWidth(s))
	}
	upper := maxWidth
	if c.commands {
		// Commands fill the editor: an aligned gloss column only reads
		// as a column if it is in the same place on every row, and the
		// shortcut needs somewhere on the right to sit.
		upper = c.available
		width = c.available
	}
	if c.available > 0 {
		upper = min(upper, c.available)
	}
	c.width = ordered.Clamp(width+2, int(minWidth), int(max(minWidth, upper)))
	c.height = ordered.Clamp(len(items), int(minHeight), int(maxHeight))
	c.list.SetSize(c.width, c.height)
	c.list.SelectFirst()
	c.list.ScrollToSelected()
}

// HasItems returns whether there are visible items.
func (c *Completions) HasItems() bool {
	return len(c.filtered) > 0
}

// Update handles key events for the completions.
func (c *Completions) Update(msg tea.KeyPressMsg) (tea.Msg, bool) {
	if !c.open {
		return nil, false
	}

	switch {
	case key.Matches(msg, c.keyMap.Up):
		c.selectPrev()
		return nil, true

	case key.Matches(msg, c.keyMap.Down):
		c.selectNext()
		return nil, true

	case key.Matches(msg, c.keyMap.UpInsert):
		c.selectPrev()
		return c.selectCurrent(true), true

	case key.Matches(msg, c.keyMap.DownInsert):
		c.selectNext()
		return c.selectCurrent(true), true

	case key.Matches(msg, c.keyMap.Select):
		return c.selectCurrent(false), true

	case key.Matches(msg, c.keyMap.Cancel):
		c.Close()
		return ClosedMsg{}, true
	}

	return nil, false
}

// selectPrev selects the previous item with circular navigation.
func (c *Completions) selectPrev() {
	items := c.filtered
	if len(items) == 0 {
		return
	}
	if !c.list.SelectPrev() {
		c.list.WrapToEnd()
	}
	c.list.ScrollToSelected()
}

// selectNext selects the next item with circular navigation.
func (c *Completions) selectNext() {
	items := c.filtered
	if len(items) == 0 {
		return
	}
	if !c.list.SelectNext() {
		c.list.WrapToStart()
	}
	c.list.ScrollToSelected()
}

// selectCurrent returns a command with the currently selected item.
func (c *Completions) selectCurrent(keepOpen bool) tea.Msg {
	items := c.filtered
	if len(items) == 0 {
		return nil
	}

	selected := c.list.Selected()
	if selected < 0 || selected >= len(items) {
		return nil
	}

	item, ok := items[selected].(*CompletionItem)
	if !ok {
		return nil
	}

	if !keepOpen {
		c.open = false
	}

	switch item := item.Value().(type) {
	case ResourceCompletionValue:
		return SelectionMsg[ResourceCompletionValue]{
			Value:    item,
			KeepOpen: keepOpen,
		}
	case FileCompletionValue:
		return SelectionMsg[FileCompletionValue]{
			Value:    item,
			KeepOpen: keepOpen,
		}
	case CommandCompletionValue:
		return SelectionMsg[CommandCompletionValue]{
			Value:    item,
			KeepOpen: keepOpen,
		}
	default:
		return nil
	}
}

// Render renders the completions popup.
func (c *Completions) Render() string {
	if !c.open {
		return ""
	}

	items := c.filtered
	if len(items) == 0 {
		return ""
	}

	return c.list.List.Render()
}

func loadFiles(depth, limit int) []FileCompletionValue {
	files, _, _ := fsext.ListDirectory(".", nil, depth, limit)
	slices.Sort(files)
	result := make([]FileCompletionValue, 0, len(files))
	for _, file := range files {
		result = append(result, FileCompletionValue{
			Path: strings.TrimPrefix(file, "./"),
		})
	}
	return result
}

func loadMCPResources() []ResourceCompletionValue {
	var resources []ResourceCompletionValue
	for mcpName, mcpResources := range mcp.Resources() {
		for _, r := range mcpResources {
			resources = append(resources, ResourceCompletionValue{
				MCPName:  mcpName,
				URI:      r.URI,
				Title:    r.Name,
				MIMEType: r.MIMEType,
			})
		}
	}
	return resources
}
