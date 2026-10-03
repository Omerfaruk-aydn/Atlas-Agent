package dialog

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-style/v2"
	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	uv "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ultraviolet"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/help"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/key"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/common"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/styles"
)

// FileDiffID is the identifier for the sidebar file-diff dialog.
const FileDiffID = "file_diff"

// ActionReviewLine requests user feedback for an explicit current source line.
type ActionReviewLine struct {
	Path string
	Line int
}

// fileDiffMaxWidth/Height bound the dialog; it otherwise fills most of the
// screen since diffs need real space to be readable.
const (
	fileDiffMaxWidth  = 120
	fileDiffMaxHeight = 40
)

// FileDiff shows the cumulative diff (session start to latest version) for
// one file selected from the sidebar's "Modified Files" list.
type FileDiff struct {
	com                                  *common.Common
	path                                 string
	before, after                        string
	additions                            int
	deletions                            int
	yOffset                              int
	reviewLine, lineCount                int
	cache                                string
	cacheWidth, cacheHeight, cacheOffset int
	cacheStyles                          *styles.Styles

	keyMap struct {
		Up           key.Binding
		Down         key.Binding
		PageUp       key.Binding
		PageDown     key.Binding
		Close        key.Binding
		Review       key.Binding
		PreviousLine key.Binding
		NextLine     key.Binding
	}
	help help.Model
}

var _ Dialog = (*FileDiff)(nil)

// NewFileDiff creates a new FileDiff dialog. before/after are the file's
// content at the start and end of the session; additions/deletions are the
// precomputed line counts shown in the title.
func NewFileDiff(com *common.Common, path, before, after string, additions, deletions int) *FileDiff {
	d := &FileDiff{com: com, path: path, before: before, after: after, additions: additions, deletions: deletions}
	d.reviewLine, d.lineCount = 1, max(1, strings.Count(after, "\n")+1)

	h := help.New()
	h.Styles = com.Styles.DialogHelpStyles()
	d.help = h

	d.keyMap.Up = key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "scroll up"))
	d.keyMap.Down = key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "scroll down"))
	d.keyMap.PageUp = key.NewBinding(key.WithKeys("pgup", "b"), key.WithHelp("pgup", "page up"))
	d.keyMap.PageDown = key.NewBinding(key.WithKeys("pgdown", "f", " "), key.WithHelp("pgdn", "page down"))
	d.keyMap.Close = CloseKey
	d.keyMap.Review = key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "line feedback"))
	d.keyMap.PreviousLine = key.NewBinding(key.WithKeys("["), key.WithHelp("[", "previous source line"))
	d.keyMap.NextLine = key.NewBinding(key.WithKeys("]"), key.WithHelp("]", "next source line"))

	return d
}

// ID implements Dialog.
func (d *FileDiff) ID() string {
	return FileDiffID
}

// HandleMsg implements Dialog.
func (d *FileDiff) HandleMsg(msg tea.Msg) Action {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	switch {
	case key.Matches(keyMsg, d.keyMap.Review):
		return ActionReviewLine{Path: d.path, Line: d.reviewLine}
	case key.Matches(keyMsg, d.keyMap.PreviousLine):
		d.reviewLine = max(1, d.reviewLine-1)
	case key.Matches(keyMsg, d.keyMap.NextLine):
		d.reviewLine = min(d.lineCount, d.reviewLine+1)
	case key.Matches(keyMsg, d.keyMap.Close):
		return ActionClose{}
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

// Refresh replaces cached content after the session history changes.
func (d *FileDiff) Refresh(entry FileDiffEntry) {
	if entry.Path != d.path {
		return
	}
	if d.before == entry.Before && d.after == entry.After && d.additions == entry.Additions && d.deletions == entry.Deletions {
		return
	}
	d.cacheStyles = nil
	d.before, d.after, d.additions, d.deletions = entry.Before, entry.After, entry.Additions, entry.Deletions
	d.lineCount = max(1, strings.Count(d.after, "\n")+1)
	d.reviewLine = min(d.reviewLine, d.lineCount)
}

func (d *FileDiff) scroll(delta int) {
	d.yOffset = max(0, d.yOffset+delta)
}

// Cursor implements Dialog. The file diff view has no text input.
func (d *FileDiff) Cursor() *tea.Cursor {
	return nil
}

// Draw implements [Dialog].
func (d *FileDiff) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := d.com.Styles
	width := max(0, min(fileDiffMaxWidth, area.Dx()-t.Dialog.View.GetHorizontalBorderSize()))
	height := max(0, min(fileDiffMaxHeight, area.Dy()-t.Dialog.View.GetVerticalBorderSize()))
	innerWidth := width - t.Dialog.View.GetHorizontalFrameSize()

	helpView := renderDialogHelp(t, &d.help, d, innerWidth)
	bodyHeight := max(1, height-t.Dialog.View.GetVerticalFrameSize()-lipgloss.Height(helpView)-2)

	if d.cacheStyles != t || d.cacheWidth != innerWidth || d.cacheHeight != bodyHeight || d.cacheOffset != d.yOffset {
		d.cache = common.DiffFormatter(t).
			Unified().
			Before(filepath.Base(d.path), d.before).
			After(filepath.Base(d.path), d.after).
			Width(innerWidth).
			Height(bodyHeight).
			YOffset(d.yOffset).
			LineNumbers(true).
			String()
		d.cacheStyles, d.cacheWidth, d.cacheHeight, d.cacheOffset = t, innerWidth, bodyHeight, d.yOffset
	}
	body := d.cache

	rc := NewRenderContext(t, width)
	rc.Title = fmt.Sprintf("%s | source line %d", filepath.Base(d.path), d.reviewLine)
	rc.TitleInfo = t.Files.Additions.Render(fmt.Sprintf("+%d", d.additions)) +
		" " + t.Files.Deletions.Render(fmt.Sprintf("-%d", d.deletions))
	rc.Help = helpView
	rc.AddPart(body)

	view := rc.Render()
	DrawCenter(scr, area, view)
	return nil
}

// ShortHelp implements [help.KeyMap].
func (d *FileDiff) ShortHelp() []key.Binding {
	return []key.Binding{d.keyMap.Up, d.keyMap.Down, d.keyMap.PreviousLine, d.keyMap.NextLine, d.keyMap.Review, d.keyMap.Close}
}

// FullHelp implements [help.KeyMap].
func (d *FileDiff) FullHelp() [][]key.Binding {
	return [][]key.Binding{d.ShortHelp()}
}
