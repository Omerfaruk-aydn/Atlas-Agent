package dialog

import (
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-style/v2"
	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	uv "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ultraviolet"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/help"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/key"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/common"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/styles"
)

// SideQuestionID is the identifier for the side question ("/btw") panel.
const SideQuestionID = "side_question"

// ActionDismissSideQuestion is returned when the side question panel is
// dismissed. Unlike a plain close it tells the caller to also stop the
// question if it is still being answered.
type ActionDismissSideQuestion struct{}

const (
	sideQuestionMaxWidth  = 100
	sideQuestionMaxHeight = 30
)

// SideQuestion shows the answer to a "/btw" side question. It is a
// panel over the chat, not part of it: the answer lives only here and
// is gone once the panel is dismissed.
type SideQuestion struct {
	com      *common.Common
	question string
	answer   string
	model    string
	err      string
	done     bool
	yOffset  int

	// rendered is the body wrapped for renderedWidth, rebuilt when the
	// width, the styles or the content change.
	rendered      []string
	renderedWidth int
	renderedFor   *styles.Styles
	version       int
	renderedVer   int

	help   help.Model
	keyMap struct {
		Up       key.Binding
		Down     key.Binding
		PageUp   key.Binding
		PageDown key.Binding
		Close    key.Binding
	}
}

var _ Dialog = (*SideQuestion)(nil)

// NewSideQuestion creates the panel in its waiting state.
func NewSideQuestion(com *common.Common, question string) *SideQuestion {
	d := &SideQuestion{com: com, question: question, version: 1}
	h := help.New()
	h.Styles = com.Styles.DialogHelpStyles()
	d.help = h

	d.keyMap.Up = key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", com.Text("scroll up")))
	d.keyMap.Down = key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", com.Text("scroll down")))
	d.keyMap.PageUp = key.NewBinding(key.WithKeys("pgup", "b"), key.WithHelp("pgup", com.Text("page up")))
	d.keyMap.PageDown = key.NewBinding(key.WithKeys("pgdown", "f", " "), key.WithHelp("pgdn", com.Text("page down")))
	d.keyMap.Close = key.NewBinding(key.WithKeys("esc", "alt+esc", "q", "enter"), key.WithHelp("esc", com.Text("dismiss")))
	return d
}

// SetAnswer shows the finished answer.
func (d *SideQuestion) SetAnswer(answer, model string) {
	d.answer, d.model, d.err, d.done = answer, model, "", true
	d.yOffset = 0
	d.version++
}

// SetError shows why there is no answer.
func (d *SideQuestion) SetError(err error) {
	d.answer, d.err, d.done = "", err.Error(), true
	d.yOffset = 0
	d.version++
}

// Done reports whether the panel has its answer or error.
func (d *SideQuestion) Done() bool {
	return d.done
}

// ID implements Dialog.
func (d *SideQuestion) ID() string {
	return SideQuestionID
}

// HandleMsg implements Dialog.
func (d *SideQuestion) HandleMsg(msg tea.Msg) Action {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	switch {
	case key.Matches(keyMsg, d.keyMap.Close):
		return ActionDismissSideQuestion{}
	case key.Matches(keyMsg, d.keyMap.Up):
		d.scroll(-1)
	case key.Matches(keyMsg, d.keyMap.Down):
		d.scroll(1)
	case key.Matches(keyMsg, d.keyMap.PageUp):
		d.scroll(-10)
	case key.Matches(keyMsg, d.keyMap.PageDown):
		d.scroll(10)
	}
	return nil
}

func (d *SideQuestion) scroll(delta int) {
	d.yOffset = max(0, d.yOffset+delta)
}

// body renders the question and the answer (or the wait) to lines no
// wider than width.
func (d *SideQuestion) body(width int) []string {
	t := d.com.Styles
	if d.rendered != nil && d.renderedWidth == width && d.renderedFor == t && d.renderedVer == d.version {
		return d.rendered
	}

	wrap := lipgloss.NewStyle().Width(width)
	lines := strings.Split(wrap.Render(t.Resource.Name.Render(d.question)), "\n")
	lines = append(lines, "")
	switch {
	case d.err != "":
		lines = append(lines, strings.Split(wrap.Render(t.Status.ErrorMessage.Render(d.err)), "\n")...)
	case !d.done:
		lines = append(lines, t.Resource.AdditionalText.Render(d.com.Text("Thinking…")))
	default:
		lines = append(lines, d.renderAnswer(width)...)
	}

	d.rendered, d.renderedWidth, d.renderedFor, d.renderedVer = lines, width, t, d.version
	return lines
}

func (d *SideQuestion) renderAnswer(width int) []string {
	renderer := common.MarkdownRenderer(d.com.Styles, width)
	mu := common.LockMarkdownRenderer(renderer)
	mu.Lock()
	out, err := renderer.Render(d.answer)
	mu.Unlock()
	if err != nil || strings.TrimSpace(out) == "" {
		return strings.Split(lipgloss.NewStyle().Width(width).Render(d.answer), "\n")
	}
	return strings.Split(strings.Trim(out, "\n"), "\n")
}

// Cursor implements Dialog. The panel has no text input.
func (d *SideQuestion) Cursor() *tea.Cursor {
	return nil
}

// Draw implements [Dialog].
func (d *SideQuestion) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := d.com.Styles
	width := max(0, min(sideQuestionMaxWidth, area.Dx()-t.Dialog.View.GetHorizontalBorderSize()))
	height := max(0, min(sideQuestionMaxHeight, area.Dy()-t.Dialog.View.GetVerticalBorderSize()))
	innerWidth := max(1, width-t.Dialog.View.GetHorizontalFrameSize())

	helpView := renderDialogHelp(t, &d.help, d, innerWidth)
	bodyHeight := max(1, height-t.Dialog.View.GetVerticalFrameSize()-lipgloss.Height(helpView)-2)

	lines := d.body(innerWidth)
	d.yOffset = min(d.yOffset, max(0, len(lines)-bodyHeight))
	visible := lines[d.yOffset:min(len(lines), d.yOffset+bodyHeight)]

	rc := NewRenderContext(t, width)
	rc.Title = "/btw"
	if d.done && d.model != "" {
		rc.TitleInfo = t.Resource.AdditionalText.Render(d.model)
	}
	rc.Help = helpView
	rc.AddPart(strings.Join(visible, "\n"))

	DrawCenter(scr, area, rc.Render())
	return nil
}

// ShortHelp implements [help.KeyMap].
func (d *SideQuestion) ShortHelp() []key.Binding {
	return []key.Binding{d.keyMap.Up, d.keyMap.Down, d.keyMap.Close}
}

// FullHelp implements [help.KeyMap].
func (d *SideQuestion) FullHelp() [][]key.Binding {
	return [][]key.Binding{d.ShortHelp()}
}
