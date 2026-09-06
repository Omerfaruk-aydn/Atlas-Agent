package completions

import (
	"slices"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ansi"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-style/v2"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/dialog"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/list"
	"github.com/rivo/uniseg"
	"github.com/sahilm/fuzzy"
)

// FileCompletionValue represents a file path completion value.
type FileCompletionValue struct {
	Path string
}

// ResourceCompletionValue represents a MCP resource completion value.
type ResourceCompletionValue struct {
	MCPName  string
	URI      string
	Title    string
	MIMEType string
}

// CommandCompletionValue wraps one command-palette entry for the "/"
// inline completion popup: selecting it runs Action directly, exactly
// as picking the same entry from the full command palette would,
// rather than inserting text the way a file or resource completion
// does.
//
// Name is what the popup lists -- the command as it would be typed,
// slash and all -- because the reader has just typed "/" and a list of
// prose titles does not answer what they can type next. Detail is the
// gloss beside it, Hint the shortcut trailing both, and Aliases are
// matched against without being shown, so "/clear" finds New Session
// the way the full palette already does.
type CommandCompletionValue struct {
	Name    string
	Detail  string
	Hint    string
	Aliases []string
	Action  dialog.Action
}

// CompletionItem represents an item in the completions list.
type CompletionItem struct {
	*list.Versioned

	text    string
	value   any
	match   fuzzy.Match
	focused bool
	cache   map[int]string

	// cols, when set, lays the item out as a slash command: its name in
	// a column shared with every other row, then a gloss, then the
	// keyboard shortcut. filter, when set, replaces text for matching
	// so aliases can be searched without being listed.
	cols   *commandColumns
	filter string

	// Styles
	normalStyle  lipgloss.Style
	focusedStyle lipgloss.Style
	matchStyle   lipgloss.Style
}

// commandColumns is the aligned three-column layout a slash command
// row is rendered in. label is the width every row pads its name to,
// computed over the whole list rather than the visible window, so the
// gloss column does not shift as the reader scrolls.
type commandColumns struct {
	label  int
	detail string
	hint   string
	style  lipgloss.Style
}

// WithCommandColumns returns the item laid out as a slash command:
// name, gloss and shortcut in aligned columns, matched additionally
// against filter.
func (c *CompletionItem) WithCommandColumns(detail, hint, filter string, label int, style lipgloss.Style) *CompletionItem {
	c.cols = &commandColumns{label: label, detail: detail, hint: hint, style: style}
	c.filter = filter
	return c
}

// FullWidth reports the width the item wants: for a slash command, its
// whole three-column line, so the popup sizes itself to the layout
// rather than to the longest name.
func (c *CompletionItem) FullWidth() int {
	if c.cols == nil {
		return ansi.StringWidth(c.text)
	}
	w := max(c.cols.label, ansi.StringWidth(c.text))
	if c.cols.detail != "" {
		w += colGap + ansi.StringWidth(c.cols.detail)
	}
	if c.cols.hint != "" {
		w += colGap + ansi.StringWidth(c.cols.hint)
	}
	return w
}

// colGap is the run of spaces separating two columns.
const colGap = 2

// minDetailWidth is the narrowest the gloss column may be squeezed to
// before it is dropped rather than shown as an ellipsis alone.
const minDetailWidth = 12

// NewCompletionItem creates a new completion item.
func NewCompletionItem(text string, value any, normalStyle, focusedStyle, matchStyle lipgloss.Style) *CompletionItem {
	return &CompletionItem{
		Versioned:    list.NewVersioned(),
		text:         text,
		value:        value,
		normalStyle:  normalStyle,
		focusedStyle: focusedStyle,
		matchStyle:   matchStyle,
	}
}

// Finished implements list.Item. Completion items render purely from
// (text, match, focus); any mutation (SetMatch / SetFocused) bumps
// Version() so the frozen cache entry invalidates on the next
// render. Marking them finished lets the F6 list memo skip the
// per-line work for the steady completions popup.
func (c *CompletionItem) Finished() bool {
	return true
}

// Text returns the display text of the item.
func (c *CompletionItem) Text() string {
	return c.text
}

// Value returns the value of the item.
func (c *CompletionItem) Value() any {
	return c.value
}

// Filter implements [list.FilterableItem]. Items carrying extra
// searchable text (a command's aliases and description) match on it;
// everything else matches on what it displays.
func (c *CompletionItem) Filter() string {
	if c.filter != "" {
		return c.filter
	}
	return c.text
}

// SetMatch implements [list.MatchSettable].
func (c *CompletionItem) SetMatch(m fuzzy.Match) {
	if sameFuzzyMatch(c.match, m) {
		return
	}
	c.cache = nil
	c.match = m
	c.Bump()
}

// sameFuzzyMatch reports whether two fuzzy.Match values are
// observably equal. Because Match contains a slice (MatchedIndexes)
// it is not directly comparable with ==; we compare the scalar
// fields and then walk the indexes. SetMatch uses this to skip
// gratuitous version bumps when the same match is reapplied.
func sameFuzzyMatch(a, b fuzzy.Match) bool {
	return a.Str == b.Str &&
		a.Index == b.Index &&
		a.Score == b.Score &&
		slices.Equal(a.MatchedIndexes, b.MatchedIndexes)
}

// SetFocused implements [list.Focusable].
func (c *CompletionItem) SetFocused(focused bool) {
	if c.focused == focused {
		return
	}
	c.cache = nil
	c.focused = focused
	c.Bump()
}

