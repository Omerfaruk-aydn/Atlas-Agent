package dialog

import (
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/list"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/styles"
	"github.com/sahilm/fuzzy"
)

// RewindItem wraps a user [message.Message] to implement the [ListItem]
// interface for the rewind checkpoint picker.
type RewindItem struct {
	*list.Versioned
	message.Message
	t        *styles.Styles
	m        fuzzy.Match
	cache    map[int]string
	focused  bool
	hideInfo bool
}

var _ ListItem = &RewindItem{}

// Finished implements list.Item. Rewind items are render-stable outside of
// explicit SetFocused / SetMatch calls.
func (r *RewindItem) Finished() bool {
	return true
}

// Filter returns the filterable value of the message.
func (r *RewindItem) Filter() string {
	return r.preview()
}

// ID returns the unique identifier of the message.
func (r *RewindItem) ID() string {
	return r.Message.ID
}

// SetMatch sets the fuzzy match for the item.
func (r *RewindItem) SetMatch(m fuzzy.Match) {
	if sameFuzzyMatch(r.m, m) {
		return
	}
	r.cache = nil
	r.m = m
	if r.Versioned != nil {
		r.Bump()
	}
}

// SetFocused sets the focus state of the item.
func (r *RewindItem) SetFocused(focused bool) {
	if r.focused == focused {
		return
	}
	r.cache = nil
	r.focused = focused
	if r.Versioned != nil {
		r.Bump()
	}
}

// SetHideInfo controls whether the timestamp info column is shown.
func (r *RewindItem) SetHideInfo(v bool) {
	if r.hideInfo == v {
		return
	}
	r.cache = nil
	r.hideInfo = v
	if r.Versioned != nil {
		r.Bump()
	}
}

// InfoText returns the secondary text shown on the right of the item.
func (r *RewindItem) InfoText() string {
	return r.t.RelativeTime(time.Unix(r.CreatedAt/1000, 0))
}

func (r *RewindItem) preview() string {
	text := strings.TrimSpace(r.Content().Text)
	text = strings.ReplaceAll(text, "\n", " ")
	return text
}

// Render returns the string representation of the rewind item.
func (r *RewindItem) Render(width int) string {
	info := r.InfoText()
	if r.hideInfo {
		info = ""
	}
	itemStyles := ListItemStyles{
		ItemBlurred:     r.t.Dialog.NormalItem,
		ItemFocused:     r.t.Dialog.SelectedItem,
		InfoTextBlurred: r.t.Dialog.Sessions.InfoBlurred,
		InfoTextFocused: r.t.Dialog.Sessions.InfoFocused,
	}
	title := r.preview()
	if title == "" {
		title = r.t.Text("(no text)")
	}
	return renderItem(itemStyles, title, info, r.focused, width, r.cache, &r.m)
}

// rewindItems converts a slice of user [message.Message]s into
// [list.FilterableItem]s for the rewind checkpoint picker, oldest first.
func rewindItems(t *styles.Styles, messages []message.Message) []list.FilterableItem {
	items := make([]list.FilterableItem, len(messages))
	for i, m := range messages {
		items[i] = &RewindItem{Versioned: list.NewVersioned(), Message: m, t: t}
	}
	return items
}
