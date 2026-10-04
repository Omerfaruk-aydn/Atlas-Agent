package dialog

import (
	"fmt"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-style/v2"
	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	uv "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ultraviolet"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/help"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/key"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/common"
	"github.com/dustin/go-humanize"
)

// UsageID is the identifier for the usage/cost dialog.
const UsageID = "usage"

// UsageStats is the data the Usage dialog displays, computed by the caller
// (the dialog package has no session/workspace access of its own).
type UsageStats struct {
	SessionTitle          string
	MessageCount          int64
	PromptTokens          int64
	CompletionTokens      int64
	Cost                  float64
	ContextWindow         int64
	EstimatedUsage        bool
	TotalSessions         int
	TotalCost             float64
	TotalPromptTokens     int64
	TotalCompletionTokens int64
}

// Usage shows a token/cost breakdown for the current session alongside a
// workspace-wide total across every session.
type Usage struct {
	com   *common.Common
	stats UsageStats
	help  help.Model

	keyMap struct {
		Close key.Binding
	}
}

var _ Dialog = (*Usage)(nil)

// NewUsage creates a new Usage dialog.
func NewUsage(com *common.Common, stats UsageStats) *Usage {
	d := &Usage{com: com, stats: stats}
	h := help.New()
	h.Styles = com.Styles.DialogHelpStyles()
	d.help = h
	d.keyMap.Close = CloseKey
	return d
}

// ID implements Dialog.
func (d *Usage) ID() string {
	return UsageID
}

// HandleMsg implements Dialog.
func (d *Usage) HandleMsg(msg tea.Msg) Action {
	if keyMsg, ok := msg.(tea.KeyPressMsg); ok && key.Matches(keyMsg, d.keyMap.Close) {
		return ActionClose{}
	}
	return nil
}

// Cursor implements Dialog. The usage dialog has no text input.
func (d *Usage) Cursor() *tea.Cursor {
	return nil
}

// row renders a "label ......... value" line, dot-filling the gap so
// values line up regardless of label length.
func (d *Usage) row(width int, label, value string) string {
	t := d.com.Styles
	labelStyled := t.Resource.StatusText.Render(label)
	valueStyled := t.Resource.Name.Render(value)
	gap := width - lipgloss.Width(label) - lipgloss.Width(value)
	if gap < 1 {
		return labelStyled + " " + valueStyled
	}
	return labelStyled + t.Resource.AdditionalText.Render(" "+dotFill(gap-1)+" ") + valueStyled
}

func dotFill(n int) string {
	if n <= 0 {
		return ""
	}
	b := make([]byte, n)
	for i := range b {
		b[i] = '.'
	}
	return string(b)
}

// Draw implements [Dialog].
func (d *Usage) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := d.com.Styles
	width := max(0, min(defaultDialogMaxWidth, area.Dx()-t.Dialog.View.GetHorizontalBorderSize()))
	innerWidth := width - t.Dialog.View.GetHorizontalFrameSize()

	s := d.stats
	totalTokens := s.PromptTokens + s.CompletionTokens
	contextPct := 0.0
	if s.ContextWindow > 0 {
		contextPct = float64(totalTokens) / float64(s.ContextWindow) * 100
	}

	rc := NewRenderContext(t, width)
	rc.Title = d.com.Text("Usage")
	rc.Gap = 1

	sessionLines := []string{
		t.Resource.Heading.Render(d.com.Text("This session")),
		d.row(innerWidth, d.com.Text("Messages"), humanize.Comma(s.MessageCount)),
		d.row(innerWidth, d.com.Text("Prompt tokens"), humanize.Comma(s.PromptTokens)),
		d.row(innerWidth, d.com.Text("Completion tokens"), humanize.Comma(s.CompletionTokens)),
		d.row(innerWidth, d.com.Text("Context used"), fmt.Sprintf(d.com.Text("%.1f%% of %s"), contextPct, humanize.Comma(s.ContextWindow))),
		d.row(innerWidth, d.com.Text("Cost"), formatUsageCost(s.Cost, s.EstimatedUsage)),
	}
	rc.AddPart(lipgloss.JoinVertical(lipgloss.Left, sessionLines...))

	totalLines := []string{
		t.Resource.Heading.Render(d.com.Text("All sessions")),
		d.row(innerWidth, d.com.Text("Sessions"), humanize.Comma(int64(s.TotalSessions))),
		d.row(innerWidth, d.com.Text("Total tokens"), humanize.Comma(s.TotalPromptTokens+s.TotalCompletionTokens)),
		d.row(innerWidth, d.com.Text("Total cost"), formatUsageCost(s.TotalCost, false)),
	}
	rc.AddPart(lipgloss.JoinVertical(lipgloss.Left, totalLines...))

	rc.Help = renderDialogHelp(t, &d.help, d, innerWidth)

	view := rc.Render()
	DrawCenter(scr, area, view)
	return nil
}

func formatUsageCost(cost float64, estimated bool) string {
	s := fmt.Sprintf("$%.4f", cost)
	if estimated {
		s += " (est.)"
	}
	return s
}

// ShortHelp implements [help.KeyMap].
func (d *Usage) ShortHelp() []key.Binding {
	return []key.Binding{d.keyMap.Close}
}

// FullHelp implements [help.KeyMap].
func (d *Usage) FullHelp() [][]key.Binding {
	return [][]key.Binding{d.ShortHelp()}
}