// Render implements [list.Item].
func (c *CompletionItem) Render(width int) string {
	return renderItem(
		c.normalStyle,
		c.focusedStyle,
		c.matchStyle,
		c.text,
		c.cols,
		c.focused,
		width,
		c.cache,
		&c.match,
	)
}

func renderItem(
	normalStyle, focusedStyle, matchStyle lipgloss.Style,
	text string,
	cols *commandColumns,
	focused bool,
	width int,
	cache map[int]string,
	match *fuzzy.Match,
) string {
	if cache == nil {
		cache = make(map[int]string)
	}

	cached, ok := cache[width]
	if ok {
		return cached
	}

	innerWidth := width - 2 // Account for padding

	// Select base style.
	style := normalStyle
	if focused {
		style = focusedStyle
	}
	matchStyle = matchStyle.Background(style.GetBackground())

	body := text
	if cols == nil {
		if ansi.StringWidth(text) > innerWidth {
			text = ansi.Truncate(text, innerWidth, "…")
		}
		body = text
	} else {
		text, body = layoutCommand(text, cols, style, focused, innerWidth)
	}

	content := style.Padding(0, 1).Width(width).Render(body)

	// Apply match highlighting using StyleRanges. Indexes are byte
	// offsets into Filter(), which for a command is its name followed
	// by its aliases; only the part that lands inside the visible name
	// can be underlined, so the rest is dropped rather than smeared
	// onto the name's last character.
	if len(match.MatchedIndexes) > 0 {
		var ranges []lipgloss.Range
		for _, rng := range matchedRanges(match.MatchedIndexes) {
			if rng[0] >= len(text) {
				continue
			}
			rng[1] = min(rng[1], len(text))
			start, stop := bytePosToVisibleCharPos(text, rng)
			// Offset by 1 for the padding space.
			ranges = append(ranges, lipgloss.NewRange(start+1, stop+2, matchStyle))
		}
		content = lipgloss.StyleRanges(content, ranges...)
	}

	cache[width] = content
	return content
}

// layoutCommand lays a slash command out across its three columns and
// returns the (possibly truncated) name alongside the composed line.
//
// The name is laid out first and never gives up room to the columns
// after it: the reader typed "/" to find out what they can type, so the
// thing they would type is the last thing that should be cut. The gloss
// takes whatever is left and the shortcut is dropped before the gloss
// is squeezed below legibility.
//
// A focused row is not banded; instead every column takes the focused
// style, so the whole line moves to the accent at once.
func layoutCommand(text string, cols *commandColumns, style lipgloss.Style, focused bool, innerWidth int) (string, string) {
	nameCol := min(cols.label, innerWidth)
	if ansi.StringWidth(text) > nameCol {
		text = ansi.Truncate(text, nameCol, "…")
	}
	line := text + strings.Repeat(" ", max(0, nameCol-ansi.StringWidth(text)))

	rest := innerWidth - nameCol - colGap
	if rest < minDetailWidth {
		return text, line
	}

	detail, hint := cols.detail, cols.hint
	if hint != "" && rest-ansi.StringWidth(hint)-colGap < minDetailWidth {
		hint = ""
	}
	detailRoom := rest
	if hint != "" {
		detailRoom = rest - ansi.StringWidth(hint) - colGap
	}
	if ansi.StringWidth(detail) > detailRoom {
		detail = ansi.Truncate(detail, detailRoom, "…")
	}

	sub := cols.style
	if focused {
		sub = style
	}
	sub = sub.Background(style.GetBackground())

	line += strings.Repeat(" ", colGap) + sub.Render(detail)
	if hint != "" {
		fill := max(colGap, rest-ansi.StringWidth(detail)-ansi.StringWidth(hint))
		line += strings.Repeat(" ", fill) + sub.Render(hint)
	}
	return text, line
}

// matchedRanges converts a list of match indexes into contiguous ranges.
func matchedRanges(in []int) [][2]int {
	if len(in) == 0 {
		return [][2]int{}
	}
	current := [2]int{in[0], in[0]}
	if len(in) == 1 {
		return [][2]int{current}
	}
	var out [][2]int
	for i := 1; i < len(in); i++ {
		if in[i] == current[1]+1 {
			current[1] = in[i]
		} else {
			out = append(out, current)
			current = [2]int{in[i], in[i]}
		}
	}
	out = append(out, current)
	return out
}

// bytePosToVisibleCharPos converts byte positions to visible character positions.
func bytePosToVisibleCharPos(str string, rng [2]int) (int, int) {
	bytePos, byteStart, byteStop := 0, rng[0], rng[1]
	pos, start, stop := 0, 0, 0
	gr := uniseg.NewGraphemes(str)
	for byteStart > bytePos {
		if !gr.Next() {
			break
		}
		bytePos += len(gr.Str())
		pos += max(1, gr.Width())
	}
	start = pos
	for byteStop > bytePos {
		if !gr.Next() {
			break
		}
		bytePos += len(gr.Str())
		pos += max(1, gr.Width())
	}
	stop = pos
	return start, stop
}

// Ensure CompletionItem implements the required interfaces.
var (
	_ list.Item           = (*CompletionItem)(nil)
	_ list.FilterableItem = (*CompletionItem)(nil)
	_ list.MatchSettable  = (*CompletionItem)(nil)
	_ list.Focusable      = (*CompletionItem)(nil)
)
